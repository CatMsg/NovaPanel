package service

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
)

func TestParseMieruEndpointDefaults(t *testing.T) {
	endpoint := &model.Endpoint{
		Type: "mieru",
		Tag:  "mieru-main",
		Options: json.RawMessage(`{
			"server":"proxy.example.com",
			"port":23456,
			"username":"alice",
			"password":"secret"
		}`),
	}

	config, err := parseMieruEndpoint(endpoint)
	if err != nil {
		t.Fatalf("parse mieru endpoint: %v", err)
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

func TestParseMieruEndpointPortRange(t *testing.T) {
	endpoint := &model.Endpoint{
		Type: "mieru",
		Tag:  "mieru-range",
		Options: json.RawMessage(`{
			"server":"proxy.example.com",
			"port_range":"23000-23002",
			"transport":"udp",
			"username":"bob",
			"password":"secret",
			"multiplexing":"multiplexing_middle",
			"handshake_mode":"handshake_no_wait",
			"mtu":1380
		}`),
	}

	config, err := parseMieruEndpoint(endpoint)
	if err != nil {
		t.Fatalf("parse mieru range endpoint: %v", err)
	}
	if config.Transport != "UDP" || config.HandshakeMode != "HANDSHAKE_NO_WAIT" {
		t.Fatalf("unexpected normalized config: %#v", config)
	}
	if !reflect.DeepEqual(config.Ports, []int{23000, 23001, 23002}) {
		t.Fatalf("unexpected ports: %#v", config.Ports)
	}
}

func TestParseMieruPortsRejectsInvalidRanges(t *testing.T) {
	tests := []struct {
		port      int
		portRange string
	}{
		{port: 443, portRange: "500-510"},
		{port: 1024},
		{portRange: "1024-1030"},
		{portRange: "510-500"},
		{portRange: "1000-1512"},
		{portRange: "invalid"},
	}
	for _, test := range tests {
		if _, err := parseMieruPorts(test.port, test.portRange); err == nil {
			t.Fatalf("expected invalid port selection to fail: port=%d range=%q", test.port, test.portRange)
		}
	}
}

func TestBuildMitaServerConfigMergesEndpoints(t *testing.T) {
	config, err := buildMitaServerConfig([]*mieruEndpointConfig{
		{
			Tag: "one", Server: "one.example.com", Port: 20001, Ports: []int{20001},
			Transport: "TCP", Username: "alice", Password: "secret-one",
			Multiplexing: "MULTIPLEXING_LOW", HandshakeMode: "HANDSHAKE_STANDARD", MTU: 1400,
		},
		{
			Tag: "two", Server: "two.example.com", PortRange: "20100-20102", Ports: []int{20100, 20101, 20102},
			Transport: "UDP", Username: "bob", Password: "secret-two",
			Multiplexing: "MULTIPLEXING_LOW", HandshakeMode: "HANDSHAKE_STANDARD", MTU: 1380,
		},
	})
	if err != nil {
		t.Fatalf("build mita config: %v", err)
	}
	if len(config.PortBindings) != 2 || len(config.Users) != 2 {
		t.Fatalf("unexpected merged config: %#v", config)
	}
	if config.PortBindings[1].PortRange != "20100-20102" {
		t.Fatalf("unexpected range binding: %#v", config.PortBindings[1])
	}
	if config.MTU != 1380 {
		t.Fatalf("expected minimum configured MTU, got %d", config.MTU)
	}
	if config.DNS.DualStack != "PREFER_IPv4" {
		t.Fatalf("unexpected DNS mode: %s", config.DNS.DualStack)
	}
}

func TestBuildMitaServerConfigRejectsDuplicateUser(t *testing.T) {
	base := func(tag string) *mieruEndpointConfig {
		return &mieruEndpointConfig{
			Tag: tag, Server: "proxy.example.com", Port: 20000, Ports: []int{20000},
			Transport: "TCP", Username: "duplicate", Password: "secret",
			Multiplexing: "MULTIPLEXING_LOW", HandshakeMode: "HANDSHAKE_STANDARD", MTU: 1400,
		}
	}
	if _, err := buildMitaServerConfig([]*mieruEndpointConfig{base("one"), base("two")}); err == nil {
		t.Fatal("expected duplicate Mieru username to be rejected")
	}
}

func TestParseMitaUserStatsSelectsEndpointUser(t *testing.T) {
	output := `User   LastActive                  1DayDown  1DayUp  7DaysDown  7DaysUp  30DaysDown  30DaysUp
alice  2026-07-29T10:20:30+08:00  10 MiB    2 MiB   40 MiB     8 MiB    90 MiB      20 MiB
bob    -                           1 KiB     2 KiB   3 KiB      4 KiB    5 KiB       6 KiB`

	stats := parseMitaUserStats(output, "bob")
	if stats["last_active"] != "-" || stats["day_download"] != "1 KiB" || stats["month_upload"] != "6 KiB" {
		t.Fatalf("unexpected user stats: %#v", stats)
	}
}

func TestParseMitaMetricsExtractsRuntimeSignals(t *testing.T) {
	output := []byte(`INFO metrics:
	{
		"connections":{"CurrEstablished":3,"MaxConn":9},
		"underlay":{"CurrEstablished":2,"UnsolicitedUDP":4},
		"cipher - server":{"FailedDirectDecrypt":7}
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
}
