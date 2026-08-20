package service

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"
	connectip "github.com/metacubex/connect-ip-go"
	mhttp "github.com/metacubex/http"
	mquic "github.com/metacubex/quic-go"
	mhttp3 "github.com/metacubex/quic-go/http3"
	mtls "github.com/metacubex/tls"
	"github.com/yosida95/uritemplate/v3"
)

var masquePtr *MasqueService
var masqueStatusCache = newTimedCache()

const (
	masqueStatusCacheTTL     = 2 * time.Second
	masqueFlowTTL            = 5 * time.Minute
	masqueSessionIdleTimeout = 5 * time.Minute
	masqueSessionSweep       = 30 * time.Second
	masqueSessionQueue       = 256
)

type masqueFlowKey struct {
	Src     [4]byte
	Dst     [4]byte
	Proto   uint8
	SrcPort uint16
	DstPort uint16
}

type masqueFlowEntry struct {
	SessionID uint64
	LastSeen  time.Time
}

type masqueUserTraffic struct {
	Upload   atomic.Uint64
	Download atomic.Uint64
}

type masqueRateLimiter interface {
	WaitUpload(context.Context, string, int) error
	WaitDownload(context.Context, string, int) error
}

func currentMasqueRateLimiter() masqueRateLimiter {
	if corePtr == nil || !corePtr.IsRunning() {
		return nil
	}
	instance := corePtr.GetInstance()
	if instance == nil {
		return nil
	}
	return instance.RateLimitTracker()
}

func (r *masqueRuntime) waitUpload(ctx context.Context, user string, size int) error {
	if r == nil || r.rateLimiter == nil {
		return nil
	}
	limiter := r.rateLimiter()
	if limiter == nil {
		return nil
	}
	return limiter.WaitUpload(ctx, user, size)
}

func (r *masqueRuntime) waitDownload(ctx context.Context, user string, size int) error {
	if r == nil || r.rateLimiter == nil {
		return nil
	}
	limiter := r.rateLimiter()
	if limiter == nil {
		return nil
	}
	return limiter.WaitDownload(ctx, user, size)
}

type masqueRuntime struct {
	tag          string
	port         int
	host         string
	bindAddr     string
	certFile     string
	keyFile      string
	certSource   string
	templateStr  string
	proxy        *connectip.Proxy
	server       *mhttp3.Server
	template     *uritemplate.Template
	tun          *masqueTun
	packetConn   net.PacketConn
	certificate  *masqueCertificateReloader
	clientSubnet netip.Prefix
	clients      map[string]masqueClientIdentity
	usersByIP    map[netip.Addr]string
	traffic      map[string]*masqueUserTraffic
	rateLimiter  func() masqueRateLimiter
	ctx          context.Context
	cancel       context.CancelFunc
	tunWriteMu   sync.Mutex
	sessionMu    sync.Mutex
	sessions     map[uint64]*masqueSession
	userSessions map[string]map[uint64]*masqueSession
	flows        map[masqueFlowKey]masqueFlowEntry
	nextSession  uint64
	lastConnect  time.Time
	lastClose    time.Time
	lastError    string
	lastFlowGC   time.Time
	running      atomic.Bool
	rxBytes      atomic.Uint64
	txBytes      atomic.Uint64
	rxPackets    atomic.Uint64
	txPackets    atomic.Uint64
}

type masqueSession struct {
	id         uint64
	identity   masqueClientIdentity
	startedAt  time.Time
	remote     string
	ctx        context.Context
	cancel     context.CancelFunc
	conn       *connectip.Conn
	outgoing   chan []byte
	closeOnce  sync.Once
	rxBytes    atomic.Uint64
	txBytes    atomic.Uint64
	rxPackets  atomic.Uint64
	txPackets  atomic.Uint64
	lastActive atomic.Int64
	closing    atomic.Bool
}

type masqueTrafficSnapshot struct {
	Upload   uint64
	Download uint64
}

