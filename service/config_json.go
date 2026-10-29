package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strconv"

	"github.com/sagernet/sing-box/option"
)

// ValidateSingBoxConfig applies safe forward migrations and validates
// panel-specific invariants. sing-box remains the authority for core validation.
func ValidateSingBoxConfig(raw []byte) ([]byte, error) {
	var config map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&config); err != nil {
		return nil, err
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, errors.New("sing-box config contains multiple JSON values")
		}
		return nil, err
	}
	if config == nil {
		return nil, errors.New("sing-box config must be a JSON object")
	}
	if err := migrateSingBox116Config(config); err != nil {
		return nil, err
	}
	if err := migrateLegacyDirectRouteActions(config); err != nil {
		return nil, err
	}
	if err := validateDeprecatedHysteriaOptions(config); err != nil {
		return nil, err
	}
	if err := validateHTTPProxyConfig(config); err != nil {
		return nil, err
	}
	if err := validateShadowsocksInboundUsers(config); err != nil {
		return nil, err
	}
	if err := validateDNSRuleMapValues(config); err != nil {
		return nil, err
	}
	formatted, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("format sing-box config: %w", err)
	}
	return formatted, nil
}

// migrateSingBox116Config applies only upstream-documented, lossless migrations.
// Deprecated fields with no safe one-to-one replacement are rejected with a
// JSON path instead of being silently discarded.
func migrateSingBox116Config(config map[string]interface{}) error {
	if dns := objectValue(config["dns"]); dns != nil {
		if value, exists := dns["independent_cache"]; exists {
			if value != nil {
				if _, ok := value.(bool); !ok {
					return errors.New("dns.independent_cache must be a boolean")
				}
			}
			delete(dns, "independent_cache")
		}
		if err := validateSingBox116DNSRules(dns["rules"], "dns.rules"); err != nil {
			return err
		}
	}

	if experimental := objectValue(config["experimental"]); experimental != nil {
		if cacheFile := objectValue(experimental["cache_file"]); cacheFile != nil {
			if value, exists := cacheFile["rdrc_timeout"]; exists {
				if value != nil {
					return errors.New("experimental.cache_file.rdrc_timeout has no lossless sing-box 1.16 replacement; review and remove it manually")
				}
				delete(cacheFile, "rdrc_timeout")
			}
			if value, exists := cacheFile["store_rdrc"]; exists {
				legacyValue, ok := value.(bool)
				if !ok && value != nil {
					return errors.New("experimental.cache_file.store_rdrc must be a boolean")
				}
				if legacyValue {
					if currentValue, set := cacheFile["store_dns"]; set && currentValue != nil {
						current, valid := currentValue.(bool)
						if !valid {
							return errors.New("experimental.cache_file.store_dns must be a boolean")
						}
						if !current {
							return errors.New("experimental.cache_file.store_rdrc=true conflicts with store_dns=false")
						}
					} else {
						cacheFile["store_dns"] = true
					}
				}
				delete(cacheFile, "store_rdrc")
			}
		}
	}

	route := objectValue(config["route"])
	if route == nil {
		return nil
	}
	ruleSets := objectList(route["rule_set"])
	for index, ruleSet := range ruleSets {
		if stringValue(ruleSet["type"]) != "remote" {
			continue
		}
		if err := migrateRuleSetDownloadDetour(ruleSet, fmt.Sprintf("route.rule_set[%d]", index)); err != nil {
			return err
		}
	}

	defaultHTTPClient, validDefaultHTTPClient := route["default_http_client"].(string)
	if value, exists := route["default_http_client"]; exists && value != nil && !validDefaultHTTPClient {
		return errors.New("route.default_http_client must be a string")
	}
	if !validDefaultHTTPClient {
		defaultHTTPClient = ""
	}
	if value, exists := route["final"]; exists && value != nil {
		if _, ok := value.(string); !ok {
			return errors.New("route.final must be a string")
		}
	}
	if defaultHTTPClient != "" && !hasHTTPClientTag(config["http_clients"], defaultHTTPClient) {
		return fmt.Errorf("route.default_http_client 引用了不存在的 HTTP 客户端: %s", defaultHTTPClient)
	}
	needsDefaultHTTPClient := false
	for index, ruleSet := range ruleSets {
		if stringValue(ruleSet["type"]) != "remote" {
			continue
		}
		client := ruleSet["http_client"]
		if !hasExplicitRuleSetHTTPClient(client) {
			needsDefaultHTTPClient = true
			continue
		}
		if tag, ok := client.(string); ok && !hasHTTPClientTag(config["http_clients"], tag) {
			return fmt.Errorf("route.rule_set[%d].http_client 引用了不存在的 HTTP 客户端: %s", index, tag)
		}
	}
	if needsDefaultHTTPClient {
		if err := ensureDefaultRuleSetHTTPClient(config, route, defaultHTTPClient); err != nil {
			return err
		}
	}
	return nil
}

