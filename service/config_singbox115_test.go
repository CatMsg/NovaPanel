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