type MasqueService struct {
	SettingService

	mu              sync.Mutex
	runtimes        map[string]*masqueRuntime
	startErr        map[string]string
	trafficBaseline map[string]masqueTrafficSnapshot
}

func NewMasqueService() *MasqueService {
	return &MasqueService{
		runtimes:        map[string]*masqueRuntime{},
		startErr:        map[string]string{},
		trafficBaseline: map[string]masqueTrafficSnapshot{},
	}
}

func SetMasqueService(s *MasqueService) { masquePtr = s }
func GetMasqueService() *MasqueService  { return masquePtr }

func (s *MasqueService) GetSummary() map[string]int {
	result := map[string]int{"total": 0, "running": 0}
	if s == nil {
		return result
	}
	inbounds, err := s.loadMasqueInbounds()
	if err != nil {
		return result
	}
	result["total"] = len(inbounds)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, inbound := range inbounds {
		if runtime := s.runtimes[inbound.Tag]; runtime != nil && runtime.running.Load() {
			result["running"]++
		}
	}
	return result
}

func (s *MasqueService) SyncFromDB() error {
	if s == nil {
		return nil
	}
	if err := MigrateLegacyMasqueEndpoints(); err != nil {
		return fmt.Errorf("migrate legacy masque endpoints: %w", err)
	}
	inbounds, err := s.loadMasqueInbounds()
	if err != nil {
		return err
	}

	s.mu.Lock()
	oldRuntimes := s.runtimes
	s.runtimes = map[string]*masqueRuntime{}
	s.startErr = map[string]string{}
	s.mu.Unlock()
	for _, runtime := range oldRuntimes {
		runtime.stop()
	}
	masqueStatusCache.clear()
	if runtime.GOOS != "linux" {
		s.mu.Lock()
		for _, inbound := range inbounds {
			s.startErr[inbound.Tag] = "MASQUE 服务端仅支持 Linux TUN；配置和订阅仍可在当前平台管理"
		}
		s.mu.Unlock()
		return nil
	}

	var errs []error
	for _, inbound := range inbounds {
		runtime, err := s.startInbound(inbound)
		if err != nil {
			s.mu.Lock()
			s.startErr[inbound.Tag] = err.Error()
			s.mu.Unlock()
			errs = append(errs, fmt.Errorf("start masque inbound %s failed: %w", inbound.Tag, err))
			continue
		}
		s.mu.Lock()
		s.runtimes[runtime.tag] = runtime
		s.mu.Unlock()
	}
	return errors.Join(errs...)
}

func (s *MasqueService) Stop() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	runtimes := s.runtimes
	s.runtimes = map[string]*masqueRuntime{}
	s.startErr = map[string]string{}
	s.mu.Unlock()
	for _, runtime := range runtimes {
		runtime.stop()
	}
	masqueStatusCache.clear()
	return nil
}