// migrateLegacyDirectRouteActions converts removed direct actions on route
// rules to dedicated direct outbounds. Nested logical match rules do not carry
// actions and are intentionally left untouched.
func migrateLegacyDirectRouteActions(config map[string]interface{}) error {
	route := objectValue(config["route"])
	if route == nil {
		return nil
	}
	rules, ok := route["rules"].([]interface{})
	if !ok || len(rules) == 0 {
		return nil
	}

	var outbounds []interface{}
	if value, exists := config["outbounds"]; exists && value != nil {
		var valid bool
		outbounds, valid = value.([]interface{})
		if !valid {
			return errors.New("outbounds must be an array to migrate legacy direct route actions")
		}
	}
	usedTags := make(map[string]struct{}, len(outbounds))
	for _, outbound := range objectList(outbounds) {
		if tag := stringValue(outbound["tag"]); tag != "" {
			usedTags[tag] = struct{}{}
		}
	}

	changed := false
	for index, rule := range rules {
		object, ok := rule.(map[string]interface{})
		if !ok {
			continue
		}
		if err := migrateLegacyDirectRouteRule(object, strconv.Itoa(index), &outbounds, usedTags, &changed); err != nil {
			return err
		}
	}
	if changed {
		config["outbounds"] = outbounds
	}
	return nil
}

var legacyDirectActionDialerFields = []string{
	"bind_interface",
	"inet4_bind_address",
	"inet6_bind_address",
	"bind_address_no_port",
	"protect_path",
	"routing_mark",
	"reuse_addr",
	"netns",
	"connect_timeout",
	"tcp_fast_open",
	"tcp_multi_path",
	"disable_tcp_keep_alive",
	"tcp_keep_alive",
	"tcp_keep_alive_interval",
	"udp_fragment",
	"domain_resolver",
	"network_strategy",
	"network_type",
	"fallback_network_type",
	"fallback_delay",
	"domain_strategy",
}

func migrateLegacyDirectRouteRule(rule map[string]interface{}, path string, outbounds *[]interface{}, usedTags map[string]struct{}, changed *bool) error {
	if stringValue(rule["action"]) == "direct" {
		if outbound, exists := rule["outbound"]; exists && outbound != nil && outbound != "" {
			return fmt.Errorf("route.rules[%s] legacy direct action cannot also specify outbound", path)
		}
		tag := "novapanel-direct-action-" + path
		for suffix := 2; ; suffix++ {
			if _, exists := usedTags[tag]; !exists {
				break
			}
			tag = fmt.Sprintf("novapanel-direct-action-%s-%d", path, suffix)
		}
		usedTags[tag] = struct{}{}

		dialerOptions := make(map[string]interface{})
		for _, field := range legacyDirectActionDialerFields {
			if value, exists := rule[field]; exists {
				dialerOptions[field] = value
				delete(rule, field)
			}
		}
		if fragment, exists := dialerOptions["udp_fragment"]; !exists || fragment == nil {
			// The old direct action defaulted UDP fragmentation off; direct
			// outbounds default it on unless the option is explicit.
			dialerOptions["udp_fragment"] = false
		}
		outbound := map[string]interface{}{"type": "direct", "tag": tag}
		for field, value := range dialerOptions {
			outbound[field] = value
		}
		*outbounds = append(*outbounds, outbound)
		rule["action"] = "route"
		rule["outbound"] = tag
		*changed = true
	}

	return nil
}

