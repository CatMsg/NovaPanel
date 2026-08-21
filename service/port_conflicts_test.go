package service

import (
	"encoding/json"
	"path/filepath"
	"strings"
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

func TestManagedPortRangesStayCompressedAndDetectOverlap(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "range-ports.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	db := database.GetDB()
	inbound := model.Inbound{
		Type:    "hysteria2",
		Tag:     "hy2-large-range",
		Options: json.RawMessage(`{"listen_port":20000}`),
		OutJson: json.RawMessage(`{"server_ports":["20000-49999"]}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	if err := RebuildManagedPortEntries(); err != nil {
		t.Fatalf("rebuild managed port entries: %v", err)
	}

	var entries []model.ManagedPortEntry
	if err := db.Find(&entries).Error; err != nil {
		t.Fatalf("query managed ranges: %v", err)
	}
	if len(entries) != 1 || entries[0].Port != 20000 || entries[0].EndPort != 49999 {
		t.Fatalf("large range was expanded instead of compressed: %#v", entries)
	}
	if err := validateManagedPortRangeConflicts(db, "入站", "overlap", 0, 0, []managedPortRange{{start: 49990, end: 50010}}); err == nil || !strings.Contains(err.Error(), "49990-49999") {
		t.Fatalf("expected compact overlap conflict, got %v", err)
	}
	if err := validateManagedPortRangeConflicts(db, "入站", "safe", 0, 0, []managedPortRange{{start: 50000, end: 50010}}); err != nil {
		t.Fatalf("unexpected non-overlapping conflict: %v", err)
	}
}
