package migration

import (
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
