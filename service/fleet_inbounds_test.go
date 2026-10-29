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
	"gorm.io/gorm"
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

func fleetInboundTestPort(t *testing.T) int {
	t.Helper()
	seed, err := net.Listen("tcp", "[::]:0")
	if err != nil {
		t.Fatal(err)
	}
	start := seed.Addr().(*net.TCPAddr).Port
	_ = seed.Close()

	const firstPort = 20000
	const portCount = 49152 - firstPort
	start = firstPort + start%portCount
	for offset := 0; offset < portCount; offset++ {
		port := firstPort + (start-firstPort+offset)%portCount
		listener, err := net.Listen("tcp", net.JoinHostPort("::", strconv.Itoa(port)))
		if err == nil {
			_ = listener.Close()
			return port
		}
	}
	t.Fatal("no free non-ephemeral TCP port found for fleet inbound test")
	return 0
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

func TestFleetInboundRealityTLSCompatibility(t *testing.T) {
	initFleetInboundTestDB(t)
	profile := model.Tls{Name: "reality", Server: json.RawMessage(`{"enabled":true,"reality":{"enabled":true}}`), Client: json.RawMessage(`{}`)}
	if err := database.GetDB().Create(&profile).Error; err != nil {
		t.Fatal(err)
	}
	request := fleetInboundRequest(t, `{"type":"hysteria2","tag":"la-hy2","listen_port":12345,"tls_id":`+strconv.Itoa(int(profile.Id))+`}`)
	_, _, _, err := prepareFleetInboundRemotePayloadForTarget(request, "la.example")
	if err == nil || !strings.Contains(err.Error(), "Reality") {
		t.Fatalf("expected incompatible Reality error, got %v", err)
	}
	if !fleetInboundTLSCompatible("vless", summarizeFleetInboundTLS(profile)) {
		t.Fatal("VLESS must allow Reality")
	}
	if fleetInboundTLSCompatible("tuic", summarizeFleetInboundTLS(profile)) {
		t.Fatal("TUIC must reject Reality")
	}
}

func TestFleetInboundDeleteChecksIdentityAndPreservesHistory(t *testing.T) {
	_ = testutil.NewManagedPortSandbox(t,
		[]string{"bash", "scripts/hy2-forward.sh", "remove", "la-socks", "12345", "12345", "tcp,udp"},
		[]string{"bash", "scripts/hy2-forward.sh", "apply", "la-socks", "12345", "12345", "tcp"},
	)
	initFleetInboundTestDB(t)
	runtimeCore := core.NewCore()
	NewConfigService(runtimeCore)
	t.Cleanup(func() { _ = runtimeCore.Stop() })
	inbound := model.Inbound{Type: "socks", Tag: "la-socks", Options: json.RawMessage(`{"listen":"127.0.0.1","listen_port":12345}`), Addrs: json.RawMessage(`[]`), OutJson: json.RawMessage(`{}`)}
	if err := database.GetDB().Create(&inbound).Error; err != nil {
		t.Fatal(err)
	}
	capabilities, err := (&FleetService{}).LocalFleetInboundCapabilities("la.example")
	if err != nil || len(capabilities.Inbounds) != 1 || capabilities.Inbounds[0].ID != inbound.Id || capabilities.Inbounds[0].Tag != inbound.Tag || !capabilities.TLSProfileDetails {
		t.Fatalf("unexpected inbound capabilities: %+v, %v", capabilities, err)
	}
	client := addFleetInboundTestClient(t, "local-user", true, `{"socks":{"username":"u","password":"p"}}`)
	if err := database.GetDB().Model(&client).Updates(map[string]interface{}{"inbounds": json.RawMessage("[" + strconv.Itoa(int(inbound.Id)) + "]"), "history": json.RawMessage(`[{"domain":"example.com"}]`)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := FleetInboundRemoteDelete(FleetInboundDeleteRequest{ID: inbound.Id + 1, Tag: inbound.Tag}, "admin", "la.example"); err == nil {
		t.Fatal("mismatched ID must not delete")
	}
	if err := FleetInboundRemoteDelete(FleetInboundDeleteRequest{ID: inbound.Id, Tag: inbound.Tag}, "admin", "la.example"); err != nil {
		t.Fatal(err)
	}
	var remaining int64
	if err := database.GetDB().Model(&model.Inbound{}).Where("id = ?", inbound.Id).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatalf("inbound remains: %d, %v", remaining, err)
	}
	if err := database.GetDB().First(&client, client.Id).Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(client.Inbounds), strconv.Itoa(int(inbound.Id))) || !strings.Contains(string(client.History), "example.com") {
		t.Fatal("user association or history was not preserved correctly")
	}
}

func TestFleetInboundDeleteUsesSelectedRemote(t *testing.T) {
	initFleetInboundTestDB(t)
	var received FleetInboundDeleteRequest
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/apiv2/fleet-inbound-delete" || r.Header.Get("Token") != "test-token" {
			http.Error(w, "wrong target", http.StatusBadRequest)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			http.Error(w, "invalid body", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"success":true}`))
	}))
	defer target.Close()
	fleet := &FleetService{}
	if err := fleet.SaveFleet([]FleetServerInput{{ID: "la", Name: "LA", URL: target.URL, Token: "test-token", Enabled: true}}); err != nil {
		t.Fatal(err)
	}
	if err := fleet.DeleteFleetInbound(FleetInboundDeleteRequest{ServerID: "la", ID: 7, Tag: "la-hy2"}); err != nil {
		t.Fatal(err)
	}
	if received.ID != 7 || received.Tag != "la-hy2" || received.ServerID != "" {
		t.Fatalf("incorrect forwarded target: %+v", received)
	}
	if err := fleet.DeleteFleetInbound(FleetInboundDeleteRequest{ServerID: "local", ID: 7, Tag: "la-hy2"}); err == nil {
		t.Fatal("local target must be rejected")
	}
}

func TestFleetRequestsDoNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()

	for _, status := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, destination.URL, status)
			}))
			defer server.Close()
			fleet := &FleetService{}
			if _, err := fleet.fetchFleetAPI(server.URL, "secret", "status", ""); err == nil || !strings.Contains(err.Error(), "直达地址") {
				t.Fatalf("fleet status request did not report redirect: %v", err)
			}
			if _, err := fleet.fetchFleetJSON(server.URL, "secret", "fleet-inbound-delete", struct{}{}); err == nil || !strings.Contains(err.Error(), "直达地址") {
				t.Fatalf("fleet management request did not report redirect: %v", err)
			}
			if got := redirected.Load(); got != 0 {
				t.Fatalf("redirect target received %d requests", got)
			}
		})
	}
}

func TestFleetResponsesHaveSizeLimit(t *testing.T) {
	const prefix = `{"success":true,"obj":"`
	const suffix = `"}`
	for _, size := range []int{len(prefix) + len(suffix), fleetResponseLimit, fleetResponseLimit + 1} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			payload := prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(payload))
			}))
			defer server.Close()
			fleet := &FleetService{}
			for _, request := range []func() (fleetAPIResponse, error){
				func() (fleetAPIResponse, error) { return fleet.fetchFleetAPI(server.URL, "secret", "status", "") },
				func() (fleetAPIResponse, error) {
					return fleet.fetchFleetJSON(server.URL, "secret", "fleet-inbound-preflight", struct{}{})
				},
			} {
				_, err := request()
				if size <= fleetResponseLimit && err != nil {
					t.Fatalf("valid response of %d bytes rejected: %v", size, err)
				}
				if size > fleetResponseLimit && (err == nil || !strings.Contains(err.Error(), "响应超过")) {
					t.Fatalf("oversized response of %d bytes not rejected: %v", size, err)
				}
			}
		})
	}
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

