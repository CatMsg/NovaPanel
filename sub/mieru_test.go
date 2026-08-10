package sub

import (
	"testing"

	"github.com/CatMsg/NovaPanel/util"
	"gopkg.in/yaml.v3"
)

func TestMieruLinkParsesForOrdinaryAggregate(t *testing.T) {
	node, tag, err := util.GetOutbound(
		"mierus://alice:secret@proxy.example.com?profile=mieru-main&port=22000-22010&protocol=TCP&multiplexing=MULTIPLEXING_MIDDLE&handshake-mode=HANDSHAKE_NO_WAIT&mtu=1380&traffic-pattern=CIcIEAAaBAgAEAAiCAgCEAAYBCAGKgQIIBBA",
		0,
	)
	if err != nil {
		t.Fatalf("parse Mieru link: %v", err)
	}
	if tag != "mieru-main" {
		t.Fatalf("unexpected tag: %q", tag)
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
	if got := (*node)["traffic-pattern"]; got != "CIcIEAAaBAgAEAAiCAgCEAAYBCAGKgQIIBBA" {
		t.Fatalf("unexpected traffic pattern: %#v", got)
	}
	if got := (*node)["mtu"]; got != 1380 {
		t.Fatalf("unexpected MTU: %#v", got)
	}
}

func TestConvertToClashMetaPreservesMieruFields(t *testing.T) {
	service := ClashService{}
	outbounds := &[]map[string]interface{}{
		{
			"type":            "mieru",
			"tag":             "mieru-main",
			"server":          "proxy.example.com",
			"server_port":     22000,
			"transport":       "TCP",
			"udp":             true,
			"username":        "alice",
			"password":        "secret",
			"multiplexing":    "MULTIPLEXING_LOW",
			"handshake-mode":  "HANDSHAKE_STANDARD",
			"traffic-pattern": "CIcIEAAaBAgAEAAiCAgCEAAYBCAGKgQIIBBA",
			"mtu":             1380,
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
	if proxy["traffic-pattern"] != "CIcIEAAaBAgAEAAiCAgCEAAYBCAGKgQIIBBA" {
		t.Fatalf("Mieru traffic pattern was not preserved: %#v", proxy)
	}
	if proxy["mtu"] != 1380 {
		t.Fatalf("Mieru MTU was not preserved: %#v", proxy)
	}
}
