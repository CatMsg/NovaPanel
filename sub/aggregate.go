package sub

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
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
	service.EndpointService
	JsonService
	ClashService
}

type aggregateUsage struct {
	upload   int64
	download int64
	total    int64
	expire   int64
}

func (a *AggregateService) GetAggregate(format string, host string) (*string, []string, error) {
	mode, err := a.SettingService.GetSubMode()
	if err != nil {
		return nil, nil, err
	}
	if mode != "master" {
		return nil, nil, common.NewError("aggregate subscription is disabled in slave mode")
	}

	links, usage, err := a.collectAggregateLinks(host)
	if err != nil {
		return nil, nil, err
	}

	switch format {
	case "json":
		return a.buildAggregateJson(links, usage)
	case "clash":
		return a.buildAggregateClash(links, usage)
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
	return a.buildSubscriptionHeaders("NovaPanel Aggregate", usage)
}

func (a *AggregateService) endpointAggregateHeaders() []string {
	return a.buildSubscriptionHeaders("NovaPanel Endpoint Aggregate", aggregateUsage{})
}

func (a *AggregateService) endpointSourceHeaders() []string {
	return a.buildSubscriptionHeaders("NovaPanel Endpoint Source", aggregateUsage{})
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

func (a *AggregateService) GetEndpointSource(format string, host string) (*string, []string, error) {
	outbounds, err := a.collectEndpointSourceOutbounds(host)
	if err != nil {
		return nil, nil, err
	}

	switch format {
	case "json":
		return a.buildEndpointSourceJson(outbounds)
	case "clash":
		return a.buildEndpointSourceClash(outbounds)
	default:
		return a.buildEndpointSourceClash(outbounds)
	}
}

func (a *AggregateService) GetEndpointAggregate(format string, host string) (*string, []string, error) {
	mode, err := a.SettingService.GetEndpointMode()
	if err != nil {
		return nil, nil, err
	}
	if mode != "master" {
		return nil, nil, common.NewError("endpoint aggregate is disabled in slave mode")
	}

	outbounds, err := a.collectEndpointAggregateOutbounds(host)
	if err != nil {
		return nil, nil, err
	}

	switch format {
	case "json":
		return a.buildEndpointAggregateJson(outbounds)
	case "clash":
		return a.buildEndpointAggregateClash(outbounds)
	default:
		return a.buildEndpointAggregateClash(outbounds)
	}
}

func (a *AggregateService) buildEndpointSourceJson(outbounds []map[string]interface{}) (*string, []string, error) {
	result, err := json.MarshalIndent(outbounds, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	resultStr := string(result)
	return &resultStr, a.endpointSourceHeaders(), nil
}

func (a *AggregateService) buildEndpointSourceClash(outbounds []map[string]interface{}) (*string, []string, error) {
	basicConfig, err := a.ClashService.getClashConfig()
	if err != nil || len(basicConfig) == 0 {
		basicConfig = basicClashConfig
	}

	result, err := a.ClashService.ConvertToClashMeta(&outbounds, basicConfig)
	if err != nil {
		return nil, nil, err
	}

	return &result, a.endpointSourceHeaders(), nil
}

func (a *AggregateService) buildEndpointAggregateJson(outbounds []map[string]interface{}) (*string, []string, error) {
	result, err := json.MarshalIndent(outbounds, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	resultStr := string(result)
	return &resultStr, a.endpointAggregateHeaders(), nil
}

func (a *AggregateService) buildEndpointAggregateClash(outbounds []map[string]interface{}) (*string, []string, error) {
	basicConfig, err := a.ClashService.getClashConfig()
	if err != nil || len(basicConfig) == 0 {
		basicConfig = basicClashConfig
	}

	result, err := a.ClashService.ConvertToClashMeta(&outbounds, basicConfig)
	if err != nil {
		return nil, nil, err
	}

	return &result, a.endpointAggregateHeaders(), nil
}

func (a *AggregateService) collectEndpointSourceOutbounds(host string) ([]map[string]interface{}, error) {
	outbounds, err := a.collectLocalEndpointOutbounds(host)
	if err != nil {
		return nil, err
	}

	if len(outbounds) == 0 {
		return nil, common.NewError("no endpoint nodes found")
	}

	return outbounds, nil
}

func (a *AggregateService) collectEndpointAggregateOutbounds(host string) ([]map[string]interface{}, error) {
	localOutbounds, err := a.collectLocalEndpointOutbounds(host)
	if err != nil {
		return nil, err
	}

	sources, err := a.SettingService.GetEndpointSources()
	if err != nil {
		return nil, err
	}
	selfSourceURI, err := a.selfEndpointSourceURI(host)
	if err != nil {
		return nil, err
	}
	selfAggregateURI, err := a.selfEndpointAggregateURI(host)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	outbounds := make([]map[string]interface{}, 0, len(localOutbounds))
	for _, node := range localOutbounds {
		tag := endpointNodeTag(node)
		if tag == "" {
			continue
		}
		if _, exists := seen[tag]; exists {
			continue
		}
		seen[tag] = struct{}{}
		outbounds = append(outbounds, node)
	}

	for _, source := range sources {
		if sameSubscriptionSource(source, selfSourceURI) || sameSubscriptionSource(source, selfAggregateURI) {
			logger.Warning("endpoint aggregate: skip self source:", source)
			continue
		}

		data, _ := util.GetExternalLinkWithHeaders(source)
		if strings.TrimSpace(data) == "" {
			logger.Warning("endpoint aggregate: failed to load remote source:", source)
			continue
		}
		nodes, err := endpointOutboundsFromSource(data)
		if err != nil {
			logger.Warning("endpoint aggregate: skip invalid source:", source, err)
			continue
		}
		for _, node := range nodes {
			tag := endpointNodeTag(node)
			if tag == "" {
				continue
			}
			if _, exists := seen[tag]; exists {
				continue
			}
			seen[tag] = struct{}{}
			outbounds = append(outbounds, node)
		}
	}

	if len(outbounds) == 0 {
		return nil, common.NewError("no endpoint nodes found")
	}
	return outbounds, nil
}

func (a *AggregateService) collectLocalEndpointOutbounds(host string) ([]map[string]interface{}, error) {
	endpoints, err := a.EndpointService.GetAll()
	if err != nil {
		return nil, err
	}

	outbounds := make([]map[string]interface{}, 0)
	for _, rawEndpoint := range *endpoints {
		endpoint, err := normalizeEndpointAggregateMap(rawEndpoint)
		if err != nil {
			logger.Warning("aggregate: skip invalid endpoint while building node aggregate:", err)
			continue
		}

		switch strings.ToLower(strings.TrimSpace(asString(endpoint["type"]))) {
		case "wireguard":
			outbounds = append(outbounds, buildWireguardAggregateOutbounds(endpoint, host)...)
		case "warp":
			if ob := buildWarpAggregateOutbound(endpoint); ob != nil {
				outbounds = append(outbounds, *ob)
			}
		case "masque":
			if ob := buildMasqueAggregateOutbound(endpoint); ob != nil {
				outbounds = append(outbounds, *ob)
			}
		case "tailscale":
			if ob := buildTailscaleAggregateOutbound(endpoint); ob != nil {
				outbounds = append(outbounds, *ob)
			}
		}
	}

	return outbounds, nil
}

func (a *AggregateService) selfEndpointSourceURI(host string) (string, error) {
	base, err := a.SettingService.GetFinalSubURI(host)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(strings.TrimSpace(base), "/") + "/endpoints", nil
}

func (a *AggregateService) selfEndpointAggregateURI(host string) (string, error) {
	base, err := a.SettingService.GetFinalSubURI(host)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(strings.TrimSpace(base), "/") + "/endpoints/aggregate", nil
}

func endpointOutboundsFromSource(data string) ([]map[string]interface{}, error) {
	data = strings.TrimSpace(data)
	if data == "" {
		return nil, common.NewError("empty endpoint source")
	}

	var jsonOutbounds []map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jsonOutbounds); err == nil {
		return normalizeEndpointSourceOutbounds(jsonOutbounds), nil
	}

	var clashConfig map[string]interface{}
	if err := yaml.Unmarshal([]byte(data), &clashConfig); err != nil {
		return nil, err
	}
	rawProxies, ok := clashConfig["proxies"].([]interface{})
	if !ok || len(rawProxies) == 0 {
		return nil, common.NewError("no proxies in endpoint source")
	}

	outbounds := make([]map[string]interface{}, 0, len(rawProxies))
	for _, rawProxy := range rawProxies {
		proxy := asMap(rawProxy)
		if len(proxy) == 0 {
			continue
		}
		outbounds = append(outbounds, normalizeEndpointSourceOutbound(proxy))
	}
	return outbounds, nil
}

func normalizeEndpointSourceOutbounds(outbounds []map[string]interface{}) []map[string]interface{} {
	normalized := make([]map[string]interface{}, 0, len(outbounds))
	for _, outbound := range outbounds {
		if len(outbound) == 0 {
			continue
		}
		normalized = append(normalized, normalizeEndpointSourceOutbound(outbound))
	}
	return normalized
}

func normalizeEndpointSourceOutbound(outbound map[string]interface{}) map[string]interface{} {
	if _, ok := outbound["tag"]; !ok {
		if name := asString(outbound["name"]); name != "" {
			outbound["tag"] = name
		}
	}
	if _, ok := outbound["server_port"]; !ok {
		if port, exists := outbound["port"]; exists {
			outbound["server_port"] = port
		}
	}
	if outbound["type"] == "warp" {
		outbound["type"] = "wireguard"
	}
	return outbound
}

func endpointNodeTag(node map[string]interface{}) string {
	tag := asString(node["tag"])
	if tag == "" {
		tag = asString(node["name"])
	}
	return tag
}

func normalizeEndpointAggregateMap(raw map[string]interface{}) (map[string]interface{}, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var endpoint map[string]interface{}
	if err := json.Unmarshal(data, &endpoint); err != nil {
		return nil, err
	}
	return endpoint, nil
}

func buildWireguardAggregateOutbounds(endpoint map[string]interface{}, host string) []map[string]interface{} {
	tag := asString(endpoint["tag"])
	serverPort := asInt(endpoint["listen_port"])
	serverKey := asStringFromMap(endpoint["ext"], "public_key")
	dnsServers := asStringSliceFromMap(endpoint["ext"], "dns")
	if len(dnsServers) == 0 {
		dnsServers = []string{"1.1.1.1", "9.9.9.9"}
	}
	if len(strings.TrimSpace(host)) == 0 || serverPort <= 0 || len(serverKey) == 0 {
		return nil
	}

	peers := asSlice(endpoint["peers"])
	keys := asSliceFromMap(endpoint["ext"], "keys")
	nodes := make([]map[string]interface{}, 0, len(peers))
	for index, rawPeer := range peers {
		peer := asMap(rawPeer)
		if len(peer) == 0 {
			continue
		}
		publicKey := asString(peer["public_key"])
		privateKey := findPrivateKeyForPeer(keys, publicKey)
		if len(privateKey) == 0 || len(serverKey) == 0 {
			continue
		}

		addresses := asStringSlice(peer["allowed_ips"])
		ipv4, ipv6 := splitAllowedIPs(addresses)
		name := tag
		if len(peers) > 1 {
			name = fmt.Sprintf("%s-%d", tag, index+1)
		}

		node := map[string]interface{}{
			"type":               "wireguard",
			"tag":                name,
			"server":             host,
			"server_port":        serverPort,
			"private-key":        privateKey,
			"public-key":         serverKey,
			"dns":                dnsServers,
			"remote-dns-resolve": true,
			"udp":                true,
		}
		if len(ipv4) > 0 {
			node["ip"] = ipv4
		}
		if len(ipv6) > 0 {
			node["ipv6"] = ipv6
		}
		if mtu := asInt(endpoint["mtu"]); mtu > 0 {
			node["mtu"] = mtu
		}
		if keepAlive := asInt(peer["persistent_keepalive_interval"]); keepAlive > 0 {
			node["persistent-keepalive"] = keepAlive
		}
		node["pre-shared-key"] = ""
		if psk, ok := normalizeWireguardPreSharedKey(peer["pre_shared_key"]); ok {
			node["pre-shared-key"] = psk
		} else if psk := asString(peer["pre_shared_key"]); psk != "" {
			logger.Warning("aggregate: normalize invalid wireguard pre-shared-key as empty while building endpoint aggregate: ", tag)
		}
		if reserved := asIntSlice(peer["reserved"]); len(reserved) > 0 {
			node["reserved"] = reserved
		}
		nodes = append(nodes, node)
	}
	return nodes
}

func buildWarpAggregateOutbound(endpoint map[string]interface{}) *map[string]interface{} {
	tag := asString(endpoint["tag"])
	privateKey := asString(endpoint["private_key"])
	peers := asSlice(endpoint["peers"])
	if len(peers) == 0 {
		return nil
	}
	peer := asMap(peers[0])
	if len(peer) == 0 {
		return nil
	}

	serverHost, serverPort := splitServerAddress(asString(peer["address"]), asInt(peer["port"]))
	serverKey := asString(peer["public_key"])
	addresses := asStringSlice(endpoint["address"])
	ipv4, ipv6 := splitAllowedIPs(addresses)
	dnsServers := asStringSliceFromMap(endpoint["ext"], "dns")
	if len(dnsServers) == 0 {
		dnsServers = []string{"1.1.1.1", "9.9.9.9"}
	}
	if len(privateKey) == 0 || len(serverHost) == 0 || serverPort <= 0 || len(serverKey) == 0 {
		return nil
	}

	node := map[string]interface{}{
		"type":               "wireguard",
		"tag":                tag,
		"server":             serverHost,
		"server_port":        serverPort,
		"private-key":        privateKey,
		"public-key":         serverKey,
		"dns":                dnsServers,
		"remote-dns-resolve": true,
		"udp":                true,
	}
	if len(ipv4) > 0 {
		node["ip"] = ipv4
	}
	if len(ipv6) > 0 {
		node["ipv6"] = ipv6
	}
	if mtu := asInt(endpoint["mtu"]); mtu > 0 {
		node["mtu"] = mtu
	}
	if keepAlive := asInt(peer["persistent_keepalive_interval"]); keepAlive > 0 {
		node["persistent-keepalive"] = keepAlive
	}
	node["pre-shared-key"] = ""
	if psk, ok := normalizeWireguardPreSharedKey(peer["pre_shared_key"]); ok {
		node["pre-shared-key"] = psk
	} else if psk := asString(peer["pre_shared_key"]); psk != "" {
		logger.Warning("aggregate: normalize invalid wireguard pre-shared-key as empty while building warp aggregate: ", tag)
	}
	if reserved := asIntSlice(peer["reserved"]); len(reserved) > 0 {
		node["reserved"] = reserved
	}
	return &node
}

func buildMasqueAggregateOutbound(endpoint map[string]interface{}) *map[string]interface{} {
	server := asString(endpoint["server"])
	port := asInt(endpoint["port"])
	privateKey := asString(endpoint["private_key"])
	publicKey := asString(endpoint["public_key"])
	sni := normalizeMasqueSNI(server, asString(endpoint["sni"]))
	ip := asString(endpoint["ip"])
	network := normalizeEndpointMasqueNetwork(asString(endpoint["network"]))
	keepAlive := asInt(endpoint["keepalive"])

	if len(server) == 0 || port <= 0 || len(privateKey) == 0 || len(publicKey) == 0 || network != "quic" {
		return nil
	}
	if keepAlive <= 0 {
		keepAlive = 25
	}

	node := map[string]interface{}{
		"type":                  "masque",
		"tag":                   asString(endpoint["tag"]),
		"server":                server,
		"server_port":           port,
		"network":               network,
		"private-key":           privateKey,
		"public-key":            publicKey,
		"proto":                 "bbr",
		"congestion-controller": "bbr",
		"keepalive":             keepAlive,
	}
	if remoteDNSResolve, ok := endpoint["remote_dns_resolve"].(bool); ok && remoteDNSResolve {
		node["remote-dns-resolve"] = true
		node["dns"] = []string{"1.1.1.1", "8.8.8.8"}
	}
	if len(ip) > 0 {
		node["ip"] = ip
	}
	if len(sni) > 0 {
		node["sni"] = sni
	}
	if mtu := asInt(endpoint["mtu"]); mtu > 0 {
		node["mtu"] = mtu
	}
	if udp, ok := endpoint["udp"].(bool); ok {
		node["udp"] = udp
	} else {
		node["udp"] = true
	}
	return &node
}

func normalizeEndpointMasqueNetwork(network string) string {
	network = strings.TrimSpace(network)
	if network == "" {
		return "quic"
	}
	return network
}

func normalizeMasqueSNI(server, sni string) string {
	sni = strings.TrimSpace(sni)
	if sni != "" {
		return sni
	}
	server = strings.TrimSpace(server)
	if server == "" {
		return ""
	}
	if isIPLiteral(server) {
		return ""
	}
	return server
}

func isIPLiteral(host string) bool {
	host = strings.TrimSpace(host)
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")
	return net.ParseIP(host) != nil
}

func buildTailscaleAggregateOutbound(endpoint map[string]interface{}) *map[string]interface{} {
	node := map[string]interface{}{
		"type": "tailscale",
		"tag":  asString(endpoint["tag"]),
	}

	copyEndpointField(node, endpoint, "state_directory", "state-dir")
	copyEndpointField(node, endpoint, "auth_key", "auth-key")
	copyEndpointField(node, endpoint, "control_url", "control-url")
	copyEndpointField(node, endpoint, "hostname", "hostname")
	copyEndpointField(node, endpoint, "exit_node", "exit-node")
	copyEndpointField(node, endpoint, "relay_server_port", "relay-server-port")
	copyEndpointField(node, endpoint, "relay_server_static_endpoints", "relay-server-static-endpoints")
	copyEndpointField(node, endpoint, "system_interface_name", "system-interface-name")
	copyEndpointField(node, endpoint, "udp_timeout", "udp-timeout")
	copyEndpointField(node, endpoint, "routing_mark", "routing-mark")

	if v, ok := endpoint["ephemeral"].(bool); ok {
		node["ephemeral"] = v
	}
	if v, ok := endpoint["accept_routes"].(bool); ok {
		node["accept-routes"] = v
	}
	if v, ok := endpoint["exit_node_allow_lan_access"].(bool); ok {
		node["exit-node-allow-lan-access"] = v
	}
	if v, ok := endpoint["advertise_exit_node"].(bool); ok {
		node["advertise-exit-node"] = v
	}
	if v := asStringSlice(endpoint["advertise_routes"]); len(v) > 0 {
		node["advertise-routes"] = v
	}
	if v, ok := endpoint["system_interface"].(bool); ok {
		node["system-interface"] = v
	}
	if v, ok := endpoint["system_interface_mtu"]; ok {
		node["system-interface-mtu"] = v
	}
	if v, ok := endpoint["ip_version"]; ok {
		node["ip-version"] = v
	}
	if v, ok := endpoint["udp"].(bool); ok {
		node["udp"] = v
	}

	return &node
}

func copyEndpointField(dst map[string]interface{}, src map[string]interface{}, sourceKey string, targetKey string) {
	if value, ok := src[sourceKey]; ok {
		dst[targetKey] = value
	}
}

func splitServerAddress(address string, port int) (string, int) {
	address = strings.TrimSpace(address)
	if address == "" {
		return "", port
	}
	if host, serverPort, err := net.SplitHostPort(address); err == nil {
		if parsed, convErr := strconv.Atoi(serverPort); convErr == nil {
			return host, parsed
		}
		return host, port
	}
	return address, port
}

func splitAllowedIPs(addresses []string) (string, string) {
	var ipv4 string
	var ipv6 string
	for _, addr := range addresses {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		if strings.Contains(addr, ":") {
			if ipv6 == "" {
				ipv6 = addr
			}
			continue
		}
		if ipv4 == "" {
			ipv4 = addr
		}
	}
	return ipv4, ipv6
}

func findPrivateKeyForPeer(keys []interface{}, publicKey string) string {
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return ""
	}
	for _, rawKey := range keys {
		key := asMap(rawKey)
		if len(key) == 0 {
			continue
		}
		if strings.TrimSpace(asString(key["public_key"])) == publicKey {
			return asString(key["private_key"])
		}
	}
	return ""
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

func asInt(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int8:
		return int(v)
	case int16:
		return int(v)
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case json.RawMessage:
		var n float64
		if err := json.Unmarshal(v, &n); err == nil {
			return int(n)
		}
	case []byte:
		var n float64
		if err := json.Unmarshal(v, &n); err == nil {
			return int(n)
		}
	}
	if s := strings.TrimSpace(fmt.Sprint(value)); s != "" && s != "<nil>" {
		if n, err := strconv.Atoi(s); err == nil {
			return n
		}
	}
	return 0
}

func asSlice(value interface{}) []interface{} {
	switch v := value.(type) {
	case []interface{}:
		return v
	case json.RawMessage:
		var arr []interface{}
		if err := json.Unmarshal(v, &arr); err == nil {
			return arr
		}
	case []byte:
		var arr []interface{}
		if err := json.Unmarshal(v, &arr); err == nil {
			return arr
		}
	}
	return nil
}

func asMap(value interface{}) map[string]interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return v
	case json.RawMessage:
		var m map[string]interface{}
		if err := json.Unmarshal(v, &m); err == nil {
			return m
		}
	case []byte:
		var m map[string]interface{}
		if err := json.Unmarshal(v, &m); err == nil {
			return m
		}
	}
	return nil
}