func ensureDefaultRuleSetHTTPClient(config, route map[string]interface{}, defaultTag string) error {
	if defaultTag != "" {
		return nil
	}
	clientsValue, hasClients := config["http_clients"]
	clients, isList := clientsValue.([]interface{})
	if hasClients && clientsValue != nil && !isList {
		return errors.New("http_clients must be an array to migrate the implicit default client")
	}
	if len(clients) > 0 {
		tag := stringValue(objectValue(clients[0])["tag"])
		if tag == "" {
			return errors.New("http_clients[0].tag is required to migrate the implicit default client")
		}
		route["default_http_client"] = tag
		return nil
	}

	const tag = "novapanel-default"
	client := map[string]interface{}{"tag": tag}
	if outboundTag := defaultOutboundTag(route); outboundTag != "" && defaultOutboundNeedsDetour(config, outboundTag) {
		client["detour"] = outboundTag
	}
	config["http_clients"] = []interface{}{client}
	route["default_http_client"] = tag
	return nil
}

func defaultOutboundTag(route map[string]interface{}) string {
	return stringValue(route["final"])
}

func defaultOutboundNeedsDetour(config map[string]interface{}, tag string) bool {
	for _, outbound := range objectList(config["outbounds"]) {
		if stringValue(outbound["tag"]) != tag {
			continue
		}
		if stringValue(outbound["type"]) != "direct" {
			return true
		}
		rawOptions, err := json.Marshal(outbound)
		if err != nil {
			return true
		}
		var directOptions option.DirectOutboundOptions
		if err := json.Unmarshal(rawOptions, &directOptions); err != nil {
			return true
		}
		directOptions.UDPFragmentDefault = true
		defaultOptions := option.DialerOptions{
			AbstractDialerOptions: option.AbstractDialerOptions{UDPFragmentDefault: true},
		}
		return !reflect.DeepEqual(directOptions.DialerOptions, defaultOptions)
	}
	return true
}

func migrateRuleSetDownloadDetour(ruleSet map[string]interface{}, path string) error {
	legacy, exists := ruleSet["download_detour"]
	if !exists {
		return nil
	}
	detour, ok := legacy.(string)
	if !ok && legacy != nil {
		return fmt.Errorf("%s.download_detour must be a string", path)
	}
	if detour == "" {
		delete(ruleSet, "download_detour")
		return nil
	}

	client, exists := ruleSet["http_client"]
	if !exists || client == nil || client == "" {
		ruleSet["http_client"] = map[string]interface{}{"detour": detour}
	} else {
		clientOptions, ok := client.(map[string]interface{})
		if !ok {
			return fmt.Errorf("%s.download_detour conflicts with its non-object http_client; choose one HTTP client configuration", path)
		}
		if currentValue, set := clientOptions["detour"]; set && currentValue != nil {
			current, valid := currentValue.(string)
			if !valid {
				return fmt.Errorf("%s.http_client.detour must be a string", path)
			}
			if current != "" && current != detour {
				return fmt.Errorf("%s.download_detour conflicts with http_client.detour", path)
			}
		}
		clientOptions["detour"] = detour
	}
	delete(ruleSet, "download_detour")
	return nil
}

func validateSingBox116DNSRules(value interface{}, path string) error {
	for index, rule := range objectList(value) {
		if err := validateSingBox116DNSRule(rule, fmt.Sprintf("%s[%d]", path, index)); err != nil {
			return err
		}
	}
	return nil
}

