package sub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/CatMsg/NovaPanel/util"
	"github.com/CatMsg/NovaPanel/util/common"

	"gopkg.in/yaml.v3"
)

type AggregateService struct {
	service.SettingService
	JsonService
	ClashService
}

type aggregateUsage struct {
	upload   int64
	download int64
	total    int64
	expire   int64
}

func isIPLiteral(host string) bool {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return net.ParseIP(host) != nil
}

func asString(value interface{}) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case json.RawMessage:
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return strings.TrimSpace(s)
		}
	case []byte:
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			return strings.TrimSpace(s)
		}
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func (a *AggregateService) GetAggregate(format string, host string) (*string, []string, error) {
	return a.GetAggregateContext(context.Background(), format, host)
}

func (a *AggregateService) GetAggregateContext(ctx context.Context, format string, host string) (*string, []string, error) {
	mode, err := a.SettingService.GetSubMode()
	if err != nil {
		return nil, nil, err
	}
	if mode != "master" {
		return nil, nil, common.NewError("aggregate subscription is disabled in slave mode")
	}

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "clash":
		proxies, usage, err := a.collectAggregateClashProxiesContext(ctx, host)
		if err != nil {
			return nil, nil, err
		}
		return a.buildAggregateClashProxies(proxies, usage)
	case "json":
		outbounds, tags, usage, err := a.collectAggregateJSONOutboundsContext(ctx, host)
		if err != nil {
			return nil, nil, err
		}
		return a.buildAggregateJson(outbounds, tags, usage)
	}

	links, usage, err := a.collectAggregateLinksContext(ctx, host)
	if err != nil {
		return nil, nil, err
	}

	return a.buildAggregatePlain(links, usage)
}

func (a *AggregateService) buildAggregatePlain(links []string, usage aggregateUsage) (*string, []string, error) {
	result := strings.Join(links, "\n")

	subEncode, err := a.SettingService.GetSubEncode()
	if err != nil {
		return nil, nil, err
	}
	if subEncode {
		result = base64.StdEncoding.EncodeToString([]byte(result))
	}

	return &result, a.aggregateHeaders(usage), nil
}

func (a *AggregateService) buildAggregateJson(outbounds []map[string]interface{}, outTags []string, usage aggregateUsage) (*string, []string, error) {
	jsonConfig := map[string]interface{}{}
	if err := json.Unmarshal([]byte(defaultJson), &jsonConfig); err != nil {
		return nil, nil, err
	}
	a.JsonService.addDefaultOutbounds(&outbounds, &outTags)
	proxyOutbounds, endpoints := splitNativeJSONEndpoints(outbounds)
	jsonConfig["outbounds"] = proxyOutbounds
	if len(endpoints) > 0 {
		jsonConfig["endpoints"] = endpoints
	}
	if err := a.JsonService.addOthers(&jsonConfig); err != nil {
		return nil, nil, err
	}

	result, err := json.MarshalIndent(jsonConfig, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	result, err = service.ValidateSingBoxConfig(result)
	if err != nil {
		return nil, nil, err
	}

	return a.aggregateFormatResult(string(result), usage)
}

func (a *AggregateService) buildAggregateClashProxies(proxies []map[string]interface{}, usage aggregateUsage) (*string, []string, error) {
	basicConfig, err := a.ClashService.getClashConfig()
	if err != nil || len(basicConfig) == 0 {
		basicConfig = basicClashConfig
	}

	result, err := a.ClashService.ConvertRawClashProxies(proxies, basicConfig)
	if err != nil {
		return nil, nil, err
	}

	return a.aggregateFormatResult(result, usage)
}

func (a *AggregateService) collectAggregateClashProxiesContext(ctx context.Context, host string) ([]map[string]interface{}, aggregateUsage, error) {
	sources, err := a.SettingService.GetSubMasterSources()
	if err != nil {
		return nil, aggregateUsage{}, err
	}
	selfAggregateURI, err := a.selfAggregateURI(host)
	if err != nil {
		return nil, aggregateUsage{}, err
	}

	seen := make(map[string]struct{})
	seenSources := make(map[string]struct{})
	seenProxies := make(map[string]struct{})
	proxies := make([]map[string]interface{}, 0)
	usage := aggregateUsage{}
	for sourceIndex, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, aggregateUsage{}, err
		}
		if sameSubscriptionSource(source, selfAggregateURI) {
			logger.Warning("aggregate: skip self source")
			continue
		}
		sourceKey := aggregateSourceKey(source)
		if _, exists := seenSources[sourceKey]; exists {
			continue
		}
		seenSources[sourceKey] = struct{}{}

		clashSource, err := subscriptionSourceWithFormat(source, "clash")
		if err != nil {
			logger.Warning("aggregate: skip invalid source URL")
			continue
		}
		data, headers := util.GetExternalLinkWithHeadersContext(ctx, clashSource, 15*time.Second)
		if err := ctx.Err(); err != nil {
			return nil, aggregateUsage{}, err
		}
		if strings.TrimSpace(data) == "" {
			logger.Warning("aggregate: failed to load remote clash subscription")
			continue
		}
		usage.addHeader(headers.Get("Subscription-Userinfo"))

		sourceProxies, err := clashProxiesFromSource(data)
		if err != nil {
			sourceProxies, err = a.clashProxiesFromPlainSource(data)
		}
		if err != nil {
			logger.Warning("aggregate: skip invalid clash source")
			continue
		}
		sourceProxies, err = filterRawClashHTTPProxies(sourceProxies)
		if err != nil {
			logger.Warning("aggregate: skip invalid clash proxy data")
			continue
		}
		for _, proxy := range sourceProxies {
			name := strings.TrimSpace(asString(proxy["name"]))
			if name == "" {
				continue
			}
			fingerprint, err := json.Marshal(proxy)
			if err != nil {
				logger.Warning("aggregate: skip unencodable clash proxy")
				continue
			}
			if _, exists := seenProxies[string(fingerprint)]; exists {
				continue
			}
			uniqueName := uniqueAggregateTag(name, sourceIndex, seen, nil)
			proxy["name"] = uniqueName
			seen[uniqueName] = struct{}{}
			seenProxies[string(fingerprint)] = struct{}{}
			proxies = append(proxies, proxy)
		}
	}

	if len(proxies) == 0 {
		return nil, aggregateUsage{}, common.NewError("no clash proxies found")
	}
	return proxies, usage, nil
}

