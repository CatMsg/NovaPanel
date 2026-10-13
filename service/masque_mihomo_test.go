package service

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestMASQUEMihomoListMetadata(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "mihomo-list.db")); err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{Type: "masque", Tag: "dual-list", Options: json.RawMessage(`{"listen_port":443,"mihomo_compatible":true}`)}
	if err := database.GetDB().Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	rows, err := (&InboundService{}).GetAll()
	if err != nil || len(*rows) != 1 {
		t.Fatal("failed to list compatibility metadata")
	}
	raw, err := json.Marshal((*rows)[0])
	if err != nil || !bytes.Contains(raw, []byte(`"mihomo_compatible":true`)) || bytes.Contains(raw, []byte("private_key")) {
		t.Fatal("share dialog cannot determine the subscription compatibility")
	}
}

func TestMASQUEMihomoCredentialsStableBatchAndRevocation(t *testing.T) {
	db, inbound := masqueManagedConfigFixture(t)
	inbound.Options = json.RawMessage(`{"listen_port":443,"mihomo_compatible":true,"address":["172.29.0.1/29"]}`)
	if err := prepareMasqueInbound(db, &inbound, nil, "localhost"); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	makeClient := func(name string) *model.Client {
		return &model.Client{Enable: true, Name: name, Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)), Config: json.RawMessage(`{"socks":{"username":"keep","password":"untouched"}}`), Links: json.RawMessage(`[]`)}
	}
	alice := makeClient("alice")
	if err := normalizeMasqueClientConfigs(db, []*model.Client{alice}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(alice).Error; err != nil {
		t.Fatal(err)
	}
	before := bytes.Clone(alice.Config)
	bob := makeClient("bob")
	// Batch order must not affect per-user authentication keys.
	if err := normalizeMasqueClientConfigs(db, []*model.Client{bob, alice}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, alice.Config) {
		t.Fatal("batch save rotated stable credentials")
	}
	a, _, _ := decodeMasqueCredential(alice.Config)
	b, _, _ := decodeMasqueCredential(bob.Config)
	id := fmt.Sprint(inbound.Id)
	if a.Mihomo[id].IP != "172.29.0.2/32" || b.Mihomo[id].IP != "172.29.0.3/32" || a.Mihomo[id].PublicKey == b.Mihomo[id].PublicKey {
		t.Fatal("users did not receive independent bootstrap addresses and authentication keys")
	}
	if err := db.Create(bob).Error; err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 3; round++ {
		if err := normalizeMasqueClientConfigs(db, []*model.Client{alice}); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, alice.Config) {
			t.Fatal("ordinary save rotated a key")
		}
	}
	alice.Config = json.RawMessage(`{"masque":{"username":"alice","password":"new-native-password"},"socks":{"username":"keep","password":"untouched"}}`)
	if err := normalizeMasqueClientConfigs(db, []*model.Client{alice}); err != nil {
		t.Fatal(err)
	}
	rotated, _, _ := decodeMasqueCredential(alice.Config)
	if rotated.Mihomo[id].PublicKey == a.Mihomo[id].PublicKey || rotated.Mihomo[id].PrivateKey == a.Mihomo[id].PrivateKey || rotated.Password != "new-native-password" {
		t.Fatal("explicit revocation did not rotate both modes")
	}
	if err := db.Save(alice).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(bob).Update("enable", false).Error; err != nil {
		t.Fatal(err)
	}
	raw, err := buildMasqueEndpointConfig(db, &inbound)
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Users  []masqueClientCredential `json:"users"`
		Mihomo []struct {
			Username  string `json:"username"`
			PublicKey string `json:"public_key"`
			Address   string `json:"address"`
		} `json:"mihomo_users"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Users) != 1 || len(config.Mihomo) != 1 || config.Mihomo[0].Username != "alice" || config.Mihomo[0].PublicKey != rotated.Mihomo[id].PublicKey || config.Mihomo[0].Address != "172.29.0.2" || bytes.Contains(raw, []byte(a.Mihomo[id].PublicKey)) || bytes.Contains(raw, []byte(rotated.Mihomo[id].PrivateKey)) {
		t.Fatal("revoked/disabled/private key leaked into live server identities")
	}
	alice.Inbounds = json.RawMessage(`[]`)
	if err := normalizeMasqueClientConfigs(db, []*model.Client{alice}); err != nil {
		t.Fatal(err)
	}
	unbound, _, _ := decodeMasqueCredential(alice.Config)
	if len(unbound.Mihomo) != 0 {
		t.Fatal("unbound user retained a compatibility key")
	}
}

func TestMASQUEMihomoTemplateUsesDedicatedECDSAIdentity(t *testing.T) {
	db, inbound := masqueManagedConfigFixture(t)
	inbound.Options = json.RawMessage(`{"listen_port":443,"mihomo_compatible":true}`)
	if err := prepareMasqueInbound(db, &inbound, nil, "2001:db8::1"); err != nil {
		t.Fatal(err)
	}
	raw, err := buildMasqueClientTemplate(&inbound)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	compatible, ok := config["nova_mihomo"].(map[string]any)
	if !ok || compatible["uri"] != "https://[2001:db8::1]:443/.well-known/masque/ip/*/*/" || compatible["public-key"] == "" {
		t.Fatal("Mihomo template has invalid IPv6 URI or server key")
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&InboundService{}).UpdateOutJsons(db, []uint{inbound.Id}, "localhost"); err != nil {
		t.Fatal(err)
	}
	var saved model.Inbound
	if err := db.First(&saved, inbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(saved.OutJson, []byte(`"nova_mihomo"`)) {
		t.Fatal("TLS-triggered outbound refresh discarded compatibility fields")
	}
	if inbound.TlsId != 0 || inbound.Tls != nil || inbound.MasqueTLSCertificate == "" {
		t.Fatal("Mihomo compatibility still depends on a shared TLS template")
	}
	certificate, err := validateMasqueTLSMaterial(&inbound, "2001:db8::1")
	if err != nil {
		t.Fatalf("dedicated certificate is invalid: %v", err)
	}
	publicKey, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if compatible["public-key"] != base64.StdEncoding.EncodeToString(publicKey) {
		t.Fatal("Mihomo pin does not match the dedicated server certificate")
	}
}
