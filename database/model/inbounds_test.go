package model

import (
	"encoding/json"
	"strings"
	"testing"
)

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
