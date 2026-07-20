package service

import (
	"encoding/json"
	"path/filepath"
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
