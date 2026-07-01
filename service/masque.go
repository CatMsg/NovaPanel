package service

import (
	"context"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"sync"
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
	peerPrefix  netip.Prefix
	connMu      sync.Mutex
}

type MasqueService struct {
	SettingService

	mu       sync.Mutex
	runtimes map[string]*masqueRuntime
}

func NewMasqueService() *MasqueService {
	return &MasqueService{
		runtimes: map[string]*masqueRuntime{},
	}
}

func SetMasqueService(s *MasqueService) {
	masquePtr = s
}

func GetMasqueService() *MasqueService {
	return masquePtr
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
	s.mu.Unlock()

	for _, runtime := range oldRuntimes {
		runtime.stop()
	}

	var errs []error
	for _, endpoint := range endpoints {
		runtime, err := s.startEndpoint(endpoint)
		if err != nil {
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
	s.mu.Unlock()

	for _, runtime := range runtimes {
		runtime.stop()
	}
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

	db := database.GetDB()
	endpoint := &model.Endpoint{}
	if err := db.Model(model.Endpoint{}).Where("tag = ? AND type = ?", tag, "masque").First(endpoint).Error; err != nil {
		return nil, err
	}

	config, err := parseMasqueEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	var certFile, keyFile string
	var certErr error
	if config.PrivateKey == "" {
		certFile, keyFile, certErr = s.resolveMasqueCertFiles(config.Host)
	}

	s.mu.Lock()
	runtime := s.runtimes[tag]
	s.mu.Unlock()

	status := map[string]interface{}{
		"tag":       endpoint.Tag,
		"host":      config.Host,
		"port":      config.Port,
		"network":   config.Network,
		"running":   runtime != nil,
		"bind_addr": net.JoinHostPort("0.0.0.0", strconv.Itoa(config.Port)),
		"template":  masqueTemplateDescription(config),
	}

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
	}

	if config.PrivateKey != "" {
		status["cert_source"] = "endpoint-key"
	} else if certErr != nil {
		status["cert_error"] = certErr.Error()
	} else {
		status["cert_file"] = certFile
		status["key_file"] = keyFile
	}

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
	if config.Network != "" && config.Network != "quic" && config.Network != "h2" {
		logger.Warning("masque service currently supports quic/h2 config preview; network=", config.Network)
	}
	peerPrefix, err := parseMasquePeerPrefix(config.IP)
	if err != nil {
		return nil, err
	}

	cert, certFile, keyFile, certSource, err := s.loadMasqueTLSCertificate(config)
	if err != nil {
		return nil, fmt.Errorf("load masque certificate failed: %w", err)
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
		Addr:            bindAddr,
		QUICConfig:      &mquic.Config{EnableDatagrams: true},
		TLSConfig:       mhttp3.ConfigureTLSConfig(&mtls.Config{Certificates: []mtls.Certificate{cert}}),
		Handler:         mux,
		EnableDatagrams: true,
	}

	handle.server = srv

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, mhttp.ErrServerClosed) {
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
	if r.server != nil {
		if err := r.server.Close(); err != nil && !errors.Is(err, mhttp.ErrServerClosed) {
			logger.Warning("masque server close failed: ", r.tag, " err=", err)
		}
	}
	if r.tun != nil {
		if err := r.tun.Close(); err != nil {
			logger.Warning("masque tun close failed: ", r.tag, " err=", err)
		}
	}
}

func (r *masqueRuntime) serveConnectIP(ctx context.Context, w mhttp.ResponseWriter, req *connectip.Request) error {
	r.connMu.Lock()
	defer r.connMu.Unlock()

	conn, err := r.proxy.Proxy(w, req)
	if err != nil {
		return err
	}
	defer conn.Close()

	setupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
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

	logger.Info("masque connect-ip session started: ", r.tag, " peer=", r.peerPrefix)
	errc := make(chan error, 2)
	go func() {
		for ctx.Err() == nil {
			packet, err := conn.ReadPacket()
			if err != nil {
				errc <- err
				return
			}
			if len(packet) == 0 {
				continue
			}
			if err := r.tun.WritePacket(packet); err != nil {
				errc <- err
				return
			}
		}
		errc <- ctx.Err()
	}()
	go func() {
		buf := make([]byte, 65535)
		for ctx.Err() == nil {
			n, err := r.tun.ReadPacket(ctx, buf)
			if err != nil {
				errc <- err
				return
			}
			if n <= 0 {
				continue
			}
			if _, err := conn.WritePacket(append([]byte(nil), buf[:n]...)); err != nil {
				errc <- err
				return
			}
		}
		errc <- ctx.Err()
	}()

	err = <-errc
	logger.Info("masque connect-ip session stopped: ", r.tag, " err=", err)
	return err
}

type masqueEndpointConfig struct {
	Host       string
	Port       int
	Network    string
	PrivateKey string
	IP         string
	MTU        int
}

func parseMasqueEndpoint(endpoint *model.Endpoint) (*masqueEndpointConfig, error) {
	var payload struct {
		Server     string `json:"server"`
		Port       int    `json:"port"`
		Network    string `json:"network"`
		PrivateKey string `json:"private_key"`
		IP         string `json:"ip"`
		MTU        int    `json:"mtu"`
	}
	if endpoint.Options != nil {
		if err := json.Unmarshal(endpoint.Options, &payload); err != nil {
			return nil, err
		}
	}

	return &masqueEndpointConfig{
		Host:       strings.TrimSpace(payload.Server),
		Port:       payload.Port,
		Network:    normalizeMasqueNetwork(payload.Network),
		PrivateKey: strings.TrimSpace(payload.PrivateKey),
		IP:         strings.TrimSpace(payload.IP),
		MTU:        payload.MTU,
	}, nil
}

func normalizeMasqueNetwork(network string) string {
	network = strings.TrimSpace(network)
	if network == "" {
		return "quic"
	}
	return network
}

func masqueTemplateDescription(config *masqueEndpointConfig) string {
	return "https://cloudflareaccess.com"
}

func parseMasquePeerPrefix(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Prefix{}, common.NewError("masque client ip is required")
	}
	if !strings.Contains(raw, "/") {
		raw += "/32"
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid masque client ip: %w", err)
	}
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, common.NewError("masque client ip must be IPv4")
	}
	return prefix.Masked(), nil
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

