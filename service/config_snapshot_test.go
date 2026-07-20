package service

import (
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

func TestConfigSnapshotRestoresBeforeImage(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "snapshot.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	db := database.GetDB()
	if err := db.Create(&model.Endpoint{Type: "wireguard", Tag: "before", Options: []byte(`{"listen_port":505}`)}).Error; err != nil {
		t.Fatalf("seed endpoint: %v", err)
	}

	var snapshot *configSnapshot
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		snapshot, err = captureConfigSnapshot(tx, false)
		return err
	}); err != nil {
		t.Fatalf("capture snapshot: %v", err)
	}
	if err := db.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.Endpoint{}).Error; err != nil {
		t.Fatalf("delete endpoint: %v", err)
	}
	if err := db.Create(&model.Endpoint{Type: "masque", Tag: "after", Options: []byte(`{"port":443}`)}).Error; err != nil {
		t.Fatalf("mutate endpoint: %v", err)
	}
	if err := db.Transaction(snapshot.restore); err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}

	var endpoints []model.Endpoint
	if err := db.Order("id").Find(&endpoints).Error; err != nil {
		t.Fatalf("load endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Tag != "before" {
		t.Fatalf("unexpected restored endpoints: %#v", endpoints)
	}
}
