package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"net/url"
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
	"github.com/CatMsg/NovaPanel/service"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupDataVersionTest(t *testing.T) *ApiService {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := database.InitDB(filepath.Join(t.TempDir(), "data-version.db")); err != nil {
		t.Fatal(err)
	}
	loadDataCache.mu.Lock()
	loadDataCache.entries = make(map[string]apiCacheEntry)
	loadDataCache.mu.Unlock()
	service.NewConfigService(&core.Core{})
	t.Cleanup(func() {
		service.NewConfigService(nil)
		loadDataCache.mu.Lock()
		loadDataCache.entries = make(map[string]apiCacheEntry)
		loadDataCache.mu.Unlock()
		sqlDB, err := database.GetDB().DB()
		if err == nil {
			_ = sqlDB.Close()
		}
	})
	a := &ApiService{}
	if _, err := a.SettingService.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	if err := database.GetDB().Model(&model.Setting{}).Where("key = ?", "subURI").Update("value", "https://old.example/sub/").Error; err != nil {
		t.Fatal(err)
	}
	return a
}

func writeDataVersionTest(t *testing.T, a *ApiService) {
	t.Helper()
	// Independent committed writes do not use Save's snapshot gate. DeletePolicy
	// advances the real service version without calling Save inside a read gate.
	db := database.GetDB()
	if err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.Setting{}).Where("key = ?", "subURI").Update("value", "https://new.example/sub/").Error; err != nil {
			return err
		}
		return tx.Model(&model.Setting{}).Where("key = ?", "outboundFailover").Update("value", `[{"tag":"version-test"}]`).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := (&service.FailoverService{}).DeletePolicy("version-test"); err != nil {
		t.Fatalf("advance independent write version: %v", err)
	}
}

func dataVersionContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", path, nil)
	return c, recorder
}

func interleaveSettingsRead(t *testing.T, a *ApiService) *bool {
	t.Helper()
	fired := false
	db := database.GetDB()
	const callback = "api:test_interleaved_settings_write"
	if err := db.Callback().Query().After("gorm:query").Register(callback, func(tx *gorm.DB) {
		// GetAllSetting has already materialized the old rows. Do not trigger on
		// individual setting lookups or recursively inside Save's snapshot.
		if fired || tx.Statement.Table != "settings" || strings.Contains(tx.Statement.SQL.String(), "WHERE") {
			return
		}
		fired = true
		writeDataVersionTest(t, a)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Query().Remove(callback) })
	return &fired
}

func TestLoadDataInterleavedWriteKeepsReadVersion(t *testing.T) {
	a := setupDataVersionTest(t)
	start := service.CurrentDataVersion()
	fired := interleaveSettingsRead(t, a)
	c, _ := dataVersionContext("http://example.com/api/load")
	result, err := a.getData(c)
	if err != nil {
		t.Fatal(err)
	}
	data := result.(map[string]interface{})
	if !*fired || service.CurrentDataVersion() <= start {
		t.Fatal("write did not advance version during read")
	}
	if data["subURI"] != "https://old.example/sub/" {
		t.Fatalf("expected old snapshot, got %v", data["subURI"])
	}
	if data["lastUpdate"] != start {
		t.Errorf("old payload cursor = %v, want read-start %d (current %d)", data["lastUpdate"], start, service.CurrentDataVersion())
	}
	if _, ok := getCachedLoadData("load:example.com"); ok {
		t.Error("interleaved old snapshot cached as current")
	}
	c, _ = dataVersionContext("http://example.com/api/load?lu=" + strconv.FormatInt(start, 10))
	result, err = a.getData(c)
	if err != nil {
		t.Fatal(err)
	}
	if got := result.(map[string]interface{})["subURI"]; got != "https://new.example/sub/" {
		t.Errorf("immediate refresh subURI = %v, want new snapshot", got)
	}
}

type versionTestMarshaler func() ([]byte, error)

