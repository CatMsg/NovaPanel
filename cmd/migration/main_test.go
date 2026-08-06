package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateDBHandlesShortVersionWithoutPanic(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "migration.db")
	conn, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.AutoMigrate(&model.Setting{}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Create(&model.Setting{Key: "version", Value: "1"}).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := migrateDB(dbPath); err != nil {
		t.Fatalf("migrate short version: %v", err)
	}

	check, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var version string
	if err := check.Model(&model.Setting{}).Select("value").Where("key = ?", "version").Scan(&version).Error; err != nil {
		t.Fatal(err)
	}
	if version != config.GetVersion() {
		t.Fatalf("version = %q, want %q", version, config.GetVersion())
	}
}

func TestMoveJSONToDBRejectsMalformedLegacyConfigBeforeDroppingTables(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "config.json"), []byte(`{"outbounds":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	oldExecutable := os.Args[0]
	os.Args[0] = filepath.Join(dir, "sui")
	t.Cleanup(func() { os.Args[0] = oldExecutable })
	t.Setenv("SUI_BIN_FOLDER", "bin")

	dbPath := filepath.Join(dir, "legacy.db")
	conn, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.AutoMigrate(&model.Inbound{}); err != nil {
		t.Fatal(err)
	}
	if err := conn.Create(&model.Inbound{Type: "direct", Tag: "keep-me", Options: []byte(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}

	if err := moveJsonToDb(conn); err == nil {
		t.Fatal("expected malformed legacy config to fail")
	}
	var count int64
	if err := conn.Model(&model.Inbound{}).Where("tag = ?", "keep-me").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("legacy inbound table was modified before validation")
	}
}

func TestMigrateDBReturnsSchemaError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "invalid.db")
	conn, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := migrateDB(dbPath); err == nil {
		t.Fatal("expected missing settings table to return an error")
	}
}
