package sub

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/CatMsg/NovaPanel/util"
	"github.com/CatMsg/NovaPanel/util/common"
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

const (
	aggregateSourceConcurrency = 4
	aggregateSourceTimeout     = 8 * time.Second
)

type aggregateSourceResult struct {
	index  int
	source string
	links  []string
	usage  aggregateUsage
}

func (a *AggregateService) GetAggregate(format string, host string) (*string, []string, error) {
	cacheKey := "aggregate:" + strings.TrimSpace(format) + ":" + strings.TrimSpace(host)
	if body, headers, ok := getCachedSubResult(cacheKey); ok {
		return body, headers, nil
	}
	staleBody, staleHeaders, hasStale := getCachedSubResultStale(cacheKey)

	mode, err := a.SettingService.GetSubMode()
	if err != nil {
		return nil, nil, err
	}
	if mode != "master" {
		return nil, nil, common.NewError("aggregate subscription is disabled in slave mode")
	}

	links, usage, err := a.collectAggregateLinks(host)
	if err != nil {
		if hasStale {
			logger.Warning("aggregate: fallback to stale cache after collect failure:", err)
			return staleBody, staleHeaders, nil
		}
		return nil, nil, err
	}

	switch format {
	case "json":
		result, headers, err := a.buildAggregateJson(links, usage)
		if err == nil && result != nil {
			storeCachedSubResult(cacheKey, *result, headers)
		} else if hasStale {
			logger.Warning("aggregate: fallback to stale cache after json build failure:", err)
			return staleBody, staleHeaders, nil
		}
		return result, headers, err
	case "clash":
		result, headers, err := a.buildAggregateClash(links, usage)
		if err == nil && result != nil {
			storeCachedSubResult(cacheKey, *result, headers)
		} else if hasStale {
			logger.Warning("aggregate: fallback to stale cache after clash build failure:", err)
			return staleBody, staleHeaders, nil
		}
		return result, headers, err
	default:
		result, headers, err := a.buildAggregatePlain(links, usage)
		if err == nil && result != nil {
			storeCachedSubResult(cacheKey, *result, headers)
		} else if hasStale {
			logger.Warning("aggregate: fallback to stale cache after plain build failure:", err)
			return staleBody, staleHeaders, nil
		}
		return result, headers, err
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

func (a *AggregateService) buildAggregateClash(links []string, usage aggregateUsage) (*string, []string, error) {
	outbounds, _, err := a.outboundsFromLinks(links)
	if err != nil {
		return nil, nil, err
	}

	basicConfig, err := a.ClashService.getClashConfig()
	if err != nil || len(basicConfig) == 0 {
		basicConfig = basicClashConfig
	}

	result, err := a.ClashService.ConvertToClashMeta(outbounds, basicConfig)
	if err != nil {
		return nil, nil, err
	}

	return a.aggregateFormatResult(result, usage)
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

func (a *AggregateService) collectAggregateLinks(host string) ([]string, aggregateUsage, error) {
	sources, err := a.SettingService.GetSubMasterSources()
	if err != nil {
		return nil, aggregateUsage{}, err
	}
	selfAggregateURI, err := a.selfAggregateURI(host)
	if err != nil {
		return nil, aggregateUsage{}, err
	}

	results := make([]aggregateSourceResult, len(sources))
	var wg sync.WaitGroup
	sem := make(chan struct{}, aggregateSourceConcurrency)
	for index, source := range sources {
		if sameSubscriptionSource(source, selfAggregateURI) {
			logger.Warning("aggregate: skip self source:", source)
			continue
		}
		wg.Add(1)
		go func(index int, source string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			data, headers := util.GetExternalLinkWithHeadersTimeout(source, aggregateSourceTimeout)
			if len(data) == 0 {
				logger.Warning("aggregate: failed to load remote subscription:", source)
				return
			}

			result := aggregateSourceResult{
				index:  index,
				source: source,
				links:  splitAggregateLinks(data),
			}
			result.usage.addHeader(headers.Get("Subscription-Userinfo"))
			results[index] = result
		}(index, source)
	}
	wg.Wait()

	seen := make(map[string]struct{})
	links := make([]string, 0)
	usage := aggregateUsage{}
	for _, result := range results {
		if len(result.links) == 0 {
			continue
		}
		usage.upload += result.usage.upload
		usage.download += result.usage.download
		usage.total += result.usage.total
		usage.addExpire(result.usage.expire)
		for _, link := range result.links {
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

func splitAggregateLinks(data string) []string {
	links := make([]string, 0)
	for _, line := range strings.Split(data, "\n") {
		link := strings.TrimSpace(line)
		if link == "" || strings.HasPrefix(link, "#") {
			continue
		}
		links = append(links, link)
	}
	return links
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

func (u *aggregateUsage) addClient(upload, download, total, expire int64) {
	u.upload += upload
	u.download += download
	u.total += total
	u.addExpire(expire)
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
