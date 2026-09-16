package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
)

const defaultHTTPClientTag = "novapanel-default"

// NormalizeSingBoxConfig upgrades deprecated sing-box options before configs
// are persisted, validated, or returned as JSON subscriptions.
func NormalizeSingBoxConfig(raw []byte) ([]byte, error) {
	var config map[string]interface{}
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}

	normalizeHTTPClients(config)
	normalizeDefaultHTTPClient(config)
	normalizeRuleSetHTTPClients(config)
	if err := normalizeDNSCompatibility(config); err != nil {
		return nil, err
	}
	normalizeCacheFile(config)
	if err := normalizeDeprecatedObjects(config); err != nil {
		return nil, err
	}

	return json.MarshalIndent(config, "", "  ")
}

func normalizeHTTPClients(config map[string]interface{}) {
	clients, _ := config["http_clients"].([]interface{})
	for _, item := range clients {
		client := objectValue(item)
		if client != nil && strings.TrimSpace(stringValue(client["detour"])) == "direct" {
			delete(client, "detour")
		}
	}
}

func normalizeInboundCompatibility(inbound *model.Inbound) error {
	if inbound == nil || inbound.Type != "tun" || len(inbound.Options) == 0 {
		return nil
	}
	var options map[string]interface{}
	if err := json.Unmarshal(inbound.Options, &options); err != nil {
		return err
	}
	delete(options, "stack")
	normalized, err := json.MarshalIndent(options, "", "  ")
	if err != nil {
		return err
	}
	inbound.Options = normalized
	return nil
}

func normalizeDefaultHTTPClient(config map[string]interface{}) {
	route := objectValue(config["route"])
	if route == nil {
		route = make(map[string]interface{})
		config["route"] = route
	}
	if strings.TrimSpace(stringValue(route["default_http_client"])) != "" {
		return
	}

	clients, _ := config["http_clients"].([]interface{})
	for _, item := range clients {
		client := objectValue(item)
		if client != nil && stringValue(client["tag"]) == defaultHTTPClientTag {
			route["default_http_client"] = defaultHTTPClientTag
			return
		}
	}
	config["http_clients"] = append(clients, map[string]interface{}{"tag": defaultHTTPClientTag})
	route["default_http_client"] = defaultHTTPClientTag
}

func normalizeRuleSetHTTPClients(config map[string]interface{}) {
	route := objectValue(config["route"])
	if route == nil {
		return
	}
	ruleSets, _ := route["rule_set"].([]interface{})
	for _, item := range ruleSets {
		ruleSet := objectValue(item)
		if ruleSet == nil {
			continue
		}
		detour := strings.TrimSpace(stringValue(ruleSet["download_detour"]))
		if _, exists := ruleSet["http_client"]; !exists && detour != "" && detour != "direct" {
			ruleSet["http_client"] = map[string]interface{}{"detour": detour}
		}
		delete(ruleSet, "download_detour")
		if httpClient := objectValue(ruleSet["http_client"]); httpClient != nil && strings.TrimSpace(stringValue(httpClient["detour"])) == "direct" {
			delete(httpClient, "detour")
			if len(httpClient) == 0 {
				delete(ruleSet, "http_client")
			}
		}
	}
}

func normalizeDNSCompatibility(config map[string]interface{}) error {
	dns := objectValue(config["dns"])
	if dns == nil {
		return nil
	}
	delete(dns, "independent_cache")

	rules, _ := dns["rules"].([]interface{})
	if len(rules) == 0 {
		return nil
	}

	strategies := make([]string, 0, 1)
	for _, item := range rules {
		collectAndRemoveDNSRuleStrategies(item, &strategies)
	}
	if strings.TrimSpace(stringValue(dns["strategy"])) == "" && len(strategies) > 0 {
		dns["strategy"] = strategies[0]
	}

	normalized := make([]interface{}, 0, len(rules))
	lastEvaluateServer := ""
	for index, item := range rules {
		rule := objectValue(item)
		if rule == nil || !dnsRuleNeedsResponseMigration(rule) {
			normalized = append(normalized, item)
			if rule == nil || stringValue(rule["action"]) != "evaluate" {
				lastEvaluateServer = ""
			} else {
				lastEvaluateServer = strings.TrimSpace(stringValue(rule["server"]))
			}
			continue
		}

		server := dnsEvaluationServer(dns, rules, index, rule)
		if server == "" {
			return fmt.Errorf("dns.rules[%d] 使用旧版地址筛选，但无法确定 evaluate DNS 服务器", index)
		}
		if lastEvaluateServer != server {
			normalized = append(normalized, map[string]interface{}{
				"action": "evaluate",
				"server": server,
			})
			lastEvaluateServer = server
		}
		setDNSMatchResponse(rule)
		normalized = append(normalized, rule)
	}
	dns["rules"] = normalized
	return nil
}

