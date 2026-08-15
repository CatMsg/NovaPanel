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

func TestMigrateMieruHandshakeModeOnce(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "mieru-migration.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	inbound := model.Inbound{
		Type: "mieru", Tag: "mieru-main", Addrs: json.RawMessage("[]"), OutJson: json.RawMessage("{}"),
		Options: json.RawMessage(`{"listen_port":23456,"handshake_mode":"HANDSHAKE_NO_WAIT"}`),
	}
	if err := database.GetDB().Create(&inbound).Error; err != nil {
		t.Fatalf("create Mieru inbound: %v", err)
	}

	if err := migrateMieruHandshakeModeOnce(); err != nil {
		t.Fatalf("migrate Mieru handshake mode: %v", err)
	}
	var migrated model.Inbound
	if err := database.GetDB().First(&migrated, inbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	config, err := parseMieruInbound(&migrated)
	if err != nil {
		t.Fatal(err)
	}
	if config.HandshakeMode != "HANDSHAKE_STANDARD" {
		t.Fatalf("unexpected migrated handshake mode: %s", config.HandshakeMode)
	}

	var options map[string]interface{}
	if err := json.Unmarshal(migrated.Options, &options); err != nil {
		t.Fatal(err)
	}
	options["handshake_mode"] = "HANDSHAKE_NO_WAIT"
	payload, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(&model.Inbound{}).Where("id = ?", inbound.Id).Update("options", payload).Error; err != nil {
		t.Fatal(err)
	}
	if err := migrateMieruHandshakeModeOnce(); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}
	if err := database.GetDB().First(&migrated, inbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	config, err = parseMieruInbound(&migrated)
	if err != nil {
		t.Fatal(err)
	}
	if config.HandshakeMode != "HANDSHAKE_NO_WAIT" {
		t.Fatalf("migration overwrote an explicit later choice: %s", config.HandshakeMode)
	}
}
