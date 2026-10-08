package core

import (
	"encoding/json"
	"net/netip"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"

	"github.com/sagernet/sing-box/adapter"
	M "github.com/sagernet/sing/common/metadata"
	"gorm.io/gorm"
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

func TestHistoryTrackerResolvesMieruSourceIPsByUser(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "mieru-history-source.db")); err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable: true, Name: "alice", Config: json.RawMessage("{}"),
		Inbounds: json.RawMessage("[]"), Links: json.RawMessage("[]"), History: json.RawMessage("[]"),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	SetMieruBridgeInboundTag("mieru-in")
	SetMieruSourceIPsResolver(func(inboundTag, username string) []string {
		if inboundTag == "mieru-in" && username == "alice" {
			return []string{"2001:db8::8", "203.0.113.8"}
		}
		return nil
	})
	t.Cleanup(func() {
		SetMieruBridgeInboundTag("")
		SetMieruSourceIPsResolver(nil)
	})

	tracker := NewHistoryTracker()
	destination := M.SocksaddrFrom(netip.MustParseAddr("93.184.216.34"), 443)
	metadata := adapter.InboundContext{
		Inbound: "mieru-in", User: "alice", Domain: "example.com", Destination: destination,
		Source: M.SocksaddrFrom(netip.MustParseAddr("127.0.0.1"), 50001),
	}
	tracker.record(metadata, "direct", "tcp")

	var stored model.Client
	if err := database.GetDB().First(&stored, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var history []model.ClientHistoryEntry
	if err := json.Unmarshal(stored.History, &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 1 {
		t.Fatalf("history entries = %d, want 1", len(history))
	}
	entry := history[0]
	if entry.SourceIP != "" || entry.SourceIPScope != "user" || !reflect.DeepEqual(entry.SourceIPs, []string{"2001:db8::8", "203.0.113.8"}) {
		t.Fatalf("unexpected Mieru source history: %#v", entry)
	}
}

func TestAppendClientHistoryConcurrentAppendsAreNotLost(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "history-concurrent.db")); err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable: true, Name: "alice", Config: json.RawMessage("{}"),
		Inbounds: json.RawMessage("[]"), Links: json.RawMessage("[]"), History: json.RawMessage("[]"),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}

	db := database.GetDB()
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	// Allow two SQLite transactions to overlap so the barrier controls the read/write window.
	sqlDB.SetMaxOpenConns(4)
	const callbackName = "test:client_history_update_barrier"
	updateReady := make(chan struct{})
	allowUpdates := make(chan struct{})
	var releaseOnce atomic.Bool
	unblock := func() {
		if releaseOnce.CompareAndSwap(false, true) {
			close(allowUpdates)
		}
	}
	var waiting atomic.Int32
	if err := db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement.Table != "clients" {
			return
		}
		count := waiting.Add(1)
		if count > 2 {
			return
		}
		if count == 2 {
			close(updateReady)
		}
		<-allowUpdates
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		unblock()
		_ = db.Callback().Update().Remove(callbackName)
		sqlDB.SetMaxOpenConns(1)
	})

	entries := []model.ClientHistoryEntry{
		{
			DateTime: 1, Domain: "first.example", Destination: "192.0.2.1:443",
			SourceIP: "203.0.113.1", Inbound: "vless-in", Outbound: "direct", Network: "tcp", Protocol: "tls",
		},
		{
			DateTime: 2, Domain: "second.example", Destination: "192.0.2.2:443",
			SourceIP: "203.0.113.2", SourceIPs: []string{"203.0.113.2", "2001:db8::2"},
			SourceIPScope: "user", Inbound: "mieru-in", Outbound: "proxy", Network: "udp", Protocol: "quic",
		},
	}
	appendErrors := make(chan error, len(entries))
	for _, entry := range entries {
		go func(entry model.ClientHistoryEntry) {
			appendErrors <- appendClientHistory("alice", entry)
		}(entry)
	}
	select {
	case <-updateReady:
		unblock()
	case <-time.After(5 * time.Second):
		unblock()
		t.Fatal("both appends did not reach the controlled update barrier")
	}
	for range entries {
		if err := <-appendErrors; err != nil {
			t.Fatalf("append client history: %v", err)
		}
	}

	var stored model.Client
	if err := db.First(&stored, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var history []model.ClientHistoryEntry
	if err := json.Unmarshal(stored.History, &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != len(entries) {
		t.Fatalf("concurrent history entries = %d, want %d: %#v", len(history), len(entries), history)
	}
	byDomain := make(map[string]model.ClientHistoryEntry, len(history))
	for _, entry := range history {
		byDomain[entry.Domain] = entry
	}
	for _, want := range entries {
		if got, ok := byDomain[want.Domain]; !ok || !reflect.DeepEqual(got, want) {
			t.Errorf("history[%q] = %#v, want %#v", want.Domain, got, want)
		}
	}
}

func TestAppendClientHistoryRepairsMalformedJSONAndKeepsNewest200(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "history-repair.db")); err != nil {
		t.Fatal(err)
	}
	client := model.Client{
		Enable: true, Name: "alice", Config: json.RawMessage("{}"),
		Inbounds: json.RawMessage("[]"), Links: json.RawMessage("[]"), History: json.RawMessage("[]"),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Update("history", "not-json").Error; err != nil {
		t.Fatal(err)
	}
	newest := model.ClientHistoryEntry{
		DateTime: 201, Domain: "new.example", SourceIP: "203.0.113.201",
		SourceIPs: []string{"203.0.113.201", "2001:db8::201"}, SourceIPScope: "user",
	}
	if err := appendClientHistory("alice", newest); err != nil {
		t.Fatalf("append to malformed history: %v", err)
	}

	var stored model.Client
	if err := db.First(&stored, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var repaired []model.ClientHistoryEntry
	if err := json.Unmarshal(stored.History, &repaired); err != nil {
		t.Fatalf("history after malformed JSON repair is invalid: %v", err)
	}
	if len(repaired) != 1 || !reflect.DeepEqual(repaired[0], newest) {
		t.Fatalf("repaired history = %#v, want only %#v", repaired, newest)
	}

	oldHistory := make([]model.ClientHistoryEntry, maxClientHistoryEntries)
	for i := range oldHistory {
		oldHistory[i] = model.ClientHistoryEntry{DateTime: int64(maxClientHistoryEntries - i), Domain: "old.example"}
	}
	rawHistory, err := json.Marshal(oldHistory)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Update("history", rawHistory).Error; err != nil {
		t.Fatal(err)
	}
	newest.DateTime++
	if err := appendClientHistory("alice", newest); err != nil {
		t.Fatalf("append at history limit: %v", err)
	}
	if err := db.First(&stored, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	var history []model.ClientHistoryEntry
	if err := json.Unmarshal(stored.History, &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != maxClientHistoryEntries {
		t.Fatalf("history entries = %d, want %d", len(history), maxClientHistoryEntries)
	}
	if history[0].DateTime != 202 || history[maxClientHistoryEntries-1].DateTime != 2 {
		t.Fatalf("history retained wrong range: newest=%d oldest=%d", history[0].DateTime, history[len(history)-1].DateTime)
	}

	if err := appendClientHistory("missing", newest); !database.IsNotFound(err) {
		t.Fatalf("append for missing client error = %v, want record-not-found", err)
	}
}

func TestAppendClientHistoryUpdatesOnlyFirstDuplicateName(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "history-duplicate-name.db")); err != nil {
		t.Fatal(err)
	}
	clients := []model.Client{
		{
			Enable: true, Name: "alice", Config: json.RawMessage("{}"),
			Inbounds: json.RawMessage("[]"), Links: json.RawMessage("[]"),
			History: json.RawMessage(`[{"domain":"first-before.example"}]`),
		},
		{
			Enable: true, Name: "alice", Config: json.RawMessage("{}"),
			Inbounds: json.RawMessage("[]"), Links: json.RawMessage("[]"),
			History: json.RawMessage(`[{"domain":"second-before.example"}]`),
		},
	}
	if err := database.GetDB().Create(&clients).Error; err != nil {
		t.Fatal(err)
	}
	if clients[0].Id >= clients[1].Id {
		t.Fatalf("test clients were not inserted in increasing ID order: %d >= %d", clients[0].Id, clients[1].Id)
	}

	entry := model.ClientHistoryEntry{DateTime: 1, Domain: "new.example", SourceIP: "203.0.113.1"}
	if err := appendClientHistory("alice", entry); err != nil {
		t.Fatal(err)
	}

	var first, second model.Client
	if err := database.GetDB().First(&first, clients[0].Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().First(&second, clients[1].Id).Error; err != nil {
		t.Fatal(err)
	}
	var firstHistory, secondHistory []model.ClientHistoryEntry
	if err := json.Unmarshal(first.History, &firstHistory); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(second.History, &secondHistory); err != nil {
		t.Fatal(err)
	}
	if len(firstHistory) != 2 || firstHistory[0].Domain != entry.Domain || firstHistory[1].Domain != "first-before.example" {
		t.Fatalf("first duplicate history = %#v", firstHistory)
	}
	if len(secondHistory) != 1 || secondHistory[0].Domain != "second-before.example" {
		t.Fatalf("second duplicate history changed: %#v", secondHistory)
	}
}
