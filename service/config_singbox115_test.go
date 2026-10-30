package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestLegacyTLSACMEMigrationParsesWithPinnedCore(t *testing.T) {
	for name, domains := range map[string]json.RawMessage{
		"domain list":   json.RawMessage(`["edge.example","alt.example"]`),
		"single domain": json.RawMessage(`"edge.example"`),
	} {
		t.Run(name, func(t *testing.T) {
			legacyACME := map[string]json.RawMessage{
				"domain":                     domains,
				"data_directory":             json.RawMessage(`"/var/lib/novas/acme"`),
				"default_server_name":        json.RawMessage(`"edge.example"`),
				"email":                      json.RawMessage(`"ops@example.com"`),
				"provider":                   json.RawMessage(`"letsencrypt"`),
				"account_key":                json.RawMessage(`"account-key"`),
				"key_type":                   json.RawMessage(`"p256"`),
				"profile":                    json.RawMessage(`"shortlived"`),
				"external_account":           json.RawMessage(`{"key_id":"account-id","mac_key":"account-mac"}`),
				"dns01_challenge":            json.RawMessage(`{"provider":"cloudflare","api_token":"dns-token"}`),
				"disable_http_challenge":     json.RawMessage(`true`),
				"alternative_tls_port":       json.RawMessage(`8443`),
				"disable_tls_alpn_challenge": json.RawMessage(`false`),
			}
			tlsServer, err := json.Marshal(map[string]interface{}{
				"enabled":     true,
				"server_name": "edge.example",
				"acme":        legacyACME,
			})
			if err != nil {
				t.Fatalf("marshal legacy TLS profile: %v", err)
			}
			inbound := model.Inbound{
				Type:    "vless",
				Tag:     "legacy-acme",
				Tls:     &model.Tls{Server: tlsServer},
				Options: json.RawMessage(`{"listen":"::","listen_port":443,"users":[{"name":"tester","uuid":"11111111-1111-4111-8111-111111111111"}]}`),
			}
			inboundJSON, err := json.Marshal(inbound)
			if err != nil {
				t.Fatalf("marshal legacy inbound: %v", err)
			}
			raw, err := json.Marshal(map[string]json.RawMessage{
				"inbounds":  json.RawMessage("[" + string(inboundJSON) + "]"),
				"outbounds": json.RawMessage(`[{"type":"direct","tag":"direct"}]`),
				"route":     json.RawMessage(`{"final":"direct"}`),
			})
			if err != nil {
				t.Fatalf("marshal sing-box config: %v", err)
			}
			validated, err := ValidateSingBoxConfig(raw)
			if err != nil {
				t.Fatalf("validate migrated sing-box config: %v", err)
			}
			if err := core.NewCore().ValidateConfig(validated); err != nil {
				t.Fatalf("pinned sing-box parser rejected migrated legacy ACME config: %v\n%s", err, validated)
			}
		})
	}
}

func TestRawSingBoxConfigMigratesInlineACME(t *testing.T) {
	raw := []byte(`{
		"inbounds":[{
			"type":"vless",
			"tag":"legacy-acme-raw",
			"listen":"::",
			"listen_port":443,
			"users":[{"name":"tester","uuid":"11111111-1111-4111-8111-111111111111"}],
			"tls":{"enabled":true,"server_name":"edge.example","acme":{"domain":"edge.example","email":"ops@example.com","provider":"letsencrypt"}}
		}],
		"outbounds":[{"type":"direct","tag":"direct"}],
		"route":{"final":"direct"}
	}`)

	validated, err := ValidateSingBoxConfig(raw)
	if err != nil {
		t.Fatalf("validate raw legacy ACME config: %v", err)
	}
	var config struct {
		Inbounds []struct {
			TLS map[string]json.RawMessage `json:"tls"`
		} `json:"inbounds"`
	}
	if err := json.Unmarshal(validated, &config); err != nil {
		t.Fatalf("decode migrated config: %v", err)
	}
	if len(config.Inbounds) != 1 {
		t.Fatalf("unexpected inbound count: %d", len(config.Inbounds))
	}
	if _, exists := config.Inbounds[0].TLS["acme"]; exists {
		t.Fatal("raw config retained deprecated tls.acme")
	}
	var provider map[string]json.RawMessage
	if err := json.Unmarshal(config.Inbounds[0].TLS["certificate_provider"], &provider); err != nil {
		t.Fatalf("decode migrated certificate provider: %v", err)
	}
	if string(provider["type"]) != `"acme"` || string(provider["domain"]) != `"edge.example"` || string(provider["email"]) != `"ops@example.com"` {
		t.Fatalf("legacy ACME fields were not preserved: %#v", provider)
	}
	if err := core.NewCore().ValidateConfig(validated); err != nil {
		t.Fatalf("pinned sing-box parser rejected migrated raw config: %v\n%s", err, validated)
	}
}

