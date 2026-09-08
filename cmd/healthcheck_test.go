package cmd

import (
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalPanelHealthCheck(t *testing.T) {
	for _, mode := range []string{"http", "https", "redirect", "failed", "wrong-cert"} {
		t.Run(mode, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/custom/" {
					t.Errorf("wrong path: %s", r.URL.Path)
				}
				switch mode {
				case "failed":
					w.WriteHeader(http.StatusInternalServerError)
				case "redirect":
					w.Header().Set("Location", "https://must-not-be-contacted.invalid/")
					w.WriteHeader(http.StatusFound)
				default:
					w.WriteHeader(http.StatusOK)
				}
			})
			server := httptest.NewUnstartedServer(handler)
			if mode == "https" || mode == "wrong-cert" {
				server.StartTLS()
			} else {
				server.Start()
			}
			defer server.Close()
			host, port, _ := net.SplitHostPort(server.Listener.Addr().String())
			settings := map[string]string{"webListen": host, "webPort": port, "webPath": "/custom/"}
			if server.TLS != nil {
				dir := t.TempDir()
				pair := server.TLS.Certificates[0]
				key, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
				if err != nil {
					t.Fatal(err)
				}
				settings["webCertFile"] = filepath.Join(dir, "cert.pem")
				settings["webKeyFile"] = filepath.Join(dir, "key.pem")
				if err := os.WriteFile(settings["webCertFile"], pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]}), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(settings["webKeyFile"], pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
					t.Fatal(err)
				}
				if mode == "wrong-cert" {
					settings["webDomain"] = "wrong.invalid"
				}
			}
			err := probeLocalPanel(settings)
			wantFailure := mode == "failed" || mode == "wrong-cert"
			if wantFailure != (err != nil) {
				t.Fatalf("unexpected health result: %v", err)
			}
		})
	}
}
