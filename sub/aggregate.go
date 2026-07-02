package sub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
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

type aggregateSources struct {
	links        []string
	outbounds    []map[string]interface{}
	clashProxies []map[string]interface{}
	usage        aggregateUsage
}

func (a *AggregateService) GetAggregate(format string, host string) (*string, []string, error) {
	mode, err := a.SettingService.GetSubMode()
	if err != nil {
		return nil, nil, err
	}
	if mode != "master" {
		return nil, nil, common.NewError("aggregate subscription is disabled in slave mode")
	}

	sources, err := a.collectAggregateSources(host)
	if err != nil {
		return nil, nil, err
	}

	switch format {
	case "json":
		return a.buildAggregateJson(sources)
	case "clash":
		return a.buildAggregateClash(sources)
	default:
		return a.buildAggregatePlain(sources.links, sources.usage)
	}
}

func (a *AggregateService) GetClientAggregate(subId string, format string, host string) (*string, []string, error) {
	client, inDatas, err := a.JsonService.getData(subId)
	if err != nil {
		return nil, nil, err
	}

	sources, err := a.collectAggregateSources(host)
	if err != nil {
		if strings.Contains(err.Error(), "no subscription links found") {
			sources = &aggregateSources{}
		} else {
			return nil, nil, err
		}
	}
	sources.usage.addClient(client.Up, client.Down, client.Volume, client.Expiry)

	switch format {
	case "json":
		return a.buildClientAggregateJson(client, inDatas, sources)
	case "clash":
		return a.buildClientAggregateClash(client, inDatas, sources)
	default:
		return a.buildClientAggregatePlain(client, sources.links, sources.usage)
	}
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

func (a *AggregateService) buildAggregateJson(sources *aggregateSources) (*string, []string, error) {
	jsonConfig := map[string]interface{}{}
	if err := json.Unmarshal([]byte(defaultJson), &jsonConfig); err != nil {
		return nil, nil, err
	}

	outbounds := make([]map[string]interface{}, 0, len(sources.outbounds))
	outbounds = append(outbounds, sources.outbounds...)
	outTags := make([]string, 0, len(outbounds))
	seenTags := make(map[string]struct{}, len(outbounds))
	for _, outbound := range outbounds {
		if tag, _ := outbound["tag"].(string); tag != "" {
			if _, exists := seenTags[tag]; exists {
				continue
			}
			seenTags[tag] = struct{}{}
			outTags = append(outTags, tag)
		}
	}
	a.appendConvertedLinks(&outbounds, &outTags, seenTags, sources.links)

	a.JsonService.addDefaultOutbounds(&outbounds, &outTags)
	jsonConfig["outbounds"] = &outbounds
	if err := a.JsonService.addOthers(&jsonConfig); err != nil {
		return nil, nil, err
	}

	result, err := json.MarshalIndent(jsonConfig, "", "  ")
	if err != nil {
		return nil, nil, err
	}

	return a.aggregateFormatResult(string(result), sources.usage)
}

func (a *AggregateService) buildAggregateClash(sources *aggregateSources) (*string, []string, error) {
	outbounds := make([]map[string]interface{}, 0, len(sources.outbounds))
	outbounds = append(outbounds, sources.outbounds...)
	outTags := make([]string, 0, len(outbounds))
	seenTags := make(map[string]struct{}, len(outbounds))
	for _, outbound := range outbounds {
		if tag, _ := outbound["tag"].(string); tag != "" {
			if _, exists := seenTags[tag]; exists {
				continue
			}
			seenTags[tag] = struct{}{}
			outTags = append(outTags, tag)
		}
	}
	a.appendConvertedLinks(&outbounds, &outTags, seenTags, sources.links)
	basicConfig, err := a.ClashService.getClashConfig()
	if err != nil || len(basicConfig) == 0 {
		basicConfig = basicClashConfig
	}

	result, err := a.ClashService.ConvertToClashMetaWithExtraProxies(&outbounds, sources.clashProxies, basicConfig)
	if err != nil {
		return nil, nil, err
	}

	return a.aggregateFormatResult(result, sources.usage)
}

func (a *AggregateService) buildClientAggregatePlain(client *model.Client, aggregateLinks []string, usage aggregateUsage) (*string, []string, error) {
	clientInfo := ""
	subShowInfo, _ := a.SettingService.GetSubShowInfo()
	if subShowInfo {
		clientInfo = a.getClientInfo(client)
	}

	seen := make(map[string]struct{})
	links := make([]string, 0)
	links = appendUniqueLinks(links, seen, a.JsonService.LinkService.GetLinks(&client.Links, "all", clientInfo))
	links = appendUniqueLinks(links, seen, aggregateLinks)
	return a.buildAggregatePlain(links, usage)
}

func (a *AggregateService) buildClientAggregateJson(client *model.Client, inDatas []*model.Inbound, sources *aggregateSources) (*string, []string, error) {
	outbounds, outTags, err := a.JsonService.getOutbounds(client.Config, inDatas)
	if err != nil {
		return nil, nil, err
	}

	seenTags := make(map[string]struct{}, len(*outTags))
	for _, tag := range *outTags {
		seenTags[tag] = struct{}{}
	}

	for _, outbound := range sources.outbounds {
		tag, _ := outbound["tag"].(string)
		if tag == "" {
			continue
		}
		if _, exists := seenTags[tag]; exists {
			continue
		}
		seenTags[tag] = struct{}{}
		*outbounds = append(*outbounds, outbound)
		*outTags = append(*outTags, tag)
	}

	remoteLinksSeen := make(map[string]struct{})
	remoteLinks := make([]string, 0)
	remoteLinks = appendUniqueLinks(remoteLinks, remoteLinksSeen, a.JsonService.LinkService.GetRemoteLinks(&client.Links, true))
	remoteLinks = appendUniqueLinks(remoteLinks, remoteLinksSeen, sources.links)

	a.appendConvertedLinks(outbounds, outTags, seenTags, remoteLinks)
	a.JsonService.addDefaultOutbounds(outbounds, outTags)

	jsonConfig := map[string]interface{}{}
	if err := json.Unmarshal([]byte(defaultJson), &jsonConfig); err != nil {
		return nil, nil, err
	}
	jsonConfig["outbounds"] = outbounds
	if err := a.JsonService.addOthers(&jsonConfig); err != nil {
		return nil, nil, err
	}

	result, err := json.MarshalIndent(jsonConfig, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return a.aggregateFormatResult(string(result), sources.usage)
}

func (a *AggregateService) buildClientAggregateClash(client *model.Client, inDatas []*model.Inbound, sources *aggregateSources) (*string, []string, error) {
	outbounds, outTags, err := a.JsonService.getOutbounds(client.Config, inDatas)
	if err != nil {
		return nil, nil, err
	}

	seenTags := make(map[string]struct{}, len(*outTags))
	for _, tag := range *outTags {
		seenTags[tag] = struct{}{}
	}

	appendUniqueOutbounds(outbounds, outTags, seenTags, loadMasqueOutbounds(a.JsonService.LinkService.GetAssignedMasqueTags(&client.Links)))

	for _, outbound := range sources.outbounds {
		tag, _ := outbound["tag"].(string)
		if tag == "" {
			continue
		}
		if _, exists := seenTags[tag]; exists {
			continue
		}
		seenTags[tag] = struct{}{}
		*outbounds = append(*outbounds, outbound)
		*outTags = append(*outTags, tag)
	}

	remoteLinksSeen := make(map[string]struct{})
	remoteLinks := make([]string, 0)
	remoteLinks = appendUniqueLinks(remoteLinks, remoteLinksSeen, a.JsonService.LinkService.GetRemoteLinks(&client.Links, true))
	remoteLinks = appendUniqueLinks(remoteLinks, remoteLinksSeen, sources.links)

	a.appendConvertedLinks(outbounds, outTags, seenTags, remoteLinks)

	basicConfig, err := a.ClashService.getClashConfig()
	if err != nil || len(basicConfig) == 0 {
		basicConfig = basicClashConfig
	}

	result, err := a.ClashService.ConvertToClashMetaWithExtraProxies(outbounds, sources.clashProxies, basicConfig)
	if err != nil {
		return nil, nil, err
	}
	return a.aggregateFormatResult(result, sources.usage)
}

func (a *AggregateService) aggregateHeaders(usage aggregateUsage) []string {
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
		"NovaPanel Aggregate",
	}
}

func (a *AggregateService) aggregateFormatResult(result string, usage aggregateUsage) (*string, []string, error) {
	return &result, a.aggregateHeaders(usage), nil
}

func (a *AggregateService) collectAggregateSources(host string) (*aggregateSources, error) {
	sources := &aggregateSources{
		links:        make([]string, 0),
		outbounds:    make([]map[string]interface{}, 0),
		clashProxies: make([]map[string]interface{}, 0),
	}
	seenLinks := make(map[string]struct{})
	seenTags := make(map[string]struct{})
	seenProxyNames := make(map[string]struct{})

	upstreamSources, err := a.SettingService.GetSubMasterSources()
	if err != nil {
		return nil, err
	}
	selfAggregateURI, err := a.selfAggregateURI(host)
	if err != nil {
		return nil, err
	}
	for _, source := range upstreamSources {
		if sameSubscriptionSource(source, selfAggregateURI) {
			logger.Warning("aggregate: skip self source:", source)
			continue
		}
		data, headers := util.GetExternalLinkWithHeaders(source)
		if len(data) == 0 {
			logger.Warning("aggregate: failed to load remote subscription:", source)
			continue
		}
		sources.usage.addHeader(headers.Get("Subscription-Userinfo"))

		sourceLinks, sourceOutbounds, sourceClashProxies := parseAggregateSource(data)
		for _, link := range sourceLinks {
			if _, exists := seenLinks[link]; exists {
				continue
			}
			seenLinks[link] = struct{}{}
			sources.links = append(sources.links, link)
		}
		for _, outbound := range sourceOutbounds {
			tag, _ := outbound["tag"].(string)
			if tag == "" {
				continue
			}
			if _, exists := seenTags[tag]; exists {
				continue
			}
			seenTags[tag] = struct{}{}
			sources.outbounds = append(sources.outbounds, outbound)
		}
		for _, proxy := range sourceClashProxies {
			name, _ := proxy["name"].(string)
			if name == "" {
				continue
			}
			if _, exists := seenProxyNames[name]; exists {
				continue
			}
			seenProxyNames[name] = struct{}{}
			sources.clashProxies = append(sources.clashProxies, proxy)
		}
	}

	if len(sources.links) == 0 && len(sources.outbounds) == 0 && len(sources.clashProxies) == 0 {
		return nil, common.NewError("no subscription links found")
	}
	return sources, nil
}

func (a *AggregateService) selfAggregateURI(host string) (string, error) {
	base, err := a.SettingService.GetFinalSubURI(host)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(strings.TrimSpace(base), "/") + "/aggregate", nil
}

func sameSubscriptionSource(left string, right string) bool {
	return strings.TrimRight(strings.TrimSpace(left), "/") == strings.TrimRight(strings.TrimSpace(right), "/")
}

func (a *AggregateService) outboundsFromLinks(links []string) (*[]map[string]interface{}, *[]string, error) {
	outbounds := make([]map[string]interface{}, 0)
	outTags := make([]string, 0)

	for index, link := range links {
		outbound, tag, err := util.GetOutbound(link, index)
		if err != nil || len(tag) == 0 {
			if err != nil {
				logger.Warning("aggregate: failed to convert link:", err)
			}
			continue
		}
		outbounds = append(outbounds, *outbound)
		outTags = append(outTags, tag)
	}

	return &outbounds, &outTags, nil
}

func (a *AggregateService) appendConvertedLinks(outbounds *[]map[string]interface{}, outTags *[]string, seenTags map[string]struct{}, links []string) {
	tagNumEnable := 0
	if len(links) > 1 {
		tagNumEnable = 1
	}
	for index, link := range links {
		outbound, tag, err := util.GetOutbound(link, (index+1)*tagNumEnable)
		if err != nil || len(tag) == 0 {
			if err != nil {
				logger.Warning("aggregate: failed to convert link:", err)
			}
			continue
		}
		if _, exists := seenTags[tag]; exists {
			continue
		}
		seenTags[tag] = struct{}{}
		*outbounds = append(*outbounds, *outbound)
		*outTags = append(*outTags, tag)
	}
}

func parseAggregateSource(data string) ([]string, []map[string]interface{}, []map[string]interface{}) {
	trimmed := strings.TrimSpace(data)
	if trimmed == "" {
		return nil, nil, nil
	}

	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if outbounds := parseAggregateJSONOutbounds(trimmed); len(outbounds) > 0 {
			return nil, outbounds, nil
		}
	}

	if strings.Contains(trimmed, "proxies:") {
		if proxies := parseAggregateClashProxies(trimmed); len(proxies) > 0 {
			return nil, nil, proxies
		}
	}

	links := make([]string, 0)
	for _, line := range strings.Split(trimmed, "\n") {
		link := strings.TrimSpace(line)
		if link == "" || strings.HasPrefix(link, "#") {
			continue
		}
		links = append(links, link)
	}
	return links, nil, nil
}

