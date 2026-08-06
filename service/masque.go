package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
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

const masqueStatusCacheTTL = 2 * time.Second

type masqueRuntime struct {
	tag         string
	port        int
	host        string
	bindAddr    string
	certFile    string
	keyFile     string
	certSource  string
	templateStr string
	proxy       *connectip.Proxy
	server      *mhttp3.Server
	template    *uritemplate.Template
	tun         *masqueTun
	packetConn  net.PacketConn
	certificate *masqueCertificateReloader
	peerPrefix  netip.Prefix
	sessionMu   sync.Mutex
	active      *masqueSession
	nextSession uint64
	takeovers   uint64
	lastConnect time.Time
	lastClose   time.Time
	lastError   string
	running     atomic.Bool
	rxBytes     atomic.Uint64
	txBytes     atomic.Uint64
	rxPackets   atomic.Uint64
	txPackets   atomic.Uint64
}

type masqueSession struct {
	id        uint64
	startedAt time.Time
	remote    string
	ctx       context.Context
	cancel    context.CancelFunc
	conn      *connectip.Conn
	closeOnce sync.Once
	rxBytes   atomic.Uint64
	txBytes   atomic.Uint64
	rxPackets atomic.Uint64
	txPackets atomic.Uint64
}

type MasqueService struct {
	SettingService

	mu       sync.Mutex
	runtimes map[string]*masqueRuntime
	startErr map[string]string
}

func NewMasqueService() *MasqueService {
	return &MasqueService{
		runtimes: map[string]*masqueRuntime{},
		startErr: map[string]string{},
	}
}

func SetMasqueService(s *MasqueService) {
	masquePtr = s
}

func GetMasqueService() *MasqueService {
	return masquePtr
}

func (s *MasqueService) GetSummary() map[string]int {
	result := map[string]int{"total": 0, "running": 0}
	if s == nil {
		return result
	}
	endpoints, err := s.loadMasqueEndpoints()
	if err != nil {
		return result
	}
	result["total"] = len(endpoints)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, endpoint := range endpoints {
		if endpoint != nil {
			runtime := s.runtimes[endpoint.Tag]
			if runtime != nil && runtime.running.Load() {
				result["running"]++
			}
		}
	}
	return result
}

