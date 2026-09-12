package service

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
)

func TestTrafficBudgetUsageModesAndCounterReset(t *testing.T) {
	if got := trafficBudgetUsage(TrafficBudgetModeTX, 40, 60); got != 60 {
		t.Fatalf("tx usage = %d, want 60", got)
	}
	if got := trafficBudgetUsage(TrafficBudgetModeBoth, 40, 60); got != 100 {
		t.Fatalf("rx+tx usage = %d, want 100", got)
	}
	if got := trafficBudgetUsage(TrafficBudgetModeMax, 40, 60); got != 60 {
		t.Fatalf("max usage = %d, want 60", got)
	}
	if got := counterDelta(1000, 1200); got != 200 {
		t.Fatalf("regular counter delta = %d, want 200", got)
	}
	if got := counterDelta(1000, 75); got != 75 {
		t.Fatalf("reset counter delta = %d, want 75", got)
	}
}

func TestTrafficBudgetPeriodClampsMonthEnd(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	now := time.Date(2026, time.March, 1, 0, 30, 0, 0, loc)
	start, end := trafficBudgetPeriod(now, 31, 0, loc)
	wantStart := time.Date(2026, time.February, 28, 0, 0, 0, 0, loc)
	wantEnd := time.Date(2026, time.March, 31, 0, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("period = %v -> %v, want %v -> %v", start, end, wantStart, wantEnd)
	}

	january := time.Date(2026, time.January, 31, 12, 0, 0, 0, loc)
	start, end = trafficBudgetPeriod(january, 31, 0, loc)
	wantStart = time.Date(2026, time.January, 31, 0, 0, 0, 0, loc)
	wantEnd = time.Date(2026, time.February, 28, 0, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("January period = %v -> %v, want %v -> %v", start, end, wantStart, wantEnd)
	}
}

func TestTrafficBudgetHardStopAtClientPool(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "traffic-budget.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}

	oldSupported := trafficBudgetRuntimeSupported
	oldReader := trafficBudgetCounterReader
	oldCore, oldMieru, oldMasque := corePtr, mieruPtr, masquePtr
	trafficBudgetRuntimeSupported = func() bool { return true }
	var rx, tx uint64 = 500, 1000
	trafficBudgetCounterReader = func(string) (string, uint64, uint64, error) {
		return "eth0", rx, tx, nil
	}
	corePtr = (*core.Core)(nil)
	mieruPtr, masquePtr = nil, nil
	trafficBudgetBlocked.Store(false)
	trafficBudgetRuntimeState.Store(0)
	t.Cleanup(func() {
		trafficBudgetRuntimeSupported = oldSupported
		trafficBudgetCounterReader = oldReader
		corePtr, mieruPtr, masquePtr = oldCore, oldMieru, oldMasque
		trafficBudgetBlocked.Store(false)
		trafficBudgetRuntimeState.Store(0)
	})

	settings := &SettingService{}
	values := map[string]string{
		"trafficBudgetEnabled":         "true",
		"trafficBudgetLimitBytes":      "500000000000",
		"trafficBudgetReserveBytes":    "50000000000",
		"trafficBudgetOffsetBytes":     "100000000000",
		"trafficBudgetAccountingMode":  "tx",
		"trafficBudgetInterface":       "eth0",
		"trafficBudgetCycleDay":        "1",
		"trafficBudgetCycleHour":       "0",
		"trafficBudgetWarningPercent":  "80",
		"trafficBudgetCriticalPercent": "90",
	}
	for key, value := range values {
		if err := settings.saveSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}

	budget := GetTrafficBudgetService()
	if err := budget.CheckAndEnforce(); err != nil {
		t.Fatal(err)
	}
	status := budget.GetStatus()
	if status.Blocked || status.UsedBytes != 100000000000 || status.ClientPoolBytes != 450000000000 {
		t.Fatalf("unexpected initial status: %#v", status)
	}

	tx += 350000000000
	if err := budget.CheckAndEnforce(); err != nil {
		t.Fatal(err)
	}
	status = budget.GetStatus()
	if !status.Blocked || status.Level != "blocked" || status.UsedBytes != 450000000000 {
		t.Fatalf("hard stop not triggered at pool boundary: %#v", status)
	}
	if !IsTrafficBudgetBlocked() {
		t.Fatal("global traffic budget guard is not blocked")
	}

	if err := settings.saveSetting("trafficBudgetPeriodStart", "1"); err != nil {
		t.Fatal(err)
	}
	if err := budget.CheckAndEnforce(); err != nil {
		t.Fatal(err)
	}
	status = budget.GetStatus()
	if status.Blocked || status.UsedBytes != 0 || status.OffsetBytes != 0 {
		t.Fatalf("new billing period did not clear hard stop: %#v", status)
	}
	if IsTrafficBudgetBlocked() {
		t.Fatal("global traffic budget guard remained blocked after period reset")
	}
}

