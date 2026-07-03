package service

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
)

func TestValidateManagedPortConflicts(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	originalPorts := getSSHListenPorts()
	t.Cleanup(func() {
		_ = storeSSHListenPorts(originalPorts, nil)
	})
	if err := storeSSHListenPorts([]int{2222}, nil); err != nil {
		t.Fatalf("store ssh listen ports: %v", err)
	}

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "ports.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	inbound := model.Inbound{
		Type: "vless",
		Tag:  "inbound-a",
		Options: json.RawMessage(`{
			"listen_port": 3000
		}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	endpoint := model.Endpoint{
		Type: "wireguard",
		Tag:  "endpoint-a",
		Options: json.RawMessage(`{
			"listen_port": 4000
		}`),
	}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatalf("create endpoint: %v", err)
	}
	if err := RebuildManagedPortEntries(); err != nil {
		t.Fatalf("rebuild managed port entries: %v", err)
	}

	if err := validateManagedPortConflicts(db, "入站", "inbound-b", 0, 0, []int{4000}); err == nil {
		t.Fatal("expected inbound port conflict with endpoint")
	}

	if err := validateManagedPortConflicts(db, "节点", "endpoint-b", 0, 0, []int{3000}); err == nil {
		t.Fatal("expected endpoint port conflict with inbound")
	}

	if err := validateManagedPortConflicts(db, "节点", "endpoint-a", 0, endpoint.Id, []int{4000}); err != nil {
		t.Fatalf("expected self port to be ignored on edit: %v", err)
	}
}

func TestValidateManagedPanelPortConflicts(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	originalPorts := getSSHListenPorts()
	t.Cleanup(func() {
		_ = storeSSHListenPorts(originalPorts, nil)
	})
	if err := storeSSHListenPorts([]int{2222}, nil); err != nil {
		t.Fatalf("store ssh listen ports: %v", err)
	}

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "panel-ports.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	inbound := model.Inbound{
		Type: "vless",
		Tag:  "inbound-panel-conflict",
		Options: json.RawMessage(`{
			"listen_port": 2095
		}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	if err := RebuildManagedPortEntries(); err != nil {
		t.Fatalf("rebuild managed port entries: %v", err)
	}

	if err := ValidateManagedPanelPortsWithConflicts(db, 2095, 2096); err == nil {
		t.Fatal("expected panel port conflict with inbound")
	}

	if err := ValidateManagedPanelPortsWithConflicts(db, 3000, 3001); err != nil {
		t.Fatalf("expected non-conflicting panel ports to pass: %v", err)
	}
}
