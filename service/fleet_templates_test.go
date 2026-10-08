package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/internal/testutil"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func TestApplyFleetTemplateFailurePreservesPostCommitOperations(t *testing.T) {
	commands := testutil.NewManagedPortSandbox(t)
	if err := database.InitDB(filepath.Join(t.TempDir(), "fleet-compensation.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	previousCore, previousMasque, previousMieru := corePtr, masquePtr, mieruPtr
	corePtr, masquePtr, mieruPtr = nil, NewMasqueService(), nil
	t.Cleanup(func() {
		corePtr, masquePtr, mieruPtr = previousCore, previousMasque, previousMieru
	})
	config := model.Setting{Key: "config", Value: `{"log":{"disabled":true},"route":{"final":"direct"}}`}
	if err := db.Create(&config).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Name: "fleet-user", Enable: true, Config: json.RawMessage(`{}`),
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Desc: "before", Group: "keep",
		Up: 100, Down: 200, TotalUp: 300, TotalDown: 400,
		History: json.RawMessage(`[{"dateTime":1,"domain":"old","future":9007199254740993}]`)}
	if err := db.Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	oldStat := model.Stats{DateTime: 1, Resource: "user", Tag: client.Name, Traffic: 100}
	if err := db.Create(&oldStat).Error; err != nil {
		t.Fatal(err)
	}
	liveStat := model.Stats{DateTime: 2, Resource: "user", Tag: client.Name, Traffic: 7}
	liveHistory := json.RawMessage(`[{"dateTime":2,"domain":"live","sourceIps":["192.0.2.1"],"future":{"keep":true}},{"dateTime":1,"domain":"old","future":9007199254740993}]`)
	injected := errors.New("injected fleet post-commit runtime failure")
	injectedOnce := false
	runtimeReads := 0
	const callback = "test:fleet_post_commit_failure"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		_, inTransaction := tx.Statement.ConnPool.(*sql.Tx)
		// Linux's managed-port rebuild also reads inbounds before MASQUE. Only
		// inject into the MASQUE type-filtered query, on both operating systems.
		masqueWhere := clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "type = ?", Vars: []interface{}{"masque"}}}}
		if inTransaction || tx.Statement.Table != "inbounds" || !reflect.DeepEqual(tx.Statement.Clauses["WHERE"].Expression, masqueWhere) {
			return
		}
		runtimeReads++
		if injectedOnce {
			return
		}
		injectedOnce = true
		var wantCalls [][]string
		if runtime.GOOS == "linux" {
			wantCalls = testutil.ManagedPortRebuildCalls()
		}
		commands.AssertCalls(t, wantCalls)
		// MASQUE's first SyncFromDB is after template commit and port rebuild.
		// No MASQUE inbound or listener is started.
		writeErr := db.Transaction(func(writeTx *gorm.DB) error {
			var committed model.Client
			if err := writeTx.First(&committed, client.Id).Error; err != nil {
				return err
			}
			if committed.Desc != "template intent" {
				return errors.New("fixture did not reach the committed template")
			}
			if err := writeTx.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{
				"up": gorm.Expr("up + 7"), "down": gorm.Expr("down + 11"), "history": liveHistory,
			}).Error; err != nil {
				return err
			}
			return writeTx.Create(&liveStat).Error
		})
		if writeErr != nil {
			tx.AddError(writeErr)
			return
		}
		tx.AddError(injected)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(callback) })
	template := FleetTemplate{
		Sections: FleetTemplateSections{Clients: true, Outbounds: true},
		Clients: []model.Client{
			{Name: client.Name, Enable: false, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Desc: "template intent"},
			{Name: "failed-new-user", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`)},
		},
		Outbounds: []FleetTemplateOutbound{{Type: "direct", Tag: "failed-new-outbound", Options: json.RawMessage(`{}`)}},
	}
	err := (&ConfigService{}).ApplyFleetTemplate(template, "localhost")
	if !injectedOnce || err == nil || !strings.Contains(err.Error(), injected.Error()) || !strings.Contains(err.Error(), "目标服务器已恢复旧配置") {
		t.Fatalf("expected runtime failure with successful compensation: injected=%v err=%v", injectedOnce, err)
	}
	var restored []model.Client
	if err := db.Order("id").Find(&restored).Error; err != nil {
		t.Fatal(err)
	}
	expected := client
	expected.Up += 7
	expected.Down += 11
	expected.History = liveHistory
	if len(restored) != 1 || !reflect.DeepEqual(restored[0], expected) {
		t.Fatalf("template compensation lost operations or retained failed intent:\n got: %+v\nwant: %+v", restored, expected)
	}
	var stats []model.Stats
	if err := db.Order("id").Find(&stats).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stats, []model.Stats{oldStat, liveStat}) {
		t.Fatalf("template compensation lost live stats/IDs: %+v", stats)
	}
	var failedOutbounds int64
	if err := db.Model(&model.Outbound{}).Where("tag = ?", "failed-new-outbound").Count(&failedOutbounds).Error; err != nil || failedOutbounds != 0 {
		t.Fatalf("template compensation retained failed outbound: count=%d err=%v", failedOutbounds, err)
	}
	if runtimeReads != 2 {
		t.Fatalf("expected runtime apply failure followed by successful compensation sync, reads=%d", runtimeReads)
	}
	var wantCalls [][]string
	if runtime.GOOS == "linux" {
		wantCalls = testutil.ManagedPortRebuildCalls()
		wantCalls = append(wantCalls, testutil.ManagedPortRebuildCalls()...)
	}
	commands.AssertCalls(t, wantCalls)
}

func TestApplyFleetTemplateCoreStartFailureRestoresRealCoreAndPostCommitOperations(t *testing.T) {
	commands := testutil.NewManagedPortSandbox(t)
	dir := t.TempDir()
	if err := database.InitDB(filepath.Join(dir, "fleet-core-compensation.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	previousCore, previousMasque, previousMieru := corePtr, masquePtr, mieruPtr
	testCore := core.NewCore()
	corePtr, masquePtr, mieruPtr = testCore, nil, nil
	t.Cleanup(func() {
		if err := testCore.Stop(); err != nil {
			t.Errorf("stop restored test core: %v", err)
		}
		corePtr, masquePtr, mieruPtr = previousCore, previousMasque, previousMieru
	})
	config := model.Setting{Key: "config", Value: `{"log":{"disabled":true},"route":{"final":"stable-direct"}}`}
	stableOutbound := model.Outbound{Type: "direct", Tag: "stable-direct", Options: json.RawMessage(`{}`)}
	client := model.Client{Name: "fleet-core-user", Enable: true, Config: json.RawMessage(`{}`),
		Inbounds: json.RawMessage(`[]`), Links: json.RawMessage(`[]`), Desc: "before", Group: "keep",
		Up: 100, Down: 200, TotalUp: 300, TotalDown: 400,
		History: json.RawMessage(`[{"dateTime":1,"domain":"old","future":9007199254740993}]`)}
	for _, row := range []interface{}{&config, &stableOutbound, &client} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	oldStat := model.Stats{DateTime: 1, Resource: "user", Tag: client.Name, Traffic: 100}
	if err := db.Create(&oldStat).Error; err != nil {
		t.Fatal(err)
	}
	s := &ConfigService{}
	if err := s.StartCore(); err != nil {
		t.Fatalf("start real baseline core: %v", err)
	}
	baseline := testCore.GetInstance()
	if !testCore.IsRunning() || baseline == nil {
		t.Fatal("baseline core is not running")
	}
	if _, ok := baseline.Outbound().Outbound(stableOutbound.Tag); !ok {
		t.Fatal("baseline core did not install stable outbound")
	}
	initialVersion := CurrentDataVersion()
	liveStat := model.Stats{DateTime: 2, Resource: "user", Tag: client.Name, Traffic: 7}
	liveHistory := json.RawMessage(`[{"dateTime":2,"domain":"live","sourceIps":["192.0.2.1"],"future":{"keep":true}},{"dateTime":1,"domain":"old","future":9007199254740993}]`)
	runtimeReads := 0
	const callback = "test:fleet_core_post_commit_operations"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		_, inTransaction := tx.Statement.ConnPool.(*sql.Tx)
		configWhere := clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "key = ?", Vars: []interface{}{"config"}}}}
		if inTransaction || tx.Statement.Table != "settings" || !reflect.DeepEqual(tx.Statement.Clauses["WHERE"].Expression, configWhere) {
			return
		}
		runtimeReads++
		if runtimeReads == 2 {
			// Compensation must cold-start the old core after RestartCore stopped
			// its baseline and the real Core.Start rejected the template.
			if testCore.IsRunning() || testCore.GetInstance() != nil {
				t.Error("compensation did not follow a real stopped/failed core")
			}
			return
		}
		if runtimeReads != 1 {
			return
		}
		if !testCore.IsRunning() || testCore.GetInstance() != baseline {
			t.Error("template did not reach real RestartCore with the baseline running")
		}
		// This callback only records post-commit operations. The failure comes
		// from Core.Start opening a missing temporary TLS certificate, not a hook.
		tx.AddError(db.Transaction(func(writeTx *gorm.DB) error {
			var committed model.Client
			if err := writeTx.First(&committed, client.Id).Error; err != nil {
				return err
			}
			if committed.Desc != "template intent" {
				return errors.New("fixture did not reach the committed template")
			}
			if err := writeTx.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{
				"up": gorm.Expr("up + 7"), "down": gorm.Expr("down + 11"), "history": liveHistory,
			}).Error; err != nil {
				return err
			}
			return writeTx.Create(&liveStat).Error
		}))
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(callback) })
	missingCertificate := filepath.Join(dir, "missing-test-certificate.pem")
	failedOptions, err := json.Marshal(map[string]interface{}{
		"server": "127.0.0.1", "server_port": 1,
		"tls": map[string]interface{}{"enabled": true, "certificate_path": missingCertificate},
	})
	if err != nil {
		t.Fatal(err)
	}
	// No inbound, listener, or TUN device is configured. Typed parsing accepts
	// these options; outbound construction fails before any connection attempt.
	template := FleetTemplate{
		Sections: FleetTemplateSections{Clients: true, Outbounds: true},
		Clients: []model.Client{
			{Name: client.Name, Enable: false, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`), Desc: "template intent"},
			{Name: "failed-new-core-user", Enable: true, Config: json.RawMessage(`{}`), Inbounds: json.RawMessage(`[]`)},
		},
		Outbounds: []FleetTemplateOutbound{{Type: "http", Tag: "failed-core-outbound", Options: failedOptions}},
	}
	err = s.ApplyFleetTemplate(template, "localhost")
	if err == nil || !strings.Contains(err.Error(), "initialize outbound") || !strings.Contains(err.Error(), "read certificate") || !strings.Contains(err.Error(), missingCertificate) || !strings.Contains(err.Error(), "目标服务器已恢复旧配置") || runtimeReads != 2 {
		t.Fatalf("expected actual Core.Start failure followed by successful compensation: reads=%d err=%v", runtimeReads, err)
	}
	restoredCore := testCore.GetInstance()
	if !testCore.IsRunning() || restoredCore == nil || restoredCore == baseline {
		t.Fatal("compensation did not start a new real core instance")
	}
	if _, ok := restoredCore.Outbound().Outbound(stableOutbound.Tag); !ok {
		t.Fatal("restored core lost stable outbound")
	}
	if _, ok := restoredCore.Outbound().Outbound("failed-core-outbound"); ok {
		t.Fatal("restored core retained failed template outbound")
	}
	var restoredClients []model.Client
	if err := db.Order("id").Find(&restoredClients).Error; err != nil {
		t.Fatal(err)
	}
	expected := client
	expected.Up += 7
	expected.Down += 11
	expected.History = liveHistory
	if !reflect.DeepEqual(restoredClients, []model.Client{expected}) {
		t.Fatalf("core compensation lost operations or retained failed intent: got %+v, want %+v", restoredClients, expected)
	}
	var stats []model.Stats
	if err := db.Order("id").Find(&stats).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stats, []model.Stats{oldStat, liveStat}) {
		t.Fatalf("core compensation lost live stats/IDs: %+v", stats)
	}
	var outbounds []model.Outbound
	if err := db.Where("tag != ?", "direct").Order("id").Find(&outbounds).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(outbounds, []model.Outbound{stableOutbound}) || CurrentDataVersion() != initialVersion {
		t.Fatalf("failed core template changed outbounds/version: outbounds=%+v version=%d want=%d", outbounds, CurrentDataVersion(), initialVersion)
	}
	var wantCalls [][]string
	if runtime.GOOS == "linux" {
		wantCalls = testutil.ManagedPortRebuildCalls()
		wantCalls = append(wantCalls, testutil.ManagedPortRebuildCalls()...)
	}
	commands.AssertCalls(t, wantCalls)
}