func (f versionTestMarshaler) MarshalJSON() ([]byte, error) { return f() }

func TestLoadDataCacheWriteDuringMarshal(t *testing.T) {
	a := setupDataVersionTest(t)
	start := service.CurrentDataVersion()
	data := map[string]interface{}{
		"lastUpdate": start,
		"subURI": versionTestMarshaler(func() ([]byte, error) {
			writeDataVersionTest(t, a)
			return json.Marshal("https://old.example/sub/")
		}),
	}
	if err := storeCachedLoadData("marshal", data, start); err != nil {
		t.Fatal(err)
	}
	if _, ok := getCachedLoadData("marshal"); ok {
		t.Fatalf("old marshaled payload accepted at current version %d (read-start %d)", service.CurrentDataVersion(), start)
	}
	loadDataCache.mu.RLock()
	_, stored := loadDataCache.entries["marshal"]
	loadDataCache.mu.RUnlock()
	if stored {
		t.Fatal("write during marshal must skip cache insertion")
	}
}

func TestLoadDataStableCacheAndEqualCursor(t *testing.T) {
	a := setupDataVersionTest(t)
	start := service.CurrentDataVersion()
	for i := 0; i < 2; i++ {
		c, _ := dataVersionContext("http://example.com/api/load")
		result, err := a.getData(c)
		if err != nil {
			t.Fatal(err)
		}
		data := result.(map[string]interface{})
		if data["lastUpdate"] != start || data["subURI"] != "https://old.example/sub/" || data["config"] == nil {
			t.Fatalf("stable full/cache response: %+v", data)
		}
		// Mutating a response must not mutate the serialized cache.
		data["subURI"] = "mutated"
	}
	if _, ok := getCachedLoadData("load:example.com"); !ok {
		t.Fatal("stable snapshot was not cached")
	}
	c, _ := dataVersionContext("http://example.com/api/load?lu=" + strconv.FormatInt(start, 10))
	result, err := a.getData(c)
	if err != nil {
		t.Fatal(err)
	}
	data := result.(map[string]interface{})
	if data["lastUpdate"] != start || data["onlines"] == nil {
		t.Fatalf("unchanged response lost cursor/onlines: %+v", data)
	}
	if _, ok := data["config"]; ok {
		t.Fatal("equal cursor should not return full configuration")
	}
}

func TestLoadDataCacheRejectsAlreadyStaleSnapshot(t *testing.T) {
	a := setupDataVersionTest(t)
	start := service.CurrentDataVersion()
	writeDataVersionTest(t, a)
	if err := storeCachedLoadData("stale", map[string]interface{}{"lastUpdate": start, "subURI": "old"}, start); err != nil {
		t.Fatal(err)
	}
	loadDataCache.mu.RLock()
	_, stored := loadDataCache.entries["stale"]
	loadDataCache.mu.RUnlock()
	if stored {
		t.Fatal("already-stale snapshot must not be cached")
	}
}

func TestLoadDataFutureCursorReturnsCurrentFullSnapshot(t *testing.T) {
	a := setupDataVersionTest(t)
	current := service.CurrentDataVersion()
	c, _ := dataVersionContext("http://example.com/api/load?lu=" + strconv.FormatInt(current+1, 10))
	result, err := a.getData(c)
	if err != nil {
		t.Fatal(err)
	}
	data := result.(map[string]interface{})
	if data["lastUpdate"] != current || data["config"] == nil || data["subURI"] != "https://old.example/sub/" {
		t.Fatalf("future cursor must receive a current full snapshot: %+v", data)
	}
}

func TestLoadDataWaitsForSnapshotGate(t *testing.T) {
	testAPIDataWaitsForSnapshotGate(t, false)
}

func TestPartialDataWaitsForSnapshotGate(t *testing.T) {
	testAPIDataWaitsForSnapshotGate(t, true)
}