func (a *AggregateService) collectAggregateJSONOutboundsContext(ctx context.Context, host string) ([]map[string]interface{}, []string, aggregateUsage, error) {
	sources, err := a.SettingService.GetSubMasterSources()
	if err != nil {
		return nil, nil, aggregateUsage{}, err
	}
	jsonExt, err := a.SettingService.GetSubJsonExt()
	if err != nil {
		return nil, nil, aggregateUsage{}, err
	}
	localDNSTags := aggregateJSONDNSTags(jsonExt)
	selfAggregateURI, err := a.selfAggregateURI(host)
	if err != nil {
		return nil, nil, aggregateUsage{}, err
	}

	outbounds := make([]map[string]interface{}, 0)
	tags := make([]string, 0)
	usage := aggregateUsage{}
	seenSources := make(map[string]struct{})
	seenNodes := make(map[string]string)
	seenTags := map[string]struct{}{"proxy": {}, "auto": {}, "direct": {}}
	for sourceIndex, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, nil, aggregateUsage{}, err
		}
		if sameSubscriptionSource(source, selfAggregateURI) {
			logger.Warning("aggregate: skip self source")
			continue
		}
		sourceKey := aggregateSourceKey(source)
		if _, exists := seenSources[sourceKey]; exists {
			continue
		}
		seenSources[sourceKey] = struct{}{}

		jsonSource, err := subscriptionSourceWithFormat(source, "json")
		if err != nil {
			logger.Warning("aggregate: skip invalid source URL")
			continue
		}
		data, headers := util.GetExternalLinkWithHeadersContext(ctx, jsonSource, 15*time.Second)
		if err := ctx.Err(); err != nil {
			return nil, nil, aggregateUsage{}, err
		}
		if strings.TrimSpace(data) == "" {
			logger.Warning("aggregate: failed to load remote JSON subscription")
			continue
		}
		usage.addHeader(headers.Get("Subscription-Userinfo"))

		sourceOutbounds, structured, parseErr := aggregateJSONOutbounds(data)
		if parseErr != nil {
			logger.Warning("aggregate: skip malformed JSON subscription")
			continue
		}
		if !structured {
			links := aggregatePlainLinks(data)
			converted, _, convertErr := a.outboundsFromLinks(links)
			if convertErr == nil && converted != nil {
				sourceOutbounds = *converted
			} else {
				err = convertErr
			}
			if err != nil {
				logger.Warning("aggregate: skip invalid plain subscription")
				continue
			}
		}
		if err := appendAggregateOutbounds(sourceOutbounds, sourceIndex, localDNSTags, &outbounds, &tags, seenNodes, seenTags); err != nil {
			logger.Warning("aggregate: skip invalid outbound data")
		}
	}

	if len(outbounds) == 0 {
		return nil, nil, aggregateUsage{}, common.NewError("no subscription outbounds found")
	}
	return outbounds, tags, usage, nil
}

