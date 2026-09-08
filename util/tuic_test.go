package util

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
)

func TestLinkGeneratorAddsTuicH3Alpn(t *testing.T) {
	inb := &model.Inbound{
		Id:    1,
		Type:  "tuic",
		Tag:   "tuic-demo",
		TlsId: 1,
		Tls: &model.Tls{
			Server: json.RawMessage(`{
				"enabled": true,
				"server_name": "example.com"
			}`),
			Client: json.RawMessage(`{
				"utls": {
					"enabled": true,
					"fingerprint": "chrome"
				}
			}`),
		},
		Addrs: json.RawMessage(`[]`),
		Options: json.RawMessage(`{
			"listen_port": 8080,
			"congestion_control": "cubic"
		}`),
	}

	links := LinkGenerator(json.RawMessage(`{
		"tuic": {
			"uuid": "11111111-1111-1111-1111-111111111111",
			"password": "secret"
		}
	}`), inb, "example.com")
	if len(links) != 1 {
		t.Fatalf("expected one tuic link, got %d", len(links))
	}
	if !strings.Contains(links[0], "alpn=h3") {
		t.Fatalf("expected tuic link to include h3 alpn: %s", links[0])
	}
}

func TestGetOutboundTuicAddsH3Alpn(t *testing.T) {
	out, _, err := GetOutbound("tuic://11111111-1111-1111-1111-111111111111:secret@example.com:8080?security=tls", 0)
	if err != nil {
		t.Fatalf("GetOutbound returned error: %v", err)
	}
	tls, ok := (*out)["tls"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected tls map in tuic outbound: %#v", *out)
	}
	alpn, ok := tls["alpn"].([]string)
	if !ok {
		raw, ok := tls["alpn"].([]interface{})
		if !ok {
			t.Fatalf("expected alpn array in tuic outbound: %#v", tls)
		}
		if len(raw) != 1 || raw[0] != "h3" {
			t.Fatalf("expected tuic outbound to include h3 alpn: %#v", tls)
		}
		return
	}
	if len(alpn) != 1 || alpn[0] != "h3" {
		t.Fatalf("expected tuic outbound to include h3 alpn: %#v", tls)
	}
}

func TestLinkGeneratorPreservesExistingTuicAlpn(t *testing.T) {
	inb := &model.Inbound{
		Id:    1,
		Type:  "tuic",
		Tag:   "tuic-demo",
		TlsId: 1,
		Tls: &model.Tls{
			Server: json.RawMessage(`{
				"enabled": true,
				"server_name": "example.com",
				"alpn": ["h2", "http/1.1"]
			}`),
			Client: json.RawMessage(`{
				"utls": {
					"enabled": true,
					"fingerprint": "chrome"
				}
			}`),
		},
		Addrs: json.RawMessage(`[]`),
		Options: json.RawMessage(`{
			"listen_port": 8080,
			"congestion_control": "cubic"
		}`),
	}

	links := LinkGenerator(json.RawMessage(`{
		"tuic": {
			"uuid": "11111111-1111-1111-1111-111111111111",
			"password": "secret"
		}
	}`), inb, "example.com")
	if len(links) != 1 {
		t.Fatalf("expected one tuic link, got %d", len(links))
	}
	if !strings.Contains(links[0], "alpn=h2,http/1.1") {
		t.Fatalf("expected tuic link to preserve existing alpn: %s", links[0])
	}
	if strings.Contains(links[0], "alpn=h3,") || strings.Contains(links[0], "alpn=h3") {
		t.Fatalf("expected tuic link not to inject h3 when alpn exists: %s", links[0])
	}
}
