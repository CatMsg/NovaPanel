package service

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/core"
)

func TestValidateSingBoxConfigMigratesLosslessSingBox116Fields(t *testing.T) {
	raw := []byte(`{
		"dns":{"independent_cache":true,"strategy":"ipv4_only","rules":[{"rule_set_ip_cidr_accept_empty":false}]},
		"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs","download_detour":"proxy"}]},
		"experimental":{"cache_file":{"store_rdrc":true}}
	}`)

	validated, err := ValidateSingBoxConfig(raw)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	var got map[string]interface{}
	if err := json.Unmarshal(validated, &got); err != nil {
		t.Fatal(err)
	}
	if _, exists := objectValue(got["dns"])["independent_cache"]; exists {
		t.Fatal("deprecated independent_cache was not removed")
	}
	if objectValue(got["dns"])["strategy"] != "ipv4_only" {
		t.Fatalf("current DNS strategy was unexpectedly changed: %#v", got["dns"])
	}
	if _, exists := objectList(objectValue(got["dns"])["rules"])[0]["rule_set_ip_cidr_accept_empty"]; exists {
		t.Fatal("false rule_set_ip_cidr_accept_empty was not removed")
	}
	ruleSet := objectList(objectValue(got["route"])["rule_set"])[0]
	if _, exists := ruleSet["download_detour"]; exists {
		t.Fatal("deprecated download_detour was not removed")
	}
	if objectValue(ruleSet["http_client"])["detour"] != "proxy" {
		t.Fatalf("download_detour was not migrated: %#v", ruleSet)
	}
	cacheFile := objectValue(objectValue(got["experimental"])["cache_file"])
	if _, exists := cacheFile["store_rdrc"]; exists || !boolValue(cacheFile["store_dns"]) {
		t.Fatalf("store_rdrc was not migrated to store_dns: %#v", cacheFile)
	}
	if string(raw) != `{
		"dns":{"independent_cache":true,"strategy":"ipv4_only","rules":[{"rule_set_ip_cidr_accept_empty":false}]},
		"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs","download_detour":"proxy"}]},
		"experimental":{"cache_file":{"store_rdrc":true}}
	}` {
		t.Fatal("normalization mutated the caller's input buffer")
	}
}

func TestMigratedSingBox116FieldsParseWithPinnedCore(t *testing.T) {
	raw := []byte(`{
		"dns": {
			"strategy": "ipv4_only",
			"independent_cache": false,
			"servers": [{"type": "local", "tag": "local"}],
			"rules": [{"domain": ["example.org"], "rule_set_ip_cidr_accept_empty": false, "action": "route", "server": "local"}]
		},
		"outbounds": [{"type": "direct", "tag": "direct"}],
		"route": {
			"final": "direct",
			"rule_set": [{"type": "remote", "tag": "rules", "format": "binary", "url": "https://example.org/rules.srs", "download_detour": "direct"}]
		},
		"experimental": {"cache_file": {"enabled": true, "path": "/tmp/novapanel-migration-cache.db", "store_rdrc": true}}
	}`)

	validated, err := ValidateSingBoxConfig(raw)
	if err != nil {
		t.Fatalf("migrate config: %v", err)
	}
	if err := core.NewCore().ValidateConfig(validated); err != nil {
		t.Fatalf("pinned sing-box parser rejected migrated config: %v\n%s", err, validated)
	}
}

