package api

import (
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"
	sbLog "github.com/sagernet/sing-box/log"
)

func TestMaintenanceStoppedCoreDoesNotReportDebugAsError(t *testing.T) {
	a := setupDataVersionTest(t)
	debugMessage := "inbound/tuic[maintenance-test]: listener closed: context canceled"
	core.PlatformWriter{}.WriteMessage(sbLog.LevelDebug, debugMessage)
	c, _ := dataVersionContext("/api/load?lu=0")
	result, err := a.getData(c)
	if err != nil {
		t.Fatal(err)
	}
	data := result.(map[string]interface{})
	if message, _ := data["lastLog"].(string); strings.Contains(message, debugMessage) {
		t.Fatalf("normal DEBUG shutdown was exposed as a core error: %s", message)
	}

	// A cached full snapshot must not retain an old DEBUG-derived toast either.
	key := "load:" + getHostname(c)
	cached, ok := getCachedLoadData(key)
	if !ok {
		t.Fatal("full snapshot was not cached")
	}
	cached["lastLog"] = debugMessage
	if err := storeCachedLoadData(key, cached, service.CurrentDataVersion()); err != nil {
		t.Fatal(err)
	}
	c, _ = dataVersionContext("/api/load?lu=0")
	result, err = a.getData(c)
	if err != nil {
		t.Fatal(err)
	}
	if message, _ := result.(map[string]interface{})["lastLog"].(string); strings.Contains(message, debugMessage) {
		t.Fatalf("cached DEBUG shutdown was exposed as a core error: %s", message)
	}
}

func TestMaintenanceStoppedCoreStillReportsStartupError(t *testing.T) {
	a := setupDataVersionTest(t)
	startupError := "start sing-box err: listen tcp 127.0.0.1:12345: bind: address already in use"
	logger.Error(startupError)
	core.PlatformWriter{}.WriteMessage(sbLog.LevelDebug, "listener closed: context canceled")
	c, _ := dataVersionContext("/api/load?lu=0")
	result, err := a.getData(c)
	if err != nil {
		t.Fatal(err)
	}
	message, _ := result.(map[string]interface{})["lastLog"].(string)
	if !strings.Contains(message, startupError) || !strings.Contains(message, "ERROR") {
		t.Fatalf("real startup error was lost behind DEBUG shutdown: %q", message)
	}
}

func TestMaintenanceResumedCoreDropsCachedError(t *testing.T) {
	a := setupDataVersionTest(t)
	logger.Error("synthetic old startup error before maintenance resume")
	c, _ := dataVersionContext("/api/load?lu=0")
	if _, err := a.getData(c); err != nil {
		t.Fatal(err)
	}
	runtime := core.NewCore()
	service.NewConfigService(runtime)
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Errorf("stop temporary core: %v", err)
		}
	})
	if err := runtime.Start([]byte(`{"log":{"disabled":true},"outbounds":[{"type":"direct","tag":"direct"}]}`)); err != nil {
		t.Fatal(err)
	}
	c, _ = dataVersionContext("/api/load?lu=0")
	result, err := a.getData(c)
	if err != nil {
		t.Fatal(err)
	}
	if message, exists := result.(map[string]interface{})["lastLog"]; exists {
		t.Fatalf("running core retained a cached error toast: %v", message)
	}
}
