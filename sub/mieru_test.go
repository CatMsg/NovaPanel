package sub

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBuildMieruAggregateOutbound(t *testing.T) {
	node := buildMieruAggregateOutbound(map[string]interface{}{
		"type":           "mieru",
		"tag":            "mieru-main",
		"server":         "proxy.example.com",
		"port_range":     "22000-22010",
		"transport":      "tcp",
		"username":       "alice",
		"password":       "secret",
		"multiplexing":   "multiplexing_middle",
		"handshake_mode": "handshake_no_wait",
	})
	if node == nil {
		t.Fatal("expected Mieru outbound")
	}
	if got := (*node)["port-range"]; got != "22000-22010" {
		t.Fatalf("unexpected port range: %#v", got)
	}
	if got := (*node)["transport"]; got != "TCP" {
		t.Fatalf("unexpected transport: %#v", got)
	}
	if got := (*node)["handshake-mode"]; got != "HANDSHAKE_NO_WAIT" {
		t.Fatalf("unexpected handshake mode: %#v", got)
	}
	if got := (*node)["udp"]; got != true {
		t.Fatalf("expected UDP associate support, got %#v", got)
	}
}

func TestConvertToClashMetaPreservesMieruFields(t *testing.T) {
	service := ClashService{}
	outbounds := &[]map[string]interface{}{
		{
			"type":           "mieru",
			"tag":            "mieru-main",
			"server":         "proxy.example.com",
			"server_port":    22000,
			"transport":      "TCP",
			"udp":            true,
			"username":       "alice",
			"password":       "secret",
			"multiplexing":   "MULTIPLEXING_LOW",
			"handshake-mode": "HANDSHAKE_STANDARD",
		},
	}

	result, err := service.ConvertToClashMeta(outbounds, basicClashConfig)
	if err != nil {
		t.Fatalf("convert Mieru to Clash: %v", err)
	}
	var config map[string]interface{}
	if err := yaml.Unmarshal([]byte(result), &config); err != nil {
		t.Fatalf("parse Clash output: %v", err)
	}
	proxies, ok := config["proxies"].([]interface{})
	if !ok || len(proxies) != 1 {
		t.Fatalf("unexpected proxies: %#v", config["proxies"])
	}
	proxy, ok := proxies[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected proxy: %#v", proxies[0])
	}
	if proxy["type"] != "mieru" || proxy["username"] != "alice" {
		t.Fatalf("Mieru fields were not preserved: %#v", proxy)
	}
	if proxy["udp"] != true {
		t.Fatalf("expected UDP associate support: %#v", proxy)
	}
	if proxy["port"] != 22000 {
		t.Fatalf("unexpected Mieru port: %#v", proxy["port"])
	}
}