func TestValidateSingBoxConfigRejectsAmbiguousSingBox116Fields(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{name: "DNS action strategy", config: `{"dns":{"rules":[{"action":"route","server":"local","strategy":"ipv4_only"}]}}`, wantErr: "dns.rules[0].strategy"},
		{name: "rule set accepts empty IP match", config: `{"dns":{"rules":[{"rule_set":"geoip","rule_set_ip_cidr_accept_empty":true}]}}`, wantErr: "dns.rules[0].rule_set_ip_cidr_accept_empty"},
		{name: "DNS response CIDR", config: `{"dns":{"rules":[{"ip_cidr":"10.0.0.0/8","action":"route","server":"local"}]}}`, wantErr: "dns.rules[0] uses deprecated DNS response address matching"},
		{name: "DNS private IP response", config: `{"dns":{"rules":[{"ip_is_private":true,"action":"route","server":"local"}]}}`, wantErr: "dns.rules[0] uses deprecated DNS response address matching"},
		{name: "DNS response matcher with explicit migration", config: `{"dns":{"rules":[{"ip_cidr":"10.0.0.0/8","match_response":true,"action":"route","server":"local"}]}}`},
		{name: "RDRC timeout", config: `{"experimental":{"cache_file":{"rdrc_timeout":"7d"}}}`, wantErr: "experimental.cache_file.rdrc_timeout"},
		{name: "store DNS conflict", config: `{"experimental":{"cache_file":{"store_rdrc":true,"store_dns":false}}}`, wantErr: "conflicts with store_dns=false"},
		{name: "remote ruleset invalid default HTTP client", config: `{"route":{"default_http_client":{},"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`, wantErr: "route.default_http_client must be a string"},
		{name: "remote ruleset invalid final outbound", config: `{"route":{"final":{},"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`, wantErr: "route.final must be a string"},
		{name: "remote ruleset inline HTTP client", config: `{"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs","http_client":{"detour":"proxy-a"}}]}}`},
		{name: "remote ruleset explicit default HTTP client", config: `{"http_clients":[{"tag":"default-client"}],"route":{"default_http_client":"default-client","rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`},
		{name: "remote ruleset unknown named HTTP client", config: `{"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs","http_client":"missing"}]}}`, wantErr: "route.rule_set[0].http_client 引用了不存在的 HTTP 客户端"},
		{name: "conflicting rule set clients", config: `{"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs","download_detour":"proxy-a","http_client":{"detour":"proxy-b"}}]}}`, wantErr: "download_detour conflicts with http_client.detour"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateSingBoxConfig([]byte(test.config))
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("valid config rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateSingBoxConfigMigratesLegacyDirectRouteActions(t *testing.T) {
	raw := []byte(`{
		"outbounds":[{"type":"direct","tag":"novapanel-direct-action-0"}],
		"route":{"rules":[
			{"domain":"example.com","action":"direct","bind_interface":"eth0","routing_mark":42,"tcp_fast_open":true},
			{"type":"logical","mode":"and","rules":[{"domain":"nested.example"}]},
			{"domain":"other.example","action":"direct","reuse_addr":true,"udp_fragment":true}
		]}
	}`)
	validated, err := ValidateSingBoxConfig(raw)
	if err != nil {
		t.Fatalf("migrate config: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(validated, &config); err != nil {
		t.Fatal(err)
	}
	rules := objectList(objectValue(config["route"])["rules"])
	if rules[0]["action"] != "route" || rules[0]["outbound"] != "novapanel-direct-action-0-2" {
		t.Fatalf("top-level direct action was not rewritten safely: %#v", rules[0])
	}
	if _, exists := rules[0]["bind_interface"]; exists {
		t.Fatal("legacy dialer option remained on the route rule")
	}
	nested := objectList(rules[1]["rules"])
	if len(nested) != 1 || nested[0]["domain"] != "nested.example" || nested[0]["action"] != nil {
		t.Fatalf("logical match rule was unexpectedly modified: %#v", nested)
	}
	if rules[2]["action"] != "route" || rules[2]["outbound"] != "novapanel-direct-action-2" {
		t.Fatalf("second direct action was not migrated: %#v", rules[2])
	}
	outbounds := objectList(config["outbounds"])
	if len(outbounds) != 3 {
		t.Fatalf("expected existing outbound and two generated outbounds, got %#v", config["outbounds"])
	}
	generated := outbounds[1:]
	if generated[0]["type"] != "direct" || generated[0]["bind_interface"] != "eth0" || generated[0]["routing_mark"] != float64(42) || generated[0]["tcp_fast_open"] != true || generated[0]["udp_fragment"] != false {
		t.Fatalf("top-level dialer options were not preserved: %#v", generated[0])
	}
	if generated[1]["type"] != "direct" || generated[1]["tag"] != "novapanel-direct-action-2" || generated[1]["reuse_addr"] != true || generated[1]["udp_fragment"] != true {
		t.Fatalf("nested dialer options were not preserved: %#v", generated[1])
	}
	if err := core.NewCore().ValidateConfig(validated); err != nil {
		t.Fatalf("pinned sing-box parser rejected migrated config: %v", err)
	}
}

func TestValidateSingBoxConfigMigratesImplicitRuleSetHTTPClient(t *testing.T) {
	tests := []struct {
		name             string
		config           string
		wantDefaultTag   string
		wantClientTag    string
		wantClientDetour string
		wantClientCount  int
	}{
		{
			name:            "generated direct fallback",
			config:          `{"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
			wantDefaultTag:  "novapanel-default",
			wantClientTag:   "novapanel-default",
			wantClientCount: 1,
		},
		{
			name:             "configured final outbound",
			config:           `{"outbounds":[{"type":"socks","tag":"proxy-a","server":"proxy.example","server_port":1080}],"route":{"final":"proxy-a","rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
			wantDefaultTag:   "novapanel-default",
			wantClientTag:    "novapanel-default",
			wantClientDetour: "proxy-a",
			wantClientCount:  1,
		},
		{
			name:            "no final outbound retains generated direct despite other outbounds",
			config:          `{"outbounds":[{"type":"socks","tag":"proxy-a","server":"proxy.example","server_port":1080}],"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
			wantDefaultTag:  "novapanel-default",
			wantClientTag:   "novapanel-default",
			wantClientCount: 1,
		},
		{
			name:            "existing client list uses core default first entry",
			config:          `{"http_clients":[{"tag":"first-client"},{"tag":"second-client"}],"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
			wantDefaultTag:  "first-client",
			wantClientTag:   "first-client",
			wantClientCount: 2,
		},
		{
			name:            "existing direct outbound stays direct",
			config:          `{"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct","rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
			wantDefaultTag:  "novapanel-default",
			wantClientTag:   "novapanel-default",
			wantClientCount: 1,
		},
		{
			name:             "configured direct outbound settings are retained",
			config:           `{"outbounds":[{"type":"direct","tag":"direct","bind_interface":"eth0"}],"route":{"final":"direct","rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
			wantDefaultTag:   "novapanel-default",
			wantClientTag:    "novapanel-default",
			wantClientDetour: "direct",
			wantClientCount:  1,
		},
		{
			name:            "zero direct timeout remains equivalent to direct fallback",
			config:          `{"outbounds":[{"type":"direct","tag":"direct","connect_timeout":"0s"}],"route":{"final":"direct","rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
			wantDefaultTag:  "novapanel-default",
			wantClientTag:   "novapanel-default",
			wantClientCount: 1,
		},
		{
			name:             "empty explicit resolver pointer is retained",
			config:           `{"outbounds":[{"type":"direct","tag":"direct","domain_resolver":{}}],"route":{"final":"direct","rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
			wantDefaultTag:   "novapanel-default",
			wantClientTag:    "novapanel-default",
			wantClientDetour: "direct",
			wantClientCount:  1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			validated, err := ValidateSingBoxConfig([]byte(test.config))
			if err != nil {
				t.Fatalf("validate config: %v", err)
			}
			var config map[string]interface{}
			if err := json.Unmarshal(validated, &config); err != nil {
				t.Fatal(err)
			}
			route := objectValue(config["route"])
			if route["default_http_client"] != test.wantDefaultTag {
				t.Fatalf("default_http_client = %#v, want %q", route["default_http_client"], test.wantDefaultTag)
			}
			clients := objectList(config["http_clients"])
			if len(clients) != test.wantClientCount || stringValue(clients[0]["tag"]) != test.wantClientTag {
				t.Fatalf("http_clients = %#v, want %d entries beginning with %q", clients, test.wantClientCount, test.wantClientTag)
			}
			if got := stringValue(clients[0]["detour"]); got != test.wantClientDetour {
				t.Fatalf("default client detour = %q, want %q", got, test.wantClientDetour)
			}
		})
	}
}

func TestImplicitRuleSetHTTPClientMigrationParsesWithPinnedCore(t *testing.T) {
	configs := []string{
		`{"route":{"rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
		`{"outbounds":[{"type":"socks","tag":"proxy-a","server":"proxy.example","server_port":1080}],"route":{"final":"proxy-a","rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
		`{"outbounds":[{"type":"direct","tag":"direct"}],"route":{"final":"direct","rule_set":[{"type":"remote","tag":"rules","url":"https://example.com/rules.srs"}]}}`,
	}
	for index, raw := range configs {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			validated, err := ValidateSingBoxConfig([]byte(raw))
			if err != nil {
				t.Fatalf("migrate config: %v", err)
			}
			if err := core.NewCore().ValidateConfig(validated); err != nil {
				t.Fatalf("pinned sing-box parser rejected migrated config: %v", err)
			}
		})
	}
}

func TestValidateSingBoxConfigRequiresObject(t *testing.T) {
	if _, err := ValidateSingBoxConfig([]byte(`null`)); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("expected a root object validation error, got %v", err)
	}
	if _, err := ValidateSingBoxConfig([]byte(`{} {}`)); err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("expected trailing JSON values to be rejected, got %v", err)
	}
}

func TestValidateSingBoxConfigPreservesLargeIntegerDuringMigration(t *testing.T) {
	raw := []byte(`{"experimental":{"cache_file":{"store_rdrc":false}},"inbounds":[{"type":"openvpn","renegotiate_bytes":9007199254740993}]}`)
	validated, err := ValidateSingBoxConfig(raw)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	if !strings.Contains(string(validated), `"renegotiate_bytes": 9007199254740993`) {
		t.Fatalf("large integer was changed during migration: %s", validated)
	}
}

func TestDefaultConfigUsesExplicitHTTPClientForRemoteRuleSets(t *testing.T) {
	validated, err := ValidateSingBoxConfig([]byte(defaultConfig))
	if err != nil {
		t.Fatalf("validate default config: %v", err)
	}
	var config map[string]interface{}
	if err := json.Unmarshal(validated, &config); err != nil {
		t.Fatal(err)
	}
	route := objectValue(config["route"])
	if route["default_http_client"] != "novapanel-default" || !hasHTTPClientTag(config["http_clients"], "novapanel-default") {
		t.Fatalf("default config lacks an explicit default HTTP client: %#v", config)
	}
}

func TestValidateSingBoxConfigRejectsShadowsocksNoneWithUsers(t *testing.T) {
	if _, err := ValidateSingBoxConfig([]byte(`{"inbounds":[{"type":"shadowsocks","tag":"ss","method":"none","users":[{"name":"user","password":"secret"}]}]}`)); err == nil || !strings.Contains(err.Error(), "不支持多用户入站") {
		t.Fatalf("expected a clear multi-user Shadowsocks method error, got %v", err)
	}
	if _, err := ValidateSingBoxConfig([]byte(`{"inbounds":[{"type":"shadowsocks","tag":"ss","method":"none"}]}`)); err != nil {
		t.Fatalf("standalone Shadowsocks method=none should remain valid: %v", err)
	}
}

func TestValidateSingBoxConfigValidatesDNSRuleMapReferences(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:   "valid scalar and list values in nested rules",
			config: `{"dns":{"servers":[{"tag":"local","type":"local"},{"tag":"resolved","type":"resolved"}],"rules":[{"type":"logical","mode":"and","rules":[{"dns_server_address":{"local":["192.0.2.0/24","2001:db8::1"],"resolved":"198.51.100.2"},"dns_search_domain":{"local":["Example.COM.","example.net"],"resolved":"example.org"}}]}]},"route":{"rules":[]}}`,
		},
		{
			name:   "implicit local resolver",
			config: `{"route":{"rules":[{"type":"logical","mode":"and","rules":[{"dns_search_domain":{"local":"example.org"}}]}]}}`,
		},
		{name: "empty map", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_server_address":{}}]}}`, wantErr: "non-empty map"},
		{name: "empty list", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_server_address":{"local":[]}}]}}`, wantErr: "non-empty string or string list"},
		{name: "blank key", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_search_domain":{" ":"example.org"}}]}}`, wantErr: "blank DNS server tag"},
		{name: "blank value", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_search_domain":{"local":[" "]}}]}}`, wantErr: "must not be blank"},
		{name: "invalid CIDR", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_server_address":{"local":"192.0.2.1/99"}}]}}`, wantErr: "IP address or CIDR"},
		{name: "missing DNS server", config: `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"type":"logical","mode":"and","rules":[{"dns_server_address":{"missing":"192.0.2.1"}}]}]}}`, wantErr: "missing DNS server tag"},
		{name: "unsupported DNS transport", config: `{"dns":{"servers":[{"tag":"remote","type":"udp"}],"rules":[{"dns_server_address":{"remote":"192.0.2.1"}}]}}`, wantErr: "does not support dns_server_address"},
		{name: "implicit local unavailable with explicit final", config: `{"dns":{"final":"local","rules":[{"dns_search_domain":{"local":"example.org"}}]}}`, wantErr: "missing DNS server tag"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateSingBoxConfig([]byte(test.config))
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("valid config rejected: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateSingBoxConfigRejectsHTTPVersionAndTLSMismatches(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{name: "HTTP3 requires enabled TLS", config: `{"inbounds":[{"type":"http","version":3,"tls":{"enabled":false}}]}`, wantErr: "version 3 requires an enabled TLS"},
		{name: "invalid HTTP inbound version", config: `{"inbounds":[{"type":"http","version":[1,4]}]}`, wantErr: "version must contain only 1, 2, or 3"},
		{name: "HTTP3 outbound requires TLS", config: `{"outbounds":[{"type":"http","version":3}]}`, wantErr: "version 3 requires TLS to be enabled"},
		{name: "HTTP3 with Host is invalid", config: `{"outbounds":[{"type":"http","version":3,"tls":{"enabled":true},"headers":{"Host":["proxy.example"]}}]}`, wantErr: "versions 2 and 3 do not support path or Host"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ValidateSingBoxConfig([]byte(test.config))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateSingBoxConfigAllowsStandaloneH2CInbound(t *testing.T) {
	if _, err := ValidateSingBoxConfig([]byte(`{"inbounds":[{"type":"http","tag":"standalone-h2c","version":[2]}]}`)); err != nil {
		t.Fatalf("standalone raw H2C inbound should remain valid: %v", err)
	}
}

func TestValidateSingBoxConfigMigratesDeprecatedHysteriaTuningFields(t *testing.T) {
	tests := []struct {
		section string
		field   string
		newName string
	}{
		{section: "inbounds", field: "recv_window_conn", newName: "stream_receive_window"},
		{section: "inbounds", field: "recv_window_client", newName: "connection_receive_window"},
		{section: "inbounds", field: "max_conn_client", newName: "max_concurrent_streams"},
		{section: "inbounds", field: "disable_mtu_discovery", newName: "disable_path_mtu_discovery"},
		{section: "outbounds", field: "recv_window", newName: "connection_receive_window"},
		{section: "outbounds", field: "recv_window_conn", newName: "stream_receive_window"},
		{section: "outbounds", field: "disable_mtu_discovery", newName: "disable_path_mtu_discovery"},
	}
	for _, test := range tests {
		t.Run(test.section+"/"+test.field, func(t *testing.T) {
			config := `{"` + test.section + `":[{"type":"hysteria","tag":"hy","` + test.field + `":1}]}`
			validated, err := ValidateSingBoxConfig([]byte(config))
			if err != nil {
				t.Fatalf("migrate legacy option: %v", err)
			}
			var got map[string]interface{}
			if err := json.Unmarshal(validated, &got); err != nil {
				t.Fatal(err)
			}
			item := objectList(got[test.section])[0]
			if _, exists := item[test.field]; exists {
				t.Fatalf("legacy field %q remains: %#v", test.field, item)
			}
			if item[test.newName] != float64(1) {
				t.Fatalf("migrated %q = %#v, want 1", test.newName, item[test.newName])
			}
		})
	}
}

func TestValidateSingBoxConfigRejectsConflictingHysteriaTuningFields(t *testing.T) {
	config := `{"outbounds":[{"type":"hysteria","tag":"hy","recv_window_conn":1,"stream_receive_window":2}]}`
	_, err := ValidateSingBoxConfig([]byte(config))
	if err == nil || !strings.Contains(err.Error(), "outbounds[0].recv_window_conn conflicts with outbounds[0].stream_receive_window") {
		t.Fatalf("error = %v, want conflicting legacy/current Hysteria field error", err)
	}
}

func TestMigrateDeprecatedHysteriaProtocolOptionsOnSave(t *testing.T) {
	tests := []struct {
		section string
		legacy  string
		current string
	}{
		{section: "inbounds", legacy: "recv_window_client", current: "connection_receive_window"},
		{section: "outbounds", legacy: "recv_window", current: "connection_receive_window"},
	}
	for _, test := range tests {
		t.Run(test.section, func(t *testing.T) {
			raw, err := migrateDeprecatedHysteriaProtocolOptions(test.section, "hysteria", json.RawMessage(`{"`+test.legacy+`":1234}`))
			if err != nil {
				t.Fatalf("migrate saved options: %v", err)
			}
			var got map[string]interface{}
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatal(err)
			}
			if _, exists := got[test.legacy]; exists {
				t.Fatalf("saved options retained legacy field %q: %#v", test.legacy, got)
			}
			if got[test.current] != float64(1234) {
				t.Fatalf("saved field %q = %#v, want 1234", test.current, got[test.current])
			}
		})
	}
}

func TestValidateSingBoxConfigAllowsCurrentHysteriaTuningFields(t *testing.T) {
	config := `{"inbounds":[{"type":"hysteria","tag":"hy-in","connection_receive_window":1234,"stream_receive_window":5678,"max_concurrent_streams":32,"disable_path_mtu_discovery":true}],"outbounds":[{"type":"hysteria","tag":"hy-out","connection_receive_window":1234,"stream_receive_window":5678,"disable_path_mtu_discovery":true}]}`
	if _, err := ValidateSingBoxConfig([]byte(config)); err != nil {
		t.Fatalf("current Hysteria fields rejected: %v", err)
	}
}
