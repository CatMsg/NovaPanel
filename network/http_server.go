package network

import (
	"crypto/tls"
	"net/http"
	"time"
)

const maxHTTPHeaderBytes = 64 << 10

func NewHTTPServer(handler http.Handler) *http.Server {
	return &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    maxHTTPHeaderBytes,
	}
}

func NewServerTLSConfig(certificate tls.Certificate) *tls.Config {
	return &tls.Config{
		Certificates: []tls.Certificate{certificate},
		MinVersion:   tls.VersionTLS12,
	}
}
