package sub

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util"
)

func filterClashHTTPOutbounds(inbounds []*model.Inbound, outbounds []map[string]interface{}, tags []string) ([]map[string]interface{}, []string, error) {
	inboundByTag := make(map[string]*model.Inbound)
	for _, inbound := range inbounds {
		if inbound == nil || inbound.Type != "http" {
			continue
		}
		var addresses []map[string]interface{}
		if len(strings.TrimSpace(string(inbound.Addrs))) > 0 && strings.TrimSpace(string(inbound.Addrs)) != "null" {
			if err := json.Unmarshal(inbound.Addrs, &addresses); err != nil {
				return nil, nil, fmt.Errorf("HTTP inbound %q addresses: %w", inbound.Tag, err)
			}
		}
		if len(addresses) == 0 {
			inboundByTag[inbound.Tag] = inbound
			continue
		}
		for index, address := range addresses {
			remark, _ := address["remark"].(string)
			tag := inboundAddressTag(inbound.Tag, remark, index, len(addresses))
			inboundByTag[tag] = inbound
		}
	}

	filteredOutbounds := make([]map[string]interface{}, 0, len(outbounds))
	filteredTags := make([]string, 0, len(tags))
	for index, outbound := range outbounds {
		tag := ""
		if index < len(tags) {
			tag = tags[index]
		}
		if !strings.EqualFold(strings.TrimSpace(asString(outbound["type"])), "http") {
			filteredOutbounds = append(filteredOutbounds, outbound)
			filteredTags = append(filteredTags, tag)
			continue
		}
		inbound := inboundByTag[tag]
		if inbound == nil {
			if !clashHTTPVersionIsHTTP1(outbound) {
				continue
			}
			filtered, err := util.CloneJSONMap(outbound)
			if err != nil {
				return nil, nil, fmt.Errorf("copy external HTTP outbound: %w", err)
			}
			delete(filtered, "version")
			clampClashHTTP1ALPN(filtered)
			filteredOutbounds = append(filteredOutbounds, filtered)
			filteredTags = append(filteredTags, tag)
			continue
		}
		versions, err := util.ValidateHTTPInbound(inbound)
		if err != nil {
			return nil, nil, fmt.Errorf("HTTP inbound %q: %w", inbound.Tag, err)
		}
		if !slices.Contains(versions, 1) {
			continue
		}
		explicitVersion := util.HasExplicitHTTPInboundVersion(inbound.Options)
		if explicitVersion && versions[0] != 1 {
			continue
		}
		if _, err := util.ResolveHTTPOutboundVersion(outbound, versions); err != nil {
			return nil, nil, fmt.Errorf("HTTP inbound %q Clash subscription: %w", inbound.Tag, err)
		}
		filtered, err := util.CloneJSONMap(outbound)
		if err != nil {
			return nil, nil, fmt.Errorf("copy HTTP inbound %q Clash outbound: %w", inbound.Tag, err)
		}
		if err := util.SetHTTPOutboundVersion(filtered, 1); err != nil {
			return nil, nil, fmt.Errorf("HTTP inbound %q Clash subscription: %w", inbound.Tag, err)
		}
		version, err := util.ResolveHTTPOutboundVersion(filtered, versions)
		if err != nil {
			return nil, nil, fmt.Errorf("HTTP inbound %q Clash subscription: %w", inbound.Tag, err)
		}
		if version != 1 {
			continue
		}
		if err := util.ValidateHTTPOutboundTLSForInbound(filtered, inbound); err != nil {
			return nil, nil, fmt.Errorf("HTTP inbound %q Clash subscription: %w", inbound.Tag, err)
		}
		if util.HTTPOutboundHasPathOrHost(filtered) {
			continue
		}
		clampClashHTTP1ALPN(filtered)
		filteredOutbounds = append(filteredOutbounds, filtered)
		filteredTags = append(filteredTags, tag)
	}
	return filteredOutbounds, filteredTags, nil
}

func inboundAddressTag(tag, remark string, index, total int) string {
	if total == 1 {
		return tag + remark
	}
	return fmt.Sprintf("%d.%s%s", index+1, tag, remark)
}

func clampClashHTTP1ALPN(outbound map[string]interface{}) {
	if outbound == nil {
		return
	}
	if tls, ok := outbound["tls"].(map[string]interface{}); ok {
		if enabled, _ := tls["enabled"].(bool); enabled {
			if clone, err := util.CloneJSONMap(tls); err == nil {
				clone["alpn"] = []interface{}{"http/1.1"}
				outbound["tls"] = clone
			}
		}
		return
	}
	if enabled, _ := outbound["tls"].(bool); enabled {
		outbound["alpn"] = []interface{}{"http/1.1"}
	}
}

func filterRawClashHTTPProxies(proxies []map[string]interface{}) ([]map[string]interface{}, error) {
	filtered := make([]map[string]interface{}, 0, len(proxies))
	for _, proxy := range proxies {
		copy, err := util.CloneJSONMap(proxy)
		if err != nil {
			return nil, fmt.Errorf("copy raw Clash proxy: %w", err)
		}
		if strings.EqualFold(strings.TrimSpace(asString(copy["type"])), "http") {
			if !clashHTTPVersionIsHTTP1(copy) {
				continue
			}
			delete(copy, "version")
			clampClashHTTP1ALPN(copy)
		}
		filtered = append(filtered, copy)
	}
	return filtered, nil
}

func clashHTTPVersionIsHTTP1(outbound map[string]interface{}) bool {
	raw, exists := outbound["version"]
	if !exists || raw == nil {
		return true
	}
	switch version := raw.(type) {
	case int:
		return version == 1
	case int64:
		return version == 1
	case float64:
		return version == 1
	default:
		return false
	}
}
