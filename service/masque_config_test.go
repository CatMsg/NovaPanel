package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestParseMasqueEndpointNormalizesFields(t *testing.T) {
	endpoint := &model.Endpoint{
		Options: json.RawMessage(`{
			"server":"  tk.mile.news  ",
			"port":8443,
			"network":"",
			"private_key":"  key  ",
			"ip":" 172.16.0.9/32 ",
			"mtu":1380
		}`),
	}

	config, err := parseMasqueEndpoint(endpoint)
	if err != nil {
		t.Fatalf("parse endpoint: %v", err)
	}
	if config.Host != "tk.mile.news" || config.Port != 8443 || config.Network != "quic" || config.PrivateKey != "key" || config.IP != "172.16.0.9/32" || config.MTU != 1380 {
		t.Fatalf("unexpected config: %#v", config)
	}
}

func TestParseMasquePeerPrefixRequiresIPv4(t *testing.T) {
	prefix, err := parseMasquePeerPrefix("172.16.0.9")
	if err != nil {
		t.Fatalf("parse ipv4 prefix: %v", err)
	}
	if got := prefix.String(); got != "172.16.0.9/32" {
		t.Fatalf("unexpected ipv4 prefix: %s", got)
	}

	if _, err := parseMasquePeerPrefix("fd00::9/128"); err == nil {
		t.Fatal("expected ipv6 peer prefix to fail")
	}
}

func TestGenerateMasqueTLSCertificateFromPrivateKey(t *testing.T) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	cert, err := generateMasqueTLSCertificate(base64.StdEncoding.EncodeToString(der))
	if err != nil {
		t.Fatalf("generate cert: %v", err)
	}
	if len(cert.Certificate) != 1 || cert.PrivateKey == nil {
		t.Fatalf("unexpected generated cert: %#v", cert)
	}
}

func TestResolveMasqueCertFilesFallsBackToSettings(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "masque-cert.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	certFile := filepath.Join(workDir, "panel.crt")
	keyFile := filepath.Join(workDir, "panel.key")
	if err := os.WriteFile(certFile, []byte("cert"), 0o644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyFile, []byte("key"), 0o644); err != nil {
		t.Fatalf("write key: %v", err)
	}

	svc := &MasqueService{}
	if _, err := svc.GetAllSetting(); err != nil {
		t.Fatalf("init settings: %v", err)
	}
	if err := svc.saveSetting("subCertFile", certFile); err != nil {
		t.Fatalf("save sub cert: %v", err)
	}
	if err := svc.saveSetting("subKeyFile", keyFile); err != nil {
		t.Fatalf("save sub key: %v", err)
	}

	gotCert, gotKey, err := svc.resolveMasqueCertFiles("")
	if err != nil {
		t.Fatalf("resolve cert files: %v", err)
	}
	if gotCert != certFile || gotKey != keyFile {
		t.Fatalf("unexpected resolved files: %s %s", gotCert, gotKey)
	}
}
