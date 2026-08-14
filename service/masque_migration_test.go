package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestMigrateLegacyMasqueEndpointsCreatesMultiUserInbound(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "masque-migration.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	serverKey, err := newMasqueClientCredential()
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	legacy := model.Endpoint{
		Type: "masque",
		Tag:  "legacy-masque",
		Options: json.RawMessage(`{
			"server":"example.com","port":8443,"network":"quic",
			"private_key":"` + serverKey.PrivateKey + `","public_key":"` + serverKey.PublicKey + `",
			"ip":"172.16.7.24/32","mtu":1380,"keepalive":25,"udp":true
		}`),
	}
	if err := database.GetDB().Create(&legacy).Error; err != nil {
		t.Fatalf("create legacy endpoint: %v", err)
	}
	clients := []model.Client{
		{Name: "alice", Enable: true, Config: json.RawMessage("{}"), Inbounds: json.RawMessage("[]"), Links: json.RawMessage("[]")},
		{Name: "bob", Enable: true, Config: json.RawMessage("{}"), Inbounds: json.RawMessage("[]"), Links: json.RawMessage("[]")},
	}
	if err := database.GetDB().Create(&clients).Error; err != nil {
		t.Fatalf("create clients: %v", err)
	}

	if err := MigrateLegacyMasqueEndpoints(); err != nil {
		t.Fatalf("migrate legacy endpoint: %v", err)
	}

	var endpointCount int64
	if err := database.GetDB().Model(&model.Endpoint{}).Where("type = ?", "masque").Count(&endpointCount).Error; err != nil {
		t.Fatal(err)
	}
	if endpointCount != 0 {
		t.Fatalf("legacy endpoint still exists: %d", endpointCount)
	}
	var inbound model.Inbound
	if err := database.GetDB().Where("type = ? AND tag = ?", "masque", legacy.Tag).First(&inbound).Error; err != nil {
		t.Fatalf("load migrated inbound: %v", err)
	}
	config, err := parseMasqueInbound(&inbound)
	if err != nil {
		t.Fatalf("parse migrated inbound: %v", err)
	}
	if config.Port != 8443 || config.ClientSubnet != "172.16.7.0/24" {
		t.Fatalf("unexpected migrated config: %#v", config)
	}

	var migratedClients []model.Client
	if err := database.GetDB().Order("id ASC").Find(&migratedClients).Error; err != nil {
		t.Fatal(err)
	}
	if len(migratedClients) != 2 {
		t.Fatalf("unexpected client count: %d", len(migratedClients))
	}
	publicKeys := make(map[string]struct{}, len(migratedClients))
	addresses := make(map[string]struct{}, len(migratedClients))
	for _, client := range migratedClients {
		ids, err := decodeClientInboundIDs(client.Inbounds)
		if err != nil || len(ids) != 1 || ids[0] != inbound.Id {
			t.Fatalf("client %s was not assigned to migrated inbound: ids=%v err=%v", client.Name, ids, err)
		}
		credentials, _, err := decodeMasqueCredentials(client.Config)
		if err != nil {
			t.Fatal(err)
		}
		credential := credentials[masqueCredentialKey(inbound.Id)]
		if credential.PrivateKey == "" || credential.PublicKey == "" || credential.IP == "" {
			t.Fatalf("client %s has incomplete MASQUE credentials: %#v", client.Name, credential)
		}
		publicKeys[credential.PublicKey] = struct{}{}
		addresses[credential.IP] = struct{}{}
	}
	if len(publicKeys) != 2 || len(addresses) != 2 {
		t.Fatalf("migrated clients do not have unique identities: keys=%v addresses=%v", publicKeys, addresses)
	}
	if err := MigrateLegacyMasqueEndpoints(); err != nil {
		t.Fatalf("repeat migration must be idempotent: %v", err)
	}
}