func (s *MasqueService) GetStatus(tag string) (map[string]interface{}, error) {
	if s == nil {
		return nil, common.NewError("masque service not initialized")
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil, common.NewError("missing inbound tag")
	}
	version := CurrentDataVersion()
	if cached, ok := masqueStatusCache.get(tag, version); ok {
		if status, ok := cached.(map[string]interface{}); ok {
			return status, nil
		}
	}
	inbound := &model.Inbound{}
	if err := database.GetDB().Model(model.Inbound{}).Where("tag = ? AND type = ?", tag, "masque").First(inbound).Error; err != nil {
		return nil, err
	}
	config, err := parseMasqueInbound(inbound)
	if err != nil {
		return nil, err
	}
	identities, identityErr := loadMasqueClientIdentities(database.GetDB(), inbound)
	s.mu.Lock()
	runtime := s.runtimes[tag]
	startError := s.startErr[tag]
	s.mu.Unlock()
	status := map[string]interface{}{
		"tag": tag, "host": config.Host, "port": config.Port, "network": config.Network,
		"client_subnet": config.ClientSubnet, "configured_users": len(identities),
		"running":   runtime != nil && runtime.running.Load(),
		"bind_addr": net.JoinHostPort("0.0.0.0", strconv.Itoa(config.Port)),
		"template":  masqueTemplateDescription(config),
	}
	if startError != "" {
		status["start_error"] = startError
	}
	if identityErr != nil {
		status["client_error"] = identityErr.Error()
	}
	var certSnapshot masqueCertificateSnapshot
	var certErr error
	if runtime != nil {
		status["bind_addr"] = runtime.bindAddr
		status["template"] = runtime.templateStr
		status["cert_file"] = runtime.certFile
		status["key_file"] = runtime.keyFile
		status["cert_source"] = runtime.certSource
		for key, value := range runtime.statusSnapshot() {
			status[key] = value
		}
		if runtime.certificate != nil {
			certSnapshot = runtime.certificate.snapshot()
		}
	} else {
		cert, certFile, keyFile, certSource, err := s.loadMasqueTLSCertificate(config)
		if err != nil {
			certErr = err
		} else {
			certSnapshot = certificateSnapshot(cert, certFile, keyFile, certSource)
		}
	}
	if certErr != nil {
		status["cert_error"] = certErr.Error()
	}
	for key, value := range certSnapshot.statusFields() {
		status[key] = value
	}
	status["diagnostics"] = buildMasqueDiagnostics(config, runtime, certSnapshot, certErr, startError)
	masqueStatusCache.set(tag, version, masqueStatusCacheTTL, status)
	return status, nil
}

func (s *MasqueService) loadMasqueInbounds() ([]*model.Inbound, error) {
	inbounds := []*model.Inbound{}
	if err := database.GetDB().Model(model.Inbound{}).Where("type = ?", "masque").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	return inbounds, nil
}

