package sub

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
)

var masqueProxyDNS = []string{"1.1.1.1", "8.8.8.8"}

type masqueEndpointOptions struct {
	Server     string `json:"server"`
	Port       int    `json:"port"`
	Network    string `json:"network"`
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	IP         string `json:"ip"`
	MTU        int    `json:"mtu"`
	UDP        *bool  `json:"udp"`
}

func loadMasqueOutbounds(tags []string) []map[string]interface{} {
	if len(tags) == 0 {
		return nil
	}

	db := database.GetDB()
	endpoints := []model.Endpoint{}
	if err := db.Model(model.Endpoint{}).Where("type = ? AND tag IN ?", "masque", tags).Scan(&endpoints).Error; err != nil {
		logger.Warning("load masque endpoints for subscription failed:", err)
		return nil
	}

	byTag := make(map[string]model.Endpoint, len(endpoints))
	for _, endpoint := range endpoints {
		byTag[endpoint.Tag] = endpoint
	}

	result := make([]map[string]interface{}, 0, len(tags))
	for _, tag := range tags {
		endpoint, ok := byTag[tag]
		if !ok {
			continue
		}
		outbound, err := buildMasqueOutbound(&endpoint)
		if err != nil {
			logger.Warning("skip invalid masque endpoint in subscription: ", tag, " err=", err)
			continue
		}
		result = append(result, outbound)
	}
	return result
}

func buildMasqueOutbound(endpoint *model.Endpoint) (map[string]interface{}, error) {
	options := masqueEndpointOptions{
		Network: "quic",
		Port:    443,
		MTU:     1280,
	}
	if err := json.Unmarshal(endpoint.Options, &options); err != nil {
		return nil, err
	}

	if options.Port <= 0 {
		options.Port = 443
	}
	if options.MTU <= 0 {
		options.MTU = 1280
	}
	if strings.TrimSpace(options.Network) == "" {
		options.Network = "quic"
	}

	outbound := map[string]interface{}{
		"type":               "masque",
		"tag":                endpoint.Tag,
		"server":             strings.TrimSpace(options.Server),
		"server_port":        options.Port,
		"network":            options.Network,
		"private_key":        options.PrivateKey,
		"public_key":         options.PublicKey,
		"remote_dns_resolve": true,
		"dns":                masqueProxyDNS,
	}

	if ip := strings.TrimSpace(options.IP); ip != "" {
		outbound["ip"] = ip
	}
	if options.MTU > 0 {
		outbound["mtu"] = options.MTU
	}
	if options.UDP != nil {
		outbound["udp"] = *options.UDP
	}
	return outbound, nil
}

func appendUniqueOutbounds(dst *[]map[string]interface{}, tags *[]string, seen map[string]struct{}, src []map[string]interface{}) {
	for _, outbound := range src {
		tag, _ := outbound["tag"].(string)
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		*dst = append(*dst, outbound)
		*tags = append(*tags, tag)
	}
}

func appendUniqueLinks(dst []string, seen map[string]struct{}, src []string) []string {
	for _, link := range src {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		if _, ok := seen[link]; ok {
			continue
		}
		seen[link] = struct{}{}
		dst = append(dst, link)
	}
	return dst
}

func normalizePort(value interface{}) int {
	switch port := value.(type) {
	case int:
		return port
	case int64:
		return int(port)
	case float64:
		return int(port)
	case json.Number:
		v, _ := port.Int64()
		return int(v)
	case string:
		v, _ := strconv.Atoi(port)
		return v
	default:
		return 0
	}
}