func aggregateJSONOutbounds(data string) ([]map[string]interface{}, bool, error) {
	trimmed := strings.TrimSpace(data)
	if !strings.HasPrefix(trimmed, "{") {
		return nil, false, nil
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal([]byte(trimmed), &config); err != nil {
		return nil, true, err
	}
	rawOutbounds, hasOutbounds := config["outbounds"]
	rawEndpoints, hasEndpoints := config["endpoints"]
	if !hasOutbounds && !hasEndpoints {
		return nil, false, nil
	}
	var rawNodes []json.RawMessage
	if hasOutbounds {
		if err := json.Unmarshal(rawOutbounds, &rawNodes); err != nil {
			return nil, true, err
		}
	}
	if hasEndpoints {
		var endpoints []json.RawMessage
		if err := json.Unmarshal(rawEndpoints, &endpoints); err != nil {
			return nil, true, err
		}
		for _, raw := range endpoints {
			var node map[string]interface{}
			if err := json.Unmarshal(raw, &node); err != nil {
				return nil, true, err
			}
			// Only client endpoints are subscription proxy targets. Do not copy
			// remote server listeners or host/network-specific endpoint state.
			if node["type"] == "masque-client" {
				rawNodes = append(rawNodes, raw)
			}
		}
	}
	outbounds := make([]map[string]interface{}, 0, len(rawNodes))
	for _, rawNode := range rawNodes {
		var outbound map[string]interface{}
		if err := json.Unmarshal(rawNode, &outbound); err != nil {
			return nil, true, err
		}
		if outbound == nil {
			continue
		}
		outbounds = append(outbounds, outbound)
	}
	return outbounds, true, nil
}

func skipAggregateJSONOutbound(outbound map[string]interface{}) bool {
	switch strings.ToLower(strings.TrimSpace(asString(outbound["type"]))) {
	case "", "direct", "selector", "urltest", "dns", "block":
		return true
	default:
		return false
	}
}

func aggregatePlainLinks(data string) []string {
	links := make([]string, 0)
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			links = append(links, line)
		}
	}
	return links
}

func appendAggregateOutbounds(sourceOutbounds []map[string]interface{}, sourceIndex int, localDNSTags map[string]struct{}, outbounds *[]map[string]interface{}, tags *[]string, seenNodes map[string]string, seenTags map[string]struct{}) error {
	type candidate struct {
		outbound    map[string]interface{}
		fingerprint string
	}
	tagCounts := make(map[string]int, len(sourceOutbounds))
	for _, outbound := range sourceOutbounds {
		if outbound == nil {
			continue
		}
		if tag := strings.TrimSpace(asString(outbound["tag"])); tag != "" {
			tagCounts[tag]++
		}
	}
	prepared := make([]candidate, 0, len(sourceOutbounds))
	sourceTagToFinal := make(map[string]string)
	tempTags := make(map[string]struct{})
	pendingNodes := make(map[string]string)
	for _, sourceOutbound := range sourceOutbounds {
		if sourceOutbound == nil || skipAggregateJSONOutbound(sourceOutbound) {
			continue
		}
		outbound, err := util.CloneJSONMap(sourceOutbound)
		if err != nil {
			return err
		}
		tag := strings.TrimSpace(asString(outbound["tag"]))
		if tag == "" {
			continue
		}
		fingerprintBytes, err := json.Marshal(outbound)
		if err != nil {
			return err
		}
		fingerprint := string(fingerprintBytes)
		// Detour tags are source-local, so identical raw nodes can use different upstreams.
		if asString(outbound["detour"]) == "" {
			if priorTag, duplicate := seenNodes[fingerprint]; duplicate {
				if tagCounts[tag] == 1 {
					sourceTagToFinal[tag] = priorTag
				}
				continue
			}
		}
		if _, duplicate := pendingNodes[fingerprint]; duplicate {
			continue
		}

		finalTag := uniqueAggregateTag(tag, sourceIndex, seenTags, tempTags)
		if tagCounts[tag] == 1 {
			sourceTagToFinal[tag] = finalTag
		}
		outbound["tag"] = finalTag
		tempTags[finalTag] = struct{}{}
		prepared = append(prepared, candidate{outbound: outbound, fingerprint: fingerprint})
		pendingNodes[fingerprint] = finalTag
	}

	active := make([]bool, len(prepared))
	for index := range prepared {
		active[index] = true
	}
	for changed := true; changed; {
		changed = false
		activeTags := make(map[string]struct{}, len(seenTags)+len(prepared))
		for tag := range seenTags {
			activeTags[tag] = struct{}{}
		}
		for index, item := range prepared {
			if active[index] {
				activeTags[asString(item.outbound["tag"])] = struct{}{}
			}
		}
		for index, item := range prepared {
			if !active[index] {
				continue
			}
			if detour := strings.TrimSpace(asString(item.outbound["detour"])); detour != "" {
				mapped, exists := sourceTagToFinal[detour]
				if !exists {
					active[index] = false
					changed = true
					continue
				}
				if _, exists := activeTags[mapped]; !exists {
					active[index] = false
					changed = true
				}
			}
			if resolver := strings.TrimSpace(asString(item.outbound["domain_resolver"])); resolver != "" {
				if _, exists := localDNSTags[resolver]; !exists {
					active[index] = false
					changed = true
				}
			}
		}
	}
	for index, item := range prepared {
		if !active[index] {
			continue
		}
		if detour := strings.TrimSpace(asString(item.outbound["detour"])); detour != "" {
			item.outbound["detour"] = sourceTagToFinal[detour]
		} else {
			seenNodes[item.fingerprint] = asString(item.outbound["tag"])
		}
		seenTags[asString(item.outbound["tag"])] = struct{}{}
		*outbounds = append(*outbounds, item.outbound)
		*tags = append(*tags, asString(item.outbound["tag"]))
	}
	return nil
}

