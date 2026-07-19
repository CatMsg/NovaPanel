package database

import (
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestValidateAndSanitizeRestoreDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restore.db")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.AutoMigrate(
		&model.Setting{}, &model.Tls{}, &model.Inbound{}, &model.Outbound{},
		&model.Endpoint{}, &model.User{}, &model.Client{},
	); err != nil {
		t.Fatal(err)
	}
	settings := []model.Setting{
		{Key: "webCertFile", Value: "/missing/web.crt"},
		{Key: "webKeyFile", Value: "/missing/web.key"},
		{Key: "webDomain", Value: "example.com"},
		{Key: "subCertFile", Value: "/missing/sub.crt"},
		{Key: "subKeyFile", Value: "/missing/sub.key"},
		{Key: "subDomain", Value: "sub.example.com"},
		{Key: "fleetServers", Value: `[{"id":"tk","name":"tk"}]`},
	}
	if err := conn.Create(&settings).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := conn.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := validateAndSanitizeRestoreDB(path); err != nil {
		t.Fatalf("validate restore db: %v", err)
	}
	check, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var got []model.Setting
	if err := check.Find(&got).Error; err != nil {
		t.Fatal(err)
	}
	values := make(map[string]string, len(got))
	for _, setting := range got {
		values[setting.Key] = setting.Value
	}
	for _, key := range []string{"webCertFile", "webKeyFile", "webDomain", "subCertFile", "subKeyFile", "subDomain"} {
		if values[key] != "" {
			t.Fatalf("expected restored setting %s to be cleared, got %q", key, values[key])
		}
	}
	if values["fleetServers"] == "" {
		t.Fatal("fleetServers was not preserved")
	}
}

func TestValidateAndSanitizeRestoreDBRejectsInvalidFleetConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid-fleet.db")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.AutoMigrate(&model.Setting{}, &model.Tls{}, &model.Inbound{}, &model.Outbound{}, &model.Endpoint{}, &model.User{}, &model.Client{}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Create(&model.Setting{Key: "fleetServers", Value: "not-json"}).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := conn.DB()
	_ = sqlDB.Close()
	if err := validateAndSanitizeRestoreDB(path); err == nil {
		t.Fatal("expected invalid fleet config to be rejected")
	}
}
