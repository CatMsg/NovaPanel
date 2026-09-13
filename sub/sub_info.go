package sub

import (
	"math"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/CatMsg/NovaPanel/util"
)

type clientSubscriptionUsage struct {
	upload    int64
	download  int64
	total     int64
	remaining int64
	inherited bool
}

var trafficBudgetStatusSnapshot = func() service.TrafficBudgetStatus {
	return service.GetTrafficBudgetService().GetStatusSnapshot()
}

func resolveClientSubscriptionUsage(client *model.Client) clientSubscriptionUsage {
	if client == nil {
		return clientSubscriptionUsage{}
	}
	upload := clampTraffic(client.Up)
	download := clampTraffic(client.Down)
	usage := clientSubscriptionUsage{upload: upload, download: download, total: client.Volume}
	if client.Volume > 0 {
		used := saturatingTrafficAdd(upload, download)
		usage.remaining = client.Volume - used
		if usage.remaining < 0 {
			usage.remaining = 0
		}
		return usage
	}

	status := trafficBudgetStatusSnapshot()
	if !status.Enabled || !status.Supported {
		usage.total = 0
		return usage
	}

	usage.inherited = true
	usage.remaining = uint64ToTraffic(status.PoolRemainingBytes)
	used := saturatingTrafficAdd(upload, download)
	usage.total = saturatingTrafficAdd(used, usage.remaining)
	return usage
}

func getClientSubscriptionHeaders(client *model.Client, updateInterval int) []string {
	if client == nil {
		client = &model.Client{}
	}
	usage := resolveClientSubscriptionUsage(client)
	effective := *client
	effective.Up = usage.upload
	effective.Down = usage.download
	effective.Volume = usage.total
	return util.GetHeaders(&effective, updateInterval)
}

func decorateSubscriptionOutboundTags(outbounds *[]map[string]interface{}, outTags *[]string, client *model.Client, enabled bool) {
	if !enabled || outbounds == nil || outTags == nil {
		return
	}
	info := (&SubService{}).getClientInfo(client)
	if info == "" {
		return
	}

	renamed := make(map[string]string, len(*outbounds))
	for _, outbound := range *outbounds {
		tag, _ := outbound["tag"].(string)
		if tag == "" {
			continue
		}
		decorated := tag + info
		outbound["tag"] = decorated
		renamed[tag] = decorated
	}
	for index, tag := range *outTags {
		if decorated, ok := renamed[tag]; ok {
			(*outTags)[index] = decorated
		}
	}
}

func clampTraffic(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func saturatingTrafficAdd(left, right int64) int64 {
	if left > math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func uint64ToTraffic(value uint64) int64 {
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}
