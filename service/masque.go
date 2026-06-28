package service

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"
	masque "github.com/quic-go/masque-go"
	"github.com/quic-go/quic-go/http3"
	"github.com/yosida95/uritemplate/v3"
	"log/slog"
)

var masquePtr *MasqueService

type masqueRuntime struct {
	tag      string
	port     int
	host     string
	proxy    *masque.Proxy
	server   *http3.Server
	template *uritemplate.Template
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
	if config.Network != "" && config.Network != "quic" {
		logger.Warning("masque service currently runs with quic/h3; network=", config.Network, " is kept for config preview only")
	}

	certFile, keyFile, err := s.resolveMasqueCertFiles(config.Host)
	if err != nil {
		return nil, err
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("load masque certificate failed: %w", err)
	}

	bindAddr := net.JoinHostPort("0.0.0.0", strconv.Itoa(config.Port))
	templateStr := fmt.Sprintf("https://%s/?h={target_host}&p={target_port}", config.Host)
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
				w.WriteHeader(perr.HTTPStatus)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := proxy.Proxy(w, req); err != nil && !errors.Is(err, net.ErrClosed) {
			logger.Warning("masque proxy failed: ", err)
		}
	})

	srv := &http3.Server{
		Addr:            bindAddr,
		TLSConfig:       http3.ConfigureTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}}),
		Handler:         mux,
		EnableDatagrams: true,
		Logger:          slog.Default(),
	}

	handle := &masqueRuntime{
		tag:      endpoint.Tag,
		port:     config.Port,
		host:     config.Host,
		proxy:    proxy,
		server:   srv,
		template: template,
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
	Host    string
	Port    int
	Network string
}

func parseMasqueEndpoint(endpoint *model.Endpoint) (*masqueEndpointConfig, error) {
	var payload struct {
		Server  string `json:"server"`
		Port    int    `json:"port"`
		Network string `json:"network"`
	}
	if endpoint.Options != nil {
		if err := json.Unmarshal(endpoint.Options, &payload); err != nil {
			return nil, err
		}
	}

	return &masqueEndpointConfig{
		Host:    strings.TrimSpace(payload.Server),
		Port:    payload.Port,
		Network: strings.TrimSpace(payload.Network),
	}, nil
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
