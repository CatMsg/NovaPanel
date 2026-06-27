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