func parseAggregateJSONOutbounds(data string) []map[string]interface{} {
	jsonData := map[string]interface{}{}
	if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
		logger.Warning("aggregate: failed to parse json source:", err)
		return nil
	}

	items, ok := jsonData["outbounds"].([]interface{})
	if !ok {
		return nil
	}

	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		outbound, ok := item.(map[string]interface{})
		if !ok || len(outbound) == 0 {
			continue
		}
		switch outbound["type"] {
		case "urltest", "direct", "selector", "block":
			continue
		}
		result = append(result, outbound)
	}
	return result
}

func parseAggregateClashProxies(data string) []map[string]interface{} {
	root := map[string]interface{}{}
	if err := yaml.Unmarshal([]byte(data), &root); err != nil {
		return nil
	}

	items, ok := root["proxies"].([]interface{})
	if !ok {
		return nil
	}

	result := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		proxy, ok := item.(map[string]interface{})
		if !ok || len(proxy) == 0 {
			continue
		}
		name, _ := proxy["name"].(string)
		if name == "" {
			continue
		}
		result = append(result, proxy)
	}
	return result
}

func (u *aggregateUsage) addClient(upload, download, total, expire int64) {
	u.upload += upload
	u.download += download
	u.total += total
	u.addExpire(expire)
}

