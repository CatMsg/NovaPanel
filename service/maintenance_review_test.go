package service

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/internal/testutil"
	"gorm.io/gorm"
)

func maintenanceReviewCore(t *testing.T) string {
	t.Helper()
	setupMaintenanceTest(t)
	previousFailure := lastStartFailTime
	lastStartFailTime = time.Time{}
	t.Cleanup(func() { lastStartFailTime = previousFailure })
	if err := (&SettingService{}).setString("config", `{"log":{"disabled":true}}`); err != nil {
		t.Fatal(err)
	}
	options := inboundTestOptions(t, "mixed")
	if err := database.GetDB().Create(&model.Inbound{
		Type: "mixed", Tag: "maintenance-review", Options: options,
	}).Error; err != nil {
		t.Fatal(err)
	}
	var listener struct {
		Listen string `json:"listen"`
		Port   int    `json:"listen_port"`
	}
	if err := json.Unmarshal(options, &listener); err != nil {
		t.Fatal(err)
	}
	var address net.TCPAddr
	address.IP = net.ParseIP(listener.Listen)
	address.Port = listener.Port
	runtime := core.NewCore()
	corePtr = runtime
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Errorf("stop temporary core: %v", err)
		}
	})
	if err := (&ConfigService{}).StartCore(); err != nil {
		t.Fatal(err)
	}
	connection, err := net.DialTimeout("tcp", address.String(), time.Second)
	if err != nil {
		t.Fatalf("initial loopback listener: %v", err)
	}
	_ = connection.Close()
	return address.String()
}

func assertMaintenanceReviewStopped(t *testing.T, address string) {
	t.Helper()
	if !IsMaintenanceMode() || corePtr.IsRunning() {
		t.Fatalf("data plane escaped pause: maintenance=%t running=%t", IsMaintenanceMode(), corePtr.IsRunning())
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("paused core retained its listener: %v", err)
	}
	_ = listener.Close()
}

func TestMaintenanceReviewRealCoreSaveAndAutomaticStartsRemainPaused(t *testing.T) {
	address := maintenanceReviewCore(t)
	maintenance := NewMaintenanceService()
	if err := maintenance.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	assertMaintenanceReviewStopped(t, address)
	config := &ConfigService{}
	var newInbound map[string]interface{}
	if err := json.Unmarshal(inboundTestOptions(t, "mixed"), &newInbound); err != nil {
		t.Fatal(err)
	}
	newInbound["type"], newInbound["tag"] = "mixed", "paused-inbound"
	newInboundData, err := json.Marshal(newInbound)
	if err != nil {
		t.Fatal(err)
	}
	var newInboundPort struct {
		ListenPort int `json:"listen_port"`
	}
	if err := json.Unmarshal(newInboundData, &newInboundPort); err != nil {
		t.Fatal(err)
	}
	var hostCommands *testutil.HostCommandSandbox
	var wantHostCalls [][]string
	if runtime.GOOS == "linux" {
		port := strconv.Itoa(newInboundPort.ListenPort)
		forwardCall := []string{"scripts/hy2-forward.sh", "apply", "paused-inbound", port, port, "tcp"}
		hostCommands = testutil.NewHostCommandSandbox(t, forwardCall)
		wantHostCalls = [][]string{{"bash", "scripts/hy2-forward.sh", "apply", "paused-inbound", port, port, "tcp"}}
	}
	for _, save := range []struct {
		object string
		data   json.RawMessage
	}{
		{"config", json.RawMessage(`{"log":{"disabled":true},"route":{"auto_detect_interface":false}}`)},
		{"outbounds", json.RawMessage(`{"type":"direct","tag":"paused-direct"}`)},
		{"inbounds", newInboundData},
		{"settings", json.RawMessage(`{"subURI":"https://maintenance.invalid/sub/"}`)},
	} {
		if _, _, err := config.Save(save.object, "new", save.data, "", "maintenance-review", "localhost"); err != nil {
			t.Fatalf("save %s while paused: %v", save.object, err)
		}
		assertMaintenanceReviewStopped(t, address)
	}
	if hostCommands != nil {
		hostCommands.AssertCalls(t, wantHostCalls)
	}
	for name, start := range map[string]func() error{
		"start":           config.StartCore,
		"restart":         config.RestartCore,
		"budget recovery": restoreProxyDataPlaneAfterTrafficBudget,
	} {
		if err := start(); err != nil {
			t.Fatalf("%s while paused: %v", name, err)
		}
		assertMaintenanceReviewStopped(t, address)
	}
	if _, _, err := config.Save("settings", "edit", json.RawMessage(`{"maintenanceMode":"false"}`), "", "test", "localhost"); err == nil {
		t.Fatal("generic settings bypassed the maintenance lifecycle")
	}
	assertMaintenanceReviewStopped(t, address)
	if err := maintenance.SetEnabled(false); err != nil {
		t.Fatal(err)
	}
	if IsMaintenanceMode() || !corePtr.IsRunning() {
		t.Fatal("explicit resume did not start the saved configuration")
	}
	connection, err := net.DialTimeout("tcp", address, time.Second)
	if err != nil {
		t.Fatalf("listener after explicit resume: %v", err)
	}
	_ = connection.Close()
}