func TestOrderFleetTemplateTargetsCanaryRemoteAndLocalLast(t *testing.T) {
	got := orderFleetTemplateTargets([]string{"local", "b", "a", "b"}, "a")
	want := []string{"a", "b", "local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderFleetTemplateTargets() = %#v, want %#v", got, want)
	}
}

func TestOrderFleetTemplateTargetsIgnoresUnknownCanary(t *testing.T) {
	got := orderFleetTemplateTargets([]string{"b", "local", "a"}, "missing")
	want := []string{"b", "a", "local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderFleetTemplateTargets() = %#v, want %#v", got, want)
	}
}

func TestReplaceFleetTemplateHost(t *testing.T) {
	input := json.RawMessage(`{"server_name":"la.mile.news","certificate_path":"/etc/la.mile.news/fullchain.pem"}`)
	got := replaceFleetTemplateHost(input, "la.mile.news", "tk.mile.news")
	want := `{"server_name":"tk.mile.news","certificate_path":"/etc/tk.mile.news/fullchain.pem"}`
	if string(got) != want {
		t.Fatalf("replaceFleetTemplateHost() = %s, want %s", got, want)
	}
}

func TestFleetTemplateHostname(t *testing.T) {
	for input, want := range map[string]string{
		"tk.mile.news:9999":  "tk.mile.news",
		"[2001:db8::1]:9999": "2001:db8::1",
		"la.mile.news":       "la.mile.news",
	} {
		if got := fleetTemplateHostname(input); got != want {
			t.Fatalf("fleetTemplateHostname(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFleetTemplateSummaryDoesNotExposeTemplatePayload(t *testing.T) {
	template := FleetTemplate{
		ID:        "template-id",
		Name:      "production",
		CreatedAt: time.Unix(10, 0),
		Sections:  FleetTemplateSections{Clients: true},
		Clients: []model.Client{{
			Name:   "alice",
			Config: json.RawMessage(`{"vless":{"uuid":"secret-value"}}`),
		}},
		Outbounds: []FleetTemplateOutbound{{
			Type: "socks", Tag: "media", Options: json.RawMessage(`{"server":"private-endpoint"}`),
		}},
	}

	raw, err := json.Marshal(template.Summary())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-value") || strings.Contains(string(raw), "private-endpoint") {
		t.Fatalf("template summary exposed payload: %s", raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"clients", "inbounds", "outbounds", "tls", "config", "sourceHost"} {
		if _, exists := fields[key]; exists {
			t.Fatalf("template summary exposed %q payload: %s", key, raw)
		}
	}
}

func TestCaptureFleetTemplateOutboundsKeepsOptionsAfterStorage(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fleet-capture.db")); err != nil {
		t.Fatal(err)
	}
	options := json.RawMessage(`{"server":"la.mile.news","server_port":1080,"username":"stream"}`)
	if err := database.GetDB().Create(&model.Outbound{Type: "socks", Tag: "stream", Options: options}).Error; err != nil {
		t.Fatal(err)
	}
	template, err := (&FleetService{}).CaptureFleetTemplate("streaming", FleetTemplateSections{Outbounds: true}, "la.mile.news")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := (&FleetService{}).FindFleetTemplate(template.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Sections.Outbounds || len(stored.Outbounds) != 2 {
		t.Fatalf("unexpected stored outbounds: %#v", stored.Outbounds)
	}
	var found bool
	for _, outbound := range stored.Outbounds {
		if outbound.Tag == "stream" {
			found = true
			if outbound.Type != "socks" || !equalJSONBytes(outbound.Options, options) {
				t.Fatalf("outbound options changed after storage: %#v", outbound)
			}
		}
	}
	if !found {
		t.Fatal("captured template omitted stream outbound")
	}
}

func TestFleetTemplateOutboundPreviewAndMerge(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fleet-outbound.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	if err := db.Create(&model.Outbound{
		Type: "socks", Tag: "media", Options: json.RawMessage(`{"server":"old.example","server_port":1080}`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	template := FleetTemplate{
		SourceHost: "la.mile.news",
		Sections:   FleetTemplateSections{Outbounds: true, Route: true},
		Outbounds: []FleetTemplateOutbound{
			{Type: "socks", Tag: "media", Options: json.RawMessage(`{"server":"la.mile.news","server_port":1080}`)},
			{Type: "socks", Tag: "stream", Options: json.RawMessage(`{"server":"stream.example","server_port":1081}`)},
		},
		Config: map[string]json.RawMessage{"route": json.RawMessage(`{"final":"stream","rules":[]}`)},
	}
	svc := &ConfigService{}
	preview, err := svc.PreviewFleetTemplate(template, "tk.mile.news")
	if err != nil {
		t.Fatalf("preview template: %v", err)
	}
	if preview.OutboundAdd != 1 || preview.OutboundUpdate != 1 {
		t.Fatalf("unexpected outbound preview: %+v", preview)
	}
	var count int64
	if err := db.Model(&model.Outbound{}).Where("tag = ?", "stream").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("preview modified target database: count=%d, err=%v", count, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return svc.applyFleetTemplateTx(tx, template, "tk.mile.news")
	}); err != nil {
		t.Fatalf("apply template: %v", err)
	}
	for tag, want := range map[string]string{"media": "la.mile.news", "stream": "stream.example"} {
		var outbound model.Outbound
		if err := db.Where("tag = ?", tag).First(&outbound).Error; err != nil {
			t.Fatal(err)
		}
		var options map[string]interface{}
		if err := json.Unmarshal(outbound.Options, &options); err != nil {
			t.Fatal(err)
		}
		if options["server"] != want {
			t.Fatalf("%s server = %v, want %s", tag, options["server"], want)
		}
	}
	if err := db.Model(&model.Outbound{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("unrelated direct outbound removed: count=%d, err=%v", count, err)
	}
}

func TestFleetTemplateRejectsInvalidOutbounds(t *testing.T) {
	for name, outbounds := range map[string][]FleetTemplateOutbound{
		"empty tag":       {{Type: "direct"}},
		"duplicate tag":   {{Type: "direct", Tag: "same"}, {Type: "direct", Tag: "same"}},
		"invalid options": {{Type: "direct", Tag: "bad", Options: json.RawMessage(`[]`)}},
		"overwritten tag": {{Type: "direct", Tag: "bad", Options: json.RawMessage(`{"tag":"other"}`)}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateFleetTemplateOutbounds(outbounds); err == nil {
				t.Fatal("expected invalid outbound template to be rejected")
			}
		})
	}
}

func TestFleetTemplatePreviewRollsBackInvalidOutboundReferences(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fleet-invalid-outbound.db")); err != nil {
		t.Fatal(err)
	}
	template := FleetTemplate{
		Sections: FleetTemplateSections{Outbounds: true},
		Outbounds: []FleetTemplateOutbound{{
			Type: "selector", Tag: "media", Options: json.RawMessage(`{"outbounds":["missing"]}`),
		}},
	}
	if _, err := (&ConfigService{}).PreviewFleetTemplate(template, "tk.mile.news"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing outbound reference error, got %v", err)
	}
	var count int64
	if err := database.GetDB().Model(&model.Outbound{}).Where("tag = ?", "media").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("preview retained invalid outbound: count=%d, err=%v", count, err)
	}
}

func TestFleetTemplatePreviewRejectsOlderRemoteWithoutOutboundCounts(t *testing.T) {
	template := FleetTemplate{Outbounds: []FleetTemplateOutbound{{Type: "direct", Tag: "direct"}}}
	if err := validateFleetTemplatePreviewSupport(template, &FleetTemplatePreview{}); err == nil {
		t.Fatal("expected old remote preview to be rejected")
	}
	if err := validateFleetTemplatePreviewSupport(template, &FleetTemplatePreview{OutboundUpdate: 1}); err != nil {
		t.Fatalf("current remote preview was rejected: %v", err)
	}
}
