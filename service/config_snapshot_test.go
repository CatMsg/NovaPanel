package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

func newConfigSnapshotTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "snapshot.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	return database.GetDB()
}

func takeConfigSnapshot(t *testing.T, db *gorm.DB, includeStats bool) *configSnapshot {
	t.Helper()
	var snapshot *configSnapshot
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		snapshot, err = captureConfigSnapshot(tx, includeStats)
		return err
	}); err != nil {
		t.Fatalf("capture snapshot: %v", err)
	}
	return snapshot
}

func captureSnapshotAfterImage(t *testing.T, db *gorm.DB, snapshot *configSnapshot) {
	t.Helper()
	if err := db.Transaction(snapshot.captureAfterImage); err != nil {
		t.Fatalf("capture after-image: %v", err)
	}
}

func TestConfigSnapshotPreservesPostCommitClientOperations(t *testing.T) {
	db := newConfigSnapshotTestDB(t)
	client := model.Client{Name: "before", Up: 100, Down: 200, TotalUp: 300, TotalDown: 400,
		History: json.RawMessage(`[{"dateTime":1,"domain":"old.example"}]`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := takeConfigSnapshot(t, db, false)
	// A committed configuration edit is followed by the same SQL counter update
	// used by SaveStats and history replacement used by the history tracker.
	if err := db.Model(&client).Update("name", "after").Error; err != nil {
		t.Fatal(err)
	}
	captureSnapshotAfterImage(t, db, snapshot)
	if err := db.Model(&client).Updates(map[string]interface{}{
		"up": gorm.Expr("up + ?", 7), "down": gorm.Expr("down + ?", 11),
		"history": json.RawMessage(`[{"dateTime":2,"domain":"new.example"},{"dateTime":1,"domain":"old.example"}]`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(snapshot.restore); err != nil {
		t.Fatal(err)
	}
	var restored model.Client
	if err := db.First(&restored, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if restored.Name != "before" || restored.Up != 107 || restored.Down != 211 || restored.TotalUp != 300 || restored.TotalDown != 400 {
		t.Errorf("configuration must roll back without losing traffic: %+v", restored)
	}
	var history []model.ClientHistoryEntry
	if err := json.Unmarshal(restored.History, &history); err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Domain != "new.example" || history[1].Domain != "old.example" {
		t.Errorf("post-commit history lost: %s", restored.History)
	}
}

func TestConfigSnapshotPreservesPostCommitStats(t *testing.T) {
	db := newConfigSnapshotTestDB(t)
	before := model.Stats{DateTime: 1, Resource: "user", Tag: "before", Traffic: 100}
	if err := db.Create(&before).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := takeConfigSnapshot(t, db, true)
	captureSnapshotAfterImage(t, db, snapshot)
	added := model.Stats{DateTime: 2, Resource: "user", Tag: "before", Traffic: 7}
	if err := db.Create(&added).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(snapshot.restore); err != nil {
		t.Fatal(err)
	}
	var stats []model.Stats
	if err := db.Order("id").Find(&stats).Error; err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 || stats[0] != before || stats[1] != added {
		t.Fatalf("post-commit stats lost: %+v", stats)
	}
}

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
	captureSnapshotAfterImage(t, db, snapshot)
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

func TestConfigSnapshotUndoesResetIntentButPreservesNewOperations(t *testing.T) {
	for _, traffic := range []int64{0, 7} {
		t.Run(fmt.Sprint(traffic), func(t *testing.T) {
			db := newConfigSnapshotTestDB(t)
			before := model.Client{
				Enable: true, Name: "reset", Config: json.RawMessage(`{"uuid":"keep"}`),
				Inbounds: json.RawMessage(`[1]`), Links: json.RawMessage(`["keep-link"]`),
				Volume: 999, Expiry: 1000, UploadLimit: 10, DownloadLimit: 20, Desc: "keep", Group: "group",
				DelayStart: true, AutoReset: true, ResetDays: 30, NextReset: 100,
				Up: 100, Down: 200, TotalUp: 300, TotalDown: 400,
				History: json.RawMessage(`[{"dateTime":1,"domain":"old","future":{"counter":9007199254740993},"sourceIps":["192.0.2.1","192.0.2.2"]}]`),
			}
			if err := db.Create(&before).Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&before, before.Id).Error; err != nil {
				t.Fatal(err)
			}
			var snapshot *configSnapshot
			if err := db.Transaction(func(tx *gorm.DB) error {
				var err error
				snapshot, err = captureConfigSnapshot(tx, false)
				if err != nil {
					return err
				}
				if err := tx.Model(&before).Updates(map[string]interface{}{
					"name": "renamed", "up": 0, "down": 0, "total_up": 400, "total_down": 600,
					"history": json.RawMessage(`[]`), "next_reset": 200, "delay_start": false,
					"enable": false, "expiry": 2000, "upload_limit": 0, "download_limit": 0,
				}).Error; err != nil {
					return err
				}
				return snapshot.captureAfterImage(tx)
			}); err != nil {
				t.Fatal(err)
			}
			// GORM Updates also modifies the struct passed to Model; reload the
			// original expected image from the immutable snapshot rather than it.
			before = snapshot.clients[0]
			expected := before
			if traffic > 0 {
				history := json.RawMessage(`[{"dateTime":2,"domain":"live","sourceIp":"192.0.2.3","future":true}]`)
				if err := db.Model(&model.Client{}).Where("id = ?", before.Id).Updates(map[string]interface{}{
					"up": gorm.Expr("up + ?", traffic), "down": gorm.Expr("down + ?", traffic+1), "history": history,
				}).Error; err != nil {
					t.Fatal(err)
				}
				expected.Up += traffic
				expected.Down += traffic + 1
				var entries []json.RawMessage
				if err := json.Unmarshal(before.History, &entries); err != nil {
					t.Fatal(err)
				}
				expected.History, _ = json.Marshal(append([]json.RawMessage{history[1 : len(history)-1]}, entries...))
			}
			if err := db.Transaction(snapshot.restore); err != nil {
				t.Fatal(err)
			}
			var restored model.Client
			if err := db.First(&restored, before.Id).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(restored, expected) {
				t.Fatalf("reset/config rollback or operational preservation failed:\n got: %+v\nwant: %+v", restored, expected)
			}
		})
	}
}

func TestConfigSnapshotUsesStableIDsAndOnlyDeletesFailedAdditions(t *testing.T) {
	db := newConfigSnapshotTestDB(t)
	clients := []model.Client{
		{Name: "alice", Up: 10, History: json.RawMessage(`[{"dateTime":1,"domain":"alice-old"}]`)},
		{Name: "bob", Up: 20, History: json.RawMessage(`[{"dateTime":1,"domain":"bob-old"}]`)},
		{Name: "deleted", Up: 30, History: json.RawMessage(`[{"dateTime":1,"domain":"deleted-old"}]`)},
	}
	if err := db.Create(&clients).Error; err != nil {
		t.Fatal(err)
	}
	var snapshot *configSnapshot
	failedNew := model.Client{Name: "deleted", Up: 999, History: json.RawMessage(`[{"dateTime":2,"domain":"failed-new"}]`)}
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		snapshot, err = captureConfigSnapshot(tx, false)
		if err != nil {
			return err
		}
		if err := tx.Model(&model.Client{}).Where("id = ?", clients[0].Id).Update("name", "bob").Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Client{}).Where("id = ?", clients[1].Id).Update("name", "alice").Error; err != nil {
			return err
		}
		if err := tx.Delete(&clients[2]).Error; err != nil {
			return err
		}
		if err := tx.Create(&failedNew).Error; err != nil {
			return err
		}
		return snapshot.captureAfterImage(tx)
	}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		if err := db.Model(&model.Client{}).Where("id = ?", clients[index].Id).Updates(map[string]interface{}{
			"up":      gorm.Expr("up + ?", index+1),
			"history": json.RawMessage(fmt.Sprintf(`[{"dateTime":3,"domain":"live-%d"}]`, index)),
		}).Error; err != nil {
			t.Fatal(err)
		}
	}
	independent := model.Client{Name: "independent", Up: 77}
	if err := db.Create(&independent).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(snapshot.restore); err != nil {
		t.Fatal(err)
	}
	var restored []model.Client
	if err := db.Order("id").Find(&restored).Error; err != nil {
		t.Fatal(err)
	}
	if len(restored) != 4 {
		t.Fatalf("failed addition survived or independent client lost: %+v", restored)
	}
	for index, wantUp := range []int64{11, 22, 30, 77} {
		if restored[index].Up != wantUp {
			t.Errorf("client %d traffic = %d, want %d", index, restored[index].Up, wantUp)
		}
	}
	for index := 0; index < 3; index++ {
		if restored[index].Id != clients[index].Id || restored[index].Name != clients[index].Name {
			t.Errorf("identity/config not restored: %+v", restored[index])
		}
		var history []model.ClientHistoryEntry
		if err := json.Unmarshal(restored[index].History, &history); err != nil {
			t.Fatal(err)
		}
		if index < 2 && (len(history) != 2 || history[0].Domain != fmt.Sprintf("live-%d", index)) {
			t.Errorf("history assigned to wrong ID: %s", restored[index].History)
		}
		if index == 2 && (len(history) != 1 || history[0].Domain != "deleted-old") {
			t.Errorf("deleted client's history mixed with failed new client: %s", restored[index].History)
		}
	}
}

func TestConfigSnapshotHistoryMergePreservesOrderAndUnknownFields(t *testing.T) {
	before := json.RawMessage(`[{"dateTime":2,"domain":"old","future":9007199254740993},{"dateTime":1,"domain":"older"}]`)
	after := json.RawMessage(`[{"dateTime":4,"domain":"intent"},{"dateTime":2,"domain":"old","future":9007199254740993}]`)
	current := json.RawMessage(`[{"dateTime":6,"domain":"new","sourceIps":["192.0.2.1"],"future":{"a":1}},{"domain":"new","dateTime":6,"future":{"a":1},"sourceIps":["192.0.2.1"]},{"dateTime":5,"domain":"second"},{"dateTime":4,"domain":"intent"},{"dateTime":2,"domain":"old","future":9007199254740993}]`)
	merged, err := compensateClientHistory(before, after, current)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"dateTime":6,"domain":"new","sourceIps":["192.0.2.1"],"future":{"a":1}},{"dateTime":5,"domain":"second"},{"dateTime":2,"domain":"old","future":9007199254740993},{"dateTime":1,"domain":"older"}]`
	if string(merged) != want {
		t.Fatalf("merged history = %s, want %s", merged, want)
	}
}

func TestConfigSnapshotHistoryMergeCapsNewestEntries(t *testing.T) {
	old := make([]json.RawMessage, maxConfigSnapshotHistoryEntries)
	for index := range old {
		old[index] = json.RawMessage(fmt.Sprintf(`{"dateTime":%d,"domain":"old-%d","future":{"keep":true}}`, 1000-index, index))
	}
	before, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	newEntry := json.RawMessage(`{"dateTime":1001,"domain":"new","sourceIps":["192.0.2.1","192.0.2.2"],"future":9007199254740993}`)
	current, err := json.Marshal(append([]json.RawMessage{newEntry}, old[:len(old)-1]...))
	if err != nil {
		t.Fatal(err)
	}
	merged, err := compensateClientHistory(before, before, current)
	if err != nil {
		t.Fatal(err)
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(merged, &entries); err != nil {
		t.Fatal(err)
	}
	if len(entries) != maxConfigSnapshotHistoryEntries || string(entries[0]) != string(newEntry) {
		t.Fatalf("cap lost newest entry or unknown JSON: %s", merged)
	}
	for index := 1; index < len(entries); index++ {
		if string(entries[index]) != string(old[index-1]) {
			t.Fatalf("newest-first history changed at %d: %s", index, entries[index])
		}
	}
}

func TestConfigSnapshotCounterCompensationRejectsUnrepresentableResults(t *testing.T) {
	for _, test := range []struct{ before, after, current, want int64 }{
		{100, 0, 7, 107}, {100, 150, 157, 107}, {100, 100, 0, 0}, {300, 400, 407, 307},
	} {
		got, err := compensateClientCounter(test.before, test.after, test.current)
		if err != nil || got != test.want {
			t.Errorf("counter %+v: got %d, %v", test, got, err)
		}
	}
	for _, values := range [][3]int64{{math.MaxInt64, 0, 1}, {10, 20, 0}, {-1, 0, 0}} {
		if _, err := compensateClientCounter(values[0], values[1], values[2]); err == nil {
			t.Errorf("unsafe compensation accepted: %v", values)
		}
	}
}

func TestConfigSnapshotRestoreRequiresAfterImageAndDoesNotMutateRetryBaseline(t *testing.T) {
	db := newConfigSnapshotTestDB(t)
	client := model.Client{Name: "before", Up: 100}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := takeConfigSnapshot(t, db, false)
	if err := db.Model(&client).Update("name", "after").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(snapshot.restore); err == nil {
		t.Fatal("restore accepted a missing after-image")
	}
	captureSnapshotAfterImage(t, db, snapshot)
	if err := db.Model(&client).UpdateColumn("up", gorm.Expr("up + 7")).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("rollback restore attempt")
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := snapshot.restore(tx); err != nil {
			return err
		}
		return injected
	}); !errors.Is(err, injected) {
		t.Fatalf("first restore attempt: %v", err)
	}
	if snapshot.clients[0].Up != 100 {
		t.Fatal("restore mutated its baseline")
	}
	if err := db.Transaction(snapshot.restore); err != nil {
		t.Fatal(err)
	}
	var restored model.Client
	if err := db.First(&restored, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if restored.Name != "before" || restored.Up != 107 {
		t.Fatalf("retry double-counted traffic: %+v", restored)
	}
}

func TestConfigSnapshotPreservesConcurrentMonthlyReset(t *testing.T) {
	db := newConfigSnapshotTestDB(t)
	location, err := (&SettingService{}).getTimeLocationTx(db)
	if err != nil {
		t.Fatal(err)
	}
	dueAt := nextClientMonthlyReset(1_800_000_000, location)
	client := model.Client{Name: "before", Enable: false, Inbounds: json.RawMessage(`[]`),
		AutoReset: true, NextReset: dueAt, Up: 100, Down: 200, TotalUp: 300, TotalDown: 400}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := takeConfigSnapshot(t, db, false)
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Update("name", "after").Error; err != nil {
		t.Fatal(err)
	}
	captureSnapshotAfterImage(t, db, snapshot)
	svc := &ClientService{}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, changed, err := svc.ResetClients(tx, dueAt)
		if err == nil && !changed {
			return errors.New("expected monthly reset")
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{
		"up": gorm.Expr("up + 7"), "down": gorm.Expr("down + 11"),
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(snapshot.restore); err != nil {
		t.Fatal(err)
	}
	var restored model.Client
	if err := db.First(&restored, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	wantReset := nextClientMonthlyReset(dueAt, location)
	if restored.Name != "before" || !restored.Enable || restored.NextReset != wantReset || restored.Up != 7 || restored.Down != 11 || restored.TotalUp != 400 || restored.TotalDown != 600 {
		t.Fatalf("monthly reset state/counters lost: %+v", restored)
	}
	if _, changed, err := svc.ResetClients(db, dueAt+1); err != nil || changed {
		t.Fatalf("next cron repeated monthly reset: changed=%v err=%v", changed, err)
	}
	if err := db.First(&restored, client.Id).Error; err != nil || restored.Up != 7 || restored.Down != 11 {
		t.Fatalf("next cron erased new traffic: %+v, %v", restored, err)
	}
}

func TestConfigSnapshotUsageIntentAndConcurrentCycleConflictIsExplicit(t *testing.T) {
	db := newConfigSnapshotTestDB(t)
	location, err := (&SettingService{}).getTimeLocationTx(db)
	if err != nil {
		t.Fatal(err)
	}
	dueAt := nextClientMonthlyReset(1_800_000_000, location)
	client := model.Client{Name: "before", Enable: true, Inbounds: json.RawMessage(`[]`),
		AutoReset: true, NextReset: dueAt, Up: 100, Down: 200, TotalUp: 300, TotalDown: 400}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := takeConfigSnapshot(t, db, false)
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{
		"name": "after", "up": 0, "down": 0, "total_up": 400, "total_down": 600,
	}).Error; err != nil {
		t.Fatal(err)
	}
	captureSnapshotAfterImage(t, db, snapshot)
	if err := db.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{
		"up": 7, "down": 11,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if _, changed, err := (&ClientService{}).ResetClients(db, dueAt); err != nil || !changed {
		t.Fatalf("monthly reset: changed=%v err=%v", changed, err)
	}
	var live model.Client
	if err := db.First(&live, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if live.Up != 0 || live.TotalUp != 407 || live.NextReset == dueAt {
		t.Fatalf("fixture missed mixed intent/cycle transition: %+v", live)
	}
	if err := db.Transaction(snapshot.restore); err == nil || !strings.Contains(err.Error(), "concurrent reset cycle") {
		t.Fatalf("ambiguous cycle rollback must fail explicitly: %v", err)
	}
	var preserved model.Client
	if err := db.First(&preserved, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(preserved, live) {
		t.Fatalf("conflict corrupted live usage/cycle: got=%+v want=%+v", preserved, live)
	}
}

func TestConfigSnapshotPreservesConcurrentDelayStartAndDepletion(t *testing.T) {
	for _, periodic := range []bool{false, true} {
		t.Run(fmt.Sprint(periodic), func(t *testing.T) {
			db := newConfigSnapshotTestDB(t)
			clients := []model.Client{
				{Name: "delayed", Enable: true, DelayStart: true, AutoReset: periodic, ResetDays: 3, Up: 100, Inbounds: json.RawMessage(`[]`)},
				{Name: "expired", Enable: true, Expiry: 1, Up: 20, Inbounds: json.RawMessage(`[]`)},
			}
			if err := db.Create(&clients).Error; err != nil {
				t.Fatal(err)
			}
			snapshot := takeConfigSnapshot(t, db, false)
			if err := db.Model(&model.Client{}).Where("id IN ?", []uint{clients[0].Id, clients[1].Id}).Update("desc", "failed intent").Error; err != nil {
				t.Fatal(err)
			}
			captureSnapshotAfterImage(t, db, snapshot)
			if _, err := (&ClientService{}).DepleteClients(); err != nil {
				t.Fatal(err)
			}
			var operated []model.Client
			if err := db.Order("id").Find(&operated).Error; err != nil {
				t.Fatal(err)
			}
			if operated[0].DelayStart || operated[1].Enable || (periodic && operated[0].NextReset == 0) || (!periodic && operated[0].Expiry == 0) {
				t.Fatalf("fixture did not perform expected operations: %+v", operated)
			}
			if err := db.Transaction(snapshot.restore); err != nil {
				t.Fatal(err)
			}
			var restored []model.Client
			if err := db.Order("id").Find(&restored).Error; err != nil {
				t.Fatal(err)
			}
			for index := range operated {
				operated[index].Desc = clients[index].Desc
			}
			if !reflect.DeepEqual(restored, operated) {
				t.Fatalf("operational state lost or failed intent retained:\n got: %+v\nwant: %+v", restored, operated)
			}
		})
	}
}

func TestConfigSnapshotStatsUndoIntentWithoutLosingLiveRowsOrRetention(t *testing.T) {
	db := newConfigSnapshotTestDB(t)
	before := []model.Stats{
		{DateTime: 1, Resource: "user", Tag: "deleted-by-intent", Traffic: 100},
		{DateTime: 2, Resource: "inbound", Tag: "updated-by-intent", Traffic: 200},
		{DateTime: 3, Resource: "outbound", Tag: "pruned-by-retention", Traffic: 300},
	}
	if err := db.Create(&before).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := takeConfigSnapshot(t, db, true)
	if err := db.Delete(&model.Stats{}, before[0].Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Stats{}).Where("id = ?", before[1].Id).Update("traffic", 999).Error; err != nil {
		t.Fatal(err)
	}
	intentNew := model.Stats{DateTime: 4, Resource: "user", Tag: "intent-new", Traffic: 400}
	if err := db.Create(&intentNew).Error; err != nil {
		t.Fatal(err)
	}
	captureSnapshotAfterImage(t, db, snapshot)
	live := model.Stats{DateTime: 5, Resource: "user", Tag: "live", Traffic: 7}
	if err := db.Create(&live).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&model.Stats{}, before[2].Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(snapshot.restore); err != nil {
		t.Fatal(err)
	}
	var restored []model.Stats
	if err := db.Order("id").Find(&restored).Error; err != nil {
		t.Fatal(err)
	}
	want := []model.Stats{before[0], before[1], live}
	if !reflect.DeepEqual(restored, want) {
		t.Fatalf("stats IDs/intent/live/retention merged incorrectly:\n got: %+v\nwant: %+v", restored, want)
	}
	appended := model.Stats{DateTime: 6, Resource: "user", Tag: "next", Traffic: 8}
	if err := db.Create(&appended).Error; err != nil || appended.Id <= live.Id {
		t.Fatalf("restoring old IDs broke sequence: %+v err=%v", appended, err)
	}
}

func TestConfigSnapshotStatsIDConflictAbortsWholeRestore(t *testing.T) {
	db := newConfigSnapshotTestDB(t)
	before := model.Stats{DateTime: 1, Tag: "before", Traffic: 100}
	client := model.Client{Name: "before", Up: 100}
	if err := db.Create(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := takeConfigSnapshot(t, db, true)
	if err := db.Delete(&before).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&client).Update("name", "after").Error; err != nil {
		t.Fatal(err)
	}
	captureSnapshotAfterImage(t, db, snapshot)
	conflict := model.Stats{Id: before.Id, DateTime: 2, Tag: "live-same-id", Traffic: 7}
	if err := db.Create(&conflict).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Transaction(snapshot.restore); err == nil {
		t.Fatal("ID conflict silently overwrote live stats")
	}
	var storedStat model.Stats
	var storedClient model.Client
	if err := db.First(&storedStat, before.Id).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&storedClient, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if storedStat != conflict || storedClient.Name != "after" || storedClient.Up != 100 {
		t.Fatalf("failed compensation partially committed: stat=%+v client=%+v", storedStat, storedClient)
	}
}

func TestConfigSnapshotSaveAfterImageFailureRollsBackWriteTransaction(t *testing.T) {
	db := newConfigSnapshotTestDB(t)
	previousCore := corePtr
	corePtr = nil
	t.Cleanup(func() { corePtr = previousCore })
	setting := model.Setting{Key: "subUpdates", Value: "12"}
	if err := db.Create(&setting).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("after-image read failed")
	clientReads := 0
	const callback = "test:snapshot_after_image_failure"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "clients" {
			clientReads++
			if clientReads == 2 {
				tx.AddError(injected)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(callback) })
	_, changed, err := (&ConfigService{}).Save("settings", "edit", json.RawMessage(`{"subUpdates":"13"}`), "", "test", "localhost")
	if !errors.Is(err, injected) || changed || clientReads != 2 {
		t.Fatalf("Save did not capture before commit: changed=%v reads=%d err=%v", changed, clientReads, err)
	}
	var stored model.Setting
	if err := db.First(&stored, setting.Id).Error; err != nil {
		t.Fatal(err)
	}
	var changes int64
	if err := db.Model(&model.Changes{}).Count(&changes).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Value != "12" || changes != 0 {
		t.Fatalf("after-image failure committed intent/audit: setting=%+v changes=%d", stored, changes)
	}
}
