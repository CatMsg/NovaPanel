package service

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func setupMaintenanceTest(t *testing.T) {
	t.Helper()
	dbDir := t.TempDir()
	t.Setenv("NOVAS_DB_FOLDER", dbDir)
	if err := database.InitDB(filepath.Join(dbDir, config.GetName()+".db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	previousMode := maintenanceModeEnabled.Load()
	previousTransition := maintenanceTransitioning.Load()
	previousBlocked := trafficBudgetBlocked.Load()
	previousRuntimeState := trafficBudgetRuntimeState.Load()
	previousCore := corePtr
	previousFailover := failoverPtr
	previousMieru := mieruPtr
	previousMasque := masquePtr
	maintenanceModeEnabled.Store(false)
	maintenanceTransitioning.Store(false)
	trafficBudgetBlocked.Store(false)
	trafficBudgetRuntimeState.Store(0)
	corePtr = nil
	failoverPtr = nil
	mieruPtr = nil
	masquePtr = nil
	t.Cleanup(func() {
		maintenanceModeEnabled.Store(previousMode)
		maintenanceTransitioning.Store(previousTransition)
		trafficBudgetBlocked.Store(previousBlocked)
		trafficBudgetRuntimeState.Store(previousRuntimeState)
		corePtr = previousCore
		failoverPtr = previousFailover
		mieruPtr = previousMieru
		masquePtr = previousMasque
		if sqlDB, err := database.GetDB().DB(); err == nil {
			_ = sqlDB.Close()
		}
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
}

func TestMaintenanceModeLoadsPersistedState(t *testing.T) {
	setupMaintenanceTest(t)
	defaultService := newMaintenanceService(maintenanceHooks{})
	if err := defaultService.Initialize(); err != nil {
		t.Fatalf("initialize default maintenance state: %v", err)
	}
	if IsMaintenanceMode() {
		t.Fatal("maintenance mode should default to false")
	}

	settings := &SettingService{}
	if err := settings.setString(maintenanceModeSetting, "true"); err != nil {
		t.Fatal(err)
	}
	service := newMaintenanceService(maintenanceHooks{})
	if err := service.Initialize(); err != nil {
		t.Fatalf("initialize maintenance state: %v", err)
	}
	if !IsMaintenanceMode() {
		t.Fatal("maintenance mode was not loaded from persisted setting")
	}
	if status := service.Status(); !status.Enabled || status.Transitioning || status.Error != "" {
		t.Fatalf("unexpected initialized status: %+v", status)
	}
}

func TestMaintenanceModeFailsClosedOnInvalidPersistedValue(t *testing.T) {
	setupMaintenanceTest(t)
	if err := (&SettingService{}).setString(maintenanceModeSetting, "not-a-boolean"); err != nil {
		t.Fatal(err)
	}
	service := newMaintenanceService(maintenanceHooks{})
	if err := service.Initialize(); err == nil {
		t.Fatal("invalid persisted maintenance value was accepted")
	}
	status := service.Status()
	if !IsMaintenanceMode() || !status.Enabled || status.Transitioning || status.Error == "" {
		t.Fatalf("invalid persisted state did not fail closed: enabled=%t status=%+v", IsMaintenanceMode(), status)
	}
}

func TestMaintenanceModeImportPreservesPauseAcrossInitialize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ImportDB restart uses process termination on Windows")
	}
	setupMaintenanceTest(t)
	settings := &SettingService{}
	if err := settings.setString(maintenanceModeSetting, "false"); err != nil {
		t.Fatal(err)
	}
	backupPath, cleanup, err := database.CreateDBBackup("")
	if err != nil {
		t.Fatalf("create pre-pause backup: %v", err)
	}
	defer cleanup()
	if err := settings.setString(maintenanceModeSetting, "true"); err != nil {
		t.Fatal(err)
	}
	maintenanceModeEnabled.Store(true)

	backup, err := os.Open(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	defer backup.Close()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGHUP)
	signalScheduled := false
	signalReceived := false
	defer func() {
		if signalScheduled && !signalReceived {
			select {
			case <-signals:
			case <-time.After(5 * time.Second):
			}
		}
		signal.Stop(signals)
	}()

	if err := GetMaintenanceService().ImportDatabase(backup); err != nil {
		t.Fatalf("import older unpaused backup while paused: %v", err)
	}
	signalScheduled = true
	select {
	case <-signals:
		signalReceived = true
	case <-time.After(5 * time.Second):
		t.Fatal("database import did not deliver its scheduled restart signal")
	}

	reloaded := newMaintenanceService(maintenanceHooks{})
	if err := reloaded.Initialize(); err != nil {
		t.Fatalf("load imported maintenance state: %v", err)
	}
	if !IsMaintenanceMode() || !reloaded.Status().Enabled {
		t.Fatalf("paused state was lost after import and Initialize: enabled=%t status=%+v", IsMaintenanceMode(), reloaded.Status())
	}
}

func TestMaintenanceModePauseAndResumePersistTransitions(t *testing.T) {
	setupMaintenanceTest(t)
	stopCalls, startCalls, reconcileCalls, afterResumeCalls := 0, 0, 0, 0
	service := newMaintenanceService(maintenanceHooks{
		stopProxyPlanes:        func() error { stopCalls++; return nil },
		reconcileTrafficBudget: func() error { reconcileCalls++; return nil },
		startProxyPlanes:       func() error { startCalls++; return nil },
		startAfterResume:       func() { afterResumeCalls++ },
	})

	if err := service.SetEnabled(true); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if !IsMaintenanceMode() || stopCalls != 1 {
		t.Fatalf("pause failed: enabled=%v stopCalls=%d", IsMaintenanceMode(), stopCalls)
	}
	if stored, err := service.SettingService.getBool(maintenanceModeSetting); err != nil || !stored {
		t.Fatalf("persisted maintenance value = %v, err=%v; want true", stored, err)
	}

	if err := service.SetEnabled(false); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if IsMaintenanceMode() || startCalls != 1 || reconcileCalls != 1 || afterResumeCalls != 1 {
		t.Fatalf("resume failed: enabled=%v starts=%d reconciles=%d postResume=%d", IsMaintenanceMode(), startCalls, reconcileCalls, afterResumeCalls)
	}
	if stored, err := service.SettingService.getBool(maintenanceModeSetting); err != nil || stored {
		t.Fatalf("persisted maintenance value = %v, err=%v; want false", stored, err)
	}
	if status := service.Status(); status.Enabled || status.Transitioning || status.Error != "" {
		t.Fatalf("unexpected resumed status: %+v", status)
	}
}

func TestMaintenanceModeFailedResumeStaysPersistentlyPaused(t *testing.T) {
	setupMaintenanceTest(t)
	startErr := errors.New("forced core start failure")
	stopCalls := 0
	service := newMaintenanceService(maintenanceHooks{
		stopProxyPlanes:        func() error { stopCalls++; return nil },
		reconcileTrafficBudget: func() error { return nil },
		startProxyPlanes:       func() error { return startErr },
	})
	if err := service.SetEnabled(true); err != nil {
		t.Fatalf("pause: %v", err)
	}

	err := service.SetEnabled(false)
	if !errors.Is(err, startErr) {
		t.Fatalf("resume error = %v, want %v", err, startErr)
	}
	if !IsMaintenanceMode() || stopCalls != 2 {
		t.Fatalf("failed resume did not retain pause: enabled=%v stopCalls=%d", IsMaintenanceMode(), stopCalls)
	}
	if stored, err := service.SettingService.getBool(maintenanceModeSetting); err != nil || !stored {
		t.Fatalf("persisted maintenance value = %v, err=%v; want true", stored, err)
	}
	status := service.Status()
	if !status.Enabled || status.Transitioning || !strings.Contains(status.Error, startErr.Error()) {
		t.Fatalf("failed resume status does not expose the failure: %+v", status)
	}
}

func TestMaintenanceGatesRuntimeWatchdogsAndBudgetButAllowsValidatedSave(t *testing.T) {
	setupMaintenanceTest(t)
	maintenanceModeEnabled.Store(true)

	if err := (&ConfigService{}).StartCore(); err != nil {
		t.Fatalf("StartCore should safely no-op while paused: %v", err)
	}
	if err := (&ConfigService{}).RestartCore(); err != nil {
		t.Fatalf("RestartCore should safely no-op while paused: %v", err)
	}
	if err := (&ConfigService{}).restartCoreWithConfig(json.RawMessage(`{"log":{"level":"info"}}`)); err != nil {
		t.Fatalf("internal restart should safely no-op while paused: %v", err)
	}
	if err := GetTrafficBudgetService().CheckAndEnforce(); err != nil {
		t.Fatalf("traffic budget should no-op while paused: %v", err)
	}

	mieru := NewMieruService()
	if err := mieru.SyncFromDB(); err != nil {
		t.Fatalf("Mieru SyncFromDB should stop/no-op while paused: %v", err)
	}
	mieru.StartWatchdog()
	mieru.mu.Lock()
	watchdogStarted := mieru.watchCancel != nil
	mieru.mu.Unlock()
	if watchdogStarted {
		t.Fatal("Mieru watchdog started while maintenance mode was active")
	}
	failover := NewFailoverService(nil)
	failover.Start()
	failover.mu.Lock()
	failoverStarted := failover.cancel != nil
	failover.mu.Unlock()
	if failoverStarted {
		t.Fatal("failover scheduler started while maintenance mode was active")
	}
	failoverPtr = nil

	configService := &ConfigService{}
	if _, _, err := configService.Save("outbounds", "new", json.RawMessage(`{"type":"direct","tag":"maintenance-save-test"}`), "", "maintenance-test", "localhost"); err != nil {
		t.Fatalf("validated config save should remain available while paused: %v", err)
	}
	var count int64
	if err := database.GetDB().Model(&model.Outbound{}).Where("tag = ?", "maintenance-save-test").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("configuration save did not persist during maintenance; rows=%d", count)
	}
}

func TestMaintenanceResumeSerializesWithConfigurationSave(t *testing.T) {
	setupMaintenanceTest(t)
	if err := (&SettingService{}).setString(maintenanceModeSetting, "true"); err != nil {
		t.Fatal(err)
	}
	maintenanceModeEnabled.Store(true)
	startEntered := make(chan struct{})
	releaseStart := make(chan struct{})
	defer func() {
		select {
		case <-releaseStart:
		default:
			close(releaseStart)
		}
	}()
	service := newMaintenanceService(maintenanceHooks{
		reconcileTrafficBudget: func() error { return nil },
		startProxyPlanes: func() error {
			close(startEntered)
			<-releaseStart
			return nil
		},
	})
	resumeDone := make(chan error, 1)
	go func() { resumeDone <- service.SetEnabled(false) }()
	select {
	case <-startEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("resume did not reach proxy startup")
	}

	saveStarted := make(chan struct{})
	saveDone := make(chan error, 1)
	go func() {
		close(saveStarted)
		_, _, err := (&ConfigService{}).Save("outbounds", "new", json.RawMessage(`{"type":"direct","tag":"maintenance-resume-save-test"}`), "", "test", "localhost")
		saveDone <- err
	}()
	<-saveStarted
	select {
	case err := <-saveDone:
		t.Fatalf("configuration save completed during resume: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	close(releaseStart)
	select {
	case err := <-resumeDone:
		if err != nil {
			t.Fatalf("resume: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resume remained blocked after startup was released")
	}
	select {
	case err := <-saveDone:
		if err != nil {
			t.Fatalf("configuration save after resume: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("configuration save remained blocked after resume")
	}
}

func TestTrafficBudgetKeepsAccountingDuringMaintenanceWithoutResuming(t *testing.T) {
	setupMaintenanceTest(t)
	maintenanceModeEnabled.Store(true)
	oldSupported := trafficBudgetRuntimeSupported
	oldReader := trafficBudgetCounterReader
	oldStop := trafficBudgetStopDataPlane
	oldRestore := trafficBudgetRestoreDataPlane
	t.Cleanup(func() {
		trafficBudgetRuntimeSupported = oldSupported
		trafficBudgetCounterReader = oldReader
		trafficBudgetStopDataPlane = oldStop
		trafficBudgetRestoreDataPlane = oldRestore
	})
	trafficBudgetRuntimeSupported = func() bool { return true }
	var txBytes uint64 = 1000
	trafficBudgetCounterReader = func(string) (string, uint64, uint64, error) {
		return "eth0", 500, txBytes, nil
	}
	stopCalls, restoreCalls := 0, 0
	trafficBudgetStopDataPlane = func() error { stopCalls++; return nil }
	trafficBudgetRestoreDataPlane = func() error { restoreCalls++; return nil }
	for key, value := range map[string]string{
		"trafficBudgetEnabled":         "true",
		"trafficBudgetLimitBytes":      "100",
		"trafficBudgetReserveBytes":    "0",
		"trafficBudgetOffsetBytes":     "0",
		"trafficBudgetAccountingMode":  "tx",
		"trafficBudgetInterface":       "eth0",
		"trafficBudgetCycleDay":        "1",
		"trafficBudgetCycleHour":       "0",
		"trafficBudgetWarningPercent":  "80",
		"trafficBudgetCriticalPercent": "90",
	} {
		if err := (&SettingService{}).setString(key, value); err != nil {
			t.Fatalf("set traffic budget setting %s: %v", key, err)
		}
	}
	budget := GetTrafficBudgetService()
	if err := budget.CheckAndEnforce(); err != nil {
		t.Fatalf("initial accounting while paused: %v", err)
	}
	txBytes += 101
	if err := budget.CheckAndEnforce(); err != nil {
		t.Fatalf("threshold accounting while paused: %v", err)
	}
	status := budget.GetStatusSnapshot()
	state, err := budget.loadTrafficBudgetState(database.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	if status.UsedBytes != 101 || !status.Blocked || !IsTrafficBudgetBlocked() || state.AccumulatedTx != 101 {
		t.Fatalf("budget sampling/threshold did not advance during maintenance: used=%d blocked=%t tx=%d", status.UsedBytes, status.Blocked, state.AccumulatedTx)
	}
	if stopCalls != 1 || restoreCalls != 0 || !IsMaintenanceMode() {
		t.Fatalf("maintenance changed data-plane lifecycle: stops=%d restores=%d paused=%t", stopCalls, restoreCalls, IsMaintenanceMode())
	}
}

func TestMaintenanceWatchdogCheckNoOps(t *testing.T) {
	setupMaintenanceTest(t)
	maintenanceModeEnabled.Store(true)
	service := NewMieruService()
	service.runWatchdogCheck(context.Background())
	if service.watchFailures != 0 || service.active != nil {
		t.Fatalf("watchdog mutated runtime while paused: failures=%d active=%v", service.watchFailures, service.active != nil)
	}
}
