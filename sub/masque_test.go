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
	if got, ok := (*node)["cwnd"].(int); !ok || got != 32 {
		t.Fatalf("unexpected cwnd: %#v", (*node)["cwnd"])
	}
	if got, ok := (*node)["bbr-profile"].(string); !ok || got != "standard" {
		t.Fatalf("unexpected bbr-profile: %#v", (*node)["bbr-profile"])
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
			"congestion-controller": "bbr",
			"cwnd":                  32,
			"bbr-profile":           "standard",
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
	if got, ok := proxy["cwnd"].(int); !ok || got != 32 {
		t.Fatalf("unexpected cwnd: %#v", proxy["cwnd"])
	}
	if got, ok := proxy["bbr-profile"].(string); !ok || got != "standard" {
		t.Fatalf("unexpected bbr-profile: %#v", proxy["bbr-profile"])
	}
}