func (s *MasqueService) startInbound(inbound *model.Inbound) (*masqueRuntime, error) {
	config, err := parseMasqueInbound(inbound)
	if err != nil {
		return nil, err
	}
	clientSubnet, err := parseMasqueClientSubnet(config.ClientSubnet)
	if err != nil {
		return nil, err
	}
	identities, err := loadMasqueClientIdentities(database.GetDB(), inbound)
	if err != nil {
		return nil, err
	}
	cert, certFile, keyFile, certSource, err := s.loadMasqueTLSCertificate(config)
	if err != nil {
		return nil, fmt.Errorf("load masque certificate failed: %w", err)
	}
	keepAlive := config.KeepAlive
	idleTimeout := time.Duration(keepAlive*4) * time.Second
	if idleTimeout < 2*time.Minute {
		idleTimeout = 2 * time.Minute
	}
	bindAddr := net.JoinHostPort("0.0.0.0", strconv.Itoa(config.Port))
	templateStr := masqueTemplateDescription(config)
	template, err := uritemplate.New(templateStr)
	if err != nil {
		return nil, fmt.Errorf("build masque uri template failed: %w", err)
	}
	parsedURL, err := url.Parse(templateStr)
	if err != nil {
		return nil, fmt.Errorf("parse masque uri template failed: %w", err)
	}
	handlePath := parsedURL.Path
	if handlePath == "" {
		handlePath = "/"
	}
	tun, err := newMasqueTun(inbound.Tag, clientSubnet, config.MTU)
	if err != nil {
		return nil, fmt.Errorf("setup masque tun failed: %w", err)
	}
	packetConn, err := net.ListenPacket("udp", bindAddr)
	if err != nil {
		_ = tun.Close()
		return nil, fmt.Errorf("listen masque udp %s failed: %w", bindAddr, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	handle := &masqueRuntime{
		tag: inbound.Tag, port: config.Port, host: config.Host, bindAddr: bindAddr,
		certFile: certFile, keyFile: keyFile, certSource: certSource, templateStr: templateStr,
		proxy: &connectip.Proxy{}, template: template, tun: tun, packetConn: packetConn,
		certificate:  newMasqueCertificateReloader(cert, certFile, keyFile, certSource),
		clientSubnet: clientSubnet, clients: identities, usersByIP: map[netip.Addr]string{},
		traffic: map[string]*masqueUserTraffic{}, ctx: ctx, cancel: cancel,
		rateLimiter: currentMasqueRateLimiter,
		sessions:    map[uint64]*masqueSession{}, userSessions: map[string]map[uint64]*masqueSession{},
		flows: map[masqueFlowKey]masqueFlowEntry{},
	}
	for _, identity := range identities {
		handle.usersByIP[identity.Prefix.Addr()] = identity.Name
		handle.traffic[identity.Name] = &masqueUserTraffic{}
	}

	mux := mhttp.NewServeMux()
	mux.HandleFunc(handlePath, func(w mhttp.ResponseWriter, r *mhttp.Request) {
		identity, err := handle.authenticateRequest(r)
		if err != nil {
			logger.Warning("masque client authentication failed: ", inbound.Tag, " err=", err)
			w.WriteHeader(mhttp.StatusUnauthorized)
			return
		}
		req, err := parseMasqueConnectIPRequest(r, template)
		if err != nil {
			var perr *connectip.RequestParseError
			if errors.As(err, &perr) {
				w.WriteHeader(perr.HTTPStatus)
			} else {
				w.WriteHeader(mhttp.StatusBadRequest)
			}
			logger.Warning("masque request parse failed: ", inbound.Tag, " err=", err)
			return
		}
		if err := handle.serveConnectIP(r.Context(), w, req, identity); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, context.Canceled) {
			logger.Warning("masque connect-ip bridge failed: ", inbound.Tag, " user=", identity.Name, " err=", err)
		}
	})

	srv := &mhttp3.Server{
		Addr: bindAddr,
		QUICConfig: &mquic.Config{
			EnableDatagrams: true, KeepAlivePeriod: time.Duration(keepAlive) * time.Second, MaxIdleTimeout: idleTimeout,
		},
		TLSConfig: mhttp3.ConfigureTLSConfig(&mtls.Config{
			GetCertificate: handle.certificate.getCertificate,
			ClientAuth:     mtls.RequireAnyClientCert,
		}),
		Handler: mux, EnableDatagrams: true,
	}
	handle.server = srv
	handle.running.Store(true)
	go handle.dispatchTunPackets()
	go handle.reapSessions()
	go func() {
		err := srv.Serve(packetConn)
		handle.running.Store(false)
		if err != nil && !errors.Is(err, mhttp.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			handle.setLastError(err)
			logger.Warning("masque server stopped unexpectedly: ", inbound.Tag, " err=", err)
		}
	}()
	logger.Info("masque server started: ", inbound.Tag, " addr=", bindAddr, " users=", len(identities))
	return handle, nil
}

func (r *masqueRuntime) authenticateRequest(req *mhttp.Request) (masqueClientIdentity, error) {
	if req == nil || req.TLS == nil || len(req.TLS.PeerCertificates) == 0 {
		return masqueClientIdentity{}, common.NewError("client certificate is required")
	}
	publicDER, err := x509.MarshalPKIXPublicKey(req.TLS.PeerCertificates[0].PublicKey)
	if err != nil {
		return masqueClientIdentity{}, fmt.Errorf("marshal client public key: %w", err)
	}
	publicKey := base64.StdEncoding.EncodeToString(publicDER)
	identity, ok := r.clients[publicKey]
	if !ok {
		return masqueClientIdentity{}, common.NewError("unknown or disabled masque client")
	}
	return identity, nil
}

func (r *masqueRuntime) stop() {
	if r == nil {
		return
	}
	r.cancel()
	r.sessionMu.Lock()
	sessions := make([]*masqueSession, 0, len(r.sessions))
	for _, session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.sessions = map[uint64]*masqueSession{}
	r.userSessions = map[string]map[uint64]*masqueSession{}
	r.flows = map[masqueFlowKey]masqueFlowEntry{}
	r.sessionMu.Unlock()
	for _, session := range sessions {
		session.close()
	}
	if r.server != nil {
		if err := r.server.Close(); err != nil && !errors.Is(err, mhttp.ErrServerClosed) {
			logger.Warning("masque server close failed: ", r.tag, " err=", err)
		}
	}
	if r.packetConn != nil {
		_ = r.packetConn.Close()
	}
	r.running.Store(false)
	if r.tun != nil {
		if err := r.tun.Close(); err != nil {
			logger.Warning("masque tun close failed: ", r.tag, " err=", err)
		}
	}
}

func (s *masqueSession) close() {
	if s == nil {
		return
	}
	s.closing.Store(true)
	s.closeOnce.Do(func() {
		s.cancel()
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
}

func (s *masqueSession) touch(now time.Time) {
	if s != nil {
		s.lastActive.Store(now.UnixNano())
	}
}

func (s *masqueSession) lastActivity() time.Time {
	if s == nil {
		return time.Time{}
	}
	value := s.lastActive.Load()
	if value == 0 {
		return s.startedAt
	}
	return time.Unix(0, value)
}

func (r *masqueRuntime) addSession(session *masqueSession) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	session.touch(session.startedAt)
	r.nextSession++
	session.id = r.nextSession
	r.sessions[session.id] = session
	if r.userSessions[session.identity.Name] == nil {
		r.userSessions[session.identity.Name] = map[uint64]*masqueSession{}
	}
	r.userSessions[session.identity.Name][session.id] = session
	r.lastConnect = session.startedAt
}

func (r *masqueRuntime) reapSessions() {
	ticker := time.NewTicker(masqueSessionSweep)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case now := <-ticker.C:
			if count := r.closeExpiredSessions(now); count > 0 {
				logger.Info("masque sessions recycled: ", r.tag, " count=", count)
			}
		}
	}
}