func uniqueAggregateTag(tag string, sourceIndex int, usedTags, reserved map[string]struct{}) string {
	if _, exists := usedTags[tag]; !exists {
		if _, exists := reserved[tag]; !exists {
			return tag
		}
	}
	base := fmt.Sprintf("%d.%s", sourceIndex+1, tag)
	candidate := base
	for suffix := 2; ; suffix++ {
		_, used := usedTags[candidate]
		_, held := reserved[candidate]
		if !used && !held {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, suffix)
	}
}

func aggregateJSONDNSTags(extension string) map[string]struct{} {
	tags := make(map[string]struct{})
	var config map[string]interface{}
	if json.Unmarshal([]byte(extension), &config) != nil {
		return tags
	}
	dns, _ := config["dns"].(map[string]interface{})
	servers, _ := dns["servers"].([]interface{})
	for _, item := range servers {
		server, _ := item.(map[string]interface{})
		if tag := strings.TrimSpace(asString(server["tag"])); tag != "" {
			tags[tag] = struct{}{}
		}
	}
	return tags
}

func aggregateSourceKey(source string) string {
	parsed, err := url.Parse(strings.TrimSpace(source))
	if err != nil {
		return strings.TrimSpace(source)
	}
	query := parsed.Query()
	query.Del("format")
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func subscriptionSourceWithFormat(source string, format string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(source))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", common.NewError("invalid subscription source")
	}
	query := parsed.Query()
	query.Set("format", format)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func clashProxiesFromSource(data string) ([]map[string]interface{}, error) {
	var config struct {
		Proxies []map[string]interface{} `yaml:"proxies"`
	}
	if err := yaml.Unmarshal([]byte(strings.TrimSpace(data)), &config); err != nil {
		return nil, err
	}
	if len(config.Proxies) == 0 {
		return nil, common.NewError("no proxies in clash subscription")
	}
	return config.Proxies, nil
}

func (a *AggregateService) clashProxiesFromPlainSource(data string) ([]map[string]interface{}, error) {
	links := make([]string, 0)
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			links = append(links, line)
		}
	}
	if len(links) == 0 {
		return nil, common.NewError("no subscription links found")
	}
	outbounds, _, err := a.outboundsFromLinks(links)
	if err != nil {
		return nil, err
	}
	result, err := a.ClashService.ConvertToClashMeta(outbounds, basicClashConfig)
	if err != nil {
		return nil, err
	}
	return clashProxiesFromSource(result)
}

func (a *AggregateService) aggregateHeaders(usage aggregateUsage) []string {
	return a.buildSubscriptionHeaders("NovaPanel Aggregate", usage)
}