func testAPIDataWaitsForSnapshotGate(t *testing.T, partial bool) {
	t.Helper()
	var commands *testutil.HostCommandSandbox
	inboundCall := []string{"bash", "scripts/hy2-forward.sh", "apply", "stable-inbound", "12345", "12345", "tcp"}
	if partial {
		commands = testutil.NewManagedPortSandbox(t, inboundCall)
		previousMasque, previousMieru := service.GetMasqueService(), service.GetMieruService()
		service.SetMasqueService(nil)
		service.SetMieruService(nil)
		t.Cleanup(func() {
			service.SetMasqueService(previousMasque)
			service.SetMieruService(previousMieru)
		})
	}
	a := setupDataVersionTest(t)
	start := service.CurrentDataVersion()
	// Synthetic credentials exist only in the temporary test database.
	inbound := model.Inbound{Type: "http", Tag: "stable-inbound", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":12345,"password":"stable-test-password"}`)}
	if err := database.GetDB().Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	client := model.Client{Name: "stable-client", Config: json.RawMessage(`{"password":"stable-test-password"}`), Inbounds: json.RawMessage("[" + strconv.FormatUint(uint64(inbound.Id), 10) + "]")}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	updateRows := func(tx *gorm.DB, temporary bool) error {
		name, tag, clientConfig, inboundOptions, subURI := client.Name, inbound.Tag, client.Config, inbound.Options, "https://old.example/sub/"
		if temporary {
			name, tag, subURI = "temporary-client", "temporary-inbound", "https://temporary.example/sub/"
			clientConfig = json.RawMessage(`{"password":"temporary-test-password"}`)
			inboundOptions = json.RawMessage(`{"listen":"127.0.0.1","listen_port":12345,"password":"temporary-test-password"}`)
		}
		if err := tx.Model(&model.Client{}).Where("id = ?", client.Id).Updates(map[string]interface{}{"name": name, "config": clientConfig}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Inbound{}).Where("id = ?", inbound.Id).Updates(map[string]interface{}{"tag": tag, "options": inboundOptions}).Error; err != nil {
			return err
		}
		return tx.Model(&model.Setting{}).Where("key = ?", "subURI").Update("value", subURI).Error
	}
	committed := make(chan struct{})
	release := make(chan struct{})
	gateDone := make(chan error, 1)
	var releaseOnce sync.Once
	allowRollback := func() { releaseOnce.Do(func() { close(release) }) }
	if partial {
		service.NewConfigService(nil)
		db := database.GetDB()
		const callback = "api:test_partial_save_rows"
		// Stage fixture edits inside the real Save transaction, before its
		// after-image is captured. No nested Save or read gate is acquired.
		if err := db.Callback().Create().After("gorm:create").Register(callback, func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*model.Changes); ok {
				tx.AddError(updateRows(tx.Session(&gorm.Session{NewDB: true}), true))
			}
		}); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })
		firstRestart := true
		service.SetSubServerRestartFunc(func() error {
			if firstRestart {
				firstRestart = false
				close(committed)
				<-release
				return fmt.Errorf("forced partial post-commit failure")
			}
			return nil
		})
		t.Cleanup(func() { service.SetSubServerRestartFunc(nil) })
	}
	go func() {
		if partial {
			_, _, err := a.ConfigService.Save("settings", "edit", json.RawMessage(`{"subDomain":"temporary.example"}`), "", "test", "example.com")
			if err == nil || !strings.Contains(err.Error(), "forced partial post-commit failure") || !strings.Contains(err.Error(), "配置已自动回滚") {
				gateDone <- fmt.Errorf("Save did not fail and compensate: %v", err)
			} else {
				gateDone <- nil
			}
			return
		}
		_, err := a.ConfigService.ReadDataSnapshot(func() (interface{}, error) {
			if err := database.GetDB().Transaction(func(tx *gorm.DB) error { return updateRows(tx, true) }); err != nil {
				return nil, err
			}
			close(committed)
			<-release
			return nil, database.GetDB().Transaction(func(tx *gorm.DB) error { return updateRows(tx, false) })
		})
		gateDone <- err
	}()
	gateFinished := false
	t.Cleanup(func() {
		allowRollback()
		if !gateFinished {
			select {
			case <-gateDone:
			case <-time.After(5 * time.Second):
				t.Error("snapshot gate did not finish")
			}
		}
	})
	select {
	case <-committed:
	case err := <-gateDone:
		gateFinished = true
		t.Fatalf("could not commit temporary state: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("temporary commit timed out")
	}
	if partial {
		c, _ := dataVersionContext("http://example.com/api/details?id=" + strconv.FormatUint(uint64(client.Id), 10))
		data, err := a.getPartialDataSnapshot(c, []string{"clients", "inbounds"})
		if err != nil {
			t.Fatal(err)
		}
		rows := data.(map[string]interface{})
		clients := rows["clients"].(*[]model.Client)
		inbounds := rows["inbounds"].(*[]map[string]interface{})
		if len(*clients) != 1 || (*clients)[0].Name != "temporary-client" || len(*inbounds) != 1 || (*inbounds)[0]["password"] != "temporary-test-password" || service.CurrentDataVersion() != start {
			t.Fatalf("fixture did not reproduce temporary details: clients=%+v inbounds=%+v version=%d start=%d", *clients, *inbounds, service.CurrentDataVersion(), start)
		}
		t.Log("ungated partial details observed temporary client and inbound credentials before failed Save compensation")
	}
	type readResult struct {
		data interface{}
		err  error
	}
	readDone := make(chan readResult, 1)
	readStarted := make(chan struct{})
	readFinished := false
	go func() {
		c, _ := dataVersionContext("http://example.com/api/load")
		close(readStarted)
		var data interface{}
		var err error
		if partial {
			c, recorder := dataVersionContext("http://example.com/api/details?id=" + strconv.FormatUint(uint64(client.Id), 10))
			err = a.LoadPartialData(c, []string{"clients", "inbounds"})
			if err == nil {
				var response struct {
					Obj map[string]interface{} `json:"obj"`
				}
				err = json.Unmarshal(recorder.Body.Bytes(), &response)
				data = response.Obj
			}
		} else {
			data, err = a.getData(c)
		}
		readDone <- readResult{data, err}
	}()
	t.Cleanup(func() {
		allowRollback()
		if !readFinished {
			select {
			case <-readDone:
			case <-time.After(5 * time.Second):
				t.Error("load did not finish after releasing gate")
			}
		}
	})
	<-readStarted
	select {
	case result := <-readDone:
		readFinished = true
		t.Fatalf("load escaped snapshot gate: %+v", result)
	case <-time.After(50 * time.Millisecond):
	}
	if partial {
		commands.AssertCalls(t, nil)
	}
	allowRollback()
	select {
	case err := <-gateDone:
		gateFinished = true
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("rollback timed out")
	}
	select {
	case result := <-readDone:
		readFinished = true
		if result.err != nil {
			t.Fatal(result.err)
		}
		data := result.data.(map[string]interface{})
		if partial {
			if _, hasCursor := data["lastUpdate"]; hasCursor {
				t.Fatal("partial details must not advance full-load cursor")
			}
			clients := data["clients"].([]interface{})
			inbounds := data["inbounds"].([]interface{})
			if len(clients) != 1 || len(inbounds) != 1 {
				t.Fatalf("partial details missing restored rows: %+v", data)
			}
			clientData := clients[0].(map[string]interface{})
			inboundData := inbounds[0].(map[string]interface{})
			if clientData["name"] != client.Name || clientData["config"].(map[string]interface{})["password"] != "stable-test-password" || inboundData["tag"] != inbound.Tag || inboundData["password"] != "stable-test-password" {
				t.Fatalf("partial details exposed temporary rows: %+v", data)
			}
			t.Log("gated partial details observed restored client and inbound credentials after failed Save compensation")
		} else if data["subURI"] != "https://old.example/sub/" || data["lastUpdate"] != start {
			t.Fatalf("full load returned temporary state: %+v", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("load did not finish after rollback")
	}
	if partial {
		var wantCalls [][]string
		if runtime.GOOS == "linux" {
			wantCalls = testutil.ManagedPortRebuildCalls(inboundCall)
			if testutil.SystemdPresent() {
				wantCalls = append(wantCalls, []string{"bash", "scripts/login-guard.sh", "sync", "2095", ""})
			}
		}
		commands.AssertCalls(t, wantCalls)
		if service.CurrentDataVersion() != start {
			t.Fatalf("failed Save advanced version: got %d, want %d", service.CurrentDataVersion(), start)
		}
	}
}

func TestPartialDataDoesNotAdvanceFullLoadCursor(t *testing.T) {
	a := setupDataVersionTest(t)
	fired := interleaveSettingsRead(t, a)
	c, recorder := dataVersionContext("http://example.com/api/settings")
	if err := a.LoadPartialData(c, []string{"settings"}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Success bool `json:"success"`
		Obj     struct {
			LastUpdate *int64            `json:"lastUpdate"`
			Settings   map[string]string `json:"settings"`
		} `json:"obj"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !*fired || !response.Success || response.Obj.Settings["subURI"] != "https://old.example/sub/" {
		t.Fatalf("partial snapshot did not reproduce interleaving: %+v", response)
	}
	if response.Obj.LastUpdate != nil {
		t.Fatalf("partial response must not certify a full snapshot: cursor=%d", *response.Obj.LastUpdate)
	}
}

func TestSaveResponseDoesNotAdvanceFullLoadCursor(t *testing.T) {
	a := setupDataVersionTest(t)
	c, recorder := dataVersionContext("http://example.com/api/save")
	c.Request = httptest.NewRequest("POST", "http://example.com/api/save", strings.NewReader(url.Values{
		"object": {"settings"}, "action": {"edit"}, "data": {`{"subURI":"https://old.example/sub/"}`},
	}.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// A no-op settings Save exercises the response without scheduling a restart.
	a.Save(c, "test")
	var response struct {
		Success bool `json:"success"`
		Obj     struct {
			LastUpdate *int64            `json:"lastUpdate"`
			Settings   map[string]string `json:"settings"`
		} `json:"obj"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Obj.LastUpdate != nil || response.Obj.Settings["subURI"] != "https://old.example/sub/" {
		t.Fatalf("Save must return partial settings without a full-load cursor: %+v", response)
	}
}

func TestChangedSaveResponseDoesNotAdvanceFullLoadCursor(t *testing.T) {
	a := setupDataVersionTest(t)
	start := service.CurrentDataVersion()
	c, recorder := dataVersionContext("http://example.com/api/save")
	c.Request = httptest.NewRequest("POST", "http://example.com/api/save", strings.NewReader(url.Values{
		"object": {"outbounds"}, "action": {"new"}, "data": {`{"type":"direct","tag":"extra"}`},
	}.Encode()))
	c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	service.NewConfigService(nil)
	a.Save(c, "test")
	var response struct {
		Success bool `json:"success"`
		Obj     struct {
			LastUpdate *int64 `json:"lastUpdate"`
			Outbounds  []struct {
				Tag string `json:"tag"`
			} `json:"outbounds"`
		} `json:"obj"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Success || response.Obj.LastUpdate != nil || service.CurrentDataVersion() <= start {
		t.Fatalf("changed Save must return partial objects without full cursor: %s", recorder.Body.String())
	}
	for _, outbound := range response.Obj.Outbounds {
		if outbound.Tag == "extra" {
			return
		}
	}
	t.Fatal("changed Save response missing saved outbound")
}
