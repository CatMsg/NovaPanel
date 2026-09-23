package core

import (
	"encoding/json"
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
)

func TestHistoryTrackerStoresAndSeparatesSourceIP(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "history-source.db")); err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable:   true,
		Name:     "alice",
		Config:   json.RawMessage("{}"),
		Inbounds: json.RawMessage("[]"),
		Links:    json.RawMessage("[]"),
		History:  json.RawMessage("[]"),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	tracker := NewHistoryTracker()
	destination := M.SocksaddrFrom(netip.MustParseAddr("93.184.216.34"), 443)
	base := adapter.InboundContext{
		Inbound:     "vless-in",
		User:        "alice",
		Domain:      "example.com",
		Destination: destination,
		Protocol:    "tls",
	}
	first := base
	first.Source = M.SocksaddrFrom(netip.MustParseAddr("203.0.113.10"), 50001)
	second := base
	second.Source = M.SocksaddrFrom(netip.MustParseAddr("203.0.113.11"), 50002)

	tracker.record(first, "direct", "tcp")
	tracker.record(second, "direct", "tcp")

	var stored model.Client
	if err := database.GetDB().First(&stored, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var history []model.ClientHistoryEntry
	if err := json.Unmarshal(stored.History, &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 {
		t.Fatalf("history entries = %d, want 2", len(history))
	}
	if history[0].SourceIP != "203.0.113.11" || history[1].SourceIP != "203.0.113.10" {
		t.Fatalf("unexpected source IP history: %#v", history)
	}
}
