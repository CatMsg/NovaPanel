package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeSingBoxConfigFor115Plus(t *testing.T) {
	raw := []byte(`{
		"dns":{
			"servers":[
				{"type":"local","tag":"local"},
				{"type":"udp","tag":"remote","server":"1.1.1.1"}
			],
			"rules":[
				{"ip_cidr":["10.0.0.0/8"],"action":"route","server":"local","strategy":"ipv4_only"},
				{"action":"route","server":"remote"}
			],
			"independent_cache":true
		},
		"inbounds":[{
			"type":"tun",
			"tag":"tun-in",
			"address":["172.19.0.1/30"],
			"stack":"system",
			"tls":{"enabled":true,"acme":{"domain":["example.com"]}}
		}],
		"route":{
			"rule_set":[{
				"type":"remote",
				"tag":"remote-rules",
				"url":"https://example.com/rules.srs",
				"download_detour":"direct"
			}]
		},
		"experimental":{"cache_file":{"enabled":true,"store_rdrc":true,"rdrc_timeout":"7d"}}
	}`)

	normalized, err := NormalizeSingBoxConfig(raw)
	if err != nil {
		t.Fatalf("normalize config: %v", err)
	}
	var config map[string]interface{}
	if err = json.Unmarshal(normalized, &config); err != nil {
		t.Fatalf("decode normalized config: %v", err)
	}

	route := objectValue(config["route"])
	if got := stringValue(route["default_http_client"]); got != defaultHTTPClientTag {
		t.Fatalf("unexpected default HTTP client: %q", got)
	}
	clients := objectList(config["http_clients"])
	if len(clients) != 1 || stringValue(clients[0]["tag"]) != defaultHTTPClientTag {
		t.Fatalf("default HTTP client was not added: %#v", config["http_clients"])
	}
	ruleSets := objectList(route["rule_set"])
	if _, exists := ruleSets[0]["download_detour"]; exists {
		t.Fatal("deprecated download_detour was retained")
	}
	if _, exists := ruleSets[0]["http_client"]; exists {
		t.Fatalf("direct rule-set download must use the default HTTP client: %#v", ruleSets[0])
	}

	dns := objectValue(config["dns"])
	if _, exists := dns["independent_cache"]; exists {
		t.Fatal("deprecated independent_cache was retained")
	}
	if stringValue(dns["strategy"]) != "ipv4_only" {
		t.Fatalf("DNS rule strategy was not promoted to the global strategy: %#v", dns)
	}
	rules, _ := dns["rules"].([]interface{})
	if len(rules) != 3 {
		t.Fatalf("expected evaluate rule to be inserted, got %d rules", len(rules))
	}
	evaluate := objectValue(rules[0])
	if stringValue(evaluate["action"]) != "evaluate" || stringValue(evaluate["server"]) != "remote" {
		t.Fatalf("unexpected evaluate rule: %#v", evaluate)
	}
	filter := objectValue(rules[1])
	if !boolValue(filter["match_response"]) {
		t.Fatalf("address filter was not migrated to match_response: %#v", filter)
	}
	if _, exists := filter["strategy"]; exists {
		t.Fatal("deprecated DNS rule strategy was retained")
	}

	inbounds := objectList(config["inbounds"])
	if _, exists := inbounds[0]["stack"]; exists {
		t.Fatal("deprecated TUN stack was retained")
	}
	tlsOptions := objectValue(inbounds[0]["tls"])
	if _, exists := tlsOptions["acme"]; exists {
		t.Fatal("inline tls.acme was retained")
	}
	provider := objectValue(tlsOptions["certificate_provider"])
	if stringValue(provider["type"]) != "acme" {
		t.Fatalf("ACME certificate provider was not created: %#v", provider)
	}

	experimental := objectValue(config["experimental"])
	cacheFile := objectValue(experimental["cache_file"])
	if !boolValue(cacheFile["store_dns"]) {
		t.Fatalf("store_rdrc was not migrated to store_dns: %#v", cacheFile)
	}
	if _, exists := cacheFile["store_rdrc"]; exists {
		t.Fatal("deprecated store_rdrc was retained")
	}
	if _, exists := cacheFile["rdrc_timeout"]; exists {
		t.Fatal("deprecated rdrc_timeout was retained")
	}

	second, err := NormalizeSingBoxConfig(normalized)
	if err != nil {
		t.Fatalf("normalize config twice: %v", err)
	}
	var firstObject, secondObject interface{}
	if err = json.Unmarshal(normalized, &firstObject); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(second, &secondObject); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstObject, secondObject) {
		t.Fatalf("normalizer is not idempotent\nfirst: %s\nsecond: %s", normalized, second)
	}
}

