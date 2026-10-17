package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/internal/testutil"
)

func TestFleetInboundTargetTagIncludesFleetName(t *testing.T) {
	if got, want := fleetInboundTargetTag("anytls", "Korea - KR"), "anytls [Korea - KR]"; got != want {
		t.Fatalf("target tag = %q, want %q", got, want)
	}
}

func TestFleetInboundTLSProfileSummaryDoesNotExposeSecrets(t *testing.T) {
	tlsConfig := model.Tls{
		Id:     7,
		Name:   "local-cert",
		Server: json.RawMessage(`{"server_name":"edge.example","private_key":"PRIVATE-KEY-MUST-NOT-LEAK","certificate":["-----BEGIN CERTIFICATE-----\\nCERTIFICATE-MUST-NOT-LEAK\\n-----END CERTIFICATE-----"],"acme":{"domain":["edge.example"]}}`),
	}
	profile := summarizeFleetInboundTLS(tlsConfig)
	if profile.ID != tlsConfig.Id || profile.Name != tlsConfig.Name || profile.ServerName != "edge.example" {
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

func TestFleetInboundPayloadUsesTargetTLSAndPort(t *testing.T) {
	source := &model.Inbound{
		Type:    "anytls",
		Tag:     "anytls-source",
		TlsId:   2,
		Tls:     &model.Tls{Server: json.RawMessage(`{"private_key":"source-private-key","certificate":["source-cert"]}`)},
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{}`),
		Options: json.RawMessage(`{"listen":"::","listen_port":443,"network":"tcp"}`),
	}
	source.Addrs = json.RawMessage(`[{"server":"source.example","server_port":443,"remark":" alt","tls":{"server_name":"source.example"}}]`)
	request, err := buildFleetInboundRemotePayload(source, "kr.example", "anytls [KR]", 7443, 8)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(request.Inbound, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["tls_id"] != float64(8) || payload["listen_port"] != float64(7443) || payload["tag"] != "anytls [KR]" {
		t.Fatalf("remote payload did not use target values: %#v", payload)
	}
	if _, exists := payload["tls"]; exists {
		t.Fatalf("remote payload included the source TLS object: %#v", payload)
	}
	encoded, _ := json.Marshal(payload["addrs"])
	if !strings.Contains(string(encoded), `"server":"kr.example"`) || !strings.Contains(string(encoded), `"server_port":7443`) || strings.Contains(string(encoded), "source.example") {
		t.Fatalf("remote subscription address not rewritten for target: %s", encoded)
	}
	if strings.Contains(string(request.Inbound), "source-private-key") || strings.Contains(string(request.Inbound), "source-cert") {
		t.Fatalf("remote payload leaked source TLS material: %s", request.Inbound)
	}
}

func TestCreateFleetInboundPreflightsEveryTargetBeforeWriting(t *testing.T) {
	_ = testutil.NewManagedPortSandbox(t)
	if err := database.InitDB(filepath.Join(t.TempDir(), "fleet-inbound-preflight.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	source := model.Inbound{
		Type:    "socks",
		Tag:     "socks-source",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{}`),
		Options: json.RawMessage(`{"listen":"::","listen_port":18080,"network":"tcp"}`),
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	var creates atomic.Int32
	newTarget := func(valid bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/apiv2/fleet-inbound-capabilities":
				_, _ = w.Write([]byte(`{"success":true,"msg":"","obj":{"hostname":"target.example","subscriptionHost":"target.example","tlsProfiles":[]}}`))
			case r.Method == http.MethodPost && r.URL.Path == "/apiv2/fleet-inbound-preflight":
				validJSON := "false"
				if valid {
					validJSON = "true"
				}
				_, _ = w.Write([]byte(`{"success":true,"msg":"","obj":{"valid":` + validJSON + `,"checks":[]}}`))
			case r.Method == http.MethodPost && r.URL.Path == "/apiv2/fleet-inbound-create":
				creates.Add(1)
				_, _ = w.Write([]byte(`{"success":true,"msg":"","obj":{"id":1,"tag":"socks [target]"}}`))
			default:
				http.NotFound(w, r)
			}
		}))
	}
	good := newTarget(true)
	defer good.Close()
	bad := newTarget(false)
	defer bad.Close()
	fleet := &FleetService{}
	if err := fleet.SaveFleet([]FleetServerInput{
		{ID: "good", Name: "Good", URL: good.URL, Token: "test-token-good", Enabled: true},
		{ID: "bad", Name: "Bad", URL: bad.URL, Token: "test-token-bad", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	results, err := fleet.CreateFleetInbound(FleetInboundCreatePlan{
		SourceInboundID: source.Id,
		BaseTag:         "socks",
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

func TestCreateFleetInboundUsesTargetNamesAndPorts(t *testing.T) {
	_ = testutil.NewManagedPortSandbox(t)
	if err := database.InitDB(filepath.Join(t.TempDir(), "fleet-inbound-create.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	source := model.Inbound{
		Type:    "socks",
		Tag:     "socks-source",
		Addrs:   json.RawMessage(`[]`),
		OutJson: json.RawMessage(`{}`),
		Options: json.RawMessage(`{"listen":"::","listen_port":18080,"network":"tcp"}`),
	}
	if err := db.Create(&source).Error; err != nil {
		t.Fatal(err)
	}
	var preflights atomic.Int32
	newTarget := func(targetName string, inboundID uint) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			switch {
			case r.Method == http.MethodGet && r.URL.Path == "/apiv2/fleet-inbound-capabilities":
				body, _ := json.Marshal(map[string]interface{}{"success": true, "msg": "", "obj": map[string]interface{}{
					"hostname": targetName + ".panel.example", "subscriptionHost": targetName + ".example", "tlsProfiles": []interface{}{},
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
					ListenPort int    `json:"listen_port"`
					Addrs      []struct {
						Server string `json:"server"`
						Port   int    `json:"server_port"`
					} `json:"addrs"`
				}
				if err := json.Unmarshal(request.Inbound, &inbound); err != nil {
					t.Errorf("decode inbound payload: %v", err)
				}
				wantTag := "socks [" + strings.ToUpper(targetName) + "]"
				if inbound.Tag != wantTag || len(inbound.Addrs) != 1 || inbound.Addrs[0].Server != targetName+".example" || inbound.Addrs[0].Port != inbound.ListenPort {
					t.Errorf("target payload = %+v, want tag %q and target endpoint", inbound, wantTag)
				}
				body, _ := json.Marshal(map[string]interface{}{"success": true, "msg": "", "obj": FleetInboundRemoteCreated{ID: inboundID, Tag: inbound.Tag}})
				_, _ = w.Write(body)
			default:
				http.NotFound(w, r)
			}
		}))
	}
	la := newTarget("la", 21)
	defer la.Close()
	kr := newTarget("kr", 22)
	defer kr.Close()
	fleet := &FleetService{}
	if err := fleet.SaveFleet([]FleetServerInput{
		{ID: "la", Name: "LA", URL: la.URL, Token: "test-token-la", Enabled: true},
		{ID: "kr", Name: "KR", URL: kr.URL, Token: "test-token-kr", Enabled: true},
	}); err != nil {
		t.Fatal(err)
	}
	results, err := fleet.CreateFleetInbound(FleetInboundCreatePlan{
		SourceInboundID: source.Id,
		BaseTag:         "socks",
		Targets: []FleetInboundTargetSelection{
			{ID: "la", ListenPort: 18081},
			{ID: "kr", ListenPort: 18082},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0].Tag != "socks [LA]" || results[1].Tag != "socks [KR]" {
		t.Fatalf("target-specific names = %+v", results)
	}
	for _, result := range results {
		if !result.Success || !result.Created {
			t.Fatalf("expected target create success: %+v", result)
		}
	}
}
