package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/CatMsg/NovaPanel/util"
)

const defaultJson = `
{
  "inbounds": [
    {
      "type": "tun",
      "address": [
				"172.19.0.1/30",
				"fdfe:dcba:9876::1/126"
			],
      "mtu": 9000,
      "auto_route": true,
      "strict_route": false,
      "endpoint_independent_nat": false,
      "platform": {
        "http_proxy": {
          "enabled": true,
          "server": "127.0.0.1",
          "server_port": 2080
        }
      }
    },
    {
      "type": "mixed",
      "listen": "127.0.0.1",
      "listen_port": 2080,
      "users": []
    }
  ]
}
`

type JsonService struct {
	service.SettingService
	LinkService
}

func (j *JsonService) GetJson(subId string, format string) (*string, []string, error) {
	return j.GetJsonContext(context.Background(), subId, format)
}

func (j *JsonService) GetJsonContext(ctx context.Context, subId string, format string) (*string, []string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	client, inDatas, err := j.getData(subId)
	if err != nil {
		return nil, nil, err
	}
	visibilityKey, err := subscriptionVisibilityCacheKey(client)
	if err != nil {
		return nil, nil, err
	}
	cacheKey := "json:" + strings.TrimSpace(subId) + ":" + visibilityKey
	if body, headers, ok := getCachedSubResult(cacheKey); ok {
		return body, headers, nil
	}

	var jsonConfig map[string]interface{}

	outbounds, outTags, err := j.getOutbounds(client.Config, inDatas)
	if err != nil {
		return nil, nil, err
	}
	proxyOutbounds, endpoints := splitNativeJSONEndpoints(*outbounds)
	*outbounds = proxyOutbounds

	links, err := j.LinkService.GetLinksContext(ctx, &client.Links, "external", "")
	if err != nil {
		return nil, nil, err
	}
	tagNumEnable := 0
	if len(links) > 1 {
		tagNumEnable = 1
	}
	for index, link := range links {
		json, tag, err := util.GetOutbound(link, (index+1)*tagNumEnable)
		if err == nil && len(tag) > 0 {
			*outbounds = append(*outbounds, *json)
			*outTags = append(*outTags, tag)
		}
	}

	j.addDefaultOutbounds(outbounds, outTags)

	err = json.Unmarshal([]byte(defaultJson), &jsonConfig)
	if err != nil {
		return nil, nil, err
	}

	jsonConfig["outbounds"] = outbounds
	if len(endpoints) > 0 {
		jsonConfig["endpoints"] = endpoints
	}

	// Add other objects from settings
	if err := j.addOthers(&jsonConfig); err != nil {
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
	resultStr := string(result)

	updateInterval, err := j.SettingService.GetSubUpdates()
	if err != nil {
		return nil, nil, err
	}
	headers := getClientSubscriptionHeaders(client, updateInterval)

	storeCachedSubResult(cacheKey, resultStr, headers)
	return &resultStr, headers, nil
}

func (j *JsonService) getData(subId string) (*model.Client, []*model.Inbound, error) {
	db := database.GetDB()
	client := &model.Client{}
	err := db.Model(model.Client{}).Where("enable = true and name = ?", subId).First(client).Error
	if err != nil {
		return nil, nil, err
	}
	inbounds, err := visibleSubscriptionInbounds(client)
	if err != nil {
		return nil, nil, err
	}
	return client, inbounds, nil
}

func (j *JsonService) getOutbounds(clientConfig json.RawMessage, inbounds []*model.Inbound, formats ...string) (*[]map[string]interface{}, *[]string, error) {
	var outbounds []map[string]interface{}
	var configs map[string]interface{}
	var outTags []string

	err := json.Unmarshal(clientConfig, &configs)
	if err != nil {
		return nil, nil, err
	}
	for _, inData := range inbounds {
		if len(inData.OutJson) < 5 {
			continue
		}
		var outbound map[string]interface{}
		err = json.Unmarshal(inData.OutJson, &outbound)
		if err != nil {
			return nil, nil, err
		}
		protocol, _ := outbound["type"].(string)
		mihomoTemplate, _ := outbound["nova_mihomo"].(map[string]interface{})
		delete(outbound, "nova_mihomo")
		var httpVersions []int
		if protocol == "http" {
			httpVersions, err = util.ValidateHTTPInbound(inData)
			if err != nil {
				return nil, nil, fmt.Errorf("HTTP inbound %q: %w", inData.Tag, err)
			}
		}

		// Shadowsocks
		if protocol == "shadowsocks" {
			var userPass []string
			var inbOptions map[string]interface{}
			err = json.Unmarshal(inData.Options, &inbOptions)
			if err != nil {
				return nil, nil, err
			}
			method, _ := inbOptions["method"].(string)
			if strings.HasPrefix(method, "2022") {
				inbPass, _ := inbOptions["password"].(string)
				userPass = append(userPass, inbPass)
			}
			var pass string
			if method == "2022-blake3-aes-128-gcm" {
				pass, _ = configs["shadowsocks16"].(map[string]interface{})["password"].(string)
			} else {
				pass, _ = configs["shadowsocks"].(map[string]interface{})["password"].(string)
			}
			userPass = append(userPass, pass)
			outbound["password"] = strings.Join(userPass, ":")
		} else if protocol == "masque-client" {
			credential, _ := configs["masque"].(map[string]interface{})
			username, _ := credential["username"].(string)
			password, _ := credential["password"].(string)
			if username == "" || password == "" {
				return nil, nil, fmt.Errorf("native MASQUE credentials missing for inbound %q", inData.Tag)
			}
			outbound["username"], outbound["password"] = username, password
			if len(formats) > 0 && formats[0] == "clash" && mihomoTemplate != nil {
				publicKey, err := service.MasqueMihomoPublicKey(inData)
				if err != nil {
					return nil, nil, err
				}
				mihomoTemplate["public-key"] = publicKey
				entries, _ := credential["mihomo"].(map[string]interface{})
				compatible, _ := entries[strconv.FormatUint(uint64(inData.Id), 10)].(map[string]interface{})
				privateKey, _ := compatible["private_key"].(string)
				ip, _ := compatible["ip"].(string)
				if privateKey == "" || ip == "" {
					return nil, nil, fmt.Errorf("credentials for Mihomo MASQUE missing for inbound %q", inData.Tag)
				}
				outbound = map[string]interface{}{"type": "masque", "tag": outbound["tag"], "server": outbound["server"], "server_port": outbound["server_port"], "private-key": privateKey, "ip": ip}
				for key, value := range mihomoTemplate {
					outbound[key] = value
				}
				protocol = "masque"
			}
		} else { // Other protocols
			config, _ := configs[protocol].(map[string]interface{})
			if protocol == "http" {
				if err := mergeHTTPClientOverrides(outbound, config, inData.TlsId == 0); err != nil {
					return nil, nil, fmt.Errorf("HTTP inbound %q client configuration: %w", inData.Tag, err)
				}
			} else {
				for key, value := range config {
					if key == "name" || key == "alterId" || (key == "flow" && inData.TlsId == 0) {
						continue
					}
					outbound[key] = value
				}
			}
		}
		if protocol == "http" {
			if err := util.SetHTTPOutboundVersion(outbound, httpVersions[0]); err != nil {
				return nil, nil, fmt.Errorf("HTTP inbound %q subscription: %w", inData.Tag, err)
			}
			if _, err := util.ResolveHTTPOutboundVersion(outbound, httpVersions); err != nil {
				return nil, nil, fmt.Errorf("HTTP inbound %q subscription: %w", inData.Tag, err)
			}
			if err = util.ValidateHTTPOutboundTLSForInbound(outbound, inData); err != nil {
				return nil, nil, fmt.Errorf("HTTP inbound %q subscription: %w", inData.Tag, err)
			}
		}

		var addrs []map[string]interface{}
		err = json.Unmarshal(inData.Addrs, &addrs)
		if err != nil {
			return nil, nil, err
		}
		tag, _ := outbound["tag"].(string)
		if len(addrs) == 0 {
			// For mixed protocol, use separated socks and http
			if protocol == "mixed" {
				outbound["tag"] = tag
				j.pushMixed(&outbounds, &outTags, outbound)
			} else {
				outTags = append(outTags, tag)
				outbounds = append(outbounds, outbound)
			}
		} else {
			for index, addr := range addrs {
				// Each address gets an independent copy so TLS overrides cannot
				// leak into another address or the shared inbound template.
				newOut, err := util.CloneJSONMap(outbound)
				if err != nil {
					return nil, nil, fmt.Errorf("copy outbound for inbound %q address %d: %w", inData.Tag, index+1, err)
				}
				// Change and push copied config.
				newOut["server"], _ = addr["server"].(string)
				port, _ := addr["server_port"].(float64)
				newOut["server_port"] = int(port)

				// Override TLS
				if addrTls, ok := addr["tls"].(map[string]interface{}); ok {
					outTls, _ := newOut["tls"].(map[string]interface{})
					if outTls == nil {
						outTls = make(map[string]interface{})
					}
					addrTlsCopy, copyErr := util.CloneJSONMap(addrTls)
					if copyErr != nil {
						return nil, nil, fmt.Errorf("copy TLS override for inbound %q address %d: %w", inData.Tag, index+1, copyErr)
					}
					for key, value := range addrTlsCopy {
						outTls[key] = value
					}
					newOut["tls"] = outTls
				}
				if protocol == "http" {
					if err := util.SetHTTPOutboundVersion(newOut, httpVersions[0]); err != nil {
						return nil, nil, fmt.Errorf("HTTP inbound %q address %d subscription: %w", inData.Tag, index+1, err)
					}
					if _, resolveErr := util.ResolveHTTPOutboundVersion(newOut, httpVersions); resolveErr != nil {
						return nil, nil, fmt.Errorf("HTTP inbound %q address %d subscription: %w", inData.Tag, index+1, resolveErr)
					}
					if err = util.ValidateHTTPOutboundTLSForInbound(newOut, inData); err != nil {
						return nil, nil, fmt.Errorf("HTTP inbound %q address %d subscription: %w", inData.Tag, index+1, err)
					}
				}
				if protocol == "masque-client" {
					tls, _ := newOut["tls"].(map[string]interface{})
					if tls == nil || tls["enabled"] != true {
						return nil, nil, fmt.Errorf("native MASQUE requires TLS")
					}
					tls["alpn"] = []string{"h3"}
				}

				remark, _ := addr["remark"].(string)
				newTag := inboundAddressTag(tag, remark, index, len(addrs))
				newOut["tag"] = newTag
				// For mixed protocol, use separated socks and http
				if protocol == "mixed" {
					j.pushMixed(&outbounds, &outTags, newOut)
				} else {
					outTags = append(outTags, newTag)
					outbounds = append(outbounds, newOut)
				}
			}
		}
	}
	reserved := map[string]struct{}{"proxy": {}, "auto": {}, "direct": {}}
	for _, node := range outbounds {
		if node["type"] != "masque-client" {
			reserved[asString(node["tag"])] = struct{}{}
		}
	}
	for index, node := range outbounds {
		if node["type"] != "masque-client" {
			continue
		}
		tag := asString(node["tag"])
		if _, collision := reserved[tag]; collision {
			base := "masque." + tag
			for suffix := 1; ; suffix++ {
				candidate := base
				if suffix > 1 {
					candidate = fmt.Sprintf("%s.%d", base, suffix)
				}
				if _, exists := reserved[candidate]; !exists {
					tag = candidate
					break
				}
			}
		}
		node["tag"], outTags[index] = tag, tag
		reserved[tag] = struct{}{}
	}
	return &outbounds, &outTags, nil
}

func mergeHTTPClientOverrides(outbound, clientHTTP map[string]interface{}, hasNoTLS bool) error {
	if err := util.ValidateHTTPClientVersionOverride(clientHTTP); err != nil {
		return err
	}
	for key, value := range clientHTTP {
		if key == "name" || key == "alterId" || key == "version" || (key == "flow" && hasNoTLS) {
			continue
		}
		outbound[key] = value
	}
	return nil
}

func withoutUnsupportedJSONOutbounds(outbounds []map[string]interface{}, tags []string, unsupported ...string) ([]map[string]interface{}, []string) {
	blocked := make(map[string]struct{}, len(unsupported))
	for _, protocol := range unsupported {
		blocked[protocol] = struct{}{}
	}
	filtered := make([]map[string]interface{}, 0, len(outbounds))
	allowedTags := make(map[string]struct{})
	for _, outbound := range outbounds {
		protocol, _ := outbound["type"].(string)
		if _, skip := blocked[protocol]; skip {
			continue
		}
		filtered = append(filtered, outbound)
		if tag, _ := outbound["tag"].(string); tag != "" {
			allowedTags[tag] = struct{}{}
		}
	}
	filteredTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		if _, ok := allowedTags[tag]; ok {
			filteredTags = append(filteredTags, tag)
		}
	}
	return filtered, filteredTags
}