func asStringSlice(value interface{}) []string {
	if s := asString(value); s != "" {
		if strings.Contains(s, ",") {
			parts := strings.Split(s, ",")
			result := make([]string, 0, len(parts))
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					result = append(result, part)
				}
			}
			if len(result) > 0 {
				return result
			}
		}
	}
	arr := asSlice(value)
	if len(arr) == 0 {
		return nil
	}
	result := make([]string, 0, len(arr))
	for _, item := range arr {
		str := asString(item)
		if str != "" {
			result = append(result, str)
		}
	}
	return result
}

func asIntSlice(value interface{}) []int {
	arr := asSlice(value)
	if len(arr) == 0 {
		return nil
	}
	result := make([]int, 0, len(arr))
	for _, item := range arr {
		result = append(result, asInt(item))
	}
	return result
}

func asStringFromMap(value interface{}, key string) string {
	m := asMap(value)
	if len(m) == 0 {
		return ""
	}
	return asString(m[key])
}

func asStringSliceFromMap(value interface{}, key string) []string {
	m := asMap(value)
	if len(m) == 0 {
		return nil
	}
	return asStringSlice(m[key])
}

func asSliceFromMap(value interface{}, key string) []interface{} {
	m := asMap(value)
	if len(m) == 0 {
		return nil
	}
	return asSlice(m[key])
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