func (s *MasqueService) SyncFromDB() error {
	if s == nil {
		return nil
	}

	endpoints, err := s.loadMasqueEndpoints()
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

	var errs []error
	for _, endpoint := range endpoints {
		runtime, err := s.startEndpoint(endpoint)
		if err != nil {
			s.mu.Lock()
			s.startErr[endpoint.Tag] = err.Error()
			s.mu.Unlock()
			errs = append(errs, fmt.Errorf("start masque endpoint %s failed: %w", endpoint.Tag, err))
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
		return nil, common.NewError("missing endpoint tag")
	}
	version := CurrentDataVersion()
	if cached, ok := masqueStatusCache.get(tag, version); ok {
		if status, ok := cached.(map[string]interface{}); ok {
			return status, nil
		}
	}

	db := database.GetDB()
	endpoint := &model.Endpoint{}
	if err := db.Model(model.Endpoint{}).Where("tag = ? AND type = ?", tag, "masque").First(endpoint).Error; err != nil {
		return nil, err
	}

	config, err := parseMasqueEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	runtime := s.runtimes[tag]
	startError := s.startErr[tag]
	s.mu.Unlock()

	status := map[string]interface{}{
		"tag":       endpoint.Tag,
		"host":      config.Host,
		"port":      config.Port,
		"network":   config.Network,
		"running":   runtime != nil && runtime.running.Load(),
		"bind_addr": net.JoinHostPort("0.0.0.0", strconv.Itoa(config.Port)),
		"template":  masqueTemplateDescription(config),
	}
	if startError != "" {
		status["start_error"] = startError
	}

	var certSnapshot masqueCertificateSnapshot
	var certErr error
	if runtime != nil {
		if runtime.bindAddr != "" {
			status["bind_addr"] = runtime.bindAddr
		}
		if runtime.templateStr != "" {
			status["template"] = runtime.templateStr
		}
		if runtime.certFile != "" {
			status["cert_file"] = runtime.certFile
		}
		if runtime.keyFile != "" {
			status["key_file"] = runtime.keyFile
		}
		if runtime.certSource != "" {
			status["cert_source"] = runtime.certSource
		}
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

func (s *MasqueService) loadMasqueEndpoints() ([]*model.Endpoint, error) {
	db := database.GetDB()
	endpoints := []*model.Endpoint{}
	if err := db.Model(model.Endpoint{}).Scan(&endpoints).Error; err != nil {
		return nil, err
	}

	filtered := make([]*model.Endpoint, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint != nil && endpoint.Type == "masque" {
			filtered = append(filtered, endpoint)
		}
	}
	return filtered, nil
}

func (s *MasqueService) startEndpoint(endpoint *model.Endpoint) (*masqueRuntime, error) {
	if endpoint == nil {
		return nil, common.NewError("missing endpoint")
	}

	config, err := parseMasqueEndpoint(endpoint)
	if err != nil {
		return nil, err
	}
	if config.Host == "" {
		return nil, common.NewError("masque server host is required")
	}
	if config.Port < 1 || config.Port > 65535 {
		return nil, fmt.Errorf("invalid masque port: %d", config.Port)
	}
	if err := validateMasqueNetwork(config.Network); err != nil {
		return nil, err
	}
	peerPrefix, err := parseMasquePeerPrefix(config.IP)
	if err != nil {
		return nil, err
	}

	cert, certFile, keyFile, certSource, err := s.loadMasqueTLSCertificate(config)
	if err != nil {
		return nil, fmt.Errorf("load masque certificate failed: %w", err)
	}

	keepAlive := config.KeepAlive
	if keepAlive <= 0 {
		keepAlive = 25
	}
	idleTimeout := time.Duration(keepAlive*4) * time.Second
	if minIdleTimeout := 2 * time.Minute; idleTimeout < minIdleTimeout {
		idleTimeout = minIdleTimeout
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

	tun, err := newMasqueTun(endpoint.Tag, peerPrefix, config.MTU)
	if err != nil {
		return nil, fmt.Errorf("setup masque tun failed: %w", err)
	}
	packetConn, err := net.ListenPacket("udp", bindAddr)
	if err != nil {
		_ = tun.Close()
		return nil, fmt.Errorf("listen masque udp %s failed: %w", bindAddr, err)
	}
	certificate := newMasqueCertificateReloader(cert, certFile, keyFile, certSource)

	proxy := &connectip.Proxy{}
	handle := &masqueRuntime{
		tag:         endpoint.Tag,
		port:        config.Port,
		host:        config.Host,
		bindAddr:    bindAddr,
		certFile:    certFile,
		keyFile:     keyFile,
		certSource:  certSource,
		templateStr: templateStr,
		proxy:       proxy,
		template:    template,
		tun:         tun,
		packetConn:  packetConn,
		certificate: certificate,
		peerPrefix:  peerPrefix,
	}

	mux := mhttp.NewServeMux()
	mux.HandleFunc(handlePath, func(w mhttp.ResponseWriter, r *mhttp.Request) {
		req, err := parseMasqueConnectIPRequest(r, template)
		if err != nil {
			var perr *connectip.RequestParseError
			if errors.As(err, &perr) {
				logger.Warning("masque request parse failed: ", endpoint.Tag, " status=", perr.HTTPStatus, " err=", perr.Err)
				w.WriteHeader(perr.HTTPStatus)
				return
			}
			logger.Warning("masque request parse failed: ", endpoint.Tag, " err=", err)
			w.WriteHeader(mhttp.StatusBadRequest)
			return
		}
		if err := handle.serveConnectIP(r.Context(), w, req); err != nil && !errors.Is(err, net.ErrClosed) && !errors.Is(err, context.Canceled) {
			logger.Warning("masque connect-ip bridge failed: ", endpoint.Tag, " err=", err)
			return
		}
	})

	srv := &mhttp3.Server{
		Addr: bindAddr,
		QUICConfig: &mquic.Config{
			EnableDatagrams: true,
			KeepAlivePeriod: time.Duration(keepAlive) * time.Second,
			MaxIdleTimeout:  idleTimeout,
		},
		TLSConfig: mhttp3.ConfigureTLSConfig(&mtls.Config{
			GetCertificate: certificate.getCertificate,
		}),
		Handler:         mux,
		EnableDatagrams: true,
	}

	handle.server = srv
	handle.running.Store(true)

	go func() {
		err := srv.Serve(packetConn)
		handle.running.Store(false)
		if err != nil && !errors.Is(err, mhttp.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			handle.setLastError(err)
			logger.Warning("masque server stopped unexpectedly: ", endpoint.Tag, " err=", err)
		}
	}()

	logger.Info("masque server started: ", endpoint.Tag, " addr=", bindAddr, " host=", config.Host)
	return handle, nil
}

func (r *masqueRuntime) stop() {
	if r == nil {
		return
	}
	r.sessionMu.Lock()
	old := r.active
	r.active = nil
	r.sessionMu.Unlock()
	if old != nil {
		old.close()
	}
	if r.server != nil {
		if err := r.server.Close(); err != nil && !errors.Is(err, mhttp.ErrServerClosed) {
			logger.Warning("masque server close failed: ", r.tag, " err=", err)
		}
	}
	if r.packetConn != nil {
		if err := r.packetConn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Warning("masque udp socket close failed: ", r.tag, " err=", err)
		}
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
	s.closeOnce.Do(func() {
		s.cancel()
		if s.conn != nil {
			_ = s.conn.Close()
		}
	})
}

func (r *masqueRuntime) activateSession(next *masqueSession) *masqueSession {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()

	r.nextSession++
	next.id = r.nextSession
	prev := r.active
	if prev != nil {
		r.takeovers++
	}
	r.active = next
	r.lastConnect = next.startedAt
	return prev
}

func (r *masqueRuntime) clearActiveSession(sessionID uint64, err error) {
	r.sessionMu.Lock()
	defer r.sessionMu.Unlock()

	r.lastClose = time.Now()
	if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, net.ErrClosed) {
		r.lastError = err.Error()
	}
	if r.active != nil && r.active.id == sessionID {
		r.active = nil
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

func (r *masqueRuntime) serveConnectIP(ctx context.Context, w mhttp.ResponseWriter, req *connectip.Request) error {
	conn, err := r.proxy.Proxy(w, req)
	if err != nil {
		return err
	}

	sessionCtx, sessionCancel := context.WithCancel(ctx)
	session := &masqueSession{
		startedAt: time.Now(),
		ctx:       sessionCtx,
		cancel:    sessionCancel,
		conn:      conn,
	}
	if remote, ok := ctx.Value(mhttp3.RemoteAddrContextKey).(net.Addr); ok && remote != nil {
		session.remote = remote.String()
	}
	var sessionErr error
	defer func() {
		session.close()
		r.clearActiveSession(session.id, sessionErr)
	}()

	if prev := r.activateSession(session); prev != nil {
		logger.Info("masque connect-ip session takeover: ", r.tag, " old=", prev.id, " new=", session.id)
		prev.close()
	}

	setupCtx, cancel := context.WithTimeout(sessionCtx, 5*time.Second)
	if err := conn.AssignAddresses(setupCtx, []netip.Prefix{r.peerPrefix}); err != nil {
		cancel()
		return err
	}
	if err := conn.AdvertiseRoute(setupCtx, []connectip.IPRoute{
		{
			IPProtocol: 0,
			StartIP:    netip.AddrFrom4([4]byte{}),
			EndIP:      netip.AddrFrom4([4]byte{255, 255, 255, 255}),
		},
	}); err != nil {
		cancel()
		return err
	}
	cancel()

	if r.tun != nil {
		if err := r.tun.configureKernelForwarding(); err != nil {
			return err
		}
	}

	logger.Info("masque connect-ip session started: ", r.tag, " session=", session.id, " peer=", r.peerPrefix)
	errc := make(chan error, 2)
	go func() {
		for sessionCtx.Err() == nil {
			packet, err := conn.ReadPacket()
			if err != nil {
				errc <- err
				return
			}
			if len(packet) == 0 {
				continue
			}
			size := uint64(len(packet))
			session.rxBytes.Add(size)
			session.rxPackets.Add(1)
			r.rxBytes.Add(size)
			r.rxPackets.Add(1)
			if err := r.tun.WritePacket(packet); err != nil {
				errc <- err
				return
			}
		}
		errc <- sessionCtx.Err()
	}()
	go func() {
		buf := make([]byte, 65535)
		for sessionCtx.Err() == nil {
			n, err := r.tun.ReadPacket(sessionCtx, buf)
			if err != nil {
				errc <- err
				return
			}
			if n <= 0 {
				continue
			}
			size := uint64(n)
			session.txBytes.Add(size)
			session.txPackets.Add(1)
			r.txBytes.Add(size)
			r.txPackets.Add(1)
			if _, err := conn.WritePacket(append([]byte(nil), buf[:n]...)); err != nil {
				errc <- err
				return
			}
		}
		errc <- sessionCtx.Err()
	}()

	sessionErr = <-errc
	logger.Info("masque connect-ip session stopped: ", r.tag, " session=", session.id, " err=", sessionErr)
	return sessionErr
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
		return nil, &connectip.RequestParseError{
			HTTPStatus: mhttp.StatusInternalServerError,
			Err:        parseErr,
		}
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
			Err:        fmt.Errorf("missing Capsule-Protocol header"),
		}
	}
	return &connectip.Request{}, nil
}
