package service

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

func TestPreflightSaveRejectsInvalidJSON(t *testing.T) {
	report, err := (&ConfigService{}).PreflightSave("outbounds", "new", json.RawMessage(`{"tag":`), "", "localhost")
	if err == nil || report == nil || report.Valid {
		t.Fatalf("expected invalid JSON preflight failure, report=%+v err=%v", report, err)
	}
}

func TestPreflightRejectsDeletingReferencedOutbound(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "referenced-outbound.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatalf("initialize settings: %v", err)
	}
	db := database.GetDB()
	outbound := model.Outbound{Type: "direct", Tag: "proxy-a", Options: json.RawMessage(`{}`)}
	if err := db.Create(&outbound).Error; err != nil {
		t.Fatalf("create outbound: %v", err)
	}
	config := `{"log":{"level":"info"},"dns":{"servers":[],"rules":[]},"route":{"final":"proxy-a","rules":[]},"experimental":{}}`
	if err := (&SettingService{}).SetConfig(config); err != nil {
		t.Fatalf("set config: %v", err)
	}

	svc := NewConfigService(&core.Core{})
	report, err := svc.PreflightSave("outbounds", "del", json.RawMessage(`"proxy-a"`), "", "localhost")
	if err == nil || report == nil || report.Valid || !strings.Contains(err.Error(), "route.final") {
		t.Fatalf("expected referenced outbound rejection, report=%+v err=%v", report, err)
	}

	var count int64
	if err := db.Model(&model.Outbound{}).Where("tag = ?", "proxy-a").Count(&count).Error; err != nil {
		t.Fatalf("count outbound: %v", err)
	}
	if count != 1 {
		t.Fatalf("preflight delete leaked into database, count=%d", count)
	}
}

func TestPreflightSaveRollsBackDatabaseChanges(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "preflight.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	svc := NewConfigService(&core.Core{})
	report, err := svc.PreflightSave("outbounds", "new", json.RawMessage(`{
		"type":"direct",
		"tag":"preflight-only"
	}`), "", "localhost")
	if err != nil {
		t.Fatalf("preflight save: %v", err)
	}
	if !report.Valid || !report.Changed {
		t.Fatalf("unexpected report: %+v", report)
	}

	var count int64
	if err := database.GetDB().Model(&model.Outbound{}).Where("tag = ?", "preflight-only").Count(&count).Error; err != nil {
		t.Fatalf("count outbound: %v", err)
	}
	if count != 0 {
		t.Fatalf("preflight transaction leaked %d outbound rows", count)
	}
}

func TestPreflightRejectsDeletingReferencedDNSWithoutChangingDatabase(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "referenced-dns.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	settings := &SettingService{}
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatalf("initialize settings: %v", err)
	}
	before := `{"dns":{"servers":[{"type":"local","tag":"dns-main"}],"rules":[{"domain":"example.invalid","server":"dns-main"}]},"route":{"rules":[]}}`
	if err := settings.SetConfig(before); err != nil {
		t.Fatalf("set config: %v", err)
	}
	before, err := settings.GetConfig()
	if err != nil {
		t.Fatalf("get original config: %v", err)
	}
	invalid := json.RawMessage(`{"dns":{"servers":[],"rules":[{"domain":"example.invalid","server":"dns-main"}]},"route":{"rules":[]}}`)
	report, err := NewConfigService(&core.Core{}).PreflightSave("config", "set", invalid, "", "localhost")
	if err == nil || report == nil || report.Valid || !strings.Contains(err.Error(), "dns.rules[0].server") {
		t.Fatalf("expected DNS reference rejection, report=%+v err=%v", report, err)
	}
	after, err := settings.GetConfig()
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if after != before {
		t.Fatal("rejected DNS deletion changed persisted config")
	}
}

func TestSaveRejectsDNSRuleMapMissingReferenceWithoutChangingRows(t *testing.T) {
	db, svc, beforeConfig, beforeInbound := setupRejectedConfigSaveFixture(t)
	invalid := json.RawMessage(`{"dns":{"servers":[{"type":"local","tag":"dns-main"}],"rules":[{"dns_server_address":{"missing-dns":"192.0.2.1"}}]},"route":{"rules":[]}}`)

	if _, _, err := svc.Save("config", "set", invalid, "", "test", "localhost"); err == nil || !strings.Contains(err.Error(), `missing DNS server tag "missing-dns"`) {
		t.Fatalf("expected missing DNS server reference rejection, got %v", err)
	}
	assertRejectedConfigSaveRowsUnchanged(t, db, beforeConfig, beforeInbound)
}

