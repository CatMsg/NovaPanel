package service

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/internal/testutil"
)

func initFleetInboundTestDB(t *testing.T) {
	t.Helper()
	if err := database.InitDB(filepath.Join(t.TempDir(), "fleet-inbound.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	previousCore := corePtr
	NewConfigService(&core.Core{})
	t.Cleanup(func() { corePtr = previousCore })
}

func addFleetInboundTestClient(t *testing.T, name string, enabled bool, config string) model.Client {
	t.Helper()
	client := model.Client{
		Enable:   enabled,
		Name:     name,
		Config:   json.RawMessage(config),
		Inbounds: json.RawMessage(`[]`),
		Links:    json.RawMessage(`[]`),
		History:  json.RawMessage(`[]`),
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatal(err)
	}
	return client
}

func fleetInboundRequest(t *testing.T, draft string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(FleetInboundRemoteRequest{InboundDraft: json.RawMessage(draft)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestFleetInboundTagUsesTargetAndProtocolAndAvoidsCollisions(t *testing.T) {
	if got := fleetInboundTargetTagForProtocol(FleetServer{ID: "la", Name: "LA"}, "hysteria", false, nil); got != "la-hy" {
		t.Fatalf("Hysteria tag = %q, want la-hy", got)
	}
	if got := fleetInboundTargetTagForProtocol(FleetServer{ID: "tk", Name: "TK"}, "hysteria2", false, nil); got != "tk-hy2" {
		t.Fatalf("Hysteria2 tag = %q, want tk-hy2", got)
	}
	if got := fleetInboundTargetTagForProtocol(FleetServer{ID: "kr", Name: "KR"}, "hysteria2", false, []string{"kr-hy2"}); got != "kr-hy2-2" {
		t.Fatalf("colliding tag = %q, want kr-hy2-2", got)
	}
	if got := fleetInboundTargetTagForProtocol(FleetServer{ID: "kr2", Name: "KR"}, "hysteria2", true, nil); got != "kr-kr2-hy2" {
		t.Fatalf("duplicate server-name tag = %q, want kr-kr2-hy2", got)
	}
}

func TestFleetInboundTLSRequirementByProtocol(t *testing.T) {
	tests := []struct {
		name    string
		typeID  string
		options string
		want    bool
		wantErr bool
	}{
		{name: "AnyTLS", typeID: "anytls", want: true},
		{name: "Hysteria2", typeID: "hysteria2", want: true},
		{name: "HTTP 1", typeID: "http", options: `{"version":1}`, want: false},
		{name: "HTTP 2", typeID: "http", options: `{"version":2}`, want: true},
		{name: "HTTP missing version", typeID: "http", options: `{}`, wantErr: true},
		{name: "SOCKS", typeID: "socks", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inbound := &model.Inbound{Type: tt.typeID, Options: json.RawMessage(tt.options)}
			got, err := fleetInboundTLSRequired(inbound)
			if (err != nil) != tt.wantErr {
				t.Fatalf("fleetInboundTLSRequired() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("fleetInboundTLSRequired() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFleetInboundPayloadIsBuiltFromFreshDraftOnly(t *testing.T) {
	draft := json.RawMessage(`{
		"id":77,"type":"anytls","tag":"cn2-source","tls_id":99,
		"listen":"127.0.0.1","listen_port":443,
		"tls":{"private_key":"cn2-private-key"},
		"users":[{"username":"cn2-user","password":"cn2-user-password"}],
		"password":"cn2-fixed-password",
		"addrs":[{"server":"cn2.example","server_port":443,"tls":{"server_name":"cn2.example"}}],
		"out_json":{"token":"cn2-outbound-secret"},"padding_scheme":["stop=8"]
	}`)
	request, err := buildFleetInboundRemotePayload(draft, "la.example", "la-anytls", 7443, 8)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(request.InboundDraft, &payload); err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"id", "tls", "users", "password", "out_json"} {
		if _, exists := payload[forbidden]; exists {
			t.Fatalf("fresh target payload retained forbidden field %q: %s", forbidden, request.InboundDraft)
		}
	}
	var tag, listen string
	var port, tlsID int
	_ = json.Unmarshal(payload["tag"], &tag)
	_ = json.Unmarshal(payload["listen"], &listen)
	_ = json.Unmarshal(payload["listen_port"], &port)
	_ = json.Unmarshal(payload["tls_id"], &tlsID)
	var addresses []struct {
		Server string `json:"server"`
		Port   int    `json:"server_port"`
	}
	if err := json.Unmarshal(payload["addrs"], &addresses); err != nil {
		t.Fatal(err)
	}
	if tag != "la-anytls" || listen != "::" || port != 7443 || tlsID != 8 || len(addresses) != 1 || addresses[0].Server != "la.example" || addresses[0].Port != 7443 {
		t.Fatalf("target-owned fields were not rebuilt: tag=%q listen=%q port=%d tls=%d addresses=%+v", tag, listen, port, tlsID, addresses)
	}
	for _, secret := range []string{"cn2-private-key", "cn2-user-password", "cn2-fixed-password", "cn2-outbound-secret", "cn2.example", "cn2-source"} {
		if strings.Contains(string(request.InboundDraft), secret) {
			t.Fatalf("CN2 source material %q leaked into fresh target payload: %s", secret, request.InboundDraft)
		}
	}
}

func TestFleetInboundTargetSecretsAreGeneratedIndependently(t *testing.T) {
	first := &model.Inbound{Type: "hysteria2", Options: json.RawMessage(`{"obfs":{"type":"salamander","password":"__generate_per_target__"}}`)}
	second := &model.Inbound{Type: "hysteria2", Options: json.RawMessage(`{"obfs":{"type":"salamander","password":"__generate_per_target__"}}`)}
	if err := prepareFleetInboundTargetSecrets(first); err != nil {
		t.Fatal(err)
	}
	if err := prepareFleetInboundTargetSecrets(second); err != nil {
		t.Fatal(err)
	}
	var firstOptions, secondOptions map[string]json.RawMessage
	_ = json.Unmarshal(first.Options, &firstOptions)
	_ = json.Unmarshal(second.Options, &secondOptions)
	var firstObfs, secondObfs map[string]string
	_ = json.Unmarshal(firstOptions["obfs"], &firstObfs)
	_ = json.Unmarshal(secondOptions["obfs"], &secondObfs)
	if firstObfs["password"] == "" || firstObfs["password"] == fleetGenerateSecretMarker || firstObfs["password"] == secondObfs["password"] {
		t.Fatalf("target Hysteria2 obfuscation secrets were not independently generated: %q / %q", firstObfs["password"], secondObfs["password"])
	}

	for _, method := range []struct {
		name string
		size int
	}{
		{name: "2022-blake3-aes-128-gcm", size: 16},
		{name: "2022-blake3-aes-256-gcm", size: 32},
		{name: "2022-blake3-chacha20-poly1305", size: 32},
	} {
		t.Run(method.name, func(t *testing.T) {
			inbound := &model.Inbound{Type: "shadowsocks", Options: json.RawMessage(`{"method":"` + method.name + `"}`)}
			if err := prepareFleetInboundTargetSecrets(inbound); err != nil {
				t.Fatal(err)
			}
			var options map[string]string
			_ = json.Unmarshal(inbound.Options, &options)
			key, err := base64.StdEncoding.DecodeString(options["password"])
			if err != nil || len(key) != method.size {
				t.Fatalf("generated Shadowsocks 2022 key length = %d, err=%v; want %d", len(key), err, method.size)
			}
		})
	}
}

func TestFleetInboundTLSProfileSummaryDoesNotExposeSecrets(t *testing.T) {
	tlsConfig := model.Tls{
		Id:     7,
		Name:   "local-cert",
		Server: json.RawMessage(`{"enabled":true,"server_name":"edge.example","private_key":"PRIVATE-KEY-MUST-NOT-LEAK","certificate":["-----BEGIN CERTIFICATE-----\\nCERTIFICATE-MUST-NOT-LEAK\\n-----END CERTIFICATE-----"],"acme":{"domain":["edge.example"]}}`),
	}
	profile := summarizeFleetInboundTLS(tlsConfig)
	if profile.ID != tlsConfig.Id || profile.Name != tlsConfig.Name || profile.ServerName != "edge.example" || !profile.ServerEnabled {
		t.Fatalf("TLS summary lost public metadata: %+v", profile)
	}
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE-KEY-MUST-NOT-LEAK", "CERTIFICATE-MUST-NOT-LEAK", "BEGIN CERTIFICATE"} {
		if strings.Contains(string(raw), secret) {
			t.Fatalf("TLS summary leaked %q: %s", secret, raw)
		}
	}
}

func TestFleetInboundRemotePreflightRejectsBusyPortAndRequiresLocalUsers(t *testing.T) {
	_ = testutil.NewManagedPortSandbox(t)
	initFleetInboundTestDB(t)
	addFleetInboundTestClient(t, "local-user", true, `{"socks":{"username":"local-user","password":"local-password"}}`)
	listener, err := net.Listen("tcp", "[::]:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	request := fleetInboundRequest(t, `{"type":"socks","tag":"la-socks","listen":"::","listen_port":`+strconv.Itoa(port)+`}`)
	report, err := FleetInboundRemotePreflight(request, "target.example")
	if err != nil {
		t.Fatalf("remote preflight returned error: %v", err)
	}
	if report == nil || report.Valid {
		t.Fatalf("busy target port unexpectedly passed preflight: %+v", report)
	}
	foundPort := false
	for _, check := range report.Checks {
		if check.Name == "目标机监听端口" && check.Status == "error" && strings.Contains(check.Detail, "TCP") {
			foundPort = true
		}
	}
	if !foundPort {
		t.Fatalf("preflight did not report the occupied TCP listener: %+v", report.Checks)
	}

	if err := database.GetDB().Where("1 = 1").Delete(&model.Client{}).Error; err != nil {
		t.Fatal(err)
	}
	freePort, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	free := freePort.Addr().(*net.TCPAddr).Port
	_ = freePort.Close()
	request = fleetInboundRequest(t, `{"type":"socks","tag":"la-socks","listen":"::","listen_port":`+strconv.Itoa(free)+`}`)
	if _, err := FleetInboundRemotePreflight(request, "target.example"); err == nil || !strings.Contains(err.Error(), "没有启用用户") {
		t.Fatalf("remote preflight without local users = %v, want enabled-user validation", err)
	}
}

func TestFleetInboundRemoteCreateBindsAllLocalUsersAndDoesNotCopyCN2Data(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	portArg := strconv.Itoa(port)
	forwardCall := []string{"bash", "scripts/hy2-forward.sh", "apply", "la-socks", portArg, portArg, "tcp"}
	hostCommands := testutil.NewHostCommandSandbox(t, forwardCall[1:])
	initFleetInboundTestDB(t)
	previousTrafficBlock := trafficBudgetBlocked.Load()
	trafficBudgetBlocked.Store(true)
	t.Cleanup(func() { trafficBudgetBlocked.Store(previousTrafficBlock) })
	active := addFleetInboundTestClient(t, "local-active", true, `{"socks":{"username":"local-active","password":"local-active-secret"}}`)
	disabled := addFleetInboundTestClient(t, "local-disabled", false, `{"socks":{"username":"local-disabled","password":"local-disabled-secret"}}`)
	request := fleetInboundRequest(t, `{
		"id":991,"type":"socks","tag":"la-socks","tls_id":88,
		"listen":"127.0.0.1","listen_port":`+strconv.Itoa(port)+`,
		"tls":{"private_key":"cn2-private-key"},
		"users":[{"username":"cn2-user","password":"cn2-user-password"}],
		"password":"cn2-fixed-password","addrs":[{"server":"cn2.example","server_port":443}],
		"out_json":{"password":"cn2-outbound-secret"}
	}`)
	created, err := FleetInboundRemoteCreate(request, "fleet-admin", "la.panel.example")
	if err != nil {
		t.Fatalf("remote create failed: %v", err)
	}
	if created.ID == 0 || created.Tag != "la-socks" {
		t.Fatalf("created result = %+v", created)
	}
	var inbound model.Inbound
	if err := database.GetDB().First(&inbound, created.ID).Error; err != nil {
		t.Fatal(err)
	}
	if inbound.TlsId != 0 || strings.Contains(string(inbound.Options), "cn2-") || strings.Contains(string(inbound.Addrs), "cn2.example") {
		t.Fatalf("target inbound retained source credentials or addresses: %+v", inbound)
	}
	var addresses []struct {
		Server string `json:"server"`
		Port   int    `json:"server_port"`
	}
	if err := json.Unmarshal(inbound.Addrs, &addresses); err != nil {
		t.Fatal(err)
	}
	if len(addresses) != 1 || addresses[0].Server != "la.panel.example" || addresses[0].Port != port {
		t.Fatalf("target subscription addresses = %+v", addresses)
	}
	var persistedConfigs []string
	for _, client := range []model.Client{active, disabled} {
		var current model.Client
		if err := database.GetDB().First(&current, client.Id).Error; err != nil {
			t.Fatal(err)
		}
		persistedConfigs = append(persistedConfigs, string(current.Config))
		var currentConfig, originalConfig map[string]json.RawMessage
		if err := json.Unmarshal(current.Config, &currentConfig); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(client.Config, &originalConfig); err != nil {
			t.Fatal(err)
		}
		var currentSocks, originalSocks map[string]string
		if err := json.Unmarshal(currentConfig["socks"], &currentSocks); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(originalConfig["socks"], &originalSocks); err != nil {
			t.Fatal(err)
		}
		if current.Enable != client.Enable || currentSocks["username"] != originalSocks["username"] || currentSocks["password"] != originalSocks["password"] {
			t.Fatalf("target user %q enable state or SOCKS credentials changed during inbound creation", current.Name)
		}
		ids, err := decodeClientInboundIDs(current.Inbounds)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 1 || ids[0] != inbound.Id {
			t.Fatalf("target user %q was not bound to the new inbound: %v", current.Name, ids)
		}
	}
	for _, config := range persistedConfigs {
		if strings.Contains(config, "cn2-") {
			t.Fatal("CN2 user data was written to target client records")
		}
	}
	if runtime.GOOS == "linux" {
		hostCommands.AssertCalls(t, [][]string{forwardCall})
	}
}

func TestCreateFleetInboundPreflightsEveryTargetBeforeWriting(t *testing.T) {
	_ = testutil.NewManagedPortSandbox(t)
	initFleetInboundTestDB(t)
	var creates atomic.Int32
	newTarget := func(valid bool, name string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/apiv2/fleet-inbound-capabilities":
				body, _ := json.Marshal(map[string]interface{}{"success": true, "msg": "", "obj": FleetInboundCapabilities{
					Hostname: name + ".panel.example", SubscriptionHost: name + ".example", TLSProfiles: []FleetInboundTLSProfile{}, InboundTags: []string{},
				}})
				_, _ = w.Write(body)
			case r.Method == http.MethodPost && r.URL.Path == "/apiv2/fleet-inbound-preflight":
				validJSON := "false"
				if valid {
					validJSON = "true"
				}
				_, _ = w.Write([]byte(`{"success":true,"msg":"","obj":{"valid":` + validJSON + `,"checks":[]}}`))
			case r.Method == http.MethodPost && r.URL.Path == "/apiv2/fleet-inbound-create":
				creates.Add(1)
				_, _ = w.Write([]byte(`{"success":true,"msg":"","obj":{"id":1,"tag":"` + name + `-socks"}}`))
			default:
				http.NotFound(w, r)
			}
		}))
	}
	good := newTarget(true, "good")
	defer good.Close()
	bad := newTarget(false, "bad")
	defer bad.Close()
	fleet := &FleetService{}
	if err := fleet.SaveFleet([]FleetServerInput{
		{ID: "good", Name: "Good", URL: good.URL, Token: "test-token-good", Enabled: true},
		{ID: "bad", Name: "Bad", URL: bad.URL, Token: "test-token-bad", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	results, err := fleet.CreateFleetInbound(FleetInboundCreatePlan{
		InboundDraft: json.RawMessage(`{"type":"socks"}`),
		Targets: []FleetInboundTargetSelection{
			{ID: "good", ListenPort: 18080},
			{ID: "bad", ListenPort: 18080},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if creates.Load() != 0 {
		t.Fatalf("created %d remote inbounds despite a failed target preflight", creates.Load())
	}
	if len(results) != 2 {
		t.Fatalf("results length = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.Created {
			t.Fatalf("target %q was created before all preflights passed: %+v", result.ID, result)
		}
	}
}

func TestCreateFleetInboundUsesTargetNamePortAndTLS(t *testing.T) {
	_ = testutil.NewManagedPortSandbox(t)
	initFleetInboundTestDB(t)
	var preflights atomic.Int32
	newTarget := func(targetName string, tlsID, inboundID uint) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/apiv2/fleet-inbound-capabilities":
				body, _ := json.Marshal(map[string]interface{}{"success": true, "msg": "", "obj": FleetInboundCapabilities{
					Hostname: targetName + ".panel.example", SubscriptionHost: targetName + ".example",
					TLSProfiles: []FleetInboundTLSProfile{{ID: tlsID, Name: "target-tls", ServerEnabled: true}}, InboundTags: []string{},
				}})
				_, _ = w.Write(body)
			case r.Method == http.MethodPost && r.URL.Path == "/apiv2/fleet-inbound-preflight":
				preflights.Add(1)
				_, _ = w.Write([]byte(`{"success":true,"msg":"","obj":{"valid":true,"checks":[]}}`))
			case r.Method == http.MethodPost && r.URL.Path == "/apiv2/fleet-inbound-create":
				if preflights.Load() != 2 {
					t.Errorf("creation started before every target was preflighted; preflights=%d", preflights.Load())
				}
				var request FleetInboundRemoteRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode create request: %v", err)
				}
				var inbound struct {
					Tag        string `json:"tag"`
					Type       string `json:"type"`
					ListenPort int    `json:"listen_port"`
					TLSID      uint   `json:"tls_id"`
					Addrs      []struct {
						Server string `json:"server"`
						Port   int    `json:"server_port"`
					} `json:"addrs"`
				}
				if err := json.Unmarshal(request.InboundDraft, &inbound); err != nil {
					t.Errorf("decode target draft: %v", err)
				}
				wantTag := targetName + "-hy2"
				if inbound.Tag != wantTag || inbound.Type != "hysteria2" || inbound.ListenPort == 0 || inbound.TLSID != tlsID || len(inbound.Addrs) != 1 || inbound.Addrs[0].Server != targetName+".example" || inbound.Addrs[0].Port != inbound.ListenPort {
					t.Errorf("target payload = %+v, want target-specific TLS, tag and address", inbound)
				}
				body, _ := json.Marshal(map[string]interface{}{"success": true, "msg": "", "obj": FleetInboundRemoteCreated{ID: inboundID, Tag: inbound.Tag}})
				_, _ = w.Write(body)
			default:
				http.NotFound(w, r)
			}
		}))
	}
	la := newTarget("la", 11, 21)
	defer la.Close()
	tk := newTarget("tk", 22, 22)
	defer tk.Close()
	fleet := &FleetService{}
	if err := fleet.SaveFleet([]FleetServerInput{
		{ID: "la", Name: "LA", URL: la.URL, Token: "test-token-la", Enabled: true},
		{ID: "tk", Name: "TK", URL: tk.URL, Token: "test-token-tk", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	results, err := fleet.CreateFleetInbound(FleetInboundCreatePlan{
		InboundDraft: json.RawMessage(`{"type":"hysteria2","up_mbps":100,"down_mbps":100}`),
		Targets: []FleetInboundTargetSelection{
			{ID: "la", TLSID: 11, ListenPort: 18081},
			{ID: "tk", TLSID: 22, ListenPort: 18082},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Tag != "la-hy2" || results[1].Tag != "tk-hy2" {
		t.Fatalf("target-specific names = %+v", results)
	}
	for _, result := range results {
		if !result.Success || !result.Created {
			t.Fatalf("expected target create success: %+v", result)
		}
	}
}
