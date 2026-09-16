package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestNormalizeSingBoxConfigFor116(t *testing.T) {
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
