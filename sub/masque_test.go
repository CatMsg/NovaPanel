package sub

import (
	"testing"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
	"gopkg.in/yaml.v3"
)

func init() {
	logger.InitLogger(logging.ERROR)
}

func TestBuildMasqueAggregateOutboundAddsPerformanceDefaults(t *testing.T) {
	endpoint := map[string]interface{}{
		"tag":         "masque-1",
		"server":      "example.com",
		"port":        443,
		"private_key": "private",
		"public_key":  "public",
	}

	node := buildMasqueAggregateOutbound(endpoint)
	if node == nil {
		t.Fatal("expected masque outbound to be built")
	}

	if got, ok := (*node)["congestion-controller"].(string); !ok || got != "bbr" {
		t.Fatalf("unexpected congestion-controller: %#v", (*node)["congestion-controller"])
	}
	if got, ok := (*node)["proto"].(string); !ok || got != "bbr" {
		t.Fatalf("unexpected proto: %#v", (*node)["proto"])
	}
	if got, ok := (*node)["sni"].(string); !ok || got != "example.com" {
		t.Fatalf("unexpected sni: %#v", (*node)["sni"])
	}
	if got, ok := (*node)["handshake-timeout"].(int); !ok || got != 30 {
		t.Fatalf("unexpected handshake-timeout: %#v", (*node)["handshake-timeout"])
	}
	if got, ok := (*node)["keepalive"].(int); !ok || got != 25 {
		t.Fatalf("unexpected keepalive: %#v", (*node)["keepalive"])
	}
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
			"dns":                   []string{"1.1.1.1"},
			"proto":                 "bbr",
			"congestion-controller": "bbr",
			"sni":                   "example.com",
			"handshake-timeout":     30,
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
	if got, ok := proxy["handshake-timeout"].(int); !ok || got != 30 {
		t.Fatalf("unexpected handshake-timeout: %#v", proxy["handshake-timeout"])
	}
	if got, ok := proxy["keepalive"].(int); !ok || got != 25 {
		t.Fatalf("unexpected keepalive: %#v", proxy["keepalive"])
	}
}