func (r *masqueRuntime) closeExpiredSessions(now time.Time) int {
	if r == nil {
		return 0
	}
	r.sessionMu.Lock()
	expired := make([]*masqueSession, 0)
	for _, session := range r.sessions {
		quietFor := now.Sub(session.lastActivity())
		if quietFor >= masqueSessionIdleTimeout && session.closing.CompareAndSwap(false, true) {
			expired = append(expired, session)
		}
	}
	r.sessionMu.Unlock()
	for _, session := range expired {
		session.close()
	}
	return len(expired)
}

func (r *masqueRuntime) removeSession(session *masqueSession, err error) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()
	r.lastClose = time.Now()
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
		r.lastError = err.Error()
	}
	delete(r.sessions, session.id)
	if sessions := r.userSessions[session.identity.Name]; sessions != nil {
		delete(sessions, session.id)
		if len(sessions) == 0 {
			delete(r.userSessions, session.identity.Name)
		}
	}
	for key, entry := range r.flows {
		if entry.SessionID == session.id {
			delete(r.flows, key)
		}
	}
}

func (r *masqueRuntime) setLastError(err error) {
	if r == nil || err == nil {
		return
	}
	r.sessionMu.Lock()
	r.lastError = err.Error()
	r.sessionMu.Unlock()
}

