package service

import (
	"encoding/json"
	"testing"
)

func TestSingBoxConfigPreservesExtendedTopLevelOptions(t *testing.T) {
	raw := []byte(`{
		"certificate":{"store":"system"},
		"certificate_providers":[{"type":"origin-ca","tag":"origin"}],
		"http_clients":[{"tag":"rules"}],
		"network_namespaces":[{"tag":"isolated","path":"/run/netns/isolated"}]
	}`)
	var config SingBoxConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	var result map[string]json.RawMessage
	if err = json.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	for _, key := range []string{"certificate", "certificate_providers", "http_clients", "network_namespaces"} {
		if len(result[key]) == 0 {
			t.Fatalf("top-level option %q was removed", key)
		}
	}
}

func TestNormalizeSingBoxConfigFor115RemovesTunStackAndPreservesEndpointOptions(t *testing.T) {
	raw := []byte(`{
		"inbounds":[{
			"type":"tun",
			"tag":"tun-in",
			"address":["172.19.0.1/30"],
			"stack":"system"
		}],
		"endpoints":[{
			"type":"wireguard",
			"tag":"wg-endpoint",
			"on_demand":true
		}],
		"route":{}
	}`)

	normalized, err := NormalizeSingBoxConfig(raw)
	if err != nil {
		t.Fatalf("normalize 1.15 config: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal(normalized, &result); err != nil {
		t.Fatalf("decode normalized config: %v", err)
	}
	inbounds := objectList(result["inbounds"])
	if len(inbounds) != 1 {
		t.Fatalf("unexpected inbounds: %#v", result["inbounds"])
	}
	if _, exists := inbounds[0]["stack"]; exists {
		t.Fatalf("deprecated TUN stack was retained: %#v", inbounds[0])
	}
	endpoints := objectList(result["endpoints"])
	if len(endpoints) != 1 || !boolValue(endpoints[0]["on_demand"]) {
		t.Fatalf("1.15 endpoint options were not preserved: %#v", result["endpoints"])
	}
}
