package sub

import (
	"encoding/json"
	"fmt"
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
			tag := fmt.Sprintf("%d.%s%s", index+1, inbound.Tag, remark)
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
			if !clashHTTPVersionIsH1OrLegacy(outbound) {
				continue
			}
			filteredOutbounds = append(filteredOutbounds, outbound)
			filteredTags = append(filteredTags, tag)
			continue
		}
		versions, err := util.ValidateHTTPInbound(inbound)
		if err != nil {
			return nil, nil, fmt.Errorf("HTTP inbound %q: %w", inbound.Tag, err)
		}
		if !hasHTTPVersion(versions, 1) {
			continue
		}
		version, err := util.ResolveHTTPOutboundVersion(outbound, versions)
		if err != nil {
			return nil, nil, fmt.Errorf("HTTP inbound %q Clash subscription: %w", inbound.Tag, err)
		}
		if err := util.ValidateHTTPOutboundTLSForInbound(outbound, inbound); err != nil {
			return nil, nil, fmt.Errorf("HTTP inbound %q Clash subscription: %w", inbound.Tag, err)
		}
		if version == 3 || !hasHTTPVersion(versions, 1) || util.HTTPOutboundHasPathOrHost(outbound) {
			continue
		}
		// Clash's HTTP proxy schema has no HTTP-version selector. Export only
		// the HTTP/1-compatible variant when the listener serves HTTP/1.
		outbound["version"] = 1
		filteredOutbounds = append(filteredOutbounds, outbound)
		filteredTags = append(filteredTags, tag)
	}
	return filteredOutbounds, filteredTags, nil
}

func hasHTTPVersion(versions []int, wanted int) bool {
	for _, version := range versions {
		if version == wanted {
			return true
		}
	}
	return false
}

func clashHTTPVersionIsH1OrLegacy(outbound map[string]interface{}) bool {
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
