package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	mtls "github.com/metacubex/tls"
)

func TestMasqueRuntimeStatusSnapshot(t *testing.T) {
	runtime := &masqueRuntime{}
	runtime.running.Store(true)
	runtime.rxBytes.Store(4096)
	runtime.txBytes.Store(8192)
	runtime.rxPackets.Store(4)
	runtime.txPackets.Store(8)

	session := &masqueSession{
		startedAt: time.Now().Add(-5 * time.Second),
		remote:    "203.0.113.10:44321",
	}
	session.rxBytes.Store(1024)
	session.txBytes.Store(2048)
	runtime.activateSession(session)

	status := runtime.statusSnapshot()
	if status["session_active"] != true || status["client_addr"] != "203.0.113.10:44321" {
		t.Fatalf("unexpected session status: %#v", status)
	}
	if status["rx_bytes"] != uint64(4096) || status["session_tx_bytes"] != uint64(2048) {
		t.Fatalf("unexpected traffic counters: %#v", status)
	}
}

func TestMasqueCertificateReloaderReloadsChangedFiles(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "fullchain.cer")
	keyFile := filepath.Join(dir, "server.key")
	writeTestMasqueCertificate(t, certFile, keyFile, time.Now().Add(24*time.Hour), 1)

	initial, err := mtls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		t.Fatalf("load initial certificate: %v", err)
	}
	reloader := newMasqueCertificateReloader(initial, certFile, keyFile, "file")
	before := reloader.snapshot()

	writeTestMasqueCertificate(t, certFile, keyFile, time.Now().Add(48*time.Hour), 2)
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(certFile, future, future); err != nil {
		t.Fatalf("update certificate mtime: %v", err)
	}
	if err := os.Chtimes(keyFile, future, future); err != nil {
		t.Fatalf("update key mtime: %v", err)
	}
	reloader.reloadIfChanged(true)
	after := reloader.snapshot()

	if after.ReloadCount != 1 {
		t.Fatalf("expected one reload, got %#v", after)
	}
	if after.Fingerprint == "" || after.Fingerprint == before.Fingerprint {
		t.Fatalf("certificate fingerprint was not updated: before=%s after=%s", before.Fingerprint, after.Fingerprint)
	}
	if after.LastError != "" {
		t.Fatalf("unexpected reload error: %s", after.LastError)
	}
}

func TestMasqueDiagnosticsRejectInvalidCertificateWindow(t *testing.T) {
	config := &masqueEndpointConfig{Network: "quic"}
	tests := []struct {
		name string
		cert masqueCertificateSnapshot
		want string
	}{
		{
			name: "not yet valid",
			cert: masqueCertificateSnapshot{NotBefore: time.Now().Add(time.Hour), NotAfter: time.Now().Add(25 * time.Hour)},
			want: "证书尚未生效",
		},
		{
			name: "expired",
			cert: masqueCertificateSnapshot{NotBefore: time.Now().Add(-25 * time.Hour), NotAfter: time.Now().Add(-time.Hour)},
			want: "证书已过期",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checks := buildMasqueDiagnostics(config, nil, tt.cert, nil, "")
			for _, check := range checks {
				if check.ID == "certificate" {
					if check.Status != "error" || check.Detail != tt.want {
						t.Fatalf("unexpected certificate diagnostic: %#v", check)
					}
					return
				}
			}
			t.Fatal("certificate diagnostic is missing")
		})
	}
}

func writeTestMasqueCertificate(t *testing.T, certFile, keyFile string, notAfter time.Time, serial int64) {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     notAfter,
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"masque.example.com"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatalf("write certificate: %v", err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatalf("write private key: %v", err)
	}
}
