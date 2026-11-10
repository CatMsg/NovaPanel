package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestPrepareSnellInboundDefaultsAndValidatesManagedOptions(t *testing.T) {
	t.Run("generates a strong PSK and defaults to Mihomo-compatible encrypted mode", func(t *testing.T) {
		inbound := model.Inbound{Type: "snell", Tag: "snell-test", Options: json.RawMessage(`{"listen_port":24443}`)}
		if err := prepareSnellInbound(&inbound, nil); err != nil {
			t.Fatal(err)
		}
		var options map[string]any
		if err := json.Unmarshal(inbound.Options, &options); err != nil {
			t.Fatal(err)
		}
		if options["version"] != float64(6) || options["mode"] != snellV6ModeUnshaped {
			t.Fatalf("defaults = version %v, mode %v", options["version"], options["mode"])
		}
		psk, _ := options["psk"].(string)
		if len(psk) < 32 || len(psk) > 255 {
			t.Fatalf("generated PSK length = %d", len(psk))
		}
		if options["listen_port"] != float64(24443) {
			t.Fatal("preparation discarded generic listen options")
		}
	})

	t.Run("removes userkey authentication from Mihomo-compatible mode", func(t *testing.T) {
		inbound := model.Inbound{Type: "snell", Options: json.RawMessage(`{"version":6,"psk":"valid-shared-psk","users":[{"name":"alice","userkey":"user-secret"}]}`)}
		if err := prepareSnellInbound(&inbound, nil); err != nil {
			t.Fatal(err)
		}
		var options map[string]json.RawMessage
		if err := json.Unmarshal(inbound.Options, &options); err != nil {
			t.Fatal(err)
		}
		if _, exists := options["users"]; exists {
			t.Fatalf("per-user credentials survived Snell preparation: %s", inbound.Options)
		}
	})

	t.Run("preserves existing PSK when edit field is blank", func(t *testing.T) {
		previous := &model.Inbound{Type: "snell", Options: json.RawMessage(`{"version":6,"psk":"existing-shared-psk-123","mode":"unshaped"}`)}
		inbound := model.Inbound{Type: "snell", Options: json.RawMessage(`{"listen_port":24443,"version":6,"psk":"","mode":"unshaped"}`)}
		if err := prepareSnellInbound(&inbound, previous); err != nil {
			t.Fatal(err)
		}
		var options map[string]any
		_ = json.Unmarshal(inbound.Options, &options)
		if options["psk"] != "existing-shared-psk-123" {
			t.Fatalf("edit unexpectedly rotated PSK: %v", options["psk"])
		}
	})

	for _, test := range []struct {
		name    string
		options string
		tlsID   uint
		wantErr string
	}{
		{name: "v5", options: `{"version":5,"psk":"123456789012"}`, wantErr: "仅支持 Snell v6"},
		{name: "weak PSK", options: `{"version":6,"psk":"short"}`, wantErr: "12-255 字节"},
		{name: "unknown mode", options: `{"version":6,"psk":"valid-shared-psk","mode":"unknown"}`, wantErr: "不支持 Snell v6 流量模式"},
		{name: "TLS template", options: `{"version":6,"psk":"valid-shared-psk"}`, tlsID: 1, wantErr: "不使用 TLS 模板"},
	} {
		t.Run(test.name, func(t *testing.T) {
			inbound := model.Inbound{Type: "snell", TlsId: test.tlsID, Options: json.RawMessage(test.options)}
			err := prepareSnellInbound(&inbound, nil)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("prepare error = %v, want text %q", err, test.wantErr)
			}
		})
	}
	t.Run("normalizes legacy modes to encrypted Mihomo-compatible mode", func(t *testing.T) {
		inbound := model.Inbound{Type: "snell", Options: json.RawMessage(`{"version":6,"psk":"valid-shared-psk","mode":"unsafe-raw"}`)}
		if err := prepareSnellInbound(&inbound, nil); err != nil {
			t.Fatal(err)
		}
		var options map[string]any
		if err := json.Unmarshal(inbound.Options, &options); err != nil {
			t.Fatal(err)
		}
		if options["mode"] != snellV6ModeUnshaped {
			t.Fatalf("mode = %v, want %s", options["mode"], snellV6ModeUnshaped)
		}
	})
}

func TestUpdateClientsOnSnellAssignmentDoesNotCreatePerUserKeys(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "snell-clients.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	inbound := model.Inbound{Type: "snell", Tag: "server-snell", Options: json.RawMessage(`{"version":6,"psk":"server-only-secret"}`), Addrs: json.RawMessage(`[]`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Enable: true, Name: "alice", Config: json.RawMessage(`{"vless":{"uuid":"preserve"}}`), Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&ClientService{}).UpdateClientsOnInboundAdd(db, "1", inbound.Id, "snell.example"); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var config map[string]map[string]string
	if err := json.Unmarshal(client.Config, &config); err != nil {
		t.Fatal(err)
	}
	if _, exists := config["snell"]; exists || config["vless"]["uuid"] != "preserve" {
		t.Fatalf("shared-PSK Snell assignment config = %#v", config)
	}
	if strings.Contains(string(client.Config), "server-only-secret") {
		t.Fatal("server PSK leaked into the user's per-protocol client config")
	}
}

func TestAddSnellUsersFiltersDisabledClients(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "snell-users.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	inbound := model.Inbound{Type: "snell", Tag: "snell-users", Options: json.RawMessage(`{"version":6,"psk":"server-shared-psk"}`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	linkedIDs, err := json.Marshal([]uint{inbound.Id})
	if err != nil {
		t.Fatal(err)
	}
	for _, client := range []model.Client{
		{Enable: true, Name: "enabled", Config: json.RawMessage(`{"snell":{"name":"enabled","userkey":"enabled-user-key-123456"}}`), Inbounds: linkedIDs},
		{Enable: false, Name: "disabled", Config: json.RawMessage(`{"snell":{"name":"disabled","userkey":"disabled-user-key-123456"}}`), Inbounds: linkedIDs},
	} {
		if err := db.Create(&client).Error; err != nil {
			t.Fatal(err)
		}
	}
	got, err := (&InboundService{}).addUsers(db, []byte(`{"type":"snell","version":6,"users":[]}`), inbound.Id, "snell")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Users []map[string]string `json:"users"`
	}
	if err := json.Unmarshal(got, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Users) != 1 || config.Users[0]["name"] != "enabled" {
		t.Fatalf("enabled Snell assignment markers = %#v", config.Users)
	}
	filtered, err := filterInboundCoreConfig(&inbound, got)
	if err != nil {
		t.Fatal(err)
	}
	var coreConfig map[string]json.RawMessage
	if err := json.Unmarshal(filtered, &coreConfig); err != nil {
		t.Fatal(err)
	}
	if _, exists := coreConfig["users"]; exists {
		t.Fatalf("shared Snell core config unexpectedly contains user authentication: %s", filtered)
	}
}