func validateSingBox116DNSRule(rule map[string]interface{}, path string) error {
	if _, exists := rule["strategy"]; exists {
		return fmt.Errorf("%s.strategy has no lossless sing-box 1.16 replacement; migrate this rule's DNS strategy manually", path)
	}
	if value, exists := rule["rule_set_ip_cidr_accept_empty"]; exists {
		legacy, ok := value.(bool)
		if !ok && value != nil {
			return fmt.Errorf("%s.rule_set_ip_cidr_accept_empty must be a boolean", path)
		}
		if legacy {
			return fmt.Errorf("%s.rule_set_ip_cidr_accept_empty has no lossless sing-box 1.16 replacement; review the rule-set matching behavior manually", path)
		}
		delete(rule, "rule_set_ip_cidr_accept_empty")
	}

	_, hasIPCIDR := rule["ip_cidr"]
	_, hasIPIsPrivate := rule["ip_is_private"]
	if (hasIPCIDR || hasIPIsPrivate) && !dnsMatchResponseEnabled(rule["match_response"]) {
		return fmt.Errorf("%s uses deprecated DNS response address matching; migrate with evaluate and match_response before sing-box 1.16", path)
	}
	for index, nested := range objectList(rule["rules"]) {
		if err := validateSingBox116DNSRule(nested, fmt.Sprintf("%s.rules[%d]", path, index)); err != nil {
			return err
		}
	}
	return nil
}

func dnsMatchResponseEnabled(value interface{}) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return typed != ""
	default:
		return false
	}
}

func hasExplicitRuleSetHTTPClient(value interface{}) bool {
	switch typed := value.(type) {
	case string:
		return typed != ""
	case map[string]interface{}:
		return hasNonZeroOptionValue(typed)
	default:
		return false
	}
}

func hasNonZeroOptionValue(value interface{}) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case float64:
		return typed != 0
	case json.Number:
		number, err := strconv.ParseFloat(typed.String(), 64)
		return err != nil || number != 0
	case string:
		return typed != ""
	case []interface{}:
		for _, item := range typed {
			if hasNonZeroOptionValue(item) {
				return true
			}
		}
		return false
	case map[string]interface{}:
		for _, item := range typed {
			if hasNonZeroOptionValue(item) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func hasHTTPClientTag(value interface{}, tag string) bool {
	for _, client := range objectList(value) {
		if stringValue(client["tag"]) == tag {
			return true
		}
	}
	return false
}

func validateDeprecatedHysteriaOptions(config map[string]interface{}) error {
	for _, section := range []string{"inbounds", "outbounds"} {
		for index, item := range objectList(config[section]) {
			raw, err := json.Marshal(item)
			if err != nil {
				return fmt.Errorf("%s[%d]: %w", section, index, err)
			}
			if err := validateDeprecatedHysteriaProtocolOptions(stringValue(item["type"]), raw); err != nil {
				return fmt.Errorf("%s[%d]: %w", section, index, err)
			}
		}
	}
	return nil
}

func validateDeprecatedHysteriaProtocolOptions(protocol string, raw json.RawMessage) error {
	if protocol != "hysteria" || len(raw) == 0 {
		return nil
	}
	var options map[string]json.RawMessage
	if err := json.Unmarshal(raw, &options); err != nil {
		return fmt.Errorf("hysteria options: %w", err)
	}
	deprecated := map[string]string{
		"recv_window_conn":      "connection_receive_window",
		"recv_window":           "stream_receive_window",
		"recv_window_client":    "stream_receive_window",
		"max_conn_client":       "max_concurrent_streams",
		"disable_mtu_discovery": "disable_path_mtu_discovery",
	}
	for field, replacement := range deprecated {
		if _, exists := options[field]; exists {
			return fmt.Errorf("hysteria field %s is no longer supported; use %s", field, replacement)
		}
	}
	return nil
}

func objectValue(value interface{}) map[string]interface{} {
	result, _ := value.(map[string]interface{})
	return result
}

func boolValue(value interface{}) bool {
	result, _ := value.(bool)
	return result
}
