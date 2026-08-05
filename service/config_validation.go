package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type configReference struct {
	kind string
	tag  string
	path string
}

func validateRuntimeConfig(rawConfig []byte) error {
	if corePtr != nil {
		if err := corePtr.ValidateConfig(rawConfig); err != nil {
			return fmt.Errorf("配置结构无效: %w", err)
		}
	}
	return validateConfigReferences(rawConfig)
}

func validateConfigReferences(rawConfig []byte) error {
	var config map[string]interface{}
	if err := json.Unmarshal(rawConfig, &config); err != nil {
		return err
	}

	outboundTags := map[string]struct{}{"direct": {}}
	inboundTags := make(map[string]struct{})
	graph := make(map[string][]string)

	for index, item := range objectList(config["outbounds"]) {
		tag := stringValue(item["tag"])
		if tag == "" {
			return fmt.Errorf("outbounds[%d] 的标签不能为空", index)
		}
		if _, exists := outboundTags[tag]; exists && tag != "direct" {
			return fmt.Errorf("出站标签重复: %s", tag)
		}
		outboundTags[tag] = struct{}{}
		graph[tag] = append(graph[tag], outboundDependencies(item)...)
	}
	for index, item := range objectList(config["inbounds"]) {
		tag := stringValue(item["tag"])
		if tag == "" {
			return fmt.Errorf("inbounds[%d] 的标签不能为空", index)
		}
		inboundTags[tag] = struct{}{}
	}
	for index, item := range objectList(config["endpoints"]) {
		tag := stringValue(item["tag"])
		if tag == "" {
			return fmt.Errorf("endpoints[%d] 的标签不能为空", index)
		}
		if _, exists := outboundTags[tag]; exists {
			return fmt.Errorf("节点与出站标签冲突: %s", tag)
		}
		outboundTags[tag] = struct{}{}
		inboundTags[tag] = struct{}{}
	}

	route, _ := config["route"].(map[string]interface{})
	ruleSetTags := make(map[string]struct{})
	refs := make([]configReference, 0)
	if route != nil {
		if final := stringValue(route["final"]); final != "" {
			refs = append(refs, configReference{kind: "outbound", tag: final, path: "route.final"})
		}
		for index, item := range objectList(route["rule_set"]) {
			tag := stringValue(item["tag"])
			if tag == "" {
				return fmt.Errorf("route.rule_set[%d] 的标签不能为空", index)
			}
			if _, exists := ruleSetTags[tag]; exists {
				return fmt.Errorf("规则集标签重复: %s", tag)
			}
			ruleSetTags[tag] = struct{}{}
			switch stringValue(item["type"]) {
			case "local":
				if stringValue(item["path"]) == "" {
					return fmt.Errorf("route.rule_set[%d] 本地规则集缺少 path", index)
				}
			case "remote":
				if stringValue(item["url"]) == "" {
					return fmt.Errorf("route.rule_set[%d] 远程规则集缺少 url", index)
				}
			}
			if detour := stringValue(item["download_detour"]); detour != "" {
				refs = append(refs, configReference{kind: "outbound", tag: detour, path: fmt.Sprintf("route.rule_set[%d].download_detour", index)})
			}
		}
		collectRuleReferences(route["rules"], "route.rules", &refs)
	}

	for index, item := range objectList(config["outbounds"]) {
		dependencies := outboundDependencies(item)
		outboundType := stringValue(item["type"])
		if (outboundType == "selector" || outboundType == "urltest") && len(dependencies) == 0 {
			return fmt.Errorf("outbounds[%d] %s 至少需要一个成员出站", index, outboundType)
		}
		if defaultTag := stringValue(item["default"]); defaultTag != "" && !containsString(dependencies, defaultTag) {
			return fmt.Errorf("outbounds[%d].default 不在成员出站列表中: %s", index, defaultTag)
		}
		for _, dependency := range dependencies {
			refs = append(refs, configReference{kind: "outbound", tag: dependency, path: fmt.Sprintf("outbounds[%d]", index)})
		}
	}
	for index, item := range objectList(config["endpoints"]) {
		if detour := stringValue(item["detour"]); detour != "" {
			refs = append(refs, configReference{kind: "outbound", tag: detour, path: fmt.Sprintf("endpoints[%d].detour", index)})
		}
	}
	if ntp, ok := config["ntp"].(map[string]interface{}); ok {
		if detour := stringValue(ntp["detour"]); detour != "" {
			refs = append(refs, configReference{kind: "outbound", tag: detour, path: "ntp.detour"})
		}
	}
	if dns, ok := config["dns"].(map[string]interface{}); ok {
		for index, server := range objectList(dns["servers"]) {
			if detour := stringValue(server["detour"]); detour != "" {
				refs = append(refs, configReference{kind: "outbound", tag: detour, path: fmt.Sprintf("dns.servers[%d].detour", index)})
			}
		}
		collectRuleSetReferences(dns["rules"], "dns.rules", &refs)
	}
	if experimental, ok := config["experimental"].(map[string]interface{}); ok {
		if clashAPI, ok := experimental["clash_api"].(map[string]interface{}); ok {
			if detour := stringValue(clashAPI["external_ui_download_detour"]); detour != "" {
				refs = append(refs, configReference{kind: "outbound", tag: detour, path: "experimental.clash_api.external_ui_download_detour"})
			}
		}
	}

	for _, ref := range refs {
		switch ref.kind {
		case "outbound":
			if _, exists := outboundTags[ref.tag]; !exists {
				return fmt.Errorf("%s 引用了不存在的出站: %s", ref.path, ref.tag)
			}
		case "inbound":
			if _, exists := inboundTags[ref.tag]; !exists {
				return fmt.Errorf("%s 引用了不存在的入站: %s", ref.path, ref.tag)
			}
		case "rule_set":
			if _, exists := ruleSetTags[ref.tag]; !exists {
				return fmt.Errorf("%s 引用了不存在的规则集: %s", ref.path, ref.tag)
			}
		}
	}
	return validateOutboundDependencyCycles(graph)
}