func TestSaveRejectsHTTP3WithoutEnabledTLSWithoutChangingRows(t *testing.T) {
	db, svc, beforeConfig, beforeInbound := setupRejectedConfigSaveFixture(t)
	payload, err := json.Marshal(map[string]interface{}{
		"id":          beforeInbound.Id,
		"type":        "http",
		"tag":         beforeInbound.Tag,
		"tls_id":      beforeInbound.TlsId,
		"listen":      "127.0.0.1",
		"listen_port": 43871,
		"version":     3,
	})
	if err != nil {
		t.Fatalf("marshal HTTP/3 inbound payload: %v", err)
	}
	if _, _, err := svc.Save("inbounds", "edit", payload, "", "test", "localhost"); err == nil || !strings.Contains(err.Error(), "requires an enabled TLS server configuration") {
		t.Fatalf("expected HTTP/3 TLS rejection, got %v", err)
	}
	assertRejectedConfigSaveRowsUnchanged(t, db, beforeConfig, beforeInbound)
}

func TestSaveRejectsHTTPOutboundPathAndHostWithoutChangingRows(t *testing.T) {
	db, svc, beforeConfig, beforeInbound := setupRejectedConfigSaveFixture(t)
	invalid := json.RawMessage(`{"dns":{"servers":[{"type":"local","tag":"dns-main"}],"rules":[]},"outbounds":[{"type":"http","tag":"invalid-http","server":"proxy.example","server_port":8080,"path":"/proxy","headers":{"Host":["proxy.example"]}}],"route":{"rules":[]}}`)

	if _, _, err := svc.Save("config", "set", invalid, "", "test", "localhost"); err == nil || !strings.Contains(err.Error(), "path and Host header are not allowed at the same time") {
		t.Fatalf("expected HTTP path/Host config rejection, got %v", err)
	}
	assertRejectedConfigSaveRowsUnchanged(t, db, beforeConfig, beforeInbound)
}

func TestSaveRejectsH2CPanelInboundWithoutChangingRows(t *testing.T) {
	db, svc, beforeConfig, beforeInbound := setupRejectedConfigSaveFixture(t)
	payload, err := json.Marshal(map[string]interface{}{
		"id":          beforeInbound.Id,
		"type":        "http",
		"tag":         beforeInbound.Tag,
		"listen":      "127.0.0.1",
		"listen_port": 43871,
		"version":     2,
	})
	if err != nil {
		t.Fatalf("marshal H2C inbound payload: %v", err)
	}
	if _, _, err := svc.Save("inbounds", "edit", payload, "", "test", "localhost"); err == nil || !strings.Contains(err.Error(), "HTTP inbound version 2 requires an enabled TLS server configuration") {
		t.Fatalf("expected panel H2C inbound TLS rejection, got %v", err)
	}
	assertRejectedConfigSaveRowsUnchanged(t, db, beforeConfig, beforeInbound)
}

func setupRejectedConfigSaveFixture(t *testing.T) (*gorm.DB, *ConfigService, model.Setting, model.Inbound) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "rejected-save.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	previousCore := corePtr
	svc := NewConfigService(&core.Core{})
	t.Cleanup(func() { corePtr = previousCore })
	db := database.GetDB()
	if err := svc.SettingService.SetConfig(`{"dns":{"servers":[{"type":"local","tag":"dns-main"}],"rules":[]},"route":{"rules":[]}}`); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	tls := model.Tls{
		Name:   "disabled TLS",
		Server: json.RawMessage(`{"enabled":false}`),
		Client: json.RawMessage(`{"enabled":false}`),
	}
	if err := db.Create(&tls).Error; err != nil {
		t.Fatalf("seed TLS: %v", err)
	}
	inbound := model.Inbound{
		Type:    "http",
		Tag:     "existing-http",
		TlsId:   tls.Id,
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{"type":"http","version":1}`),
		Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":43871,"version":1}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	var config model.Setting
	if err := db.Where("key = ?", "config").First(&config).Error; err != nil {
		t.Fatalf("read seeded config: %v", err)
	}
	return db, svc, config, inbound
}

func assertRejectedConfigSaveRowsUnchanged(t *testing.T, db *gorm.DB, beforeConfig model.Setting, beforeInbound model.Inbound) {
	t.Helper()
	var afterConfig model.Setting
	if err := db.Where("key = ?", "config").First(&afterConfig).Error; err != nil {
		t.Fatalf("read config after rejected save: %v", err)
	}
	if afterConfig != beforeConfig {
		t.Fatalf("rejected save changed config row: before=%+v after=%+v", beforeConfig, afterConfig)
	}
	var afterInbound model.Inbound
	if err := db.First(&afterInbound, beforeInbound.Id).Error; err != nil {
		t.Fatalf("read inbound after rejected save: %v", err)
	}
	if !reflect.DeepEqual(afterInbound, beforeInbound) {
		t.Fatalf("rejected save changed inbound row: before=%+v after=%+v", beforeInbound, afterInbound)
	}
	var inboundCount int64
	if err := db.Model(&model.Inbound{}).Count(&inboundCount).Error; err != nil {
		t.Fatalf("count inbounds after rejected save: %v", err)
	}
	if inboundCount != 1 {
		t.Fatalf("rejected save changed inbound row count: got %d, want 1", inboundCount)
	}
}
