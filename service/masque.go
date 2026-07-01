package service

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"
	masque "github.com/quic-go/masque-go"
	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"
	"log/slog"
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
	proxy       *masque.Proxy
	server      *http3.Server
	template    *uritemplate.Template
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
	if config.Network != "" && config.Network != "quic" && config.Network != "h3-l4proxy" {
		logger.Warning("masque service currently runs with quic/h3; network=", config.Network, " is kept for config preview only")
	}

	cert, certFile, keyFile, certSource, err := s.loadMasqueTLSCertificate(config)
	if err != nil {
		return nil, fmt.Errorf("load masque certificate failed: %w", err)
	}

	bindAddr := net.JoinHostPort("0.0.0.0", strconv.Itoa(config.Port))
	templateStr := fmt.Sprintf("https://%s/masque?h={target_host}&p={target_port}", formatMasqueHost(config.Host))
	template, err := uritemplate.New(templateStr)
	if err != nil {
		return nil, fmt.Errorf("build masque uri template failed: %w", err)
	}
	parsedURL, err := url.Parse(templateStr)
	if err != nil {
		return nil, fmt.Errorf("parse masque uri template failed: %w", err)
	}

	proxy := &masque.Proxy{}
	mux := http.NewServeMux()
	mux.HandleFunc(parsedURL.Path, func(w http.ResponseWriter, r *http.Request) {
		req, err := masque.ParseRequest(r, template)
		if err != nil {
			var perr *masque.RequestParseError
			if errors.As(err, &perr) {
				logger.Warning("masque request parse failed: ", endpoint.Tag, " status=", perr.HTTPStatus, " err=", perr.Err)
				w.WriteHeader(perr.HTTPStatus)
				return
			}
			logger.Warning("masque request parse failed: ", endpoint.Tag, " err=", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := proxy.Proxy(w, req); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Warning("masque proxy failed: ", err)
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err := proxyMasqueConnect(w, r); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Warning("masque connect proxy failed: ", endpoint.Tag, " target=", r.Host, " err=", err)
		}
	})

	srv := &http3.Server{
		Addr:            bindAddr,
		QUICConfig:      &quic.Config{EnableDatagrams: true},
		TLSConfig:       http3.ConfigureTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}}),
		Handler:         mux,
		EnableDatagrams: true,
		Logger:          slog.Default(),
	}

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
		server:      srv,
		template:    template,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
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
	if r.proxy != nil {
		if err := r.proxy.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Warning("masque proxy close failed: ", r.tag, " err=", err)
		}
	}
	if r.server != nil {
		if err := r.server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Warning("masque server close failed: ", r.tag, " err=", err)
		}
	}
}

type masqueEndpointConfig struct {
	Host       string
	Port       int
	Network    string
	PrivateKey string
}

func parseMasqueEndpoint(endpoint *model.Endpoint) (*masqueEndpointConfig, error) {
	var payload struct {
		Server     string `json:"server"`
		Port       int    `json:"port"`
		Network    string `json:"network"`
		PrivateKey string `json:"private_key"`
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
	}, nil
}

func normalizeMasqueNetwork(network string) string {
	network = strings.TrimSpace(network)
	if network == "" {
		return "h3-l4proxy"
	}
	return network
}

func masqueTemplateDescription(config *masqueEndpointConfig) string {
	if config != nil && config.Network == "h3-l4proxy" {
		return "h3 CONNECT TCP proxy"
	}
	return fmt.Sprintf("https://%s/masque?h={target_host}&p={target_port}", formatMasqueHost(config.Host))
}

func proxyMasqueConnect(w http.ResponseWriter, r *http.Request) error {
	target := strings.TrimSpace(r.Host)
	if target == "" {
		target = strings.TrimSpace(r.URL.Host)
	}
	if target == "" {
		http.Error(w, "missing connect target", http.StatusBadRequest)
		return nil
	}
	if _, _, err := net.SplitHostPort(target); err != nil {
		http.Error(w, "invalid connect target", http.StatusBadRequest)
		return nil
	}

	streamer, ok := w.(http3.HTTPStreamer)
	if !ok {
		http.Error(w, "http3 stream unavailable", http.StatusInternalServerError)
		return nil
	}

	targetConn, err := net.DialTimeout("tcp", target, 15*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return err
	}
	defer targetConn.Close()

	w.WriteHeader(http.StatusOK)
	stream := streamer.HTTPStream()
	defer stream.Close()

	errc := make(chan error, 2)
	go func() {
		_, err := io.Copy(targetConn, stream)
		if tcpConn, ok := targetConn.(*net.TCPConn); ok {
			_ = tcpConn.CloseWrite()
		}
		errc <- err
	}()
	go func() {
		_, err := io.Copy(stream, targetConn)
		errc <- err
	}()

	err = <-errc
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (s *MasqueService) loadMasqueTLSCertificate(config *masqueEndpointConfig) (tls.Certificate, string, string, string, error) {
	if config != nil && strings.TrimSpace(config.PrivateKey) != "" {
		cert, err := generateMasqueTLSCertificate(config.PrivateKey)
		if err != nil {
			return tls.Certificate{}, "", "", "", err
		}
		return cert, "", "", "endpoint-key", nil
	}

	certFile, keyFile, err := s.resolveMasqueCertFiles(config.Host)
	if err != nil {
		return tls.Certificate{}, "", "", "", err
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, "", "", "", err
	}
	return cert, certFile, keyFile, "file", nil
}

func generateMasqueTLSCertificate(rawPrivateKey string) (tls.Certificate, error) {
	priv, err := parseMasquePrivateKey(rawPrivateKey)
	if err != nil {
		return tls.Certificate{}, err
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
		return tls.Certificate{}, err
	}
	return tls.Certificate{
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
