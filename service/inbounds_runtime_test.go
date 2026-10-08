package service

import (
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
	"gorm.io/gorm"
)

func setupInboundRuntimeTest(t *testing.T) (*gorm.DB, *InboundService) {
	t.Helper()
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "inbound-runtime.db")); err != nil {
		t.Fatal(err)
	}
	previousCore := corePtr
	corePtr = core.NewCore()
	runtime := corePtr
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Errorf("stop test core: %v", err)
		}
		corePtr = previousCore
	})
	if err := runtime.Start([]byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatalf("start test core: %v", err)
	}
	return database.GetDB(), &InboundService{}
}

func inboundTestOptions(t *testing.T, protocol ...string) json.RawMessage {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if len(protocol) > 0 && protocol[0] != "naive" {
		return json.RawMessage(fmt.Sprintf(`{"listen":"127.0.0.1","listen_port":%d}`, port))
	}
	return json.RawMessage(fmt.Sprintf(`{"listen":"127.0.0.1","listen_port":%d,"network":"tcp"}`, port))
}

func assertInboundRuntimeTag(t *testing.T, tag string, want bool) {
	t.Helper()
	_, got := corePtr.GetInstance().Inbound().Get(tag)
	if got != want {
		t.Fatalf("runtime inbound %q present = %v, want %v", tag, got, want)
	}
}

func TestInboundRuntimeRestartWithoutNaiveUsers(t *testing.T) {
	db, svc := setupInboundRuntimeTest(t)
	inbound := model.Inbound{Type: "naive", Tag: "empty-naive", Options: inboundTestOptions(t)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	mixed := model.Inbound{Type: "mixed", Tag: "mixed-after-naive", Options: inboundTestOptions(t, "mixed")}
	if err := db.Create(&mixed).Error; err != nil {
		t.Fatal(err)
	}
	action, err := svc.BuildRestartInboundsAction(db, []uint{inbound.Id, mixed.Id})
	if err != nil {
		t.Fatal(err)
	}
	if action == nil {
		t.Fatal("expected remove-only action")
	}
	if err := action(); err != nil {
		t.Fatalf("restart empty Naive: %v", err)
	}
	assertInboundRuntimeTag(t, inbound.Tag, false)
	assertInboundRuntimeColdConfig(t, db, svc, mixed.Tag, true)
}

func TestInboundRuntimeSaveWithoutNaiveUsers(t *testing.T) {
	db, svc := setupInboundRuntimeTest(t)
	inbound := model.Inbound{Type: "naive", Tag: "empty-naive", Options: inboundTestOptions(t)}
	payload, err := inbound.MarshalFull()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var action func() error
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		action, err = svc.Save(tx, "new", data, "", "localhost")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := action(); err != nil {
		t.Fatalf("save empty Naive: %v", err)
	}
	assertInboundRuntimeTag(t, inbound.Tag, false)
}

func seedInboundRuntimeClient(t *testing.T, db *gorm.DB, inboundID uint, enabled bool) model.Client {
	t.Helper()
	client := inboundRuntimeClient(inboundID, enabled)
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	return client
}

func inboundRuntimeClient(inboundID uint, enabled bool) model.Client {
	return model.Client{
		Enable: enabled, Name: "runtime-test-user",
		Config:   json.RawMessage(`{"naive":{"username":"test-user","password":"test-pass"},"mixed":{"username":"test-user","password":"test-pass"},"mieru":{"name":"runtime-test-user","password":"test-pass"}}`),
		Inbounds: json.RawMessage(fmt.Sprintf("[%d]", inboundID)),
		Links:    json.RawMessage(`[]`),
		Up:       12, Down: 34, TotalUp: 56, TotalDown: 78,
		History: json.RawMessage(`[{"dateTime":123,"domain":"example.com","inbound":"old-tag"}]`),
	}
}

func inboundRuntimePayload(t *testing.T, inbound model.Inbound) json.RawMessage {
	t.Helper()
	payload, err := inbound.MarshalFull()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func applyInboundRuntimeSave(t *testing.T, db *gorm.DB, svc *InboundService, act string, data json.RawMessage, initUsers string) error {
	t.Helper()
	var action func() error
	if err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		action, err = svc.Save(tx, act, data, initUsers, "localhost")
		return err
	}); err != nil {
		t.Fatalf("build save action: %v", err)
	}
	if action == nil {
		t.Fatal("missing save action")
	}
	return action()
}

