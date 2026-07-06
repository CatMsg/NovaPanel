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
	if _, ok := proxy["pre-shared-key"]; ok {
		t.Fatalf("expected invalid pre-shared-key to be omitted from clash output")
	}
}
