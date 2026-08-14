package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestNormalizeMasqueClientConfigsAllocatesUniqueCredentials(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "masque-clients.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	serverKey, err := newMasqueClientCredential()
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	inbound := model.Inbound{
		Type: "masque", Tag: "masque-test", Addrs: json.RawMessage("[]"), OutJson: json.RawMessage("{}"),
		Options: json.RawMessage(`{"listen":"::","listen_port":8443,"server":"example.com","network":"quic","private_key":"` + serverKey.PrivateKey + `","public_key":"` + serverKey.PublicKey + `","client_subnet":"172.20.1.0/24","mtu":1380,"keepalive":25,"udp":true}`),
	}
	if err := database.GetDB().Create(&inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	clients := []*model.Client{
		{Name: "alice", Enable: true, Config: json.RawMessage("{}"), Inbounds: json.RawMessage("[" + jsonNumber(inbound.Id) + "]")},
		{Name: "bob", Enable: true, Config: json.RawMessage("{}"), Inbounds: json.RawMessage("[" + jsonNumber(inbound.Id) + "]")},
	}
	if err := normalizeMasqueClientConfigs(database.GetDB(), clients); err != nil {
		t.Fatalf("normalize clients: %v", err)
	}
	first, _, err := decodeMasqueCredentials(clients[0].Config)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := decodeMasqueCredentials(clients[1].Config)
	if err != nil {
		t.Fatal(err)
	}
	a := first[masqueCredentialKey(inbound.Id)]
	b := second[masqueCredentialKey(inbound.Id)]
	if a.PrivateKey == "" || b.PrivateKey == "" || a.PrivateKey == b.PrivateKey {
		t.Fatalf("expected unique client keys: %#v %#v", a, b)
	}
	if a.IP == "" || b.IP == "" || a.IP == b.IP {
		t.Fatalf("expected unique client addresses: %#v %#v", a, b)
	}
	if a.PublicKey == serverKey.PublicKey || b.PublicKey == serverKey.PublicKey {
		t.Fatal("client identity must not reuse the server key")
	}
	for _, client := range clients {
		if err := database.GetDB().Create(client).Error; err != nil {
			t.Fatalf("save client %s: %v", client.Name, err)
		}
	}
	identities, err := loadMasqueClientIdentities(database.GetDB(), &inbound)
	if err != nil {
		t.Fatalf("load client identities: %v", err)
	}
	if len(identities) != 2 || identities[a.PublicKey].Name != "alice" || identities[b.PublicKey].Name != "bob" {
		t.Fatalf("unexpected client identity index: %#v", identities)
	}
}
