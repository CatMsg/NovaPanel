package database

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
)

func TestPruneInboundConflictsBySSHPorts(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	dir := t.TempDir()
	if err := InitDB(filepath.Join(dir, "restore.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	bad := model.Inbound{
		Type: "vless",
		Tag:  "ssh-conflict",
		Options: json.RawMessage(`{
			"listen_port": 22
		}`),
	}
	good := model.Inbound{
		Type: "vless",
		Tag:  "safe",
		Options: json.RawMessage(`{
			"listen_port": 8080
		}`),
	}
	if err := db.Create(&bad).Error; err != nil {
		t.Fatalf("create bad inbound: %v", err)
	}
	if err := db.Create(&good).Error; err != nil {
		t.Fatalf("create good inbound: %v", err)
	}

	client := model.Client{
		Enable: true,
		Name:   "demo",
		Inbounds: json.RawMessage(fmt.Sprintf(
			"[%d,%d]",
			bad.Id,
			good.Id,
		)),
		Links: json.RawMessage(fmt.Sprintf(`[
			{"remark":"%s"},
			{"remark":"%s"}
		]`, bad.Tag, good.Tag)),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	if err := pruneInboundConflictsBySSHPorts([]int{22}); err != nil {
		t.Fatalf("prune conflicted inbounds: %v", err)
	}

	var inbounds []model.Inbound
	if err := db.Find(&inbounds).Error; err != nil {
		t.Fatalf("load inbounds: %v", err)
	}
	if len(inbounds) != 1 || inbounds[0].Tag != good.Tag {
		t.Fatalf("unexpected inbounds after prune: %#v", inbounds)
	}

	var gotClient model.Client
	if err := db.First(&gotClient, client.Id).Error; err != nil {
		t.Fatalf("load client: %v", err)
	}

	var inboundIDs []uint
	if err := json.Unmarshal(gotClient.Inbounds, &inboundIDs); err != nil {
		t.Fatalf("decode client inbounds: %v", err)
	}
	if len(inboundIDs) != 1 || inboundIDs[0] != good.Id {
		t.Fatalf("unexpected client inbounds after prune: %v", inboundIDs)
	}

	var links []map[string]string
	if err := json.Unmarshal(gotClient.Links, &links); err != nil {
		t.Fatalf("decode client links: %v", err)
	}
	if len(links) != 1 || links[0]["remark"] != good.Tag {
		t.Fatalf("unexpected client links after prune: %#v", links)
	}
}

func TestPruneHy2InboundServerPortsConflictBySSHPorts(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	dir := t.TempDir()
	if err := InitDB(filepath.Join(dir, "restore-hy2.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	bad := model.Inbound{
		Type: "hysteria2",
		Tag:  "hy2-ssh-conflict",
		Options: json.RawMessage(`{
			"listen_port": 8443
		}`),
		OutJson: json.RawMessage(`{
			"server_ports": "20-22,9000"
		}`),
	}
	good := model.Inbound{
		Type: "hysteria2",
		Tag:  "hy2-safe",
		Options: json.RawMessage(`{
			"listen_port": 9443
		}`),
		OutJson: json.RawMessage(`{
			"server_ports": "9444-9446"
		}`),
	}
	if err := db.Create(&bad).Error; err != nil {
		t.Fatalf("create bad inbound: %v", err)
	}
	if err := db.Create(&good).Error; err != nil {
		t.Fatalf("create good inbound: %v", err)
	}

	client := model.Client{
		Enable: true,
		Name:   "hy2-demo",
		Inbounds: json.RawMessage(fmt.Sprintf(
			"[%d,%d]",
			bad.Id,
			good.Id,
		)),
		Links: json.RawMessage(fmt.Sprintf(`[
			{"remark":"%s"},
			{"remark":"%s"}
		]`, bad.Tag, good.Tag)),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}

	if err := pruneInboundConflictsBySSHPorts([]int{22}); err != nil {
		t.Fatalf("prune conflicted hy2 inbounds: %v", err)
	}

	var inbounds []model.Inbound
	if err := db.Find(&inbounds).Error; err != nil {
		t.Fatalf("load inbounds: %v", err)
	}
	if len(inbounds) != 1 || inbounds[0].Tag != good.Tag {
		t.Fatalf("unexpected hy2 inbounds after prune: %#v", inbounds)
	}

	var gotClient model.Client
	if err := db.First(&gotClient, client.Id).Error; err != nil {
		t.Fatalf("load client: %v", err)
	}

	var inboundIDs []uint
	if err := json.Unmarshal(gotClient.Inbounds, &inboundIDs); err != nil {
		t.Fatalf("decode client inbounds: %v", err)
	}
	if len(inboundIDs) != 1 || inboundIDs[0] != good.Id {
		t.Fatalf("unexpected client inbounds after hy2 prune: %v", inboundIDs)
	}
}

func TestPruneHy2InboundServerPortsConflictDeduplicatesMixedTokens(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	dir := t.TempDir()
	if err := InitDB(filepath.Join(dir, "restore-hy2-dedup.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	bad := model.Inbound{
		Type: "hysteria2",
		Tag:  "hy2-dup-conflict",
		Options: json.RawMessage(`{
			"listen_port": 2222
		}`),
		OutJson: json.RawMessage(`{
			"server_ports": "22,20-22,22,8443"
		}`),
	}
	good := model.Inbound{
		Type: "hysteria2",
		Tag:  "hy2-dup-safe",
		Options: json.RawMessage(`{
			"listen_port": 3333
		}`),
		OutJson: json.RawMessage(`{
			"server_ports": "3334,3335-3336"
		}`),
	}
	if err := db.Create(&bad).Error; err != nil {
		t.Fatalf("create bad inbound: %v", err)
	}
	if err := db.Create(&good).Error; err != nil {
		t.Fatalf("create good inbound: %v", err)
	}

	if err := pruneInboundConflictsBySSHPorts([]int{22}); err != nil {
		t.Fatalf("prune conflicted hy2 inbound with duplicate tokens: %v", err)
	}

	var inbounds []model.Inbound
	if err := db.Find(&inbounds).Error; err != nil {
		t.Fatalf("load inbounds: %v", err)
	}
	if len(inbounds) != 1 || inbounds[0].Tag != good.Tag {
		t.Fatalf("unexpected hy2 inbounds after duplicate-token prune: %#v", inbounds)
	}
}

func TestPruneInboundAndEndpointConflictsBySSHPortsMixedScenario(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	dir := t.TempDir()
	if err := InitDB(filepath.Join(dir, "restore-mixed.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	badInbound := model.Inbound{
		Type: "hysteria2",
		Tag:  "mixed-bad-inbound",
		Options: json.RawMessage(`{
			"listen_port": 8443
		}`),
		OutJson: json.RawMessage(`{
			"server_ports": "21-22"
		}`),
	}
	goodInbound := model.Inbound{
		Type: "vless",
		Tag:  "mixed-good-inbound",
		Options: json.RawMessage(`{
			"listen_port": 9443
		}`),
	}
	badEndpoint := model.Endpoint{
		Type: "masque",
		Tag:  "mixed-bad-endpoint",
		Options: json.RawMessage(`{
			"port": 22
		}`),
	}
	goodEndpoint := model.Endpoint{
		Type: "tailscale",
		Tag:  "mixed-good-endpoint",
		Options: json.RawMessage(`{
			"relay_server_port": 41641
		}`),
	}
	if err := db.Create(&badInbound).Error; err != nil {
		t.Fatalf("create bad inbound: %v", err)
	}
	if err := db.Create(&goodInbound).Error; err != nil {
		t.Fatalf("create good inbound: %v", err)
	}
	if err := db.Create(&badEndpoint).Error; err != nil {
		t.Fatalf("create bad endpoint: %v", err)
	}
	if err := db.Create(&goodEndpoint).Error; err != nil {
		t.Fatalf("create good endpoint: %v", err)
	}

	if err := pruneInboundConflictsBySSHPorts([]int{22}); err != nil {
		t.Fatalf("prune mixed inbound conflicts: %v", err)
	}
	if err := pruneEndpointConflictsBySSHPorts([]int{22}); err != nil {
		t.Fatalf("prune mixed endpoint conflicts: %v", err)
	}

	var inbounds []model.Inbound
	if err := db.Find(&inbounds).Error; err != nil {
		t.Fatalf("load inbounds: %v", err)
	}
	if len(inbounds) != 1 || inbounds[0].Tag != goodInbound.Tag {
		t.Fatalf("unexpected inbounds after mixed prune: %#v", inbounds)
	}

	var endpoints []model.Endpoint
	if err := db.Find(&endpoints).Error; err != nil {
		t.Fatalf("load endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Tag != goodEndpoint.Tag {
		t.Fatalf("unexpected endpoints after mixed prune: %#v", endpoints)
	}
}

func TestPruneEndpointConflictsBySSHPorts(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	dir := t.TempDir()
	if err := InitDB(filepath.Join(dir, "restore.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	bad := model.Endpoint{
		Type: "wireguard",
		Tag:  "ssh-conflict-endpoint",
		Options: json.RawMessage(`{
			"listen_port": 22
		}`),
	}
	good := model.Endpoint{
		Type: "wireguard",
		Tag:  "safe-endpoint",
		Options: json.RawMessage(`{
			"listen_port": 8080
		}`),
	}
	if err := db.Create(&bad).Error; err != nil {
		t.Fatalf("create bad endpoint: %v", err)
	}
	if err := db.Create(&good).Error; err != nil {
		t.Fatalf("create good endpoint: %v", err)
	}

	if err := pruneEndpointConflictsBySSHPorts([]int{22}); err != nil {
		t.Fatalf("prune conflicted endpoints: %v", err)
	}

	var endpoints []model.Endpoint
	if err := db.Find(&endpoints).Error; err != nil {
		t.Fatalf("load endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Tag != good.Tag {
		t.Fatalf("unexpected endpoints after prune: %#v", endpoints)
	}
}

func TestPruneMasqueEndpointConflictsBySSHPorts(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	dir := t.TempDir()
	if err := InitDB(filepath.Join(dir, "restore-masque.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	bad := model.Endpoint{
		Type: "masque",
		Tag:  "ssh-conflict-masque",
		Options: json.RawMessage(`{
			"port": 22
		}`),
	}
	good := model.Endpoint{
		Type: "masque",
		Tag:  "safe-masque",
		Options: json.RawMessage(`{
			"port": 9443
		}`),
	}
	if err := db.Create(&bad).Error; err != nil {
		t.Fatalf("create bad endpoint: %v", err)
	}
	if err := db.Create(&good).Error; err != nil {
		t.Fatalf("create good endpoint: %v", err)
	}

	if err := pruneEndpointConflictsBySSHPorts([]int{22}); err != nil {
		t.Fatalf("prune conflicted masque endpoints: %v", err)
	}

	var endpoints []model.Endpoint
	if err := db.Find(&endpoints).Error; err != nil {
		t.Fatalf("load endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Tag != good.Tag {
		t.Fatalf("unexpected masque endpoints after prune: %#v", endpoints)
	}
}

func TestPruneTailscaleEndpointConflictsBySSHPorts(t *testing.T) {
	logger.InitLogger(logging.ERROR)

	dir := t.TempDir()
	if err := InitDB(filepath.Join(dir, "restore-tailscale.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	bad := model.Endpoint{
		Type: "tailscale",
		Tag:  "ssh-conflict-tailscale",
		Options: json.RawMessage(`{
			"relay_server_port": 22
		}`),
	}
	good := model.Endpoint{
		Type: "tailscale",
		Tag:  "safe-tailscale",
		Options: json.RawMessage(`{
			"relay_server_port": 41641
		}`),
	}
	if err := db.Create(&bad).Error; err != nil {
		t.Fatalf("create bad endpoint: %v", err)
	}
	if err := db.Create(&good).Error; err != nil {
		t.Fatalf("create good endpoint: %v", err)
	}

	if err := pruneEndpointConflictsBySSHPorts([]int{22}); err != nil {
		t.Fatalf("prune conflicted tailscale endpoints: %v", err)
	}

	var endpoints []model.Endpoint
	if err := db.Find(&endpoints).Error; err != nil {
		t.Fatalf("load endpoints: %v", err)
	}
	if len(endpoints) != 1 || endpoints[0].Tag != good.Tag {
		t.Fatalf("unexpected tailscale endpoints after prune: %#v", endpoints)
	}
}
