package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/sagernet/sing-box/option"
)

func TestParseMieruInboundDefaults(t *testing.T) {
	inbound := &model.Inbound{
		Type: "mieru",
		Tag:  "mieru-main",
		Options: json.RawMessage(`{
			"listen":"::",
			"listen_port":23456
		}`),
	}

	config, err := parseMieruInbound(inbound)
	if err != nil {
		t.Fatalf("parse Mieru inbound: %v", err)
	}
	if config.Transport != "TCP" {
		t.Fatalf("unexpected transport: %s", config.Transport)
	}
	if config.Multiplexing != "MULTIPLEXING_LOW" {
		t.Fatalf("unexpected multiplexing: %s", config.Multiplexing)
	}
	if config.HandshakeMode != "HANDSHAKE_STANDARD" {
		t.Fatalf("unexpected handshake mode: %s", config.HandshakeMode)
	}
	if config.MTU != 1400 {
		t.Fatalf("unexpected MTU: %d", config.MTU)
	}
	if !reflect.DeepEqual(config.Ports, []int{23456}) {
		t.Fatalf("unexpected ports: %#v", config.Ports)
	}
}

func TestParseMieruInboundPortRange(t *testing.T) {
	inbound := &model.Inbound{
		Type: "mieru",
		Tag:  "mieru-range",
		Options: json.RawMessage(`{
			"listen_port":23000,
			"port_range":"23000-23002",
			"transport":"udp",
			"multiplexing":"multiplexing_middle",
			"handshake_mode":"handshake_no_wait",
			"traffic_pattern":"balanced",
			"mtu":1380
		}`),
	}

	config, err := parseMieruInbound(inbound)
	if err != nil {
		t.Fatalf("parse Mieru range inbound: %v", err)
	}
	if config.Transport != "UDP" || config.HandshakeMode != "HANDSHAKE_NO_WAIT" {
		t.Fatalf("unexpected normalized config: %#v", config)
	}
	if config.TrafficPattern != "BALANCED" {
		t.Fatalf("unexpected traffic policy: %#v", config)
	}
	if !reflect.DeepEqual(config.Ports, []int{23000, 23001, 23002}) {
		t.Fatalf("unexpected ports: %#v", config.Ports)
	}
}

func TestParseMieruInboundPortsRejectsInvalidRanges(t *testing.T) {
	tests := []struct {
		port      int
		portRange string
	}{
		{port: 443, portRange: "500-510"},
		{port: 1024},
		{port: 1025, portRange: "1024-1030"},
		{port: 510, portRange: "510-500"},
		{port: 1000, portRange: "1000-1512"},
		{port: 20000, portRange: "invalid"},
	}
	for _, test := range tests {
		if _, err := parseMieruInboundPorts(test.port, test.portRange); err == nil {
			t.Fatalf("expected invalid port selection to fail: port=%d range=%q", test.port, test.portRange)
		}
	}
}

func TestParseMieruClientCredentialUsesClientName(t *testing.T) {
	client := &model.Client{
		Name:   "alice",
		Config: json.RawMessage(`{"mieru":{"name":"stale-name","password":"secret"}}`),
	}
	credential, err := parseMieruClientCredential(client)
	if err != nil {
		t.Fatalf("parse Mieru client credential: %v", err)
	}
	if credential.Name != "alice" || credential.Password != "secret" {
		t.Fatalf("unexpected credential: %#v", credential)
	}
}

func TestNormalizeMieruClientConfigPreservesPasswordAndRenamesUser(t *testing.T) {
	client := &model.Client{
		Name:   "renamed",
		Config: json.RawMessage(`{"mieru":{"name":"old","password":"keep-me"},"tuic":{"name":"old","password":"other"}}`),
	}
	if err := normalizeMieruClientConfig(client); err != nil {
		t.Fatalf("normalize Mieru client config: %v", err)
	}
	credential, err := parseMieruClientCredential(client)
	if err != nil {
		t.Fatalf("parse normalized credential: %v", err)
	}
	if credential.Name != "renamed" || credential.Password != "keep-me" {
		t.Fatalf("unexpected normalized credential: %#v", credential)
	}
	var configs map[string]map[string]interface{}
	if err := json.Unmarshal(client.Config, &configs); err != nil {
		t.Fatalf("parse normalized client config: %v", err)
	}
	if configs["tuic"]["password"] != "other" {
		t.Fatalf("normalization modified unrelated protocol config: %#v", configs)
	}
}

