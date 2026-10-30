package cmd

import (
	"testing"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/service"
)

func TestUpdateSettingRecoversLegacyInvalidRoutePrefixes(t *testing.T) {
	dbFolder := t.TempDir()
	t.Setenv("NOVAS_DB_FOLDER", dbFolder)
	t.Cleanup(func() { _ = database.CloseDB() })

	if err := database.InitDB(config.GetDBPath()); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	if err := database.GetDB().Table("settings").Where("key = ?", "webPath").Update("value", "/panel/*rest/").Error; err != nil {
		t.Fatalf("seed legacy invalid webPath: %v", err)
	}
	if err := database.GetDB().Table("settings").Where("key = ?", "subPath").Update("value", "/sub/:id/").Error; err != nil {
		t.Fatalf("seed legacy invalid subPath: %v", err)
	}
	if err := database.CloseDB(); err != nil {
		t.Fatalf("close database before offline recovery: %v", err)
	}

	if err := updateSetting(0, "/panel-v2/", 0, "/subscriptions/v2/", "", "", "", "", false, false); err != nil {
		t.Fatalf("recover route prefixes with setting command: %v", err)
	}

	settings := &service.SettingService{}
	if path, err := settings.GetWebPath(); err != nil || path != "/panel-v2/" {
		t.Fatalf("recovered webPath = %q, %v; want /panel-v2/", path, err)
	}
	if path, err := settings.GetSubPath(); err != nil || path != "/subscriptions/v2/" {
		t.Fatalf("recovered subPath = %q, %v; want /subscriptions/v2/", path, err)
	}
}