func assertInboundRuntimeColdConfig(t *testing.T, db *gorm.DB, svc *InboundService, tag string, want bool) {
	t.Helper()
	configs, err := svc.GetAllConfig(db)
	if err != nil {
		t.Fatal(err)
	}
	got := false
	for _, raw := range configs {
		var config struct {
			Tag string `json:"tag"`
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Fatal(err)
		}
		got = got || config.Tag == tag
	}
	if got != want {
		t.Fatalf("cold config inbound %q present = %v, want %v", tag, got, want)
	}
	assertInboundRuntimeTag(t, tag, want)
}

func assertInboundRuntimeClientHistory(t *testing.T, db *gorm.DB, before model.Client) {
	t.Helper()
	var after model.Client
	if err := db.First(&after, before.Id).Error; err != nil {
		t.Fatal(err)
	}
	var beforeConfig, afterConfig map[string]interface{}
	if err := json.Unmarshal(before.Config, &beforeConfig); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after.Config, &afterConfig); err != nil {
		t.Fatal(err)
	}
	if after.Up != before.Up || after.Down != before.Down || after.TotalUp != before.TotalUp || after.TotalDown != before.TotalDown || !equalJSONBytes(after.History, before.History) || !reflect.DeepEqual(afterConfig, beforeConfig) || !equalJSONBytes(after.Inbounds, before.Inbounds) {
		t.Fatalf("client credentials, assignment or history changed: before=%+v after=%+v", before, after)
	}
}

func TestInboundRuntimeClientLifecycle(t *testing.T) {
	db, svc := setupInboundRuntimeTest(t)
	inbound := model.Inbound{Type: "naive", Tag: "naive-lifecycle", Options: inboundTestOptions(t)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	configSvc := &ConfigService{}
	if err := configSvc.SettingService.SetConfig(`{"log":{"disabled":true},"route":{"final":"direct"}}`); err != nil {
		t.Fatal(err)
	}
	stat := model.Stats{Resource: "inbound", Tag: inbound.Tag, DateTime: 123, Traffic: 456}
	if err := db.Create(&stat).Error; err != nil {
		t.Fatal(err)
	}
	assertInboundRuntimeColdConfig(t, db, svc, inbound.Tag, false)
	client := inboundRuntimeClient(inbound.Id, true)
	data, err := json.Marshal(client)
	if err != nil {
		t.Fatal(err)
	}
	_, changed, err := configSvc.Save("clients", "new", data, "", "test", "localhost")
	if err != nil || !changed {
		t.Fatalf("add first client: changed=%v err=%v", changed, err)
	}
	if err := db.Where("name = ?", client.Name).First(&client).Error; err != nil {
		t.Fatal(err)
	}
	assertInboundRuntimeColdConfig(t, db, svc, inbound.Tag, true)
	// Exercise the same coordinator used by client edits and expiry updates.
	for _, enabled := range []bool{false, true} {
		client.Enable = enabled
		data, err := json.Marshal(client)
		if err != nil {
			t.Fatal(err)
		}
		_, changed, err := configSvc.Save("clients", "edit", data, "", "test", "localhost")
		if err != nil || !changed {
			t.Fatalf("save client enable=%v: changed=%v err=%v", enabled, changed, err)
		}
		assertInboundRuntimeColdConfig(t, db, svc, inbound.Tag, enabled)
		assertInboundRuntimeClientHistory(t, db, client)
		var stored model.Inbound
		if err := db.First(&stored, inbound.Id).Error; err != nil {
			t.Fatal(err)
		}
		if stored.Tag != inbound.Tag || !equalJSONBytes(stored.Options, inbound.Options) {
			t.Fatal("stored inbound changed")
		}
	}
	var storedStat model.Stats
	if err := db.First(&storedStat, stat.Id).Error; err != nil {
		t.Fatal(err)
	}
	if storedStat != stat {
		t.Fatalf("inbound traffic history changed: %+v", storedStat)
	}
}

func TestInboundRuntimeSaveInitialUsersAndRename(t *testing.T) {
	for _, protocol := range []string{"naive", "mixed"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%v", protocol, enabled), func(t *testing.T) {
				db, svc := setupInboundRuntimeTest(t)
				client := seedInboundRuntimeClient(t, db, 999, enabled)
				inbound := model.Inbound{Type: protocol, Tag: "original", Options: inboundTestOptions(t, protocol)}
				if err := applyInboundRuntimeSave(t, db, svc, "new", inboundRuntimePayload(t, inbound), strconv.Itoa(int(client.Id))); err != nil {
					t.Fatal(err)
				}
				if err := db.Where("tag = ?", inbound.Tag).First(&inbound).Error; err != nil {
					t.Fatal(err)
				}
				wantRuntime := protocol != "naive" || enabled
				assertInboundRuntimeColdConfig(t, db, svc, inbound.Tag, wantRuntime)
				if err := db.First(&client, client.Id).Error; err != nil {
					t.Fatal(err)
				}
				inbound.Tag = "renamed"
				if err := applyInboundRuntimeSave(t, db, svc, "edit", inboundRuntimePayload(t, inbound), ""); err != nil {
					t.Fatal(err)
				}
				assertInboundRuntimeTag(t, "original", false)
				assertInboundRuntimeColdConfig(t, db, svc, inbound.Tag, wantRuntime)
				assertInboundRuntimeClientHistory(t, db, client)
				var stored model.Inbound
				if err := db.First(&stored, inbound.Id).Error; err != nil {
					t.Fatal(err)
				}
				if stored.Tag != "renamed" {
					t.Fatalf("rename not persisted: %+v", stored)
				}
			})
		}
	}
}

