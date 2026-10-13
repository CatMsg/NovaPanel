package sub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/sagernet/sing-box/option"
	transportHTTP "github.com/sagernet/sing-box/transport/http"
)

func TestAggregateNativeMASQUEEndpointsAndSourceReferences(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("format") != "json" {
			t.Errorf("native aggregate requested wrong format")
		}
		w.Header().Set("Subscription-Userinfo", "upload=1; download=2; total=100")
		if r.URL.Path == "/native-only" {
			w.Write([]byte(`{"endpoints":[{"type":"masque-client","tag":"direct","server":"127.0.0.1","server_port":14443,"version":3,"system":false,"on_demand":true,"username":"alice","password":"native-only-test","tls":{"enabled":true}}]}`))
			return
		}
		w.Write([]byte(`{"outbounds":[{"type":"http","tag":"edge","server":"127.0.0.1","server_port":18880,"version":1},{"type":"direct","tag":"custom-direct","bind_interface":"must-not-be-lost"}],"endpoints":[{"type":"masque-client","tag":"direct","server":"127.0.0.1","server_port":24443,"version":3,"system":false,"on_demand":true,"username":"bob","password":"mixed-test","detour":"edge","tls":{"enabled":true}},{"type":"masque-client","tag":"unsafe-child","server":"127.0.0.1","server_port":34443,"detour":"custom-direct"},{"type":"masque-server","tag":"not-a-client","listen_port":44443}]}`))
	}))
	defer server.Close()
	if err := database.InitDB(filepath.Join(t.TempDir(), "native-aggregate.db")); err != nil {
		t.Fatal(err)
	}
	seedSubscriptionTestSettings(t,
		model.Setting{Key: "subMode", Value: "master"},
		model.Setting{Key: "subMasterSources", Value: server.URL + "/native-only\n" + server.URL + "/mixed"},
		model.Setting{Key: "subUpdates", Value: "24"},
	)
	result, headers, err := (&AggregateService{}).GetAggregate("json", "panel.invalid")
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(*result), &config); err != nil {
		t.Fatal(err)
	}
	endpoints, ok := config["endpoints"].([]any)
	if !ok || len(endpoints) != 2 {
		t.Fatalf("native endpoints lost/unsafe endpoints retained: %#v", config["endpoints"])
	}
	first := endpoints[0].(map[string]any)
	second := endpoints[1].(map[string]any)
	if first["tag"] != "1.direct" || first["username"] != "alice" || first["password"] != "native-only-test" || second["tag"] != "2.direct" || second["detour"] != "edge" {
		t.Fatalf("source-local native endpoint mapping changed: %#v", endpoints)
	}
	proxyTargets := map[string]bool{}
	for _, raw := range config["outbounds"].([]any) {
		node := raw.(map[string]any)
		if node["type"] == "masque-client" {
			t.Fatal("native endpoint emitted in outbounds")
		}
		if node["tag"] == "proxy" {
			for _, target := range node["outbounds"].([]any) {
				proxyTargets[target.(string)] = true
			}
		}
	}
	if !proxyTargets["1.direct"] || !proxyTargets["2.direct"] || !proxyTargets["edge"] || proxyTargets["unsafe-child"] {
		t.Fatalf("native selector targets: %#v", proxyTargets)
	}
	if !strings.Contains(strings.Join(headers, "\n"), "upload=2; download=4; total=200") {
		t.Fatalf("aggregate usage not counted once: %v", headers)
	}
	// Exercise real endpoint lookup in the native outbound manager, without the
	// generated client TUN (never touch host networking in a subscription test).
	t.Run("native_runtime", func(t *testing.T) {
		if transportHTTP.ConfigureHTTP3ListenerFunc == nil {
			t.Skip("requires with_quic")
		}
		config["inbounds"] = []any{}
		content, _ := json.Marshal(config)
		ctx := core.Context(context.Background(), core.InboundRegistry(), core.OutboundRegistry(), core.EndpointRegistry(), core.DNSTransportRegistry(), core.ServiceRegistry(), core.CertificateProviderRegistry())
		var options option.Options
		if err := options.UnmarshalJSONContext(ctx, content); err != nil {
			t.Fatal(err)
		}
		runtime, err := core.NewBox(core.Options{Context: ctx, Options: options})
		if err != nil {
			t.Fatalf("native aggregate selector/endpoint config rejected: %v", err)
		}
		defer runtime.Close()
		if err := runtime.Start(); err != nil {
			t.Fatalf("native aggregate selector/endpoint config did not start: %v", err)
		}
		for _, tag := range []string{"1.direct", "2.direct"} {
			if _, exists := runtime.Endpoint().Get(tag); !exists {
				t.Fatalf("missing aggregate native endpoint %q", tag)
			}
		}
	})
}