func TestMaintenanceReviewPauseWaitsForSaveRuntimeAction(t *testing.T) {
	address := maintenanceReviewCore(t)
	db := database.GetDB()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	const callback = "test:maintenance_save_barrier"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		if tx.Statement.Table == "outbounds" {
			once.Do(func() { close(entered); <-release })
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	saveDone := make(chan error, 1)
	go func() {
		_, _, err := (&ConfigService{}).Save("outbounds", "new", json.RawMessage(`{"type":"direct","tag":"save-before-pause"}`), "", "test", "localhost")
		saveDone <- err
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("save did not reach its transaction barrier")
	}
	maintenance := NewMaintenanceService()
	pauseStarted, pauseDone := make(chan struct{}), make(chan error, 1)
	go func() { close(pauseStarted); pauseDone <- maintenance.SetEnabled(true) }()
	<-pauseStarted
	select {
	case err := <-pauseDone:
		t.Fatalf("pause bypassed an in-flight save: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	for name, done := range map[string]<-chan error{"save": saveDone, "pause": pauseDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s deadlocked after releasing the transaction", name)
		}
	}
	assertMaintenanceReviewStopped(t, address)
}

func TestMaintenanceReviewConcurrentPausedEntryPoints(t *testing.T) {
	address := maintenanceReviewCore(t)
	if err := NewMaintenanceService().SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	mieru := NewMieruService()
	failover := NewFailoverService(&ConfigService{})
	t.Cleanup(failover.Stop)
	operations := []func() error{
		(&ConfigService{}).StartCore,
		(&ConfigService{}).RestartCore,
		restoreProxyDataPlaneAfterTrafficBudget,
		mieru.SyncFromDB,
		func() error {
			mieru.StartWatchdog()
			mieru.runWatchdogCheck(context.Background())
			return mieru.restartUnhealthyRuntime(nil, "test paused watchdog")
		},
		func() error { failover.Start(); failover.runOnce(); return nil },
		func() error {
			_, _, err := (&ConfigService{}).Save("config", "edit", json.RawMessage(`{"log":{"disabled":true}}`), "", "test", "localhost")
			return err
		},
	}
	done := make(chan error, len(operations))
	for _, operation := range operations {
		go func() { done <- operation() }()
	}
	for range operations {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("paused lifecycle entry point deadlocked")
		}
	}
	assertMaintenanceReviewStopped(t, address)
	if mieru.watchCancel != nil || mieru.active != nil || failover.cancel != nil {
		t.Fatal("paused watchdog/failover resurrected a runtime")
	}
}

func TestMaintenanceReviewPartialResumeClosesRealListener(t *testing.T) {
	address := maintenanceReviewCore(t)
	startErr := errors.New("synthetic secondary data-plane failure")
	hooks := defaultMaintenanceHooks()
	hooks.startProxyPlanes = func() error {
		if err := startProxyDataPlaneForMaintenanceResume(); err != nil {
			return err
		}
		if !IsMaintenanceMode() || !corePtr.IsRunning() {
			t.Fatal("partial resume did not retain its maintenance gate")
		}
		return startErr
	}
	maintenance := newMaintenanceService(hooks)
	if err := maintenance.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.SetEnabled(false); !errors.Is(err, startErr) {
		t.Fatalf("partial resume: %v", err)
	}
	assertMaintenanceReviewStopped(t, address)
	if stored, err := maintenance.getBool(maintenanceModeSetting); err != nil || !stored {
		t.Fatalf("failed resume lost persisted pause: %t, %v", stored, err)
	}
}