func TestInboundRuntimeRestartMixedWithoutUsers(t *testing.T) {
	db, svc := setupInboundRuntimeTest(t)
	inbound := model.Inbound{Type: "mixed", Tag: "empty-mixed", Options: inboundTestOptions(t, "mixed")}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RestartInbounds(db, []uint{inbound.Id}); err != nil {
		t.Fatal(err)
	}
	assertInboundRuntimeColdConfig(t, db, svc, inbound.Tag, true)
}

func TestInboundRuntimeSaveFailureRestoresBeforeImage(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("old-user-enabled=%v", enabled), func(t *testing.T) {
			db, svc := setupInboundRuntimeTest(t)
			inbound := model.Inbound{Type: "naive", Tag: "before", Options: inboundTestOptions(t)}
			if err := db.Create(&inbound).Error; err != nil {
				t.Fatal(err)
			}
			client := seedInboundRuntimeClient(t, db, inbound.Id, enabled)
			if err := svc.RestartInbounds(db, []uint{inbound.Id}); err != nil {
				t.Fatal(err)
			}
			configSvc := &ConfigService{}
			if err := configSvc.SettingService.SetConfig(`{"log":{"disabled":true},"route":{"final":"direct"}}`); err != nil {
				t.Fatal(err)
			}
			stat := model.Stats{Resource: "inbound", Tag: inbound.Tag, DateTime: 123, Traffic: 456}
			if err := db.Create(&stat).Error; err != nil {
				t.Fatal(err)
			}
			change := model.Changes{Actor: "history", Key: "inbounds", Action: "new", Obj: json.RawMessage(`{}`)}
			if err := db.Create(&change).Error; err != nil {
				t.Fatal(err)
			}
			// A real occupied socket fails after commit, rather than in preflight.
			busy, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer busy.Close()
			updated := inbound
			updated.Type, updated.Tag = "mixed", "failed-rename"
			updated.Options = json.RawMessage(fmt.Sprintf(`{"listen":"127.0.0.1","listen_port":%d}`, busy.Addr().(*net.TCPAddr).Port))
			_, changed, err := configSvc.Save("inbounds", "edit", inboundRuntimePayload(t, updated), "", "test", "localhost")
			if err == nil || changed || !strings.Contains(err.Error(), "address already in use") {
				t.Fatalf("expected post-commit bind failure: changed=%v err=%v", changed, err)
			}
			if !strings.Contains(err.Error(), "配置已自动回滚") {
				t.Fatalf("compensation failed: %v", err)
			}
			assertInboundRuntimeTag(t, updated.Tag, false)
			assertInboundRuntimeColdConfig(t, db, svc, inbound.Tag, enabled)
			assertInboundRuntimeClientHistory(t, db, client)
			var stored model.Inbound
			if err := db.First(&stored, inbound.Id).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Tag != inbound.Tag || stored.Type != inbound.Type || !equalJSONBytes(stored.Options, inbound.Options) {
				t.Fatalf("inbound before-image not restored: %+v", stored)
			}
			var stats []model.Stats
			if err := db.Find(&stats).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(stats, []model.Stats{stat}) {
				t.Fatalf("stats changed: %+v", stats)
			}
			var changes []model.Changes
			if err := db.Find(&changes).Error; err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(changes, []model.Changes{change}) {
				t.Fatalf("history changed: %+v", changes)
			}
		})
	}
}