func (a *AggregateService) buildSubscriptionHeaders(profileTitle string, usage aggregateUsage) []string {
	updateInterval, err := a.SettingService.GetSubUpdates()
	if err != nil {
		updateInterval = 12
	}
	return []string{
		"upload=" + strconv.FormatInt(usage.upload, 10) +
			"; download=" + strconv.FormatInt(usage.download, 10) +
			"; total=" + strconv.FormatInt(usage.total, 10) +
			"; expire=" + strconv.FormatInt(usage.expire, 10),
		strconv.Itoa(updateInterval),
		profileTitle,
	}
}

func (a *AggregateService) aggregateFormatResult(result string, usage aggregateUsage) (*string, []string, error) {
	return &result, a.aggregateHeaders(usage), nil
}

func (a *AggregateService) collectAggregateLinksContext(ctx context.Context, host string) ([]string, aggregateUsage, error) {
	sources, err := a.SettingService.GetSubMasterSources()
	if err != nil {
		return nil, aggregateUsage{}, err
	}
	selfAggregateURI, err := a.selfAggregateURI(host)
	if err != nil {
		return nil, aggregateUsage{}, err
	}

	seen := make(map[string]struct{})
	seenSources := make(map[string]struct{})
	links := make([]string, 0)
	usage := aggregateUsage{}
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, aggregateUsage{}, err
		}
		if sameSubscriptionSource(source, selfAggregateURI) {
			logger.Warning("aggregate: skip self source")
			continue
		}
		sourceKey := aggregateSourceKey(source)
		if _, exists := seenSources[sourceKey]; exists {
			continue
		}
		seenSources[sourceKey] = struct{}{}

		data, headers := util.GetExternalLinkWithHeadersContext(ctx, source, 15*time.Second)
		if err := ctx.Err(); err != nil {
			return nil, aggregateUsage{}, err
		}
		if len(data) == 0 {
			logger.Warning("aggregate: failed to load remote subscription")
			continue
		}

		usage.addHeader(headers.Get("Subscription-Userinfo"))
		for _, line := range strings.Split(data, "\n") {
			link := strings.TrimSpace(line)
			if link == "" || strings.HasPrefix(link, "#") {
				continue
			}
			if _, exists := seen[link]; exists {
				continue
			}
			seen[link] = struct{}{}
			links = append(links, link)
		}
	}

	if len(links) == 0 {
		return nil, aggregateUsage{}, common.NewError("no subscription links found")
	}
	return links, usage, nil
}

func (a *AggregateService) selfAggregateURI(host string) (string, error) {
	base, err := a.SettingService.GetFinalSubURI(host)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(strings.TrimSpace(base), "/") + "/aggregate", nil
}

func sameSubscriptionSource(left string, right string) bool {
	return canonicalSubscriptionSource(left) == canonicalSubscriptionSource(right)
}

func canonicalSubscriptionSource(value string) string {
	value = strings.TrimSpace(value)
	if idx := strings.Index(value, "?"); idx >= 0 {
		value = value[:idx]
	}
	return strings.TrimRight(value, "/")
}

func (a *AggregateService) outboundsFromLinks(links []string) (*[]map[string]interface{}, *[]string, error) {
	outbounds := make([]map[string]interface{}, 0)
	outTags := make([]string, 0)

	for index, link := range links {
		outbound, tag, err := util.GetOutbound(link, index)
		if err != nil || len(tag) == 0 {
			if err != nil {
				logger.Warning("aggregate: failed to convert subscription link")
			}
			continue
		}
		outbounds = append(outbounds, *outbound)
		outTags = append(outTags, tag)
	}

	return &outbounds, &outTags, nil
}

func (u *aggregateUsage) addHeader(header string) {
	parsed, ok := parseUserInfo(header)
	if !ok {
		return
	}
	u.upload += parsed.upload
	u.download += parsed.download
	u.total += parsed.total
	u.addExpire(parsed.expire)
}

func (u *aggregateUsage) addExpire(expire int64) {
	if expire <= 0 {
		return
	}
	if u.expire == 0 || expire < u.expire {
		u.expire = expire
	}
}

func parseUserInfo(header string) (aggregateUsage, bool) {
	var usage aggregateUsage
	if len(header) == 0 {
		return usage, false
	}

	found := false
	for _, part := range strings.Split(header, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		value, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil {
			continue
		}

		switch strings.ToLower(strings.TrimSpace(kv[0])) {
		case "upload":
			usage.upload = value
			found = true
		case "download":
			usage.download = value
			found = true
		case "total":
			usage.total = value
			found = true
		case "expire":
			usage.expire = value
			found = true
		}
	}

	return usage, found
}