func splitNativeJSONEndpoints(nodes []map[string]interface{}) (outbounds, endpoints []map[string]interface{}) {
	for _, node := range nodes {
		if node["type"] == "masque-client" {
			endpoints = append(endpoints, node)
		} else {
			outbounds = append(outbounds, node)
		}
	}
	return
}

func (j *JsonService) addDefaultOutbounds(outbounds *[]map[string]interface{}, outTags *[]string) {
	outbound := []map[string]interface{}{
		{
			"outbounds": append([]string{"auto", "direct"}, *outTags...),
			"tag":       "proxy",
			"type":      "selector",
		},
		{
			"tag":       "auto",
			"type":      "urltest",
			"outbounds": outTags,
			"url":       "http://www.gstatic.com/generate_204",
			"interval":  "10m",
			"tolerance": 50,
		},
		{
			"type": "direct",
			"tag":  "direct",
		},
	}
	*outbounds = append(outbound, *outbounds...)
}

func (j *JsonService) addOthers(jsonConfig *map[string]interface{}) error {
	rules_start := []interface{}{
		map[string]interface{}{
			"action": "sniff",
		},
		map[string]interface{}{
			"clash_mode": "Direct",
			"action":     "route",
			"outbound":   "direct",
		},
	}
	rules_end := []interface{}{
		map[string]interface{}{
			"clash_mode": "Global",
			"action":     "route",
			"outbound":   "proxy",
		},
	}
	route := map[string]interface{}{
		"auto_detect_interface": true,
		"final":                 "proxy",
		"rules":                 rules_start,
	}

	othersStr, err := j.SettingService.GetSubJsonExt()
	if err != nil {
		return err
	}
	if len(othersStr) == 0 {
		(*jsonConfig)["route"] = route
		return nil
	}
	var othersJson map[string]interface{}
	err = json.Unmarshal([]byte(othersStr), &othersJson)
	if err != nil {
		return err
	}
	if _, ok := othersJson["log"]; ok {
		(*jsonConfig)["log"] = othersJson["log"]
	}
	if _, ok := othersJson["dns"]; ok {
		(*jsonConfig)["dns"] = othersJson["dns"]
	}
	if _, ok := othersJson["inbounds"]; ok {
		(*jsonConfig)["inbounds"] = othersJson["inbounds"]
	}
	if _, ok := othersJson["experimental"]; ok {
		(*jsonConfig)["experimental"] = othersJson["experimental"]
	}
	if _, ok := othersJson["rule_set"]; ok {
		route["rule_set"] = othersJson["rule_set"]
	}
	if settingRules, ok := othersJson["rules"].([]interface{}); ok {
		rules := append(rules_start, settingRules...)
		route["rules"] = append(rules, rules_end...)
	}
	if defaultDomainResolver, ok := othersJson["default_domain_resolver"].(string); ok {
		route["default_domain_resolver"] = defaultDomainResolver
	}
	(*jsonConfig)["route"] = route

	return nil
}

func (j *JsonService) pushMixed(outbounds *[]map[string]interface{}, outTags *[]string, out map[string]interface{}) {
	socksOut := make(map[string]interface{}, 1)
	httpOut := make(map[string]interface{}, 1)
	for key, value := range out {
		socksOut[key] = value
		httpOut[key] = value
	}
	socksTag := fmt.Sprintf("%s-socks", out["tag"])
	httpTag := fmt.Sprintf("%s-http", out["tag"])
	socksOut["type"] = "socks"
	httpOut["type"] = "http"
	socksOut["tag"] = socksTag
	httpOut["tag"] = httpTag
	*outbounds = append(*outbounds, socksOut, httpOut)
	*outTags = append(*outTags, socksTag, httpTag)
}