func TestBuildMitaServerConfigUsesOneInboundAndManyUsers(t *testing.T) {
	config, err := buildMitaServerConfig(
		&mieruInboundConfig{
			Tag:            "mieru-main",
			ListenPort:     20100,
			PortRange:      "20100-20102",
			Ports:          []int{20100, 20101, 20102},
			Transport:      "UDP",
			Multiplexing:   "MULTIPLEXING_LOW",
			HandshakeMode:  "HANDSHAKE_STANDARD",
			TrafficPattern: "ENHANCED",
			MTU:            1380,
		},
		[]mieruClientCredential{
			{Name: "alice", Password: "secret-one"},
			{Name: "bob", Password: "secret-two"},
		},
	)
	if err != nil {
		t.Fatalf("build mita config: %v", err)
	}
	if len(config.PortBindings) != 1 || len(config.Users) != 2 {
		t.Fatalf("unexpected shared config: %#v", config)
	}
	if config.PortBindings[0].PortRange != "20100-20102" {
		t.Fatalf("unexpected range binding: %#v", config.PortBindings[0])
	}
	if config.MTU != 1380 || config.DNS.DualStack != "PREFER_IPv4" {
		t.Fatalf("unexpected runtime defaults: %#v", config)
	}
	if len(config.Egress.Proxies) != 1 || len(config.Egress.Rules) != 1 {
		t.Fatalf("Mieru must route through the NovaPanel bridge: %#v", config.Egress)
	}
	proxy := config.Egress.Proxies[0]
	if proxy.Name != mieruBridgeProxyName || proxy.Protocol != "SOCKS5_PROXY_PROTOCOL" ||
		proxy.Host != mieruBridgeHost || proxy.Port <= 0 {
		t.Fatalf("unexpected Mieru routing bridge: %#v", proxy)
	}
	rule := config.Egress.Rules[0]
	if rule.Action != "PROXY" || !reflect.DeepEqual(rule.IPRanges, []string{"*"}) ||
		!reflect.DeepEqual(rule.DomainNames, []string{"*"}) ||
		!reflect.DeepEqual(rule.ProxyNames, []string{mieruBridgeProxyName}) {
		t.Fatalf("unexpected Mieru catch-all egress rule: %#v", rule)
	}
	if config.Users[0].HashedPassword != hashMieruPassword("alice", "secret-one") {
		t.Fatalf("unexpected hashed password: %s", config.Users[0].HashedPassword)
	}
	if config.TrafficPattern == nil || !config.TrafficPattern.UnlockAll {
		t.Fatalf("expected enhanced server traffic pattern: %#v", config.TrafficPattern)
	}
	payload, err := marshalMitaServerConfig(config)
	if err != nil {
		t.Fatalf("marshal Mieru config: %v", err)
	}
	if bytes.Contains(payload, []byte("secret-one")) || bytes.Contains(payload, []byte(`"password"`)) {
		t.Fatalf("runtime config leaked plaintext password: %s", payload)
	}
	if bytes.Contains(payload, []byte(`"quota"`)) {
		t.Fatalf("runtime config must use NovaPanel client quotas instead of native Mieru quotas: %s", payload)
	}
}

func TestBuildMieruBridgeInboundUsesOriginalTagAndLoopback(t *testing.T) {
	payload, err := buildMieruBridgeInbound("mieru-main", []mieruClientCredential{{Name: "alice", Password: "secret"}})
	if err != nil {
		t.Fatalf("build Mieru bridge inbound: %v", err)
	}
	var inbound map[string]interface{}
	if err := json.Unmarshal(payload, &inbound); err != nil {
		t.Fatalf("decode Mieru bridge inbound: %v", err)
	}
	if inbound["type"] != "socks" || inbound["tag"] != "mieru-main" || inbound["listen"] != mieruBridgeHost {
		t.Fatalf("unexpected Mieru bridge inbound: %#v", inbound)
	}
	if port, ok := inbound["listen_port"].(float64); !ok || port <= 0 {
		t.Fatalf("invalid Mieru bridge port: %#v", inbound["listen_port"])
	}
	users, ok := inbound["users"].([]interface{})
	if !ok || len(users) != 1 {
		t.Fatalf("Mieru bridge users were not configured: %#v", inbound["users"])
	}
	user := users[0].(map[string]interface{})
	if user["username"] != "alice" || user["password"] != hashMieruPassword("alice", "secret") {
		t.Fatalf("unexpected Mieru bridge credentials: %#v", user)
	}
	var parsed option.Inbound
	if err := parsed.UnmarshalJSONContext(core.NewCore().GetCtx(), payload); err != nil {
		t.Fatalf("sing-box rejected Mieru bridge inbound: %v", err)
	}
}

