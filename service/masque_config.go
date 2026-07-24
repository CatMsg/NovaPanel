package service

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"
)

type masqueEndpointConfig struct {
	Host       string
	Port       int
	Network    string
	PrivateKey string
	IP         string
	MTU        int
	KeepAlive  int
}

func parseMasqueEndpoint(endpoint *model.Endpoint) (*masqueEndpointConfig, error) {
	var payload struct {
		Server     string `json:"server"`
		Port       int    `json:"port"`
		Network    string `json:"network"`
		PrivateKey string `json:"private_key"`
		IP         string `json:"ip"`
		MTU        int    `json:"mtu"`
		KeepAlive  int    `json:"keepalive"`
	}
	if endpoint != nil && endpoint.Options != nil {
		if err := json.Unmarshal(endpoint.Options, &payload); err != nil {
			return nil, err
		}
	}

	return &masqueEndpointConfig{
		Host:       strings.TrimSpace(payload.Server),
		Port:       payload.Port,
		Network:    normalizeMasqueNetwork(payload.Network),
		PrivateKey: strings.TrimSpace(payload.PrivateKey),
		IP:         strings.TrimSpace(payload.IP),
		MTU:        payload.MTU,
		KeepAlive:  payload.KeepAlive,
	}, nil
}

func normalizeMasqueNetwork(network string) string {
	network = strings.TrimSpace(network)
	if network == "" {
		return "quic"
	}
	return network
}

func masqueTemplateDescription(config *masqueEndpointConfig) string {
	return "https://cloudflareaccess.com"
}

func parseMasquePeerPrefix(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Prefix{}, common.NewError("masque client ip is required")
	}
	if !strings.Contains(raw, "/") {
		raw += "/32"
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid masque client ip: %w", err)
	}
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, common.NewError("masque client ip must be IPv4")
	}
	return prefix.Masked(), nil
}