func TestInboundRuntimeSaveAddFailureRestoresCore(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("old-user-enabled=%v", enabled), func(t *testing.T) {
			db, svc := setupInboundRuntimeTest(t)
			inbound := model.Inbound{Type: "naive", Tag: "before", Options: inboundTestOptions(t)}
			if err := db.Create(&inbound).Error; err != nil {
				t.Fatal(err)
			}
			seedInboundRuntimeClient(t, db, inbound.Id, enabled)
			if err := svc.RestartInbounds(db, []uint{inbound.Id}); err != nil {
				t.Fatal(err)
			}
			busy, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer busy.Close()
			updated := inbound
			updated.Type, updated.Tag = "mixed", "failed-rename"
			updated.Options = json.RawMessage(fmt.Sprintf(`{"listen":"127.0.0.1","listen_port":%d}`, busy.Addr().(*net.TCPAddr).Port))
			err = applyInboundRuntimeSave(t, db, svc, "edit", inboundRuntimePayload(t, updated), "")
			if err == nil || !strings.Contains(err.Error(), "address already in use") || strings.Contains(err.Error(), "missing users") {
				t.Fatalf("expected bind failure without a failed rollback: %v", err)
			}
			// Check the local rollback before the coordinator can restart the core.
			assertInboundRuntimeTag(t, updated.Tag, false)
			assertInboundRuntimeTag(t, inbound.Tag, enabled)
		})
	}
}

func TestInboundRuntimeRollbackUsesSnapshotAfterDelete(t *testing.T) {
	db, svc := setupInboundRuntimeTest(t)
	inbound := model.Inbound{Type: "naive", Tag: "before-delete", Options: inboundTestOptions(t)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	seedInboundRuntimeClient(t, db, inbound.Id, true)
	config, err := svc.buildInboundCoreConfig(db, &inbound)
	if err != nil {
		t.Fatal(err)
	}
	if err := corePtr.AddInbound(config); err != nil {
		t.Fatal(err)
	}
	if err := applyInboundRuntimeSave(t, db, svc, "del", json.RawMessage(`"before-delete"`), ""); err != nil {
		t.Fatal(err)
	}
	assertInboundRuntimeTag(t, inbound.Tag, false)
	if err := svc.rollbackInboundCoreState("del", &inbound, nil, config); err != nil {
		t.Fatal(err)
	}
	assertInboundRuntimeTag(t, inbound.Tag, true)
}

func TestInboundRuntimeRollbackNewRemovesInbound(t *testing.T) {
	db, svc := setupInboundRuntimeTest(t)
	inbound := model.Inbound{Type: "naive", Tag: "failed-new", Options: inboundTestOptions(t)}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	seedInboundRuntimeClient(t, db, inbound.Id, true)
	config, err := svc.buildInboundCoreConfig(db, &inbound)
	if err != nil {
		t.Fatal(err)
	}
	if err := corePtr.AddInbound(config); err != nil {
		t.Fatal(err)
	}
	if err := svc.rollbackInboundCoreState("new", nil, &inbound, nil); err != nil {
		t.Fatal(err)
	}
	assertInboundRuntimeTag(t, inbound.Tag, false)
}

func TestInboundRuntimeRollbackRemovesReplacementTag(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("old-user-enabled=%v", enabled), func(t *testing.T) {
			db, svc := setupInboundRuntimeTest(t)
			old := model.Inbound{Type: "naive", Tag: "old", Options: inboundTestOptions(t)}
			if err := db.Create(&old).Error; err != nil {
				t.Fatal(err)
			}
			seedInboundRuntimeClient(t, db, old.Id, enabled)
			config, err := svc.buildInboundCoreConfig(db, &old)
			if err != nil {
				t.Fatal(err)
			}
			replacement := model.Inbound{Type: "mixed", Tag: "replacement", Options: inboundTestOptions(t, "mixed")}
			raw, err := replacement.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if err := corePtr.AddInbound(raw); err != nil {
				t.Fatal(err)
			}
			if err := svc.rollbackInboundCoreState("edit", &old, &replacement, config); err != nil {
				t.Fatal(err)
			}
			assertInboundRuntimeTag(t, replacement.Tag, false)
			assertInboundRuntimeTag(t, old.Tag, enabled)
		})
	}
}
