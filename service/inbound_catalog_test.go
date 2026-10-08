package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestInboundCatalogAllowsClientsOnEmptyUserInbounds(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "inbound-catalog.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	db := database.GetDB()

	inbounds := []model.Inbound{
		{
			Type:    "vless",
			Tag:     "vless-empty",
			Addrs:   json.RawMessage(`[]`),
			Options: json.RawMessage(`{"listen":"0.0.0.0","listen_port":443}`),
		},
		{Type: "dns", Tag: "non-client-protocol", Options: json.RawMessage(`{}`)},
		{
			Type:    "shadowsocks",
			Tag:     "managed-shadowsocks",
			Options: json.RawMessage(`{"listen_port":443,"method":"aes-128-gcm","password":"server","managed":true}`),
		},
		{
			Type:    "shadowtls",
			Tag:     "shadowtls-v2",
			Options: json.RawMessage(`{"listen_port":443,"version":2}`),
		},
	}
	for i := range inbounds {
		if err := db.Create(&inbounds[i]).Error; err != nil {
			t.Fatalf("create inbound %q: %v", inbounds[i].Tag, err)
		}
	}

	inboundService := &InboundService{}
	catalog, err := inboundService.GetAll()
	if err != nil {
		t.Fatalf("get inbound catalog: %v", err)
	}
	if len(*catalog) != len(inbounds) {
		t.Fatalf("catalog contains %d inbounds, want %d", len(*catalog), len(inbounds))
	}
	sawEmptyUserInbound := false
	for _, inbound := range *catalog {
		tag, _ := inbound["tag"].(string)
		users, hasUsers := inbound["users"]
		if tag == "vless-empty" {
			sawEmptyUserInbound = true
			userNames, ok := users.([]string)
			if !hasUsers || !ok || userNames == nil || len(userNames) != 0 {
				t.Fatalf("empty-user inbound catalog entry = %#v, want a present non-nil empty users slice", inbound)
			}
			encoded, err := json.Marshal(users)
			if err != nil || string(encoded) != "[]" {
				t.Fatalf("empty users JSON = %s, err=%v; want []", encoded, err)
			}
			continue
		}
		if hasUsers {
			t.Errorf("unsupported inbound %q unexpectedly advertises users: %#v", tag, users)
		}
	}
	if !sawEmptyUserInbound {
		t.Fatal("empty-user inbound is missing from the catalog")
	}

	inboundIDs, err := json.Marshal([]uint{inbounds[0].Id})
	if err != nil {
		t.Fatalf("marshal inbound IDs: %v", err)
	}
	clientData, err := json.Marshal(model.Client{
		Enable:   true,
		Name:     "client-for-empty-inbound",
		Config:   json.RawMessage(`{"vless":{"uuid":"550e8400-e29b-41d4-a716-446655440000"}}`),
		Inbounds: inboundIDs,
		Links:    json.RawMessage(`[]`),
	})
	if err != nil {
		t.Fatalf("marshal client: %v", err)
	}
	if _, err := (&ClientService{}).Save(db, "new", clientData, "edge.example.test"); err != nil {
		t.Fatalf("save client on empty-user inbound: %v", err)
	}

	var stored model.Client
	if err := db.Where("name = ?", "client-for-empty-inbound").First(&stored).Error; err != nil {
		t.Fatalf("load saved client: %v", err)
	}
	var links []map[string]string
	if err := json.Unmarshal(stored.Links, &links); err != nil {
		t.Fatalf("decode generated links: %v", err)
	}
	if len(links) != 1 || links[0]["type"] != "local" ||
		!strings.HasPrefix(links[0]["uri"], "vless://550e8400-e29b-41d4-a716-446655440000@edge.example.test:443") {
		t.Fatalf("generated links = %#v, want a local VLESS link for the selected inbound", links)
	}

	catalog, err = inboundService.GetAll()
	if err != nil {
		t.Fatalf("get inbound catalog after client save: %v", err)
	}
	for _, inbound := range *catalog {
		if inbound["tag"] != "vless-empty" {
			continue
		}
		userNames, ok := inbound["users"].([]string)
		if !ok || len(userNames) != 1 || userNames[0] != stored.Name {
			t.Fatalf("inbound users after binding = %#v, want [%q]", inbound["users"], stored.Name)
		}
		return
	}
	t.Fatal("bound inbound is missing from the catalog")
}
