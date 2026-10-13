package sub

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"gopkg.in/yaml.v3"
)

func TestMASQUEMihomoSubscriptionIsolation(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "dual-sub.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	inbound := model.Inbound{Type: "masque", Tag: "dual", Options: json.RawMessage(`{"mihomo_compatible":true}`), Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{"type":"masque-client","tag":"dual","server":"localhost","server_port":443,"version":3,"system":false,"on_demand":true,"disable_version_fallback":true,"tls":{"enabled":true,"alpn":["h3"]},"nova_mihomo":{"public-key":"synthetic-server-key","uri":"https://localhost:443/.well-known/masque/ip/*/*/","network":"quic","mtu":1280,"udp":true}}`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	config := json.RawMessage(fmt.Sprintf(`{"masque":{"username":"alice","password":"native-test-password","mihomo":{"%d":{"private_key":"synthetic-private-key","public_key":"synthetic-client-key","ip":"172.29.0.2/32"}}}}`, inbound.Id))
	client := model.Client{Enable: true, Name: "alice", Config: config, Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)), Links: json.RawMessage(`[]`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	native, _, err := (&JsonService{}).GetJson(client.Name, "json")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"nova_mihomo", "synthetic-private-key", "synthetic-client-key", "synthetic-server-key", "mihomo_users", "172.29.0.2"} {
		if strings.Contains(*native, forbidden) {
			t.Fatalf("native JSON leaked %s", forbidden)
		}
	}
	if !strings.Contains(*native, "native-test-password") {
		t.Fatal("native auth missing")
	}
	clash, _, err := (&ClashService{}).GetClash(client.Name)
	if err != nil {
		t.Fatal(err)
	}
	var profile struct {
		Proxies []map[string]any `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(*clash), &profile); err != nil {
		t.Fatal(err)
	}
	if len(profile.Proxies) != 1 {
		t.Fatal("dual node missing from Clash subscription")
	}
	proxy := profile.Proxies[0]
	for key, value := range map[string]any{"type": "masque", "private-key": "synthetic-private-key", "public-key": "synthetic-server-key", "ip": "172.29.0.2/32", "uri": "https://localhost:443/.well-known/masque/ip/*/*/", "udp": true} {
		if proxy[key] != value {
			t.Fatalf("Clash field %s mismatch", key)
		}
	}
	for _, forbidden := range []string{"native-test-password", "nova_mihomo", "synthetic-client-key", "masque-client"} {
		if strings.Contains(*clash, forbidden) {
			t.Fatalf("Clash leaked %s", forbidden)
		}
	}
	if _, _, err := (&JsonService{}).getOutbounds(json.RawMessage(`{"masque":{"username":"alice","password":"native-test-password"}}`), []*model.Inbound{&inbound}, "clash"); err == nil {
		t.Fatal("missing key silently downgraded to Basic auth")
	}
	certificatePath := filepath.Join(t.TempDir(), "server.pem")
	rotateCertificate := func() string {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		template := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
		der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(certificatePath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
			t.Fatal(err)
		}
		publicDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
		return base64.StdEncoding.EncodeToString(publicDER)
	}
	firstKey := rotateCertificate()
	tlsServer, _ := json.Marshal(map[string]any{"enabled": true, "certificate_path": certificatePath})
	tls := model.Tls{Name: "live-cert", Server: tlsServer, Client: json.RawMessage(`{}`)}
	if err := db.Create(&tls).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&inbound).Update("tls_id", tls.Id).Error; err != nil {
		t.Fatal(err)
	}
	clash, _, err = (&ClashService{}).GetClash(client.Name)
	if err != nil || !strings.Contains(*clash, firstKey) || strings.Contains(*clash, "synthetic-server-key") {
		t.Fatal("live certificate pin was not exported")
	}
	secondKey := rotateCertificate()
	clash, _, err = (&ClashService{}).GetClash(client.Name)
	if err != nil || !strings.Contains(*clash, secondKey) || strings.Contains(*clash, firstKey) {
		t.Fatal("ACME public-key rotation served a stale cached pin")
	}
	if err := db.Model(&inbound).Update("exclude_from_sub", true).Error; err != nil {
		t.Fatal(err)
	}
	clash, _, err = (&ClashService{}).GetClash(client.Name)
	if err != nil || strings.Contains(*clash, "synthetic-private-key") {
		t.Fatal("hidden dual inbound remained in cached Clash")
	}
}

func TestMASQUEMihomoGeneratedNodeAcceptedByBinary(t *testing.T) {
	binary := os.Getenv("NOVAPANEL_MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("requires official Mihomo binary")
	}
	// Check actual converted node fields using a real parser, without traffic.
	if _, err := exec.LookPath(binary); err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, _ := x509.MarshalECPrivateKey(key)
	publicDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	privateKey, publicKey := base64.StdEncoding.EncodeToString(privateDER), base64.StdEncoding.EncodeToString(publicDER)
	inbound := &model.Inbound{Id: 1, Type: "masque", Tag: "dual", Addrs: json.RawMessage(`[]`)}
	inbound.OutJson, _ = json.Marshal(map[string]any{"type": "masque-client", "tag": "dual", "server": "2001:db8::1", "server_port": 443, "nova_mihomo": map[string]any{"public-key": publicKey, "uri": "https://[2001:db8::1]:443/.well-known/masque/ip/*/*/", "udp": true, "mtu": 1280}})
	clientConfig, _ := json.Marshal(map[string]any{"masque": map[string]any{"username": "alice", "password": "native-test-only", "mihomo": map[string]any{"1": map[string]any{"private_key": privateKey, "ip": "172.29.0.2/32"}}}})
	nodes, _, err := (&JsonService{}).getOutbounds(clientConfig, []*model.Inbound{inbound}, "clash")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := (&ClashService{}).ConvertToClashMeta(nodes, "mode: rule\nrules:\n  - MATCH,dual\n")
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := yaml.Unmarshal([]byte(profile), &parsed); err != nil {
		t.Fatal(err)
	}
	proxy := parsed["proxies"].([]any)[0].(map[string]any)
	if proxy["server"] != "2001:db8::1" {
		t.Fatal("IPv6 server contains literal YAML quote/bracket characters")
	}
	directory := t.TempDir()
	path := filepath.Join(directory, "clash.yaml")
	if err := os.WriteFile(path, []byte(profile), 0600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(binary, "-t", "-d", directory, "-f", path).CombinedOutput(); err != nil {
		t.Fatalf("actual Mihomo rejected generated subscription: %v: %s", err, output)
	}
}