func (r *masqueRuntime) serveConnectIP(ctx context.Context, w mhttp.ResponseWriter, req *connectip.Request, identity masqueClientIdentity) error {
	conn, err := r.proxy.Proxy(w, req)
	if err != nil {
		return err
	}

	sessionCtx, sessionCancel := context.WithCancel(ctx)
	session := &masqueSession{
		identity:  identity,
		startedAt: time.Now(),
		ctx:       sessionCtx,
		cancel:    sessionCancel,
		conn:      conn,
		outgoing:  make(chan []byte, masqueSessionQueue),
	}
	if remote, ok := ctx.Value(mhttp3.RemoteAddrContextKey).(net.Addr); ok && remote != nil {
		session.remote = remote.String()
	}
	r.addSession(session)
	var sessionErr error
	defer func() {
		session.close()
		r.removeSession(session, sessionErr)
	}()

	setupCtx, cancel := context.WithTimeout(sessionCtx, 5*time.Second)
	if err := conn.AssignAddresses(setupCtx, []netip.Prefix{identity.Prefix}); err != nil {
		cancel()
		return err
	}
	if err := conn.AdvertiseRoute(setupCtx, []connectip.IPRoute{{
		IPProtocol: 0,
		StartIP:    netip.AddrFrom4([4]byte{}),
		EndIP:      netip.AddrFrom4([4]byte{255, 255, 255, 255}),
	}}); err != nil {
		cancel()
		return err
	}
	cancel()

	logger.Info("masque connect-ip session started: ", r.tag, " session=", session.id, " user=", identity.Name, " peer=", identity.Prefix)
	errCh := make(chan error, 2)
	go func() {
		for sessionCtx.Err() == nil {
			packet, err := conn.ReadPacket()
			if err != nil {
				errCh <- err
				return
			}
			if len(packet) == 0 {
				continue
			}
			if err := r.waitUpload(sessionCtx, identity.Name, len(packet)); err != nil {
				errCh <- err
				return
			}
			session.touch(time.Now())
			if !masquePacketSourceMatches(packet, identity.Prefix.Addr()) {
				errCh <- common.NewError("masque client packet source does not match assigned address")
				return
			}
			r.recordClientFlow(session, packet)
			size := uint64(len(packet))
			session.rxBytes.Add(size)
			session.rxPackets.Add(1)
			r.rxBytes.Add(size)
			r.rxPackets.Add(1)
			if traffic := r.traffic[identity.Name]; traffic != nil {
				traffic.Upload.Add(size)
			}
			r.tunWriteMu.Lock()
			err = r.tun.WritePacket(packet)
			r.tunWriteMu.Unlock()
			if err != nil {
				errCh <- err
				return
			}
		}
		errCh <- sessionCtx.Err()
	}()
	go func() {
		for sessionCtx.Err() == nil {
			select {
			case <-sessionCtx.Done():
				errCh <- sessionCtx.Err()
				return
			case packet := <-session.outgoing:
				if err := r.waitDownload(sessionCtx, identity.Name, len(packet)); err != nil {
					errCh <- err
					return
				}
				if _, err := conn.WritePacket(packet); err != nil {
					errCh <- err
					return
				}
				session.touch(time.Now())
			}
		}
		errCh <- sessionCtx.Err()
	}()

	sessionErr = <-errCh
	logger.Info("masque connect-ip session stopped: ", r.tag, " session=", session.id, " user=", identity.Name, " err=", sessionErr)
	return sessionErr
}

func (r *masqueRuntime) dispatchTunPackets() {
	buf := make([]byte, 65535)
	for r.ctx.Err() == nil {
		n, err := r.tun.ReadPacket(r.ctx, buf)
		if err != nil {
			if !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
				r.setLastError(err)
				logger.Warning("masque tun reader stopped: ", r.tag, " err=", err)
			}
			return
		}
		if n <= 0 {
			continue
		}
		packet := append([]byte(nil), buf[:n]...)
		if !r.routeTunPacket(packet) {
			continue
		}
	}
}

func (r *masqueRuntime) recordClientFlow(session *masqueSession, packet []byte) {
	key, ok := parseMasqueFlowKey(packet)
	if !ok {
		return
	}
	now := time.Now()
	r.sessionMu.Lock()
	r.flows[key.reverse()] = masqueFlowEntry{SessionID: session.id, LastSeen: now}
	if r.lastFlowGC.IsZero() || now.Sub(r.lastFlowGC) >= time.Minute {
		for flowKey, entry := range r.flows {
			if now.Sub(entry.LastSeen) > masqueFlowTTL {
				delete(r.flows, flowKey)
			}
		}
		r.lastFlowGC = now
	}
	r.sessionMu.Unlock()
}

