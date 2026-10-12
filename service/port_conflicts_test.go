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
	if entries[0].Protocols != "udp" {
		t.Fatalf("unexpected protocols for Hysteria2 range: %q", entries[0].Protocols)
	}
	if err := validateManagedPortRangeConflicts(db, "入站", "overlap", 0, 0, []managedPortRange{{start: 49990, end: 50010}}); err == nil || !strings.Contains(err.Error(), "49990-49999") {
		t.Fatalf("expected compact overlap conflict, got %v", err)
	}
	if err := validateManagedPortRangeConflicts(db, "入站", "safe", 0, 0, []managedPortRange{{start: 50000, end: 50010}}); err != nil {
		t.Fatalf("unexpected non-overlapping conflict: %v", err)
	}
}

func TestManagedPortConflictsAllowSamePortAcrossTCPAndUDP(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "protocol-ports.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	inbound := model.Inbound{
		Type:    "hysteria2",
		Tag:     "hy2-udp",
		Options: json.RawMessage(`{"listen_port":443}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	if err := RebuildManagedPortEntries(); err != nil {
		t.Fatalf("rebuild managed port entries: %v", err)
	}

	if err := validateManagedPortProtocolConflicts(db, "入站", "mieru-tcp", 0, 0, []int{443}, []string{"tcp"}); err != nil {
		t.Fatalf("expected TCP and UDP to share the same numeric port: %v", err)
	}
	err := validateManagedPortProtocolConflicts(db, "入站", "mieru-udp", 0, 0, []int{443}, []string{"udp"})
	if err == nil || !strings.Contains(err.Error(), "443/udp") {
		t.Fatalf("expected UDP conflict with protocol in message, got %v", err)
	}
}

func TestHTTPInboundManagedPortProtocolsFollowServedVersions(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "http-version-ports.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	db := database.GetDB()
	inbounds := []model.Inbound{
		{Type: "http", Tag: "http3-only", Options: json.RawMessage(`{"listen_port":443,"version":3}`)},
		{Type: "http", Tag: "http-default", Options: json.RawMessage(`{"listen_port":444,"version":[]}`)},
		{Type: "http", Tag: "http-combined", Options: json.RawMessage(`{"listen_port":445,"version":[1,3]}`)},
	}
	if err := db.Create(&inbounds).Error; err != nil {
		t.Fatalf("create HTTP inbounds: %v", err)
	}
	if err := RebuildManagedPortEntries(); err != nil {
		t.Fatalf("rebuild managed ports: %v", err)
	}
	var entries []model.ManagedPortEntry
	if err := db.Find(&entries).Error; err != nil {
		t.Fatalf("query managed ports: %v", err)
	}
	protocolByTag := make(map[string]string, len(entries))
	for _, entry := range entries {
		protocolByTag[entry.OwnerTag] = entry.Protocols
	}
	want := map[string]string{"http3-only": "udp", "http-default": "tcp", "http-combined": "tcp,udp"}
	for tag, protocols := range want {
		if protocolByTag[tag] != protocols {
			t.Errorf("%s protocols = %q, want %q", tag, protocolByTag[tag], protocols)
		}
	}
	if err := validateManagedPortProtocolConflicts(db, "入站", "tcp-on-http3", 0, 0, []int{443}, []string{"tcp"}); err != nil {
		t.Fatalf("HTTP/3 UDP listener should permit TCP on the same numeric port: %v", err)
	}
	if err := validateManagedPortProtocolConflicts(db, "入站", "udp-on-http3", 0, 0, []int{443}, []string{"udp"}); err == nil {
		t.Fatal("HTTP/3 listener must reserve UDP on its port")
	}
}

func TestVLESSCanShareNumericPortWithHysteria2(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "vless-hy2-protocol-ports.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	db := database.GetDB()
	hy2 := model.Inbound{
		Type:    "hysteria2",
		Tag:     "hy2-udp",
		Options: json.RawMessage(`{"listen_port":443}`),
	}
	if err := db.Create(&hy2).Error; err != nil {
		t.Fatalf("create Hysteria2 inbound: %v", err)
	}
	if err := RebuildManagedPortEntries(); err != nil {
		t.Fatalf("rebuild managed port entries: %v", err)
	}

	vless := &model.Inbound{
		Type:    "vless",
		Tag:     "vless-tcp",
		Options: json.RawMessage(`{"listen_port":443}`),
	}
	spec, err := collectInboundForwardSpec(vless)
	if err != nil {
		t.Fatalf("collect VLESS forwarding spec: %v", err)
	}
	if len(spec.protocols) != 1 || spec.protocols[0] != "tcp" {
		t.Fatalf("expected VLESS to reserve TCP only, got %#v", spec.protocols)
	}
	if err := validateManagedPortRangeProtocolConflicts(db, "入站", vless.Tag, 0, 0, spec.portRanges, spec.protocols); err != nil {
		t.Fatalf("expected VLESS TCP and Hysteria2 UDP to share port 443: %v", err)
	}
}

func TestManagedPanelTCPPortCanShareUDPOnlyInboundPort(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	originalPorts := getSSHListenPorts()
	t.Cleanup(func() {
		_ = storeSSHListenPorts(originalPorts, nil)
	})
	if err := storeSSHListenPorts([]int{2222}, nil); err != nil {
		t.Fatalf("store ssh listen ports: %v", err)
	}

	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "panel-protocol-ports.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	db := database.GetDB()
	if err := db.Create(&model.Inbound{
		Type:    "hysteria2",
		Tag:     "hy2-panel-port",
		Options: json.RawMessage(`{"listen_port":2095}`),
	}).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}
	if err := RebuildManagedPortEntries(); err != nil {
		t.Fatalf("rebuild managed port entries: %v", err)
	}

	if err := ValidateManagedPanelPortsWithConflicts(db, 2095, 2096); err != nil {
		t.Fatalf("expected panel TCP port to share UDP-only inbound port: %v", err)
	}
}