func TestConfigServiceBuildsLegacyTLSFromDatabaseWithPinnedCore(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "singbox-legacy-tls.db")); err != nil {
		t.Fatalf("initialize test database: %v", err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatalf("load seeded settings: %v", err)
	}
	db := database.GetDB()

	storedTLS := json.RawMessage(`{"enabled":true,"server_name":"edge.example","acme":{"domain":"edge.example","email":"ops@example.com","provider":"letsencrypt"}}`)
	tlsProfile := model.Tls{Name: "legacy-acme", Server: storedTLS, Client: json.RawMessage(`{}`)}
	if err := db.Create(&tlsProfile).Error; err != nil {
		t.Fatalf("save TLS profile: %v", err)
	}
	inbound := model.Inbound{
		Type:    "vless",
		Tag:     "legacy-acme-in",
		TlsId:   tlsProfile.Id,
		Options: json.RawMessage(`{"listen":"::","listen_port":24443}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("save inbound: %v", err)
	}
	inboundIDs, err := json.Marshal([]uint{inbound.Id})
	if err != nil {
		t.Fatalf("marshal client inbound ids: %v", err)
	}
	client := model.Client{
		Enable:   true,
		Name:     "legacy-acme-client",
		Config:   json.RawMessage(`{"vless":{"name":"tester","uuid":"11111111-1111-4111-8111-111111111111"}}`),
		Inbounds: inboundIDs,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("save client: %v", err)
	}

	runtimeConfig, err := (&ConfigService{}).getConfig(db, "")
	if err != nil {
		t.Fatalf("build runtime config from database: %v", err)
	}
	if err := core.NewCore().ValidateConfig(*runtimeConfig); err != nil {
		t.Fatalf("pinned sing-box parser rejected database-generated config: %v\n%s", err, *runtimeConfig)
	}
	var persisted model.Tls
	if err := db.First(&persisted, tlsProfile.Id).Error; err != nil {
		t.Fatalf("reload legacy TLS profile: %v", err)
	}
	if string(persisted.Server) != string(storedTLS) {
		t.Fatalf("runtime generation rewrote stored legacy TLS JSON: %s", persisted.Server)
	}
}

func TestSingBoxConfigPreservesExtendedTopLevelOptions(t *testing.T) {
	raw := []byte(`{
		"certificate":{"store":"system"},
		"certificate_providers":[{"type":"origin-ca","tag":"origin"}],
		"http_clients":[{"tag":"rules"}],
		"network_namespaces":[{"tag":"isolated","path":"/run/netns/isolated"}]
	}`)
	var config SingBoxConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	var result map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	for _, key := range []string{"certificate", "certificate_providers", "http_clients", "network_namespaces"} {
		if len(result[key]) == 0 {
			t.Fatalf("top-level option %q was removed", key)
		}
	}
}

func TestValidateSingBoxConfigPreservesTunStackAndEndpointOptions(t *testing.T) {
	raw := []byte(`{
		"inbounds":[{
			"type":"tun",
			"tag":"tun-in",
			"address":["172.19.0.1/30"],
			"stack":"system"
		}],
		"endpoints":[{
			"type":"wireguard",
			"tag":"wg-endpoint",
			"on_demand":true
		}],
		"route":{}
	}`)

	validated, err := ValidateSingBoxConfig(raw)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(validated, &result); err != nil {
		t.Fatalf("decode validated config: %v", err)
	}
	inbounds := objectList(result["inbounds"])
	if len(inbounds) != 1 {
		t.Fatalf("unexpected inbounds: %#v", result["inbounds"])
	}
	if inbounds[0]["stack"] != "system" {
		t.Fatalf("validation rewrote the TUN stack: %#v", inbounds[0])
	}
	endpoints := objectList(result["endpoints"])
	if len(endpoints) != 1 || !boolValue(endpoints[0]["on_demand"]) {
		t.Fatalf("1.15 endpoint options were not preserved: %#v", result["endpoints"])
	}
}