func TestBuildMitaServerConfigRejectsDuplicateUser(t *testing.T) {
	config := &mieruInboundConfig{
		Tag: "mieru-main", ListenPort: 20000, Ports: []int{20000},
		Transport: "TCP", Multiplexing: "MULTIPLEXING_LOW",
		HandshakeMode: "HANDSHAKE_STANDARD", TrafficPattern: "DEFAULT", MTU: 1400,
	}
	credentials := []mieruClientCredential{
		{Name: "duplicate", Password: "one"},
		{Name: "duplicate", Password: "two"},
	}
	if _, err := buildMitaServerConfig(config, credentials); err == nil {
		t.Fatal("expected duplicate Mieru username to be rejected")
	}
}

func TestHashMieruPasswordMatchesProtocol(t *testing.T) {
	sum := sha256.Sum256([]byte("secret\x00alice"))
	if got, want := hashMieruPassword("alice", "secret"), fmt.Sprintf("%x", sum); got != want {
		t.Fatalf("unexpected password hash: got %s want %s", got, want)
	}
}

func TestMieruTrafficPatternBase64(t *testing.T) {
	if got := MieruTrafficPatternBase64("balanced"); got != "CIcIEAAaBAgAEAAiCAgCEAAYBCAGKgQIIBBA" {
		t.Fatalf("unexpected balanced traffic pattern: %q", got)
	}
	if got := MieruTrafficPatternBase64("enhanced"); got != "CIUQEAEaBAgBEAUiCAgBEAEYBiAIKgUIQBCAAQ==" {
		t.Fatalf("unexpected enhanced traffic pattern: %q", got)
	}
	if got := MieruTrafficPatternBase64("default"); got != "" {
		t.Fatalf("default traffic pattern should be omitted: %q", got)
	}
}

func TestParseMitaAllUserStats(t *testing.T) {
	output := `User   LastActive                  1DayDown  1DayUp  7DaysDown  7DaysUp  30DaysDown  30DaysUp
alice  2026-07-29T10:20:30+08:00  10 MiB    2 MiB   40 MiB     8 MiB    90 MiB      20 MiB
bob    -                           1 KiB     2 KiB   3 KiB      4 KiB    5 KiB       6 KiB`

	stats := parseMitaAllUserStats(output)
	if stats["bob"]["last_active"] != "-" || stats["bob"]["day_download"] != "1 KiB" || stats["bob"]["month_upload"] != "6 KiB" {
		t.Fatalf("unexpected user stats: %#v", stats)
	}
}

func TestParseMitaMetricsAndTrafficCounters(t *testing.T) {
	output := []byte(`INFO metrics:
	{
		"connections":{"CurrEstablished":3,"MaxConn":9},
		"underlay":{"CurrEstablished":2,"UnsolicitedUDP":4},
		"cipher - server":{"FailedDirectDecrypt":7},
		"users":{"alice":{"DownloadBytes":1000,"UploadBytes":250}}
	}
	`)

	metrics := parseMitaMetrics(output)
	want := map[string]int64{
		"active_connections":   3,
		"max_connections":      9,
		"underlay_connections": 2,
		"unsolicited_udp":      4,
		"failed_decrypt":       7,
	}
	if !reflect.DeepEqual(metrics, want) {
		t.Fatalf("unexpected metrics: got %#v want %#v", metrics, want)
	}
	counters := parseMitaTrafficCounters(output)
	if counters["alice"].Download != 1000 || counters["alice"].Upload != 250 {
		t.Fatalf("unexpected traffic counters: %#v", counters)
	}
}
