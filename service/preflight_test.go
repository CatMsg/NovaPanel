package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
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