func TestTrafficBudgetFailsClosedWhenCounterUnavailable(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "traffic-budget-counter-error.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}

	oldSupported := trafficBudgetRuntimeSupported
	oldReader := trafficBudgetCounterReader
	oldCore, oldMieru, oldMasque := corePtr, mieruPtr, masquePtr
	trafficBudgetRuntimeSupported = func() bool { return true }
	trafficBudgetCounterReader = func(string) (string, uint64, uint64, error) {
		return "", 0, 0, errors.New("counter unavailable")
	}
	corePtr = nil
	mieruPtr, masquePtr = nil, nil
	trafficBudgetBlocked.Store(false)
	trafficBudgetRuntimeState.Store(0)
	t.Cleanup(func() {
		trafficBudgetRuntimeSupported = oldSupported
		trafficBudgetCounterReader = oldReader
		corePtr, mieruPtr, masquePtr = oldCore, oldMieru, oldMasque
		trafficBudgetBlocked.Store(false)
		trafficBudgetRuntimeState.Store(0)
	})

	settings := &SettingService{}
	for key, value := range map[string]string{
		"trafficBudgetEnabled": "true", "trafficBudgetLimitBytes": "500000000000",
		"trafficBudgetReserveBytes": "50000000000", "trafficBudgetAccountingMode": "tx",
	} {
		if err := settings.saveSetting(key, value); err != nil {
			t.Fatal(err)
		}
	}

	status := GetTrafficBudgetService().GetStatus()
	if !status.Blocked || status.Level != "error" || !IsTrafficBudgetBlocked() {
		t.Fatalf("counter failure did not fail closed: %#v", status)
	}
}

func TestTrafficBudgetRetriesFailedTransitions(t *testing.T) {
	oldStop, oldRestore := trafficBudgetStopDataPlane, trafficBudgetRestoreDataPlane
	trafficBudgetBlocked.Store(false)
	trafficBudgetRuntimeState.Store(0)
	stopCalls, restoreCalls := 0, 0
	trafficBudgetStopDataPlane = func() error {
		stopCalls++
		if stopCalls == 1 {
			return errors.New("stop failed")
		}
		return nil
	}
	trafficBudgetRestoreDataPlane = func() error {
		restoreCalls++
		if restoreCalls == 1 {
			return errors.New("restore failed")
		}
		return nil
	}
	t.Cleanup(func() {
		trafficBudgetStopDataPlane, trafficBudgetRestoreDataPlane = oldStop, oldRestore
		trafficBudgetBlocked.Store(false)
		trafficBudgetRuntimeState.Store(0)
	})
	service := &TrafficBudgetService{}
	status := TrafficBudgetStatus{Enabled: true, Supported: true, Level: "blocked"}
	if err := service.finishTrafficBudgetStatus(status, true); err == nil {
		t.Fatal("expected first stop transition to fail")
	}
	if err := service.finishTrafficBudgetStatus(status, true); err != nil || stopCalls != 2 || trafficBudgetRuntimeState.Load() != 1 {
		t.Fatalf("blocked transition was not retried: err=%v calls=%d state=%d", err, stopCalls, trafficBudgetRuntimeState.Load())
	}

	status.Level = "normal"
	if err := service.finishTrafficBudgetStatus(status, false); err == nil {
		t.Fatal("expected first restore transition to fail")
	}
	if err := service.finishTrafficBudgetStatus(status, false); err != nil || restoreCalls != 2 || trafficBudgetRuntimeState.Load() != 0 {
		t.Fatalf("restore transition was not retried: err=%v calls=%d state=%d", err, restoreCalls, trafficBudgetRuntimeState.Load())
	}
}
