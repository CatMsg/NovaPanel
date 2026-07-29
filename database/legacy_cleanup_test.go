package database

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestInitDBRemovesLegacyMieruEndpoints(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legacy-mieru.db")
	legacyDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}
	if err := legacyDB.AutoMigrate(&model.Endpoint{}, &model.ManagedPortEntry{}); err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}

	legacyMieru := model.Endpoint{
		Type:    " MIERU ",
		Tag:     "legacy-mieru",
		Options: json.RawMessage(`{"listen_port":22000}`),
	}
	wireGuard := model.Endpoint{
		Type:    "wireguard",
		Tag:     "wireguard-main",
		Options: json.RawMessage(`{"listen_port":22001}`),
	}
	if err := legacyDB.Create(&legacyMieru).Error; err != nil {
		t.Fatalf("create legacy Mieru endpoint: %v", err)
	}
	if err := legacyDB.Create(&wireGuard).Error; err != nil {
		t.Fatalf("create WireGuard endpoint: %v", err)
	}
	entries := []model.ManagedPortEntry{
		{Scope: "endpoint", OwnerId: legacyMieru.Id, OwnerTag: legacyMieru.Tag, Port: 22000},
		{Scope: "endpoint", OwnerId: wireGuard.Id, OwnerTag: wireGuard.Tag, Port: 22001},
	}
	if err := legacyDB.Create(&entries).Error; err != nil {
		t.Fatalf("create managed port entries: %v", err)
	}
	sqlDB, err := legacyDB.DB()
	if err != nil {
		t.Fatalf("open legacy SQL database: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close legacy database: %v", err)
	}

	if err := InitDB(dbPath); err != nil {
		t.Fatalf("initialize migrated database: %v", err)
	}

	var endpoints []model.Endpoint
	if err := GetDB().Order("id ASC").Find(&endpoints).Error; err != nil {
		t.Fatalf("load endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Tag != wireGuard.Tag {
		t.Fatalf("unexpected endpoints after cleanup: %#v", endpoints)
	}

	var managedPorts []model.ManagedPortEntry
	if err := GetDB().Order("id ASC").Find(&managedPorts).Error; err != nil {
		t.Fatalf("load managed ports: %v", err)
	}
	if len(managedPorts) != 1 ||
		managedPorts[0].OwnerId != wireGuard.Id ||
		managedPorts[0].Port != 22001 {
		t.Fatalf("unexpected managed ports after cleanup: %#v", managedPorts)
	}
}
