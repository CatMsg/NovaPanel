package service

import (
	"fmt"
	"net/netip"
	"strings"
)

var dnsRuleMapSupportedTypes = map[string]struct{}{
	"local":       {},
	"dhcp":        {},
	"resolved":    {},
	"tailscale":   {},
	"openvpn":     {},
	"openconnect": {},
}

func validateDNSRuleMapValues(config map[string]interface{}) error {
	dnsConfig := objectValue(config["dns"])
	servers := objectList(dnsConfig["servers"])
	serverTypes := make(map[string]string, len(servers)+1)
	for index, server := range servers {
		tag := stringValue(server["tag"])
		if tag == "" {
			tag = fmt.Sprint(index)
		}
		serverTypes[tag] = stringValue(server["type"])
	}
	implicitLocal := len(servers) == 0 && strings.TrimSpace(stringValue(dnsConfig["final"])) == ""
	if implicitLocal {
		serverTypes["local"] = "local"
	}

	if err := validateDNSRuleMapTree(dnsConfig["rules"], "dns.rules", serverTypes); err != nil {
		return err
	}
	route := objectValue(config["route"])
	return validateDNSRuleMapTree(route["rules"], "route.rules", serverTypes)
}

func validateDNSRuleMapTree(value interface{}, path string, serverTypes map[string]string) error {
	rules, _ := value.([]interface{})
	for index, value := range rules {
		rule := objectValue(value)
		if rule == nil {
			continue
		}
		rulePath := fmt.Sprintf("%s[%d]", path, index)
		for _, key := range []string{"dns_server_address", "dns_search_domain"} {
			raw, exists := rule[key]
			if !exists {
				continue
			}
			entries, ok := raw.(map[string]interface{})
			if !ok || len(entries) == 0 {
				return fmt.Errorf("%s.%s must be a non-empty map", rulePath, key)
			}
			for tag, entry := range entries {
				entryPath := fmt.Sprintf("%s.%s[%q]", rulePath, key, tag)
				if strings.TrimSpace(tag) == "" {
					return fmt.Errorf("%s has a blank DNS server tag", entryPath)
				}
				values, err := dnsRuleMapValues(entry, entryPath)
				if err != nil {
					return err
				}
				serverType, exists := serverTypes[tag]
				if !exists {
					return fmt.Errorf("%s references missing DNS server tag %q", entryPath, tag)
				}
				if _, supported := dnsRuleMapSupportedTypes[serverType]; !supported {
					return fmt.Errorf("%s uses DNS server type %q, which does not support %s", entryPath, serverType, key)
				}
				if key == "dns_server_address" {
					for valueIndex, address := range values {
						if err := validateDNSRuleAddress(address); err != nil {
							return fmt.Errorf("%s value[%d] must be an IP address or CIDR: %w", entryPath, valueIndex, err)
						}
					}
				} else {
					for valueIndex, domain := range values {
						if strings.TrimSpace(domain) == "" {
							return fmt.Errorf("%s value[%d] must not be blank", entryPath, valueIndex)
						}
					}
				}
			}
		}
		if err := validateDNSRuleMapTree(rule["rules"], rulePath+".rules", serverTypes); err != nil {
			return err
		}
	}
	return nil
}

func dnsRuleMapValues(value interface{}, path string) ([]string, error) {
	values := make([]string, 0, 1)
	switch typed := value.(type) {
	case string:
		values = append(values, typed)
	case []interface{}:
		if len(typed) == 0 {
			return nil, fmt.Errorf("%s must be a non-empty string or string list", path)
		}
		for index, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s value[%d] must be a string", path, index)
			}
			values = append(values, text)
		}
	default:
		return nil, fmt.Errorf("%s must be a non-empty string or string list", path)
	}
	for index, item := range values {
		if strings.TrimSpace(item) == "" {
			return nil, fmt.Errorf("%s value[%d] must not be blank", path, index)
		}
	}
	return values, nil
}

func validateDNSRuleAddress(value string) error {
	if strings.Contains(value, "/") {
		if _, err := netip.ParsePrefix(value); err != nil {
			return err
		}
		return nil
	}
	if _, err := netip.ParseAddr(value); err != nil {
		return err
	}
	return nil
}
