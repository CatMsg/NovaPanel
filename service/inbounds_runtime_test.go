package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/internal/testutil"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func setupInboundRuntimeTest(t *testing.T, hostCalls ...[]string) (*gorm.DB, *InboundService) {
	t.Helper()
	var bashCalls [][]string
	for _, call := range hostCalls {
		if call[0] == "bash" {
			bashCalls = append(bashCalls, call[1:])
		}
	}
	commands := testutil.NewHostCommandSandbox(t, bashCalls...)
	t.Cleanup(func() {
		var wantCalls [][]string
		if runtime.GOOS == "linux" {
			wantCalls = hostCalls
		}
		commands.AssertCalls(t, wantCalls)
	})
	logger.InitLogger(logging.ERROR)
	if err := database.InitDB(filepath.Join(t.TempDir(), "inbound-runtime.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
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

func inboundRuntimeForwardCall(t *testing.T, action string, inbound model.Inbound) []string {
	t.Helper()
	var options struct {
		ListenPort int `json:"listen_port"`
	}
	if err := json.Unmarshal(inbound.Options, &options); err != nil {
		t.Fatal(err)
	}
	port := strconv.Itoa(options.ListenPort)
	protocols := "tcp"
	if action == "remove" {
		protocols = "tcp,udp"
	}
	return []string{"bash", "scripts/hy2-forward.sh", action, inbound.Tag, port, port, protocols}
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
	inbound := model.Inbound{Type: "naive", Tag: "empty-naive", Options: inboundTestOptions(t)}
	db, svc := setupInboundRuntimeTest(t, inboundRuntimeForwardCall(t, "apply", inbound))
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

func TestInboundRuntimeMieruPortHolder(t *testing.T) {
	address := os.Getenv("NOVAS_TEST_MIERU_PORT_HOLDER")
	if address == "" {
		return
	}
	listener, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	select {}
}

func startInboundRuntimeMieruPortHolder(t *testing.T, address string) *mieruRuntime {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestInboundRuntimeMieruPortHolder$")
	command.Env = append(os.Environ(), "NOVAS_TEST_MIERU_PORT_HOLDER="+address)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		t.Fatalf("start Mieru port holder: %v", err)
	}
	runtimeState := &mieruRuntime{cmd: command, done: make(chan error, 1)}
	runtimeState.running.Store(true)
	go func() {
		waitErr := command.Wait()
		runtimeState.running.Store(false)
		runtimeState.done <- waitErr
		close(runtimeState.done)
	}()
	t.Cleanup(func() { stopMieruRuntime(runtimeState) })

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		connection, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
		if err == nil {
			_ = connection.Close()
			return runtimeState
		}
		select {
		case waitErr := <-runtimeState.done:
			t.Fatalf("Mieru port holder exited before listening: %v", waitErr)
		default:
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("Mieru port holder did not listen on %s", address)
	return nil
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

func setupInboundRuntimeMASQUEEndpoint(t *testing.T, db *gorm.DB) model.Inbound {
	t.Helper()
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	masque := model.Inbound{
		Type:    "masque",
		Tag:     "same-port-protocol-conversion",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{}`),
		Options: json.RawMessage(fmt.Sprintf(`{"server":"localhost","listen":"127.0.0.1","listen_port":%d,"version":[3],"system":false,"mtu":1280,"address":["172.28.0.1/24"]}`, port)),
	}
	if err := prepareMasqueInbound(db, &masque, nil, "localhost"); err != nil {
		t.Fatalf("prepare MASQUE inbound: %v", err)
	}
	if err := db.Create(&masque).Error; err != nil {
		t.Fatalf("create MASQUE inbound: %v", err)
	}
	client := model.Client{
		Enable:   true,
		Name:     "same-port-conversion-user",
		Config:   json.RawMessage(`{"masque":{"username":"same-port-conversion-user","password":"masque-test-password"},"shadowsocks":{"password":"shadowsocks-test-password"}}`),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, masque.Id)),
		Links:    json.RawMessage(`[]`),
		History:  json.RawMessage(`[]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create protocol-conversion user: %v", err)
	}

	previousMasque := masquePtr
	masqueService := NewMasqueService()
	masquePtr = masqueService
	t.Cleanup(func() {
		if err := masqueService.Stop(); err != nil {
			t.Errorf("stop test MASQUE service: %v", err)
		}
		masquePtr = previousMasque
	})
	if err := masqueService.SyncFromDB(); err != nil {
		if strings.Contains(err.Error(), "QUIC is not included in this build") {
			t.Skip("MASQUE runtime test requires the with_quic build tag")
		}
		t.Fatalf("start MASQUE endpoint: %v", err)
	}
	if _, exists := corePtr.GetInstance().Endpoint().Get(masque.Tag); !exists {
		t.Fatal("MASQUE endpoint did not acquire its UDP port")
	}
	return masque
}

func TestInboundRuntimeSaveMASQUEToShadowsocksOnSamePort(t *testing.T) {
	db, _ := setupInboundRuntimeTest(t)
	masque := setupInboundRuntimeMASQUEEndpoint(t, db)
	masqueConfig, err := parseMasqueInbound(&masque)
	if err != nil {
		t.Fatal(err)
	}
	configService := &ConfigService{}
	if err := configService.SettingService.SetConfig(`{"log":{"disabled":true},"route":{"final":"direct"}}`); err != nil {
		t.Fatalf("set runtime config: %v", err)
	}
	updated := model.Inbound{
		Id:   masque.Id,
		Type: "shadowsocks",
		Tag:  masque.Tag,
		Options: json.RawMessage(fmt.Sprintf(
			`{"listen":"127.0.0.1","listen_port":%d,"method":"aes-128-gcm"}`,
			masqueConfig.Port,
		)),
	}
	_, changed, err := configService.Save("inbounds", "edit", inboundRuntimePayload(t, updated), "", "test", "localhost")
	if err != nil || !changed {
		t.Fatalf("convert MASQUE to Shadowsocks on the same port: changed=%v err=%v", changed, err)
	}
	assertInboundRuntimeTag(t, masque.Tag, true)
	if _, exists := corePtr.GetInstance().Endpoint().Get(masque.Tag); exists {
		t.Fatal("old MASQUE endpoint still owns the converted port")
	}
	var saved model.Inbound
	if err := db.First(&saved, masque.Id).Error; err != nil {
		t.Fatalf("load converted inbound: %v", err)
	}
	if saved.Type != "shadowsocks" {
		t.Fatalf("saved inbound type = %q, want shadowsocks", saved.Type)
	}
}

func TestInboundRuntimeSaveMieruToMixedOnSamePort(t *testing.T) {
	db, _ := setupInboundRuntimeTest(t)
	probe, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("allocate Mieru test port: %v", err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	portAddress := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
	holder := startInboundRuntimeMieruPortHolder(t, portAddress)

	inbound := model.Inbound{
		Type: "mieru",
		Tag:  "same-port-mieru-conversion",
		Options: json.RawMessage(fmt.Sprintf(
			`{"listen":"0.0.0.0","listen_port":%d,"transport":"TCP"}`,
			port,
		)),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create Mieru inbound: %v", err)
	}
	client := model.Client{
		Enable:   true,
		Name:     "same-port-mieru-user",
		Config:   json.RawMessage(`{"mieru":{"password":"mieru-test-password"},"mixed":{"username":"test-user","password":"test-pass"}}`),
		Inbounds: json.RawMessage(fmt.Sprintf(`[%d]`, inbound.Id)),
		Links:    json.RawMessage(`[]`),
		History:  json.RawMessage(`[]`),
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create Mieru user: %v", err)
	}
	previousMieru := mieruPtr
	mieru := NewMieruService()
	mieru.active = holder
	mieru.total = 1
	mieru.inboundTag = inbound.Tag
	mieruPtr = mieru
	core.SetMieruBridgeInboundTag(inbound.Tag)
	t.Cleanup(func() {
		core.SetMieruBridgeInboundTag("")
		mieruPtr = previousMieru
	})
	bridgeConfig, err := buildMieruBridgeInboundFromDB(db, &inbound)
	if err != nil {
		t.Fatalf("build old Mieru routing bridge: %v", err)
	}
	if err := corePtr.AddInbound(bridgeConfig); err != nil {
		t.Fatalf("start old Mieru routing bridge: %v", err)
	}

	configService := &ConfigService{}
	if err := configService.SettingService.SetConfig(`{"log":{"disabled":true},"route":{"final":"direct"}}`); err != nil {
		t.Fatalf("set runtime config: %v", err)
	}
	updated := model.Inbound{
		Id:      inbound.Id,
		Type:    "mixed",
		Tag:     inbound.Tag,
		Options: json.RawMessage(fmt.Sprintf(`{"listen":"0.0.0.0","listen_port":%d}`, port)),
	}
	_, changed, err := configService.Save("inbounds", "edit", inboundRuntimePayload(t, updated), "", "test", "localhost")
	if err != nil || !changed {
		t.Fatalf("convert Mieru to mixed on the same port: changed=%v err=%v", changed, err)
	}
	if mieru.active != nil || holder.running.Load() {
		t.Fatal("Mieru listener remained active after conversion")
	}
	assertInboundRuntimeTag(t, inbound.Tag, true)
	var saved model.Inbound
	if err := db.First(&saved, inbound.Id).Error; err != nil {
		t.Fatalf("load converted inbound: %v", err)
	}
	if saved.Type != "mixed" {
		t.Fatalf("saved inbound type = %q, want mixed", saved.Type)
	}
}

func TestInboundRuntimeFailedMASQUEConversionRestoresEndpoint(t *testing.T) {
	db, _ := setupInboundRuntimeTest(t)
	masque := setupInboundRuntimeMASQUEEndpoint(t, db)
	masqueConfig, err := parseMasqueInbound(&masque)
	if err != nil {
		t.Fatal(err)
	}
	occupiedTCP, err := net.Listen("tcp4", fmt.Sprintf("127.0.0.1:%d", masqueConfig.Port))
	if err != nil {
		t.Fatalf("occupy TCP side of MASQUE port: %v", err)
	}
	defer occupiedTCP.Close()

	configService := &ConfigService{}
	if err := configService.SettingService.SetConfig(`{"log":{"disabled":true},"route":{"final":"direct"}}`); err != nil {
		t.Fatalf("set runtime config: %v", err)
	}
	updated := model.Inbound{
		Id:   masque.Id,
		Type: "shadowsocks",
		Tag:  masque.Tag,
		Options: json.RawMessage(fmt.Sprintf(
			`{"listen":"127.0.0.1","listen_port":%d,"method":"aes-128-gcm"}`,
			masqueConfig.Port,
		)),
	}
	_, changed, err := configService.Save("inbounds", "edit", inboundRuntimePayload(t, updated), "", "test", "localhost")
	if err == nil || changed || !strings.Contains(err.Error(), "address already in use") || !strings.Contains(err.Error(), "配置已自动回滚") {
		t.Fatalf("expected bind failure with automatic rollback: changed=%v err=%v", changed, err)
	}
	if _, exists := corePtr.GetInstance().Endpoint().Get(masque.Tag); !exists {
		t.Fatal("failed conversion did not restore the original MASQUE endpoint")
	}
	var saved model.Inbound
	if err := db.First(&saved, masque.Id).Error; err != nil {
		t.Fatalf("load rolled-back inbound: %v", err)
	}
	if saved.Type != "masque" {
		t.Fatalf("failed conversion left inbound type %q, want masque", saved.Type)
	}
	probe, err := net.ListenPacket("udp4", fmt.Sprintf("127.0.0.1:%d", masqueConfig.Port))
	if err == nil {
		_ = probe.Close()
		t.Fatal("restored MASQUE endpoint did not reacquire its UDP port")
	}
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
				inbound := model.Inbound{Type: protocol, Tag: "original", Options: inboundTestOptions(t, protocol)}
				renamed := inbound
				renamed.Tag = "renamed"
				db, svc := setupInboundRuntimeTest(t,
					inboundRuntimeForwardCall(t, "apply", inbound),
					inboundRuntimeForwardCall(t, "remove", inbound),
					inboundRuntimeForwardCall(t, "apply", renamed),
				)
				client := seedInboundRuntimeClient(t, db, 999, enabled)
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
			inbound := model.Inbound{Type: "naive", Tag: "before", Options: inboundTestOptions(t)}
			db, svc := setupInboundRuntimeTest(t, testutil.ManagedPortRebuildCalls(inboundRuntimeForwardCall(t, "apply", inbound))...)
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

func TestInboundRuntimeSaveCompensationFailureIsReported(t *testing.T) {
	inbound := model.Inbound{Type: "mixed", Tag: "before-compensation-failure", Options: inboundTestOptions(t, "mixed")}
	db, svc := setupInboundRuntimeTest(t, testutil.ManagedPortRebuildCalls(inboundRuntimeForwardCall(t, "apply", inbound))...)
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.RestartInbounds(db, []uint{inbound.Id}); err != nil {
		t.Fatal(err)
	}
	configSvc := &ConfigService{}
	if err := configSvc.SettingService.SetConfig(`{"log":{"disabled":true},"route":{"final":"direct"}}`); err != nil {
		t.Fatal(err)
	}
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	updated := inbound
	updated.Tag = "failed-compensation-rename"
	updated.Options = json.RawMessage(fmt.Sprintf(`{"listen":"127.0.0.1","listen_port":%d}`, busy.Addr().(*net.TCPAddr).Port))
	injected := errors.New("injected compensation config read failure")
	compensationReads := 0
	const callback = "test:inbound_compensation_config_failure"
	if err := db.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) {
		_, inTransaction := tx.Statement.ConnPool.(*sql.Tx)
		// Preflight reads are transactional; only compensation reloads config
		// after the real socket bind failure and managed-port restoration.
		if !inTransaction && tx.Statement.Table == "settings" {
			where := tx.Statement.Clauses["WHERE"].Expression
			if reflect.DeepEqual(where, clause.Where{Exprs: []clause.Expression{clause.Expr{SQL: "key = ?", Vars: []interface{}{"config"}}}}) {
				compensationReads++
				tx.AddError(injected)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Callback().Query().Remove(callback) })
	_, changed, err := configSvc.Save("inbounds", "edit", inboundRuntimePayload(t, updated), "", "test", "localhost")
	if err == nil || changed || compensationReads != 1 || !strings.Contains(err.Error(), "address already in use") || !strings.Contains(err.Error(), "保存补偿失败") || !strings.Contains(err.Error(), injected.Error()) || strings.Contains(err.Error(), "配置已自动回滚") {
		t.Fatalf("expected bind failure and explicit compensation failure: changed=%v reads=%d err=%v", changed, compensationReads, err)
	}
	var stored model.Inbound
	if err := db.First(&stored, inbound.Id).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Tag != inbound.Tag || stored.Type != inbound.Type || !equalJSONBytes(stored.Options, inbound.Options) {
		t.Fatalf("database before-image not restored: %+v", stored)
	}
	assertInboundRuntimeColdConfig(t, db, svc, inbound.Tag, true)
	assertInboundRuntimeTag(t, updated.Tag, false)
}

func TestInboundRuntimeRollbackUsesSnapshotAfterDelete(t *testing.T) {
	inbound := model.Inbound{Type: "naive", Tag: "before-delete", Options: inboundTestOptions(t)}
	db, svc := setupInboundRuntimeTest(t, inboundRuntimeForwardCall(t, "remove", inbound))
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
