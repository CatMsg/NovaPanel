package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInboundExcludeFromSubIsPanelMetadata(t *testing.T) {
	var inb Inbound
	if err := json.Unmarshal([]byte(`{
		"id":7,
		"type":"socks",
		"tag":"hidden-socks",
		"exclude_from_sub":true,
		"listen":"::",
		"listen_port":1080
	}`), &inb); err != nil {
		t.Fatalf("unmarshal inbound: %v", err)
	}
	if !inb.ExcludeFromSub {
		t.Fatal("exclude_from_sub was not decoded")
	}
	if strings.Contains(string(inb.Options), "exclude_from_sub") {
		t.Fatalf("panel metadata leaked into core options: %s", inb.Options)
	}

	var full map[string]interface{}
	fullJSON, err := inb.MarshalFull()
	if err != nil {
		t.Fatalf("marshal full inbound: %v", err)
	}
	fullBytes, err := json.Marshal(fullJSON)
	if err != nil {
		t.Fatalf("encode full inbound: %v", err)
	}
	if err := json.Unmarshal(fullBytes, &full); err != nil {
		t.Fatalf("decode full inbound: %v", err)
	}
	if got, ok := full["exclude_from_sub"].(bool); !ok || !got {
		t.Fatalf("full model exclude_from_sub = %#v, want true", full["exclude_from_sub"])
	}

	coreJSON, err := inb.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal core inbound: %v", err)
	}
	var core map[string]interface{}
	if err := json.Unmarshal(coreJSON, &core); err != nil {
		t.Fatalf("decode core inbound: %v", err)
	}
	if _, exists := core["exclude_from_sub"]; exists {
		t.Fatalf("panel metadata leaked into core inbound: %s", coreJSON)
	}
	if core["listen_port"] != float64(1080) {
		t.Fatalf("core inbound lost listen_port: %s", coreJSON)
	}
}

func TestInboundLegacyPayloadDefaultsExcludeFromSubFalse(t *testing.T) {
	var inb Inbound
	if err := json.Unmarshal([]byte(`{"type":"socks","tag":"legacy","listen_port":1080}`), &inb); err != nil {
		t.Fatalf("unmarshal legacy inbound: %v", err)
	}
	if inb.ExcludeFromSub {
		t.Fatal("legacy inbound without panel field must default to visible")
	}
	full, err := inb.MarshalFull()
	if err != nil {
		t.Fatalf("marshal legacy full inbound: %v", err)
	}
	if got, ok := (*full)["exclude_from_sub"].(bool); !ok || got {
		t.Fatalf("legacy full model flag = %#v, want false", (*full)["exclude_from_sub"])
	}
}

func TestInboundMarshalJSONStripsExcludeFromOptions(t *testing.T) {
	inb := Inbound{
		Type:    "socks",
		Tag:     "legacy-option",
		Options: json.RawMessage(`{"listen_port":1080,"exclude_from_sub":true}`),
	}
	raw, err := inb.MarshalJSON()
	if err != nil {
		t.Fatalf("marshal inbound: %v", err)
	}
	if strings.Contains(string(raw), "exclude_from_sub") {
		t.Fatalf("legacy panel key leaked from stored options: %s", raw)
	}
}

func TestInboundMarshalJSONStripsNaiveHttpVersion(t *testing.T) {
	inb := Inbound{
		Type: "naive",
		Tag:  "naive-demo",
		Options: json.RawMessage(`{
			"listen_port": 1234,
			"http_version": "http3",
			"quic_congestion_control": "bbr2"
		}`),
	}

	raw, err := inb.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON returned error: %v", err)
	}

	got := string(raw)
	if strings.Contains(got, "http_version") {
		t.Fatalf("MarshalJSON unexpectedly kept http_version: %s", got)
	}
	if !strings.Contains(got, `"quic_congestion_control":"bbr2"`) {
		t.Fatalf("MarshalJSON lost valid naive option: %s", got)
	}
}

func TestInboundMarshalFullKeepsNaiveHttpVersion(t *testing.T) {
	inb := Inbound{
		Id:   1,
		Type: "naive",
		Tag:  "naive-demo",
		Options: json.RawMessage(`{
			"listen_port": 1234,
			"http_version": "http3"
		}`),
	}

	got, err := inb.MarshalFull()
	if err != nil {
		t.Fatalf("MarshalFull returned error: %v", err)
	}

	if _, ok := (*got)["http_version"]; !ok {
		t.Fatalf("MarshalFull unexpectedly removed http_version: %#v", *got)
	}
}

func TestInboundMarshalJSONAddsTuicH3AlpnWhenTlsPresent(t *testing.T) {
	inb := Inbound{
		Type: "tuic",
		Tag:  "tuic-demo",
		Tls: &Tls{
			Server: json.RawMessage(`{
				"enabled": true,
				"server_name": "example.com"
			}`),
		},
		Options: json.RawMessage(`{
			"listen_port": 8080,
			"congestion_control": "cubic"
		}`),
	}

	raw, err := inb.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON returned error: %v", err)
	}

	got := string(raw)
	if !strings.Contains(got, `"alpn":["h3"]`) {
		t.Fatalf("expected tuic tls to include h3 alpn: %s", got)
	}
}

func TestInboundMarshalJSONPreservesExistingTuicAlpn(t *testing.T) {
	inb := Inbound{
		Type: "tuic",
		Tag:  "tuic-demo",
		Tls: &Tls{
			Server: json.RawMessage(`{
				"enabled": true,
				"server_name": "example.com",
				"alpn": ["h2", "http/1.1"]
			}`),
		},
		Options: json.RawMessage(`{
			"listen_port": 8080,
			"congestion_control": "cubic"
		}`),
	}

	raw, err := inb.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON returned error: %v", err)
	}

	got := string(raw)
	if !strings.Contains(got, `"alpn":["h2","http/1.1"]`) {
		t.Fatalf("expected tuic tls to preserve existing alpn: %s", got)
	}
	if strings.Contains(got, `"alpn":["h3"`) {
		t.Fatalf("expected tuic tls not to inject h3 when alpn exists: %s", got)
	}
}
