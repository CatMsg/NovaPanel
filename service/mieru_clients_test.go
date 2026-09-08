package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestLoadMieruClientCredentialsUsesOnlyEnabledBoundUsers(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "mieru-clients.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	db := database.GetDB()
	inbound := &model.Inbound{
		Type:    "mieru",
		Tag:     "mieru-main",
		Options: json.RawMessage(`{"listen_port":22000}`),
	}
	if err := db.Create(inbound).Error; err != nil {
		t.Fatalf("create Mieru inbound: %v", err)
	}
	clients := []*model.Client{
		{
			Enable:   true,
			Name:     "enabled-bound",
			Config:   json.RawMessage(`{"mieru":{"name":"ignored","password":"enabled-secret"}}`),
			Inbounds: json.RawMessage(`[1]`),
		},
		{
			Enable:   false,
			Name:     "disabled-bound",
			Config:   json.RawMessage(`{"mieru":{"name":"ignored","password":"disabled-secret"}}`),
			Inbounds: json.RawMessage(`[1]`),
		},
		{
			Enable:   true,
			Name:     "enabled-unbound",
			Config:   json.RawMessage(`{"mieru":{"name":"ignored","password":"unbound-secret"}}`),
			Inbounds: json.RawMessage(`[]`),
		},
	}
	for _, client := range clients {
		client.Inbounds = json.RawMessage("[" + jsonNumber(inbound.Id) + "]")
		if client.Name == "enabled-unbound" {
			client.Inbounds = json.RawMessage(`[]`)
		}
		if err := db.Create(client).Error; err != nil {
			t.Fatalf("create client %s: %v", client.Name, err)
		}
	}

	credentials, err := loadMieruClientCredentials(db, inbound.Id)
	if err != nil {
		t.Fatalf("load Mieru credentials: %v", err)
	}
	if len(credentials) != 1 || credentials[0].Name != "enabled-bound" || credentials[0].Password != "enabled-secret" {
		t.Fatalf("unexpected Mieru credentials: %#v", credentials)
	}
}

func jsonNumber(value uint) string {
	payload, _ := json.Marshal(value)
	return string(payload)
}
