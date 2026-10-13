package sub

import (
	"encoding/json"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
	"gopkg.in/yaml.v3"
)

func TestUserSubscriptionInjectsMasqueClientCredential(t *testing.T) {
	svc := JsonService{}
	inbound := &model.Inbound{
		Id: 42, Type: "masque", Tag: "shared-masque", Addrs: json.RawMessage("[]"),
		OutJson: json.RawMessage(`{"type":"masque-client","tag":"shared-masque","server":"example.com","server_port":443,"version":3,"system":false,"tls":{"enabled":true}}`),
	}
	config := json.RawMessage(`{"masque":{"username":"alice","password":"synthetic-native-password"}}`)
	outbounds, tags, err := svc.getOutbounds(config, []*model.Inbound{inbound})
	if err != nil {
		t.Fatalf("build outbounds: %v", err)
	}
	if len(*outbounds) != 1 || len(*tags) != 1 {
		t.Fatalf("unexpected masque subscription: %#v %#v", *outbounds, *tags)
	}
	got := (*outbounds)[0]
	if got["type"] != "masque-client" || got["username"] != "alice" || got["password"] != "synthetic-native-password" || got["version"] != float64(3) {
		t.Fatal("native MASQUE subscription did not preserve its authenticated client configuration")
	}
	for _, unsupported := range []string{"private-key", "public-key", "ip"} {
		if _, present := got[unsupported]; present {
			t.Fatalf("native subscription contains obsolete field %q", unsupported)
		}
	}
}

func init() {
	logger.InitLogger(logging.ERROR)
}

func TestConvertToClashMetaPreservesMasquePerformanceDefaults(t *testing.T) {
	svc := ClashService{}
	outbounds := &[]map[string]interface{}{
		{
			"type":                  "masque",
			"tag":                   "masque-1",
			"server":                "example.com",
			"server_port":           443,
			"network":               "quic",
			"private-key":           "private",
			"public-key":            "public",
			"remote-dns-resolve":    true,
			"dns":                   []string{"1.1.1.1", "8.8.8.8"},
			"proto":                 "bbr",
			"congestion-controller": "bbr",
			"sni":                   "example.com",
			"keepalive":             25,
		},
	}

	result, err := svc.ConvertToClashMeta(outbounds, basicClashConfig)
	if err != nil {
		t.Fatalf("convert to clash meta failed: %v", err)
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal([]byte(result), &cfg); err != nil {
		t.Fatalf("unmarshal clash config failed: %v", err)
	}

	proxies, ok := cfg["proxies"].([]interface{})
	if !ok || len(proxies) != 1 {
		t.Fatalf("expected one proxy in clash config, got %#v", cfg["proxies"])
	}

	proxy, ok := proxies[0].(map[string]interface{})
	if !ok {
		t.Fatalf("expected proxy map, got %#v", proxies[0])
	}
	if got, ok := proxy["congestion-controller"].(string); !ok || got != "bbr" {
		t.Fatalf("unexpected congestion-controller: %#v", proxy["congestion-controller"])
	}
	if got, ok := proxy["proto"].(string); !ok || got != "bbr" {
		t.Fatalf("unexpected proto: %#v", proxy["proto"])
	}
	if got, ok := proxy["sni"].(string); !ok || got != "example.com" {
		t.Fatalf("unexpected sni: %#v", proxy["sni"])
	}
	if got, ok := proxy["keepalive"].(int); !ok || got != 25 {
		t.Fatalf("unexpected keepalive: %#v", proxy["keepalive"])
	}
	if dns, ok := proxy["dns"].([]interface{}); !ok || len(dns) != 2 {
		t.Fatalf("expected dns to be preserved when remote-dns-resolve is enabled: %#v", proxy["dns"])
	}
}