func (s *MasqueService) loadMasqueTLSCertificate(config *masqueEndpointConfig) (mtls.Certificate, string, string, string, error) {
	if config != nil && strings.TrimSpace(config.PrivateKey) != "" {
		cert, err := generateMasqueTLSCertificate(config.PrivateKey)
		if err != nil {
			return mtls.Certificate{}, "", "", "", err
		}
		return cert, "", "", "endpoint-key", nil
	}

	certFile, keyFile, err := s.resolveMasqueCertFiles(config.Host)
	if err != nil {
		return mtls.Certificate{}, "", "", "", err
	}
	cert, err := mtls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return mtls.Certificate{}, "", "", "", err
	}
	return cert, certFile, keyFile, "file", nil
}

func generateMasqueTLSCertificate(rawPrivateKey string) (mtls.Certificate, error) {
	priv, err := parseMasquePrivateKey(rawPrivateKey)
	if err != nil {
		return mtls.Certificate{}, err
	}

	now := time.Now()
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()),
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return mtls.Certificate{}, err
	}
	return mtls.Certificate{
		Certificate: [][]byte{der},
		PrivateKey:  priv,
	}, nil
}

func parseMasquePrivateKey(raw string) (*ecdsa.PrivateKey, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, common.NewError("empty masque private key")
	}
	data, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		return nil, err
	}
	return x509.ParseECPrivateKey(data)
}

func formatMasqueHost(host string) string {
	host = strings.TrimSpace(host)
	if host == "" {
		return ""
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		return host
	}
	if strings.Count(host, ":") > 1 {
		return "[" + host + "]"
	}
	return host
}

func (s *MasqueService) resolveMasqueCertFiles(host string) (string, string, error) {
	host = strings.TrimSpace(host)
	if host != "" {
		if certFile, keyFile, ok := resolveAcmeCertFiles(host); ok {
			return certFile, keyFile, nil
		}
	}

	allSetting, err := s.GetAllSetting()
	if err != nil {
		return "", "", err
	}

	candidates := [][2]string{
		{(*allSetting)["subCertFile"], (*allSetting)["subKeyFile"]},
		{(*allSetting)["webCertFile"], (*allSetting)["webKeyFile"]},
	}

	for _, candidate := range candidates {
		certFile := strings.TrimSpace(candidate[0])
		keyFile := strings.TrimSpace(candidate[1])
		if certFile == "" || keyFile == "" {
			continue
		}
		if err := fileMustExist(certFile); err != nil {
			continue
		}
		if err := fileMustExist(keyFile); err != nil {
			continue
		}
		return certFile, keyFile, nil
	}

	if host != "" {
		if webDomain := strings.TrimSpace((*allSetting)["webDomain"]); webDomain != "" && webDomain != host {
			if certFile, keyFile, ok := resolveAcmeCertFiles(webDomain); ok {
				return certFile, keyFile, nil
			}
		}
		if subDomain := strings.TrimSpace((*allSetting)["subDomain"]); subDomain != "" && subDomain != host {
			if certFile, keyFile, ok := resolveAcmeCertFiles(subDomain); ok {
				return certFile, keyFile, nil
			}
		}
	}

	return "", "", common.NewError("masque certificate and key are not configured")
}
