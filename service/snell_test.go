package service

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestPrepareSnellInboundDefaultsAndValidatesManagedOptions(t *testing.T) {
	t.Run("generates a strong PSK and defaults to default mode", func(t *testing.T) {
		inbound := model.Inbound{Type: "snell", Tag: "snell-test", Options: json.RawMessage(`{"listen_port":24443}`)}
		if err := prepareSnellInbound(&inbound, nil); err != nil {
			t.Fatal(err)
		}
		var options map[string]any
		if err := json.Unmarshal(inbound.Options, &options); err != nil {
			t.Fatal(err)
		}
		if options["version"] != float64(6) || options["mode"] != snellV6ModeDefault {
			t.Fatalf("defaults = version %v, mode %v", options["version"], options["mode"])
		}
		if options["user_mode"] != snellUserModeShared {
			t.Fatalf("default user authentication mode = %v", options["user_mode"])
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

	t.Run("preserves per-user authentication when an older editor omits the field", func(t *testing.T) {
		previous := &model.Inbound{Type: "snell", Options: json.RawMessage(`{"version":6,"psk":"existing-shared-psk-123","user_mode":"per-user"}`)}
		inbound := model.Inbound{Type: "snell", Options: json.RawMessage(`{"version":6,"psk":"existing-shared-psk-123"}`)}
		if err := prepareSnellInbound(&inbound, previous); err != nil {
			t.Fatal(err)
		}
		var options map[string]any
		_ = json.Unmarshal(inbound.Options, &options)
		if options["user_mode"] != snellUserModePerUser {
			t.Fatalf("edit changed user authentication mode to %v", options["user_mode"])
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
	for _, mode := range []string{snellV6ModeDefault, snellV6ModeUnshaped, snellV6ModeUnsafeRaw} {
		t.Run("preserves selected mode "+mode, func(t *testing.T) {
			inbound := model.Inbound{Type: "snell", Options: json.RawMessage(`{"version":6,"psk":"valid-shared-psk","mode":"` + mode + `"}`)}
			if err := prepareSnellInbound(&inbound, nil); err != nil {
				t.Fatal(err)
			}
			var options map[string]any
			if err := json.Unmarshal(inbound.Options, &options); err != nil {
				t.Fatal(err)
			}
			if options["mode"] != mode {
				t.Fatalf("mode = %v, want %s", options["mode"], mode)
			}
		})
	}
	t.Run("rejects unknown user authentication mode", func(t *testing.T) {
		inbound := model.Inbound{Type: "snell", Options: json.RawMessage(`{"version":6,"psk":"valid-shared-psk","user_mode":"unexpected"}`)}
		if err := prepareSnellInbound(&inbound, nil); err == nil || !strings.Contains(err.Error(), "不支持 Snell 用户认证模式") {
			t.Fatalf("invalid user mode error = %v", err)
		}
	})
}

func TestGetAllDefaultsLegacySnellMode(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "snell-legacy-mode.db")); err != nil {
		t.Fatal(err)
	}
	inbound := model.Inbound{Type: "snell", Tag: "legacy-snell", Options: json.RawMessage(`{"version":6,"psk":"legacy-shared-psk"}`)}
	if err := database.GetDB().Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	legacyWithoutOptions := model.Inbound{Type: "snell", Tag: "legacy-snell-empty-options"}
	if err := database.GetDB().Create(&legacyWithoutOptions).Error; err != nil {
		t.Fatal(err)
	}
	inbounds, err := (&InboundService{}).GetAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(*inbounds) != 2 {
		t.Fatalf("legacy Snell inbound count = %d; want 2", len(*inbounds))
	}
	for _, got := range *inbounds {
		if got["mode"] != snellV6ModeDefault {
			t.Fatalf("legacy Snell inbound mode = %+v; want %q", *inbounds, snellV6ModeDefault)
		}
		if got["user_mode"] != snellUserModeShared {
			t.Fatalf("legacy Snell user mode = %+v; want %q", *inbounds, snellUserModeShared)
		}
	}
	byID, err := (&InboundService{}).Get(fmt.Sprint(inbound.Id))
	if err != nil {
		t.Fatal(err)
	}
	if len(*byID) != 1 || (*byID)[0]["mode"] != snellV6ModeDefault {
		t.Fatalf("legacy Snell inbound edit mode = %+v; want %q", *byID, snellV6ModeDefault)
	}
	byID, err = (&InboundService{}).Get(fmt.Sprint(legacyWithoutOptions.Id))
	if err != nil {
		t.Fatal(err)
	}
	if len(*byID) != 1 || (*byID)[0]["mode"] != snellV6ModeDefault {
		t.Fatalf("legacy Snell inbound without options edit mode = %+v; want %q", *byID, snellV6ModeDefault)
	}
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

func TestUpdateClientsOnPerUserSnellAssignmentCreatesUserKey(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "snell-per-user-assignment.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	inbound := model.Inbound{
		Type:    "snell",
		Tag:     "server-snell-per-user",
		Options: json.RawMessage(`{"version":6,"psk":"server-only-secret","user_mode":"per-user"}`),
		Addrs:   json.RawMessage(`[]`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable: true, Name: "alice", Config: json.RawMessage(`{"vless":{"uuid":"preserve"}}`),
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := (&ClientService{}).UpdateClientsOnInboundAdd(db, fmt.Sprint(client.Id), inbound.Id, "snell.example"); err != nil {
		t.Fatalf("assign local user to per-user Snell inbound: %v", err)
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var config map[string]map[string]string
	if err := json.Unmarshal(client.Config, &config); err != nil {
		t.Fatal(err)
	}
	if key := config["snell"]["userkey"]; key == "" || config["vless"]["uuid"] != "preserve" {
		t.Fatalf("per-user assignment config = %#v", config)
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
		{Enable: true, Name: "enabled", Config: json.RawMessage(`{"vless":{"uuid":"existing-client-config"}}`), Inbounds: linkedIDs},
		{Enable: false, Name: "disabled", Config: json.RawMessage(`{"vless":{"uuid":"disabled-client-config"}}`), Inbounds: linkedIDs},
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
	if len(config.Users) != 1 || config.Users[0]["name"] != "enabled" || len(config.Users[0]) != 1 {
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
	var runtimeMode string
	_ = json.Unmarshal(coreConfig["mode"], &runtimeMode)
	if runtimeMode != snellV6ModeDefault {
		t.Fatalf("legacy Snell runtime mode = %q, want %q", runtimeMode, snellV6ModeDefault)
	}

	for _, mode := range []string{snellV6ModeDefault, snellV6ModeUnshaped, snellV6ModeUnsafeRaw} {
		config := []byte(`{"type":"snell","version":6,"mode":"` + mode + `","users":[{"name":"enabled"}]}`)
		filtered, err := filterInboundCoreConfig(&inbound, config)
		if err != nil {
			t.Fatalf("filter runtime config in %s mode: %v", mode, err)
		}
		var selected map[string]json.RawMessage
		if err := json.Unmarshal(filtered, &selected); err != nil {
			t.Fatal(err)
		}
		var got string
		_ = json.Unmarshal(selected["mode"], &got)
		if got != mode {
			t.Fatalf("runtime mode = %q, want %q", got, mode)
		}
	}
}

func TestAddSnellPerUserAuthPersistsUniqueKeysAndKeepsEnabledUsers(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "snell-per-user.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	inbound := model.Inbound{Type: "snell", Tag: "snell-per-user", Options: json.RawMessage(`{"version":6,"psk":"server-shared-psk","user_mode":"per-user"}`)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	linkedIDs, _ := json.Marshal([]uint{inbound.Id})
	for _, client := range []model.Client{
		{Enable: true, Name: "alice", Config: json.RawMessage(`{"snell":{"userkey":"duplicate-key"}}`), Inbounds: linkedIDs},
		{Enable: true, Name: "bob", Config: json.RawMessage(`{"snell":{"userkey":"duplicate-key"}}`), Inbounds: linkedIDs},
		{Enable: false, Name: "disabled", Config: json.RawMessage(`{}`), Inbounds: linkedIDs},
	} {
		if err := db.Create(&client).Error; err != nil {
			t.Fatal(err)
		}
	}
	inboundJSON := []byte(`{"type":"snell","tag":"snell-per-user","version":6,"psk":"server-shared-psk","user_mode":"per-user"}`)
	configured, err := (&InboundService{}).addUsers(db, inboundJSON, inbound.Id, "snell")
	if err != nil {
		t.Fatalf("build per-user Snell config: %v", err)
	}
	var config struct {
		Users []struct {
			Name    string `json:"name"`
			UserKey string `json:"userkey"`
		} `json:"users"`
	}
	if err := json.Unmarshal(configured, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Users) != 2 || config.Users[0].Name != "alice" || config.Users[1].Name != "bob" {
		t.Fatalf("per-user Snell assignments = %+v", config.Users)
	}
	if config.Users[0].UserKey == "" || config.Users[1].UserKey == "" || config.Users[0].UserKey == config.Users[1].UserKey {
		t.Fatalf("Snell user keys must be populated and unique: %+v", config.Users)
	}
	filtered, err := filterInboundCoreConfig(&inbound, configured)
	if err != nil {
		t.Fatalf("filter per-user runtime config: %v", err)
	}
	var runtime map[string]json.RawMessage
	if err := json.Unmarshal(filtered, &runtime); err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime["users"]; !ok {
		t.Fatalf("per-user authentication missing from runtime config: %s", filtered)
	}
	if _, ok := runtime["user_mode"]; ok {
		t.Fatalf("panel-only user_mode leaked to sing-box runtime: %s", filtered)
	}
	var alice model.Client
	if err := db.Where("name = ?", "alice").First(&alice).Error; err != nil {
		t.Fatal(err)
	}
	var aliceConfig map[string]map[string]string
	if err := json.Unmarshal(alice.Config, &aliceConfig); err != nil {
		t.Fatal(err)
	}
	if aliceConfig["snell"]["userkey"] != config.Users[0].UserKey {
		t.Fatalf("user key was not persisted to the matching client: %s", alice.Config)
	}
}

func TestInitSnellUsersUsesSelectedClientWithoutSnellConfig(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "snell-init-users.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	client := model.Client{
		Enable: true,
		Name:   "alice",
		Config: json.RawMessage(`{"vless":{"uuid":"existing-client-config"}}`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	inboundJSON := []byte(`{"type":"snell","tag":"snell-create","version":6,"psk":"server-shared-psk"}`)
	initialized, err := (&InboundService{}).initUsers(db, inboundJSON, fmt.Sprintf("%d", client.Id), "snell")
	if err != nil {
		t.Fatalf("initialize selected Snell users without per-user config: %v", err)
	}
	var config struct {
		Users []map[string]string `json:"users"`
	}
	if err := json.Unmarshal(initialized, &config); err != nil {
		t.Fatal(err)
	}
	if len(config.Users) != 1 || config.Users[0]["name"] != "alice" || len(config.Users[0]) != 1 {
		t.Fatalf("selected Snell user marker = %#v", config.Users)
	}

	runtimeConfig, err := filterInboundCoreConfig(&model.Inbound{Type: "snell", Tag: "snell-create"}, initialized)
	if err != nil {
		t.Fatalf("filter Snell runtime config: %v", err)
	}
	if len(runtimeConfig) == 0 || strings.Contains(string(runtimeConfig), `"users"`) {
		t.Fatalf("expected a listener config without per-user auth fields, got %s", runtimeConfig)
	}
}