func (r *masqueRuntime) routeTunPacket(packet []byte) bool {
	key, ok := parseMasqueFlowKey(packet)
	if !ok {
		return false
	}
	now := time.Now()
	r.sessionMu.Lock()
	var session *masqueSession
	if entry, exists := r.flows[key]; exists {
		if now.Sub(entry.LastSeen) <= masqueFlowTTL {
			session = r.sessions[entry.SessionID]
			r.flows[key] = masqueFlowEntry{SessionID: entry.SessionID, LastSeen: now}
		} else {
			delete(r.flows, key)
		}
	}
	if session == nil {
		destination := netip.AddrFrom4(key.Dst)
		if user := r.usersByIP[destination]; user != "" {
			// An unsolicited packet can only be routed safely when this user has
			// one active session. Existing flows remain pinned to their origin.
			if sessions := r.userSessions[user]; len(sessions) == 1 {
				for _, candidate := range sessions {
					session = candidate
				}
			}
		}
	}
	r.sessionMu.Unlock()
	if session == nil {
		return false
	}

	select {
	case <-session.ctx.Done():
		return false
	case session.outgoing <- packet:
		size := uint64(len(packet))
		session.txBytes.Add(size)
		session.txPackets.Add(1)
		r.txBytes.Add(size)
		r.txPackets.Add(1)
		if traffic := r.traffic[session.identity.Name]; traffic != nil {
			traffic.Download.Add(size)
		}
		return true
	default:
		r.setLastError(common.NewError("masque session output queue is full"))
		return false
	}
}

func parseMasqueFlowKey(packet []byte) (masqueFlowKey, bool) {
	if len(packet) < 20 || packet[0]>>4 != 4 {
		return masqueFlowKey{}, false
	}
	headerLen := int(packet[0]&0x0f) * 4
	if headerLen < 20 || len(packet) < headerLen {
		return masqueFlowKey{}, false
	}
	key := masqueFlowKey{Proto: packet[9]}
	copy(key.Src[:], packet[12:16])
	copy(key.Dst[:], packet[16:20])
	if (key.Proto == 6 || key.Proto == 17) && len(packet) >= headerLen+4 {
		key.SrcPort = uint16(packet[headerLen])<<8 | uint16(packet[headerLen+1])
		key.DstPort = uint16(packet[headerLen+2])<<8 | uint16(packet[headerLen+3])
	}
	return key, true
}

func (key masqueFlowKey) reverse() masqueFlowKey {
	return masqueFlowKey{
		Src: key.Dst, Dst: key.Src, Proto: key.Proto,
		SrcPort: key.DstPort, DstPort: key.SrcPort,
	}
}

func masquePacketSourceMatches(packet []byte, expected netip.Addr) bool {
	key, ok := parseMasqueFlowKey(packet)
	return ok && expected.Is4() && netip.AddrFrom4(key.Src) == expected
}

func parseMasqueConnectIPRequest(r *mhttp.Request, template *uritemplate.Template) (*connectip.Request, error) {
	req, err := connectip.ParseRequest(r, template)
	if err == nil {
		return req, nil
	}
	if r == nil || r.Method != mhttp.MethodConnect || r.Proto != "cf-connect-ip" {
		return nil, err
	}
	u, parseErr := url.Parse(template.Raw())
	if parseErr != nil {
		return nil, &connectip.RequestParseError{HTTPStatus: mhttp.StatusInternalServerError, Err: parseErr}
	}
	if r.Host != u.Host {
		return nil, &connectip.RequestParseError{
			HTTPStatus: mhttp.StatusBadRequest,
			Err:        fmt.Errorf("host in :authority (%s) does not match template host (%s)", r.Host, u.Host),
		}
	}
	if _, ok := r.Header[mhttp3.CapsuleProtocolHeader]; !ok {
		return nil, &connectip.RequestParseError{
			HTTPStatus: mhttp.StatusBadRequest,
			Err:        common.NewError("missing Capsule-Protocol header"),
		}
	}
	return &connectip.Request{}, nil
}
