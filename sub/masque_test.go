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
	if got, ok := (*node)["keepalive"].(int); !ok || got != 25 {
		t.Fatalf("unexpected keepalive: %#v", (*node)["keepalive"])
	}
	if _, ok := (*node)["remote-dns-resolve"]; ok {
		t.Fatalf("did not expect remote-dns-resolve by default: %#v", (*node)["remote-dns-resolve"])
	}
	if _, ok := (*node)["dns"]; ok {
		t.Fatalf("did not expect dns by default: %#v", (*node)["dns"])
	}
}

func TestBuildMasqueAggregateOutboundCanEnableRemoteDNSResolve(t *testing.T) {
	endpoint := map[string]interface{}{
		"tag":                "masque-1",
		"server":             "example.com",
		"port":               443,
		"private_key":        "private",
		"public_key":         "public",
		"remote_dns_resolve": true,
	}

	node := buildMasqueAggregateOutbound(endpoint)
	if node == nil {
		t.Fatal("expected masque outbound to be built")
	}

	if got, ok := (*node)["remote-dns-resolve"].(bool); !ok || !got {
		t.Fatalf("unexpected remote-dns-resolve: %#v", (*node)["remote-dns-resolve"])
	}
	if dns, ok := (*node)["dns"].([]string); !ok || len(dns) != 2 {
		t.Fatalf("unexpected dns: %#v", (*node)["dns"])
	}
}

func TestBuildMasqueAggregateOutboundRejectsUnsupportedNetwork(t *testing.T) {
	node := buildMasqueAggregateOutbound(map[string]interface{}{
		"tag":         "legacy-h2",
		"server":      "masque.example.com",
		"port":        443,
		"network":     "h2",
		"private_key": "private",
		"public_key":  "public",
	})
	if node != nil {
		t.Fatalf("unsupported h2 endpoint must not be published: %#v", *node)
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
