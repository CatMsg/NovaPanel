package util

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
)

func TestMieruLinkGeneratorUsesClientCredentialAndInboundSettings(t *testing.T) {
	inbound := &model.Inbound{
		Type:  "mieru",
		Tag:   "mieru-main",
		Addrs: json.RawMessage(`[]`),
		Options: json.RawMessage(`{
			"listen":"::",
			"listen_port":22000,
			"port_range":"22000-22010",
			"transport":"TCP",
			"multiplexing":"MULTIPLEXING_MIDDLE",
			"handshake_mode":"HANDSHAKE_NO_WAIT",
			"traffic_pattern":"BALANCED",
			"mtu":1380
		}`),
	}
	clientConfig := json.RawMessage(`{"mieru":{"name":"ignored","password":"secret"}}`)

	links := LinkGenerator(clientConfig, inbound, "proxy.example.com")
	if len(links) != 1 {
		t.Fatalf("unexpected links: %#v", links)
	}
	parsed, err := url.Parse(links[0])
	if err != nil {
		t.Fatalf("parse generated Mieru link: %v", err)
	}
	if parsed.Scheme != "mierus" || parsed.User.Username() != "ignored" || parsed.Hostname() != "proxy.example.com" {
		t.Fatalf("unexpected generated link: %s", links[0])
	}
	password, _ := parsed.User.Password()
	if password != "secret" {
		t.Fatalf("unexpected generated password: %q", password)
	}
	query := parsed.Query()
	if query.Get("profile") != "mieru-main" || query.Get("port") != "22000-22010" || query.Get("protocol") != "TCP" {
		t.Fatalf("unexpected generated query: %#v", query)
	}
	if query.Get("multiplexing") != "MULTIPLEXING_MIDDLE" ||
		query.Get("handshake-mode") != "HANDSHAKE_NO_WAIT" ||
		query.Get("mtu") != "1380" ||
		query.Get("traffic-pattern") == "" {
		t.Fatalf("missing generated Mieru settings: %#v", query)
	}
}