func TestMaintenanceReviewResumeFailureStagesStayPaused(t *testing.T) {
	for _, stage := range []string{"reconcile", "hard-budget", "start", "persist"} {
		t.Run(stage, func(t *testing.T) {
			setupMaintenanceTest(t)
			failure := errors.New("synthetic " + stage + " failure")
			stops, starts, after := 0, 0, 0
			hooks := maintenanceHooks{
				stopProxyPlanes: func() error { stops++; return nil },
				reconcileTrafficBudget: func() error {
					if stage == "reconcile" {
						return failure
					}
					if stage == "hard-budget" {
						trafficBudgetBlocked.Store(true)
					}
					return nil
				},
				startProxyPlanes: func() error {
					starts++
					if stage == "start" {
						return failure
					}
					return nil
				},
				startAfterResume: func() { after++ },
			}
			maintenance := newMaintenanceService(hooks)
			if err := maintenance.SetEnabled(true); err != nil {
				t.Fatal(err)
			}
			if stage == "persist" {
				db := database.GetDB()
				const callback = "test:maintenance_resume_write_failure"
				if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
					setting, ok := tx.Statement.Dest.(*model.Setting)
					if ok && setting.Key == maintenanceModeSetting && setting.Value == "false" {
						tx.AddError(failure)
					}
				}); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
			}
			if err := maintenance.SetEnabled(false); err == nil {
				t.Fatal("failed resume was reported as successful")
			}
			status := maintenance.Status()
			if !IsMaintenanceMode() || !status.Enabled || status.Transitioning || status.Error == "" || after != 0 || stops != 2 {
				t.Fatalf("failed resume escaped pause: status=%+v stops=%d postResume=%d", status, stops, after)
			}
			if (stage == "reconcile" || stage == "hard-budget") && starts != 0 {
				t.Fatalf("started before prerequisites passed: %d", starts)
			}
			reloaded := newMaintenanceService(maintenanceHooks{})
			if err := reloaded.Initialize(); err != nil || !IsMaintenanceMode() {
				t.Fatalf("reload lost persistent pause: %v", err)
			}
		})
	}
}

func TestMaintenanceReviewFailedImportRetainsPause(t *testing.T) {
	setupMaintenanceTest(t)
	maintenance := newMaintenanceService(maintenanceHooks{})
	if err := maintenance.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(t.TempDir(), "invalid-backup-*.db")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.WriteString("not a SQLite backup"); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	if err := maintenance.ImportDatabase(file); err == nil {
		t.Fatal("invalid import succeeded")
	}
	if err := maintenance.Initialize(); err != nil || !IsMaintenanceMode() {
		t.Fatalf("invalid import lost pause: %v", err)
	}
}

func TestMaintenanceReviewScheduledBackupUnaffected(t *testing.T) {
	setupMaintenanceTest(t)
	backup, fake := remoteBackupFixture(t)
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	saveFixtureBackup(t, backup, fake, true, 2)
	_, _, before, err := backup.load()
	if err != nil {
		t.Fatal(err)
	}
	previousBackup := remoteBackups
	remoteBackups = backup
	t.Cleanup(func() { StopRemoteBackupScheduler(); remoteBackups = previousBackup })
	StartRemoteBackupScheduler()
	maintenance := NewMaintenanceService()
	if err := maintenance.SetEnabled(true); err != nil {
		t.Fatal(err)
	}
	status, err := backup.Status()
	if err != nil || !status.SchedulerRunning || !status.Settings.Enabled || !status.State.NextDueAt.Equal(before.NextDueAt) {
		t.Fatalf("pause disturbed backup scheduling: %+v, %v", status, err)
	}
	before.NextDueAt = time.Now().Add(-time.Minute)
	if err := backup.saveState(before); err != nil {
		t.Fatal(err)
	}
	backup.tick(context.Background())
	status, err = backup.Status()
	if err != nil || status.State.LastSuccessAt.IsZero() || status.State.LastError != "" || !status.SchedulerRunning || !IsMaintenanceMode() {
		t.Fatalf("scheduled backup failed during pause: %+v, %v", status, err)
	}
	fake.Lock()
	defer fake.Unlock()
	var databases, manifests int
	for name := range fake.files {
		switch filepath.Ext(name) {
		case ".db":
			databases++
		case ".json":
			manifests++
		}
		if strings.HasSuffix(name, ".part") {
			t.Fatalf("scheduled backup left a partial artifact: %s", name)
		}
	}
	if databases != 1 || manifests != 1 {
		t.Fatalf("scheduled backup did not publish its DB/manifest pair: db=%d manifest=%d", databases, manifests)
	}
}
