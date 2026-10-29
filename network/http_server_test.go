package network

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPServerRejectsOversizedHeaders(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	server.Config = NewHTTPServer(server.Config.Handler)
	server.Start()
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	request.Header.Set("X-Oversized", strings.Repeat("a", maxHTTPHeaderBytes+(8<<10)))

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("send oversized-header request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusRequestHeaderFieldsTooLarge)
	}
}

func TestServerTLSConfigRequiresTLS12(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if len(server.TLS.Certificates) == 0 {
		t.Fatal("test server has no certificate")
	}
	config := NewServerTLSConfig(server.TLS.Certificates[0])
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("minimum TLS version = %d, want TLS 1.2 (%d)", config.MinVersion, tls.VersionTLS12)
	}
}
