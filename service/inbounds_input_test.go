package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestParseClientIDs(t *testing.T) {
	ids, err := parseClientIDs("1, 2,7")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[0] != 1 || ids[2] != 7 {
		t.Fatalf("unexpected ids: %#v", ids)
	}
}

func TestParseClientIDsRejectsSQLAndZero(t *testing.T) {
	for _, value := range []string{"1); DELETE FROM clients;--", "0", "1,,2", "-1"} {
		if _, err := parseClientIDs(value); err == nil {
			t.Fatalf("expected %q to be rejected", value)
		}
	}
}

func TestShouldSkipInboundWithoutUsers(t *testing.T) {
	tests := []struct {
		name        string
		inboundType string
		config      string
		wantSkip    bool
	}{
		{
			name:        "naive without users",
			inboundType: "naive",
			config:      `{"type":"naive","users":[]}`,
			wantSkip:    true,
		},
		{
			name:        "naive with a user",
			inboundType: "naive",
			config:      `{"type":"naive","users":[{"username":"user","password":"secret"}]}`,
			wantSkip:    false,
		},
		{
			name:        "other inbound without users",
			inboundType: "vless",
			config:      `{"type":"vless","users":[]}`,
			wantSkip:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := shouldSkipInboundWithoutUsers(tt.inboundType, []byte(tt.config))
			if err != nil {
				t.Fatalf("shouldSkipInboundWithoutUsers() error = %v", err)
			}
			if got != tt.wantSkip {
				t.Fatalf("shouldSkipInboundWithoutUsers() = %v, want %v", got, tt.wantSkip)
			}
		})
	}
}

func TestShouldSkipInboundWithoutUsersRejectsInvalidJSON(t *testing.T) {
	if _, err := shouldSkipInboundWithoutUsers("naive", []byte(`{"users":`)); err == nil {
		t.Fatal("expected invalid inbound JSON to fail")
	}
}

func TestGetAllConfigSkipsNaiveInboundUntilEnabledUserIsAssigned(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "naive-inbound.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	db := database.GetDB()
	inbound := model.Inbound{
		Type:    "naive",
		Tag:     "naive-test",
		Options: json.RawMessage(`{"listen_port":443}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	linkedInboundIDs, err := json.Marshal([]uint{inbound.Id})
	if err != nil {
		t.Fatalf("marshal inbound ids: %v", err)
	}
	client := model.Client{
		Enable:   false,
		Name:     "naive-test-user",
		Config:   json.RawMessage(`{"naive":{"username":"test-user","password":"test-pass"}}`),
		Inbounds: linkedInboundIDs,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create disabled client: %v", err)
	}

	svc := &InboundService{}
	configs, err := svc.GetAllConfig(db)
	if err != nil {
		t.Fatalf("get config without enabled users: %v", err)
	}
	if len(configs) != 0 {
		t.Fatalf("naive inbound with only a disabled user should be skipped, got %#v", configs)
	}

	if err := db.Model(&client).Update("enable", true).Error; err != nil {
		t.Fatalf("enable client: %v", err)
	}
	configs, err = svc.GetAllConfig(db)
	if err != nil {
		t.Fatalf("get config with an enabled user: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("naive inbound with an enabled user should be included, got %#v", configs)
	}
	var config struct {
		Users []json.RawMessage `json:"users"`
	}
	if err := json.Unmarshal(configs[0], &config); err != nil {
		t.Fatalf("decode inbound config: %v", err)
	}
	if len(config.Users) != 1 {
		t.Fatalf("expected one enabled user in config, got %d", len(config.Users))
	}
}
