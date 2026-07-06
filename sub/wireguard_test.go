package sub

import (
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
	"gopkg.in/yaml.v3"
)

func init() {
	logger.InitLogger(logging.ERROR)
}

func TestNormalizeWireguardPreSharedKey(t *testing.T) {
	validStd := "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo="
	normalized, ok := normalizeWireguardPreSharedKey(validStd)
	if !ok {
		t.Fatalf("expected valid standard base64 pre-shared key")
	}
	if normalized != validStd {
		t.Fatalf("expected normalized value to remain standard base64, got %q", normalized)
	}

	validRaw := "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXo"
	normalized, ok = normalizeWireguardPreSharedKey(validRaw)
	if !ok {
		t.Fatalf("expected valid raw base64 pre-shared key")
	}
	if normalized != validStd {
		t.Fatalf("expected raw base64 to normalize to standard base64, got %q", normalized)
	}

	if _, ok := normalizeWireguardPreSharedKey("not-base64"); ok {
		t.Fatalf("expected invalid pre-shared key to be rejected")
	}
}

func TestConvertToClashMetaSkipsInvalidWireguardPreSharedKey(t *testing.T) {
	svc := ClashService{}
	outbounds := &[]map[string]interface{}{
		{
			"type":           "wireguard",
			"tag":            "wg-1",
			"server":         "example.com",
			"server_port":    51820,
			"private-key":    "private",
			"public-key":     "public",
			"pre-shared-key": "invalid-key",
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
	psk, ok := proxy["pre-shared-key"]
	if !ok {
		t.Fatalf("expected pre-shared-key to remain present in clash output")
	}
	if psk != "" {
		t.Fatalf("expected invalid pre-shared-key to be normalized as empty string, got %#v", psk)
	}
}

func TestBuildWireguardAggregateOutboundsSetsUdpTrue(t *testing.T) {
	endpoint := map[string]interface{}{
		"tag":         "wg-test",
		"listen_port": 505,
		"ext": map[string]interface{}{
			"public_key": "server-key",
			"dns":        []interface{}{"1.1.1.1", "9.9.9.9"},
			"keys": []interface{}{
				map[string]interface{}{
					"public_key":  "peer-key",
					"private_key": "peer-private",
				},
			},
		},
		"peers": []interface{}{
			map[string]interface{}{
				"public_key":  "peer-key",
				"allowed_ips": []interface{}{"10.0.0.2/32"},
			},
		},
	}

	outbounds := buildWireguardAggregateOutbounds(endpoint, "cn2.mile.news")
	if len(outbounds) != 1 {
		t.Fatalf("expected one outbound, got %#v", outbounds)
	}

	node := outbounds[0]
	if udp, ok := node["udp"].(bool); !ok || !udp {
		t.Fatalf("expected wireguard outbound udp=true, got %#v", node["udp"])
	}
	if server, _ := node["server"].(string); server != "cn2.mile.news" {
		t.Fatalf("unexpected server: %#v", server)
	}
	if psk, _ := node["pre-shared-key"].(string); psk != "" {
		t.Fatalf("expected empty pre-shared-key, got %#v", psk)
	}
	if key, _ := node["private-key"].(string); !strings.HasPrefix(key, "peer-private") {
		t.Fatalf("unexpected private key: %#v", node["private-key"])
	}
}