func (a *AggregateService) getClientInfo(c *model.Client) string {
	now := time.Now().Unix()

	var result []string
	if vol := c.Volume - (c.Up + c.Down); vol > 0 {
		result = append(result, fmt.Sprintf("%s%s", a.formatTraffic(vol), "📊"))
	}
	if c.Expiry > 0 {
		result = append(result, fmt.Sprintf("%d%s⏳", (c.Expiry-now)/86400, "Days"))
	}
	if len(result) > 0 {
		return " " + strings.Join(result, " ")
	}
	return " ♾"
}

func (a *AggregateService) formatTraffic(trafficBytes int64) string {
	if trafficBytes < 1024 {
		return fmt.Sprintf("%.2fB", float64(trafficBytes))
	} else if trafficBytes < (1024 * 1024) {
		return fmt.Sprintf("%.2fKB", float64(trafficBytes)/float64(1024))
	} else if trafficBytes < (1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fMB", float64(trafficBytes)/float64(1024*1024))
	} else if trafficBytes < (1024 * 1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fGB", float64(trafficBytes)/float64(1024*1024*1024))
	} else if trafficBytes < (1024 * 1024 * 1024 * 1024 * 1024) {
		return fmt.Sprintf("%.2fTB", float64(trafficBytes)/float64(1024*1024*1024*1024))
	}
	return fmt.Sprintf("%.2fEB", float64(trafficBytes)/float64(1024*1024*1024*1024*1024))
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