func TestFleetInboundNamesStayUniqueAcrossSeparateTargetSelections(t *testing.T) {
	initFleetInboundTestDB(t)
	capabilities := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apiv2/fleet-inbound-capabilities" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"obj": FleetInboundCapabilities{
				Hostname: "target.example", SubscriptionHost: "target.example",
				TLSProfiles: []FleetInboundTLSProfile{}, InboundTags: []string{},
			},
		})
	})
	first := httptest.NewServer(capabilities)
	defer first.Close()
	second := httptest.NewServer(capabilities)
	defer second.Close()
	fleet := &FleetService{}
	if err := fleet.SaveFleet([]FleetServerInput{
		{ID: "west-shared-a", Name: "LA East", URL: first.URL, Token: "test-token-a", Enabled: true},
		{ID: "west-shared-b", Name: "LA-East", URL: second.URL, Token: "test-token-b", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	for _, selected := range [][]string{{"west-shared-a"}, {"west-shared-b"}, {"west-shared-a", "west-shared-b"}} {
		targets := make([]FleetInboundTargetSelection, 0, len(selected))
		for _, id := range selected {
			targets = append(targets, FleetInboundTargetSelection{ID: id, ListenPort: 18081})
		}
		jobs, results, err := fleet.buildFleetInboundJobs(FleetInboundCreatePlan{
			InboundDraft: json.RawMessage(`{"type":"socks"}`), Targets: targets,
		})
		if err != nil || len(jobs) != len(selected) {
			t.Fatalf("build targets %v: results=%+v err=%v", selected, results, err)
		}
		for index, id := range selected {
			if want := "la-east-" + id + "-socks"; results[index].Tag != want {
				t.Fatalf("targets %v: name = %q, want %q", selected, results[index].Tag, want)
			}
		}
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
	free := fleetInboundTestPort(t)
	request = fleetInboundRequest(t, `{"type":"socks","tag":"la-socks","listen":"::","listen_port":`+strconv.Itoa(free)+`}`)
	if _, err := FleetInboundRemotePreflight(request, "target.example"); err == nil || !strings.Contains(err.Error(), "没有启用用户") {
		t.Fatalf("remote preflight without local users = %v, want enabled-user validation", err)
	}
}

func TestFleetInboundRemoteCreateBindsAllLocalUsersAndDoesNotCopyCN2Data(t *testing.T) {
	port := fleetInboundTestPort(t)
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

func TestFleetInboundRemoteCreatePreflightsSavedSecrets(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	portArg := strconv.Itoa(port)
	forwardCall := []string{"bash", "scripts/hy2-forward.sh", "apply", "la-ss", portArg, portArg, "tcp,udp"}
	hostCommands := testutil.NewManagedPortSandbox(t, forwardCall)
	initFleetInboundTestDB(t)
	previousTrafficBlock := trafficBudgetBlocked.Load()
	trafficBudgetBlocked.Store(true)
	t.Cleanup(func() { trafficBudgetBlocked.Store(previousTrafficBlock) })
	addFleetInboundTestClient(t, "local-user", true, `{"shadowsocks":{"password":"local-password"}}`)

	var passwords []string
	db := database.GetDB()
	const callback = "test:capture-fleet-inbound-password"
	if err := db.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
		inbound, ok := tx.Statement.Dest.(*model.Inbound)
		if !ok || inbound.Tag != "la-ss" {
			return
		}
		var options struct {
			Password string `json:"password"`
		}
		if json.Unmarshal(inbound.Options, &options) == nil {
			passwords = append(passwords, options.Password)
		}
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Callback().Create().Remove(callback) })

	request := fleetInboundRequest(t, `{"type":"shadowsocks","tag":"la-ss","listen":"127.0.0.1","listen_port":`+portArg+`,"method":"aes-128-gcm"}`)
	if _, err := FleetInboundRemoteCreate(request, "fleet-admin", "la.example"); err != nil {
		t.Fatalf("remote create failed: %v", err)
	}
	if len(passwords) != 2 || passwords[0] == "" || passwords[0] != passwords[1] {
		t.Fatalf("preflight and saved inbound used different generated passwords (captured %d)", len(passwords))
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
		if result.ID == "good" && (result.Success || result.Preview == nil || !result.Preview.Valid || !strings.Contains(result.Error, "预检通过") || !strings.Contains(result.Error, "未创建")) {
			t.Fatalf("passed target should be reported as preflighted but not created: %+v", result)
		}
		if result.ID == "bad" && (result.Success || result.Error == "") {
			t.Fatalf("failed target must retain its preflight error: %+v", result)
		}
	}
}

func TestFleetInboundRejectsBlankExistingAndDuplicateCustomNames(t *testing.T) {
	initFleetInboundTestDB(t)
	var postRequests atomic.Int32
	capabilities := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/apiv2/fleet-inbound-capabilities" {
			postRequests.Add(1)
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true, "obj": FleetInboundCapabilities{
				Hostname: "target.example", SubscriptionHost: "target.example",
				TLSProfiles: []FleetInboundTLSProfile{}, InboundTags: []string{"existing-http"},
			},
		})
	})
	la := httptest.NewServer(capabilities)
	defer la.Close()
	tk := httptest.NewServer(capabilities)
	defer tk.Close()
	fleet := &FleetService{}
	if err := fleet.SaveFleet([]FleetServerInput{
		{ID: "la", Name: "LA", URL: la.URL, Token: "test-la", Enabled: true},
		{ID: "tk", Name: "TK", URL: tk.URL, Token: "test-tk", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	blank, existing, first, second := "  ", "  EXISTING-HTTP ", "shared-name", " SHARED-NAME "
	for _, item := range []struct {
		name    string
		targets []FleetInboundTargetSelection
		message string
	}{
		{"blank", []FleetInboundTargetSelection{{ID: "la", ListenPort: 18081, Tag: &blank}}, "不能为空"},
		{"existing", []FleetInboundTargetSelection{{ID: "la", ListenPort: 18081, Tag: &existing}}, "已存在"},
		{"duplicate", []FleetInboundTargetSelection{{ID: "la", ListenPort: 18081, Tag: &first}, {ID: "tk", ListenPort: 18081, Tag: &second}}, "重复"},
	} {
		t.Run(item.name, func(t *testing.T) {
			results, err := fleet.CreateFleetInbound(FleetInboundCreatePlan{InboundDraft: json.RawMessage(`{"type":"socks"}`), Targets: item.targets})
			if err != nil || len(results) != len(item.targets) {
				t.Fatalf("invalid plan result: %+v, %v", results, err)
			}
			for _, result := range results {
				if result.Success || result.Created || !strings.Contains(result.Error, item.message) {
					t.Fatalf("invalid name was not rejected: %+v", result)
				}
			}
			if got := postRequests.Load(); got != 0 {
				t.Fatalf("invalid names reached a remote preflight or create endpoint: %d", got)
			}
		})
	}
}

func TestCreateFleetInboundUsesCustomAndDefaultNamesPortAndTLS(t *testing.T) {
	_ = testutil.NewManagedPortSandbox(t)
	initFleetInboundTestDB(t)
	var preflights atomic.Int32
	newTarget := func(targetName, wantTag string, tlsID, inboundID uint) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/apiv2/fleet-inbound-capabilities":
				body, _ := json.Marshal(map[string]interface{}{"success": true, "msg": "", "obj": FleetInboundCapabilities{
					Hostname: targetName + ".panel.example", SubscriptionHost: targetName + ".example",
					TLSProfiles: []FleetInboundTLSProfile{{ID: tlsID, Name: "target-tls", ServerEnabled: true}}, InboundTags: []string{}, TLSProfileDetails: true,
				}})
				_, _ = w.Write(body)
			case r.Method == http.MethodPost && r.URL.Path == "/apiv2/fleet-inbound-preflight":
				var request FleetInboundRemoteRequest
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Errorf("decode preflight request: %v", err)
				}
				var draft struct {
					Tag string `json:"tag"`
				}
				if err := json.Unmarshal(request.InboundDraft, &draft); err != nil || draft.Tag != wantTag {
					t.Errorf("preflight tag = %q, want %q: %v", draft.Tag, wantTag, err)
				}
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
	la := newTarget("la", "la-stream-hy2", 11, 21)
	defer la.Close()
	tk := newTarget("tk", "tk-hy2", 22, 22)
	defer tk.Close()
	fleet := &FleetService{}
	if err := fleet.SaveFleet([]FleetServerInput{
		{ID: "la", Name: "LA", URL: la.URL, Token: "test-token-la", Enabled: true},
		{ID: "tk", Name: "TK", URL: tk.URL, Token: "test-token-tk", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	customName := "  la-stream-hy2  "
	results, err := fleet.CreateFleetInbound(FleetInboundCreatePlan{
		InboundDraft: json.RawMessage(`{"type":"hysteria2","up_mbps":100,"down_mbps":100}`),
		Targets: []FleetInboundTargetSelection{
			{ID: "la", TLSID: 11, ListenPort: 18081, Tag: &customName},
			{ID: "tk", TLSID: 22, ListenPort: 18082},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Tag != "la-stream-hy2" || results[1].Tag != "tk-hy2" {
		t.Fatalf("target-specific names = %+v", results)
	}
	for _, result := range results {
		if !result.Success || !result.Created {
			t.Fatalf("expected target create success: %+v", result)
		}
	}
}