func collectAndRemoveDNSRuleStrategies(value interface{}, strategies *[]string) {
	rule := objectValue(value)
	if rule == nil {
		return
	}
	if strategy := strings.TrimSpace(stringValue(rule["strategy"])); strategy != "" {
		if !containsString(*strategies, strategy) {
			*strategies = append(*strategies, strategy)
		}
		delete(rule, "strategy")
	}
	children, _ := rule["rules"].([]interface{})
	for _, child := range children {
		collectAndRemoveDNSRuleStrategies(child, strategies)
	}
}

func dnsRuleNeedsResponseMigration(rule map[string]interface{}) bool {
	if _, migrated := rule["match_response"]; !migrated && (hasNonEmptyJSONValue(rule["ip_cidr"]) || boolValue(rule["ip_is_private"]) || boolValue(rule["ip_accept_any"]) || boolValue(rule["rule_set_ip_cidr_accept_empty"])) {
		return true
	}
	children, _ := rule["rules"].([]interface{})
	for _, child := range children {
		if childRule := objectValue(child); childRule != nil && dnsRuleNeedsResponseMigration(childRule) {
			return true
		}
	}
	return false
}

func setDNSMatchResponse(rule map[string]interface{}) {
	if hasNonEmptyJSONValue(rule["ip_cidr"]) || boolValue(rule["ip_is_private"]) || boolValue(rule["ip_accept_any"]) || boolValue(rule["rule_set_ip_cidr_accept_empty"]) {
		rule["match_response"] = true
	}
	delete(rule, "rule_set_ip_cidr_accept_empty")
	children, _ := rule["rules"].([]interface{})
	for _, child := range children {
		if childRule := objectValue(child); childRule != nil {
			setDNSMatchResponse(childRule)
		}
	}
}

func dnsEvaluationServer(dns map[string]interface{}, rules []interface{}, index int, current map[string]interface{}) string {
	for next := index + 1; next < len(rules); next++ {
		rule := objectValue(rules[next])
		if rule == nil || dnsRuleNeedsResponseMigration(rule) || dnsRuleHasMatchers(rule) {
			continue
		}
		if action := stringValue(rule["action"]); action != "" && action != "route" {
			continue
		}
		if server := strings.TrimSpace(stringValue(rule["server"])); server != "" {
			return server
		}
	}
	if server := strings.TrimSpace(stringValue(dns["final"])); server != "" {
		return server
	}
	servers, _ := dns["servers"].([]interface{})
	for _, item := range servers {
		if server := objectValue(item); server != nil {
			if tag := strings.TrimSpace(stringValue(server["tag"])); tag != "" {
				return tag
			}
		}
	}
	return strings.TrimSpace(stringValue(current["server"]))
}

func dnsRuleHasMatchers(rule map[string]interface{}) bool {
	for key, value := range rule {
		switch key {
		case "action", "server", "strategy", "disable_cache", "disable_optimistic_cache", "rewrite_ttl", "timeout", "client_subnet", "remove_client_subnet", "race", "speculative", "tag":
			continue
		}
		if hasNonEmptyJSONValue(value) {
			return true
		}
	}
	return false
}

func normalizeCacheFile(config map[string]interface{}) {
	experimental := objectValue(config["experimental"])
	if experimental == nil {
		return
	}
	cacheFile := objectValue(experimental["cache_file"])
	if cacheFile == nil {
		return
	}
	if _, exists := cacheFile["store_dns"]; !exists && boolValue(cacheFile["store_rdrc"]) {
		cacheFile["store_dns"] = true
	}
	delete(cacheFile, "store_rdrc")
	delete(cacheFile, "rdrc_timeout")
}

func normalizeDeprecatedObjects(value interface{}) error {
	switch current := value.(type) {
	case map[string]interface{}:
		if stringValue(current["type"]) == "tun" {
			delete(current, "stack")
		}
		if tlsOptions := objectValue(current["tls"]); tlsOptions != nil {
			if acme, exists := tlsOptions["acme"]; exists {
				if _, hasProvider := tlsOptions["certificate_provider"]; !hasProvider {
					acmeOptions := objectValue(acme)
					if acmeOptions == nil {
						return fmt.Errorf("tls.acme 必须是对象")
					}
					provider := map[string]interface{}{"type": "acme"}
					for key, item := range acmeOptions {
						provider[key] = item
					}
					tlsOptions["certificate_provider"] = provider
				}
				delete(tlsOptions, "acme")
			}
		}
		for _, child := range current {
			if err := normalizeDeprecatedObjects(child); err != nil {
				return err
			}
		}
	case []interface{}:
		for _, child := range current {
			if err := normalizeDeprecatedObjects(child); err != nil {
				return err
			}
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

func hasNonEmptyJSONValue(value interface{}) bool {
	switch current := value.(type) {
	case nil:
		return false
	case bool:
		return current
	case string:
		return strings.TrimSpace(current) != ""
	case []interface{}:
		return len(current) > 0
	case map[string]interface{}:
		return len(current) > 0
	default:
		return true
	}
}