func TestNormalizeSingBoxConfigRejectsAddressFilterWithoutDNSServer(t *testing.T) {
	_, err := NormalizeSingBoxConfig([]byte(`{
		"dns":{"rules":[{"ip_is_private":true,"action":"route","server":""}]},
		"route":{}
	}`))
	if err == nil || !strings.Contains(err.Error(), "无法确定 evaluate DNS 服务器") {
		t.Fatalf("expected an actionable DNS migration error, got %v", err)
	}
}

func TestNormalizeSingBoxConfigPreservesExplicitDefaultHTTPClient(t *testing.T) {
	normalized, err := NormalizeSingBoxConfig([]byte(`{
		"http_clients":[{"tag":"custom","detour":"direct"}],
		"dns":{},
		"route":{"default_http_client":"custom"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err = json.Unmarshal(normalized, &config); err != nil {
		t.Fatal(err)
	}
	route := objectValue(config["route"])
	if stringValue(route["default_http_client"]) != "custom" {
		t.Fatalf("explicit default HTTP client was overwritten: %#v", route)
	}
	if len(objectList(config["http_clients"])) != 1 {
		t.Fatalf("an unnecessary HTTP client was added: %#v", config["http_clients"])
	}
	if _, exists := objectList(config["http_clients"])[0]["detour"]; exists {
		t.Fatalf("a direct HTTP client detour was retained: %#v", config["http_clients"])
	}
}

func TestNormalizeSingBoxConfigPreservesNonDirectRuleSetDetour(t *testing.T) {
	normalized, err := NormalizeSingBoxConfig([]byte(`{
		"dns":{},
		"route":{"rule_set":[{
			"type":"remote",
			"tag":"remote-rules",
			"url":"https://example.com/rules.srs",
			"download_detour":"proxy-a"
		}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err = json.Unmarshal(normalized, &config); err != nil {
		t.Fatal(err)
	}
	ruleSet := objectList(objectValue(config["route"])["rule_set"])[0]
	httpClient := objectValue(ruleSet["http_client"])
	if stringValue(httpClient["detour"]) != "proxy-a" {
		t.Fatalf("non-direct detour was not migrated: %#v", ruleSet)
	}
}

func TestNormalizeSingBoxConfigRemovesDirectInlineRuleSetDetour(t *testing.T) {
	normalized, err := NormalizeSingBoxConfig([]byte(`{
		"dns":{},
		"route":{"rule_set":[{
			"type":"remote",
			"tag":"remote-rules",
			"url":"https://example.com/rules.srs",
			"http_client":{"detour":"direct"}
		}]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]interface{}
	if err = json.Unmarshal(normalized, &config); err != nil {
		t.Fatal(err)
	}
	ruleSet := objectList(objectValue(config["route"])["rule_set"])[0]
	if _, exists := ruleSet["http_client"]; exists {
		t.Fatalf("direct inline rule-set detour was retained: %#v", ruleSet)
	}
}

func TestDNSRuleNeedsResponseMigrationChecksLogicalChildren(t *testing.T) {
	rule := map[string]interface{}{
		"type":           "logical",
		"match_response": true,
		"rules": []interface{}{
			map[string]interface{}{"ip_is_private": true},
		},
	}
	if !dnsRuleNeedsResponseMigration(rule) {
		t.Fatal("a migrated logical parent must not hide a deprecated child filter")
	}
}

func TestNormalizeSingBoxConfigValidatesDNSRuleMapReferences(t *testing.T) {
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
			name:   "implicit local fallback",
			config: `{"route":{"rules":[{"type":"logical","mode":"and","rules":[{"dns_search_domain":{"local":"example.org"}}]}]}}`,
		},
		{
			name:    "empty map",
			config:  `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_server_address":{}}]}}`,
			wantErr: "non-empty map",
		},
		{
			name:    "empty list",
			config:  `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_server_address":{"local":[]}}]}}`,
			wantErr: "non-empty string or string list",
		},
		{
			name:    "blank key",
			config:  `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_search_domain":{" ":"example.org"}}]}}`,
			wantErr: "blank DNS server tag",
		},
		{
			name:    "blank value",
			config:  `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_search_domain":{"local":[" "]}}]}}`,
			wantErr: "must not be blank",
		},
		{
			name:    "invalid CIDR",
			config:  `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"dns_server_address":{"local":"192.0.2.1/99"}}]}}`,
			wantErr: "IP address or CIDR",
		},
		{
			name:    "nested DNS missing tag",
			config:  `{"dns":{"servers":[{"tag":"local","type":"local"}],"rules":[{"type":"logical","mode":"and","rules":[{"dns_server_address":{"missing":"192.0.2.1"}}]}]}}`,
			wantErr: "missing DNS server tag",
		},
		{
			name:    "nested route missing tag",
			config:  `{"dns":{"servers":[{"tag":"local","type":"local"}]},"route":{"rules":[{"type":"logical","mode":"and","rules":[{"dns_search_domain":{"missing":"example.org"}}]}]}}`,
			wantErr: "missing DNS server tag",
		},
		{
			name:    "unsupported DNS transport",
			config:  `{"dns":{"servers":[{"tag":"remote","type":"udp"}],"rules":[{"dns_server_address":{"remote":"192.0.2.1"}}]}}`,
			wantErr: "does not support dns_server_address",
		},
		{
			name:    "implicit local unavailable with explicit final",
			config:  `{"dns":{"final":"local","rules":[{"dns_search_domain":{"local":"example.org"}}]}}`,
			wantErr: "missing DNS server tag",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeSingBoxConfig([]byte(test.config))
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("normalize valid config: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestNormalizeSingBoxConfigRejectsHTTPVersionAndTLSMismatches(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "HTTP3 requires enabled TLS",
			config:  `{"inbounds":[{"type":"http","version":3,"tls":{"enabled":false}}]}`,
			wantErr: "version 3 requires an enabled TLS",
		},
		{
			name:    "invalid HTTP inbound version",
			config:  `{"inbounds":[{"type":"http","version":[1,4]}]}`,
			wantErr: "version must contain only 1, 2, or 3",
		},
		{
			name:    "HTTP3 outbound requires TLS",
			config:  `{"outbounds":[{"type":"http","version":3}]}`,
			wantErr: "version 3 requires TLS to be enabled",
		},
		{
			name:    "explicit HTTP3 with Host is invalid",
			config:  `{"outbounds":[{"type":"http","version":3,"tls":{"enabled":true},"headers":{"Host":["proxy.example"]}}]}`,
			wantErr: "versions 2 and 3 do not support path or Host",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NormalizeSingBoxConfig([]byte(test.config))
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

func TestNormalizeSingBoxConfigAllowsStandaloneH2CInbound(t *testing.T) {
	if _, err := NormalizeSingBoxConfig([]byte(`{"inbounds":[{"type":"http","tag":"standalone-h2c","version":[2]}]}`)); err != nil {
		t.Fatalf("standalone raw H2C inbound should remain valid: %v", err)
	}
}
