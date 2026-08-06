package database

import (
	"os"
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

func TestRestoreFallbackDatabaseReopensLiveConnection(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "live.db")
	fallbackPath := dbPath + ".backup"
	if err := InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	if err := GetDB().Create(&model.Setting{Key: "restore-marker", Value: "fallback"}).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := GetDB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(dbPath, fallbackPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dbPath, []byte("failed replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := restoreFallbackDatabase(dbPath, fallbackPath); err != nil {
		t.Fatalf("restore fallback: %v", err)
	}
	var setting model.Setting
	if err := GetDB().Where("key = ?", "restore-marker").First(&setting).Error; err != nil {
		t.Fatalf("query reopened fallback: %v", err)
	}
	if setting.Value != "fallback" {
		t.Fatalf("marker = %q, want fallback", setting.Value)
	}
}

func TestCreateDBBackupStreamsSnapshotAndAppliesExclusions(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "source.db")
	if err := InitDB(dbPath); err != nil {
		t.Fatal(err)
	}
	if err := GetDB().Create(&model.Stats{DateTime: 1, Resource: "user", Tag: "alice", Traffic: 10}).Error; err != nil {
		t.Fatal(err)
	}
	if err := GetDB().Create(&model.Changes{DateTime: 1, Actor: "test", Key: "clients", Action: "add", Obj: []byte(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}

	backupPath, cleanup, err := CreateDBBackup("stats,changes")
	if err != nil {
		t.Fatalf("create backup: %v", err)
	}
	defer cleanup()
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup permissions = %o, want 600", info.Mode().Perm())
	}

	backup, err := gorm.Open(sqlite.Open(backupPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]interface{}{"stats": &model.Stats{}, "changes": &model.Changes{}} {
		var count int64
		if err := backup.Model(value).Count(&count).Error; err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		if count != 0 {
			t.Fatalf("backup contains %d %s rows, want 0", count, name)
		}
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
