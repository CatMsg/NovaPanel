package sub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

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
	mode, err := a.SettingService.GetSubMode()
	if err != nil {
		return nil, nil, err
	}
	if mode != "master" {
		return nil, nil, common.NewError("aggregate subscription is disabled in slave mode")
	}

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "clash":
		proxies, usage, err := a.collectAggregateClashProxies(host)
		if err != nil {
			return nil, nil, err
		}
		return a.buildAggregateClashProxies(proxies, usage)
	}

	links, usage, err := a.collectAggregateLinks(host)
	if err != nil {
		return nil, nil, err
	}

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "json":
		return a.buildAggregateJson(links, usage)
	default:
		return a.buildAggregatePlain(links, usage)
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

func (a *AggregateService) buildAggregateJson(links []string, usage aggregateUsage) (*string, []string, error) {
	jsonConfig := map[string]interface{}{}
	if err := json.Unmarshal([]byte(defaultJson), &jsonConfig); err != nil {
		return nil, nil, err
	}

	outbounds, outTags, err := a.outboundsFromLinks(links)
	if err != nil {
		return nil, nil, err
	}

	a.JsonService.addDefaultOutbounds(outbounds, outTags)
	jsonConfig["outbounds"] = outbounds
	if err := a.JsonService.addOthers(&jsonConfig); err != nil {
		return nil, nil, err
	}

	result, err := json.MarshalIndent(jsonConfig, "", "  ")
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

func (a *AggregateService) collectAggregateClashProxies(host string) ([]map[string]interface{}, aggregateUsage, error) {
	sources, err := a.SettingService.GetSubMasterSources()
	if err != nil {
		return nil, aggregateUsage{}, err
	}
	selfAggregateURI, err := a.selfAggregateURI(host)
	if err != nil {
		return nil, aggregateUsage{}, err
	}

	seen := make(map[string]struct{})
	proxies := make([]map[string]interface{}, 0)
	usage := aggregateUsage{}
	for _, source := range sources {
		if sameSubscriptionSource(source, selfAggregateURI) {
			logger.Warning("aggregate: skip self source:", source)
			continue
		}

		clashSource, err := subscriptionSourceWithFormat(source, "clash")
		if err != nil {
			logger.Warning("aggregate: skip invalid source:", source, err)
			continue
		}
		data, headers := util.GetExternalLinkWithHeaders(clashSource)
		if strings.TrimSpace(data) == "" {
			logger.Warning("aggregate: failed to load remote clash subscription:", source)
			continue
		}
		usage.addHeader(headers.Get("Subscription-Userinfo"))

		sourceProxies, err := clashProxiesFromSource(data)
		if err != nil {
			sourceProxies, err = a.clashProxiesFromPlainSource(data)
		}
		if err != nil {
			logger.Warning("aggregate: skip invalid clash source:", source, err)
			continue
		}
		for _, proxy := range sourceProxies {
			name := strings.TrimSpace(asString(proxy["name"]))
			if name == "" {
				continue
			}
			if _, exists := seen[name]; exists {
				continue
			}
			seen[name] = struct{}{}
			proxies = append(proxies, proxy)
		}
	}

	if len(proxies) == 0 {
		return nil, aggregateUsage{}, common.NewError("no clash proxies found")
	}
	return proxies, usage, nil
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

func (a *AggregateService) collectAggregateLinks(host string) ([]string, aggregateUsage, error) {
	sources, err := a.SettingService.GetSubMasterSources()
	if err != nil {
		return nil, aggregateUsage{}, err
	}
	selfAggregateURI, err := a.selfAggregateURI(host)
	if err != nil {
		return nil, aggregateUsage{}, err
	}

	seen := make(map[string]struct{})
	links := make([]string, 0)
	usage := aggregateUsage{}
	for _, source := range sources {
		if sameSubscriptionSource(source, selfAggregateURI) {
			logger.Warning("aggregate: skip self source:", source)
			continue
		}

		data, headers := util.GetExternalLinkWithHeaders(source)
		if len(data) == 0 {
			logger.Warning("aggregate: failed to load remote subscription:", source)
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
				logger.Warning("aggregate: failed to convert link:", err)
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
