package service

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/internal/testutil"
)

func TestReadDataSnapshotUsesSaveGateAndReleasesOnError(t *testing.T) {
	s := &ConfigService{}
	wantErr := errors.New("read failed")
	_, err := s.ReadDataSnapshot(func() (interface{}, error) {
		if saveConfigMu.TryLock() {
			saveConfigMu.Unlock()
			t.Error("snapshot callback did not hold Save's gate")
		}
		return nil, wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("read error = %v, want %v", err, wantErr)
	}
	if !saveConfigMu.TryLock() {
		t.Fatal("read error leaked Save's gate")
	}
	saveConfigMu.Unlock()
}

func TestReadDataSnapshotWaitsForFailedSaveCompensation(t *testing.T) {
	commands := testutil.NewManagedPortSandbox(t)
	if err := database.InitDB(filepath.Join(t.TempDir(), "read-save-gate.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	t.Cleanup(func() {
		sqlDB, err := db.DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	previousCore, previousMasque, previousMieru := corePtr, masquePtr, mieruPtr
	corePtr, masquePtr, mieruPtr = nil, nil, nil
	previousRestart := subServerRestartFunc
	t.Cleanup(func() {
		corePtr, masquePtr, mieruPtr = previousCore, previousMasque, previousMieru
		subServerRestartFunc = previousRestart
	})
	s := &ConfigService{}
	if _, err := s.SettingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	initialVersion := CurrentDataVersion()
	committed := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	allowFailure := func() { releaseOnce.Do(func() { close(release) }) }
	saveDone := make(chan error, 1)
	firstRestart := true
	subServerRestartFunc = func() error {
		if firstRestart {
			firstRestart = false
			close(committed)
			<-release
			return errors.New("forced post-commit failure")
		}
		return nil
	}
	go func() {
		_, _, err := s.Save("settings", "edit", json.RawMessage(`{"subDomain":"temporary.example"}`), "", "test", "example.com")
		saveDone <- err
	}()
	var saveFinished bool
	t.Cleanup(func() {
		allowFailure()
		if !saveFinished {
			select {
			case <-saveDone:
			case <-time.After(5 * time.Second):
				t.Error("Save did not finish after releasing post-commit hook")
			}
		}
	})
	select {
	case <-committed:
	case <-time.After(5 * time.Second):
		t.Fatal("Save did not reach post-commit hook")
	}
	// This is the original ungated behavior: committed but unaccepted data is
	// visible while the global version is still unchanged.
	if domain, err := s.SettingService.GetSubDomain(); err != nil || domain != "temporary.example" || CurrentDataVersion() != initialVersion {
		t.Fatalf("failed to reproduce dirty read: domain=%q err=%v version=%d", domain, err, CurrentDataVersion())
	}
	t.Log("ungated read observed temporary.example at unchanged version")
	type readResult struct {
		data interface{}
		err  error
	}
	readDone := make(chan readResult, 1)
	readStarted := make(chan struct{})
	readFinished := false
	go func() {
		close(readStarted)
		data, err := s.ReadDataSnapshot(func() (interface{}, error) { return s.SettingService.GetSubDomain() })
		readDone <- readResult{data, err}
	}()
	t.Cleanup(func() {
		allowFailure()
		if !readFinished {
			select {
			case <-readDone:
			case <-time.After(5 * time.Second):
				t.Error("snapshot read did not finish during cleanup")
			}
		}
	})
	<-readStarted
	select {
	case result := <-readDone:
		readFinished = true
		t.Fatalf("snapshot read escaped pending Save: %+v", result)
	case <-time.After(50 * time.Millisecond):
	}
	commands.AssertCalls(t, nil)
	allowFailure()
	select {
	case err := <-saveDone:
		saveFinished = true
		if err == nil || !strings.Contains(err.Error(), "forced post-commit failure") || !strings.Contains(err.Error(), "配置已自动回滚") {
			t.Fatalf("Save did not fail and compensate: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Save compensation timed out")
	}
	select {
	case result := <-readDone:
		readFinished = true
		if result.err != nil || result.data != "" || CurrentDataVersion() != initialVersion {
			t.Fatalf("snapshot did not read compensated state: %+v version=%d", result, CurrentDataVersion())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("snapshot read did not finish after compensation")
	}
	var wantCalls [][]string
	if runtime.GOOS == "linux" {
		wantCalls = testutil.ManagedPortRebuildCalls()
		if testutil.SystemdPresent() {
			wantCalls = append(wantCalls, []string{"bash", "scripts/login-guard.sh", "sync", "2095", ""})
		}
	}
	commands.AssertCalls(t, wantCalls)
	t.Log("gated read observed restored domain after failed Save, at unchanged version")
}

func TestCheckChangesComparesVersionIdentity(t *testing.T) {
	previous := lastUpdate.Load()
	t.Cleanup(func() { lastUpdate.Store(previous) })
	const current int64 = 1000
	lastUpdate.Store(current)
	s := &ConfigService{}
	for _, test := range []struct {
		name    string
		cursor  string
		changed bool
		wantErr bool
	}{
		{name: "same", cursor: strconv.FormatInt(current, 10)},
		{name: "old", cursor: strconv.FormatInt(current-1, 10), changed: true},
		{name: "future", cursor: strconv.FormatInt(current+1, 10), changed: true},
		{name: "empty", changed: true},
		{name: "bad", cursor: "invalid", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed, err := s.CheckChanges(test.cursor)
			if changed != test.changed || (err != nil) != test.wantErr {
				t.Fatalf("CheckChanges(%q) = (%v, %v), want changed=%v error=%v", test.cursor, changed, err, test.changed, test.wantErr)
			}
		})
	}
}

func TestDataVersionIsMonotonicUnderConcurrentUpdates(t *testing.T) {
	lastUpdate.Store(0)
	initial := CurrentDataVersion()

	const updates = 32
	var waitGroup sync.WaitGroup
	waitGroup.Add(updates)
	for range updates {
		go func() {
			defer waitGroup.Done()
			markDataUpdated()
		}()
	}
	waitGroup.Wait()

	if got := CurrentDataVersion(); got <= initial {
		t.Fatalf("version = %d, want greater than %d", got, initial)
	}
}
