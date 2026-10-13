package database

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestBackupStripsRemoteSecretsAndRehearsalIsReadOnly(t *testing.T) {
	if err := InitDB(filepath.Join(t.TempDir(), "snapshot-source.db")); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{
		"remoteBackupConfig": `{"password":"synthetic-never-export"}`, "remoteBackupOwner": "synthetic-owner", "remoteBackupState": "synthetic-state",
		"webCertFile": "", "webKeyFile": "", "webDomain": "", "subCertFile": "", "subKeyFile": "", "subDomain": "", "maintenanceMode": "false", "test-marker": "preserve",
	} {
		if err := GetDB().Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
			t.Fatal(err)
		}
	}
	path, cleanup, err := CreateDBBackup("")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	info, err := os.Stat(filepath.Dir(path))
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatal("snapshot directory not private")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(before, []byte("synthetic-never-export")) || bytes.Contains(before, []byte("synthetic-owner")) {
		t.Fatal("snapshot contains removed secret or ownership bytes")
	}
	backupDB, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var backupSettings []model.Setting
	if err := backupDB.Where("key LIKE ?", "remoteBackup%").Find(&backupSettings).Error; err != nil {
		t.Fatal(err)
	}
	backupSQL, _ := backupDB.DB()
	_ = backupSQL.Close()
	if len(backupSettings) != 3 {
		t.Fatalf("snapshot contains %d remote backup setting keys, want 3", len(backupSettings))
	}
	for _, setting := range backupSettings {
		if setting.Value != "" {
			t.Fatalf("snapshot retained sensitive setting %s", setting.Key)
		}
	}
	counts, err := RehearseDB(context.Background(), path)
	if err != nil || counts["users"] != 1 {
		t.Fatalf("rehearsal: %v %v", counts, err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("rehearsal changed snapshot bytes")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(path + suffix); !os.IsNotExist(err) {
			t.Fatal("rehearsal created sidecar")
		}
	}
	var value model.Setting
	if err := GetDB().Where("key = ?", "remoteBackupConfig").First(&value).Error; err != nil || !stringsContainsSynthetic(value.Value) {
		t.Fatal("snapshot removed live credential")
	}
}

func stringsContainsSynthetic(value string) bool {
	return bytes.Contains([]byte(value), []byte("synthetic-never-export"))
}

func TestPreserveRestoreMaintenanceBeforeSwap(t *testing.T) {
	for _, value := range []string{"false", "true"} {
		path := filepath.Join(t.TempDir(), "replacement.db")
		conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
		if err != nil {
			t.Fatal(err)
		}
		if err := conn.Migrator().CreateTable(&model.Setting{}); err != nil {
			t.Fatal(err)
		}
		if err := conn.Create(&model.Setting{Key: "maintenanceMode", Value: value}).Error; err != nil {
			t.Fatal(err)
		}
		if err := preserveRestoreMaintenance(conn); err != nil {
			t.Fatal(err)
		}
		var rows []model.Setting
		if err := conn.Where("key = ?", "maintenanceMode").Find(&rows).Error; err != nil || len(rows) != 1 || rows[0].Value != "true" {
			t.Fatal("replacement pause not preserved")
		}
		sqlDB, _ := conn.DB()
		_ = sqlDB.Close()
	}
	path := filepath.Join(t.TempDir(), "missing-maintenance.db")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Migrator().CreateTable(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := preserveRestoreMaintenance(conn); err == nil || !strings.Contains(err.Error(), "missing maintenanceMode") {
		t.Fatalf("missing current setting should fail rather than be created, got %v", err)
	}
	sqlDB, _ := conn.DB()
	_ = sqlDB.Close()
}

func TestValidateAndSanitizeRestoreDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "restore.db")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	createCompleteTestSchema(t, conn)
	settings := []model.Setting{
		{Key: "maintenanceMode", Value: "false"},
		{Key: "remoteBackupConfig", Value: ""},
		{Key: "remoteBackupOwner", Value: ""},
		{Key: "remoteBackupState", Value: ""},
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
	if err := GetDB().Create(&model.TrafficBudgetSample{DateTime: 1, PeriodStart: 1, UsedBytes: 10}).Error; err != nil {
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
	for name, value := range map[string]interface{}{
		"stats": &model.Stats{}, "traffic_budget_samples": &model.TrafficBudgetSample{}, "changes": &model.Changes{},
	} {
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
	createCompleteTestSchema(t, conn)
	if err := conn.Create(&model.Setting{Key: "fleetServers", Value: "not-json"}).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := conn.DB()
	_ = sqlDB.Close()
	if err := validateAndSanitizeRestoreDB(path); err == nil {
		t.Fatal("expected invalid fleet config to be rejected")
	}
}