func collectRuleReferences(value interface{}, path string, refs *[]configReference) {
	collectNamedReferences(value, path, map[string]string{
		"outbound":     "outbound",
		"preferred_by": "outbound",
		"inbound":      "inbound",
		"rule_set":     "rule_set",
	}, refs)
}

func collectRuleSetReferences(value interface{}, path string, refs *[]configReference) {
	collectNamedReferences(value, path, map[string]string{"rule_set": "rule_set"}, refs)
}

func collectNamedReferences(value interface{}, path string, keys map[string]string, refs *[]configReference) {
	switch typed := value.(type) {
	case []interface{}:
		for index, item := range typed {
			collectNamedReferences(item, fmt.Sprintf("%s[%d]", path, index), keys, refs)
		}
	case map[string]interface{}:
		for key, item := range typed {
			itemPath := path + "." + key
			if kind, exists := keys[key]; exists {
				for _, tag := range stringList(item) {
					if tag != "" {
						*refs = append(*refs, configReference{kind: kind, tag: tag, path: itemPath})
					}
				}
			}
			collectNamedReferences(item, itemPath, keys, refs)
		}
	}
}

func outboundDependencies(item map[string]interface{}) []string {
	dependencies := make([]string, 0)
	if detour := stringValue(item["detour"]); detour != "" {
		dependencies = append(dependencies, detour)
	}
	dependencies = append(dependencies, stringList(item["outbounds"])...)
	return uniqueStrings(dependencies)
}

func validateOutboundDependencyCycles(graph map[string][]string) error {
	state := make(map[string]uint8)
	stack := make([]string, 0)
	var visit func(string) error
	visit = func(tag string) error {
		switch state[tag] {
		case 1:
			start := 0
			for index, item := range stack {
				if item == tag {
					start = index
					break
				}
			}
			cycle := append(append([]string(nil), stack[start:]...), tag)
			return fmt.Errorf("出站依赖形成循环: %s", strings.Join(cycle, " -> "))
		case 2:
			return nil
		}
		state[tag] = 1
		stack = append(stack, tag)
		for _, dependency := range graph[tag] {
			if _, managed := graph[dependency]; managed {
				if err := visit(dependency); err != nil {
					return err
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[tag] = 2
		return nil
	}
	tags := make([]string, 0, len(graph))
	for tag := range graph {
		tags = append(tags, tag)
	}
	sort.Strings(tags)
	for _, tag := range tags {
		if err := visit(tag); err != nil {
			return err
		}
	}
	return nil
}

func objectList(value interface{}) []map[string]interface{} {
	items, _ := value.([]interface{})
	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		if object, ok := item.(map[string]interface{}); ok {
			result = append(result, object)
		}
	}
	return result
}

func stringValue(value interface{}) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func stringList(value interface{}) []string {
	switch typed := value.(type) {
	case string:
		return []string{strings.TrimSpace(typed)}
	case []interface{}:
		result := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				result = append(result, strings.TrimSpace(text))
			}
		}
		return result
	default:
		return nil
	}
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
