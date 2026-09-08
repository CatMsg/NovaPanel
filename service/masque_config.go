package service

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"
	"gorm.io/gorm"
)

type masqueInboundConfig struct {
	ID               uint
	Tag              string
	Host             string
	Port             int
	Network          string
	PrivateKey       string
	PublicKey        string
	ClientSubnet     string
	MTU              int
	KeepAlive        int
	SNI              string
	RemoteDNSResolve bool
	UDP              bool
}

func parseMasqueInbound(inbound *model.Inbound) (*masqueInboundConfig, error) {
	var payload struct {
		Server           string `json:"server"`
		ListenPort       int    `json:"listen_port"`
		Network          string `json:"network"`
		PrivateKey       string `json:"private_key"`
		PublicKey        string `json:"public_key"`
		ClientSubnet     string `json:"client_subnet"`
		MTU              int    `json:"mtu"`
		KeepAlive        int    `json:"keepalive"`
		SNI              string `json:"sni"`
		RemoteDNSResolve bool   `json:"remote_dns_resolve"`
		UDP              *bool  `json:"udp"`
	}
	if inbound == nil {
		return nil, common.NewError("missing masque inbound")
	}
	if inbound.Options != nil {
		if err := json.Unmarshal(inbound.Options, &payload); err != nil {
			return nil, err
		}
	}
	udp := true
	if payload.UDP != nil {
		udp = *payload.UDP
	}

	config := &masqueInboundConfig{
		ID:               inbound.Id,
		Tag:              strings.TrimSpace(inbound.Tag),
		Host:             strings.TrimSpace(payload.Server),
		Port:             payload.ListenPort,
		Network:          normalizeMasqueNetwork(payload.Network),
		PrivateKey:       strings.TrimSpace(payload.PrivateKey),
		PublicKey:        strings.TrimSpace(payload.PublicKey),
		ClientSubnet:     strings.TrimSpace(payload.ClientSubnet),
		MTU:              payload.MTU,
		KeepAlive:        payload.KeepAlive,
		SNI:              strings.TrimSpace(payload.SNI),
		RemoteDNSResolve: payload.RemoteDNSResolve,
		UDP:              udp,
	}
	if config.MTU <= 0 {
		config.MTU = 1380
	}
	if config.KeepAlive <= 0 {
		config.KeepAlive = 25
	}
	if err := validateMasqueInboundConfig(config); err != nil {
		return nil, err
	}
	return config, nil
}

func normalizeMasqueNetwork(network string) string {
	network = strings.TrimSpace(network)
	if network == "" {
		return "quic"
	}
	return network
}

func validateMasqueNetwork(network string) error {
	network = normalizeMasqueNetwork(network)
	if network != "quic" {
		return fmt.Errorf("unsupported masque network %q: only quic is currently available", network)
	}
	return nil
}

func validateMasqueInboundConfig(config *masqueInboundConfig) error {
	if config == nil {
		return common.NewError("missing masque config")
	}
	if config.Tag == "" {
		return common.NewError("masque tag is required")
	}
	if config.Host == "" {
		return common.NewError("masque server host is required")
	}
	if config.Port < 1 || config.Port > 65535 {
		return fmt.Errorf("invalid masque port: %d", config.Port)
	}
	if err := validateMasqueNetwork(config.Network); err != nil {
		return err
	}
	if _, err := parseMasqueClientSubnet(config.ClientSubnet); err != nil {
		return err
	}
	if _, err := parseMasquePrivateKey(config.PrivateKey); err != nil {
		return fmt.Errorf("invalid masque server private key: %w", err)
	}
	derivedPublicKey, err := masquePublicKeyFromPrivate(config.PrivateKey)
	if err != nil {
		return fmt.Errorf("derive masque server public key: %w", err)
	}
	if config.PublicKey == "" {
		config.PublicKey = derivedPublicKey
	} else if config.PublicKey != derivedPublicKey {
		return common.NewError("masque server public key does not match private key")
	}
	if config.MTU < 576 || config.MTU > 9000 {
		return fmt.Errorf("invalid masque MTU: %d", config.MTU)
	}
	if config.KeepAlive < 1 || config.KeepAlive > 300 {
		return fmt.Errorf("invalid masque keepalive: %d", config.KeepAlive)
	}
	return nil
}

func prepareMasqueInbound(tx *gorm.DB, inbound, oldInbound *model.Inbound, hostname string) error {
	if inbound == nil {
		return common.NewError("missing masque inbound")
	}
	options := make(map[string]interface{})
	if len(inbound.Options) > 0 {
		if err := json.Unmarshal(inbound.Options, &options); err != nil {
			return err
		}
	}
	server, _ := options["server"].(string)
	if strings.TrimSpace(server) == "" {
		if oldInbound != nil && oldInbound.Type == "masque" {
			if oldConfig, parseErr := parseMasqueInbound(oldInbound); parseErr == nil {
				server = oldConfig.Host
			}
		}
		if strings.TrimSpace(server) == "" {
			server = hostname
		}
		options["server"] = strings.TrimSpace(server)
	}
	options["network"] = "quic"
	options["listen"] = "::"
	if value, ok := options["mtu"].(float64); !ok || value <= 0 {
		options["mtu"] = 1380
	}
	if value, ok := options["keepalive"].(float64); !ok || value <= 0 {
		options["keepalive"] = 25
	}
	if _, ok := options["udp"]; !ok {
		options["udp"] = true
	}

	privateKey, _ := options["private_key"].(string)
	privateKey = strings.TrimSpace(privateKey)
	if privateKey == "" && oldInbound != nil && oldInbound.Type == "masque" {
		oldConfig, err := parseMasqueInbound(oldInbound)
		if err == nil {
			privateKey = oldConfig.PrivateKey
		}
	}
	if privateKey == "" {
		credential, err := newMasqueClientCredential()
		if err != nil {
			return err
		}
		privateKey = credential.PrivateKey
	}
	publicKey, err := masquePublicKeyFromPrivate(privateKey)
	if err != nil {
		return fmt.Errorf("invalid masque server key: %w", err)
	}
	options["private_key"] = privateKey
	options["public_key"] = publicKey

	subnetRaw, _ := options["client_subnet"].(string)
	subnetRaw = strings.TrimSpace(subnetRaw)
	if subnetRaw == "" {
		if oldInbound != nil && oldInbound.Type == "masque" {
			if oldConfig, parseErr := parseMasqueInbound(oldInbound); parseErr == nil {
				subnetRaw = oldConfig.ClientSubnet
				options["client_subnet"] = subnetRaw
			}
		}
	}
	if subnetRaw == "" {
		existing, err := loadMasqueSubnetsTx(tx)
		if err != nil {
			return err
		}
		subnet, err := allocateMasqueSubnet(existing)
		if err != nil {
			return err
		}
		options["client_subnet"] = subnet.String()
	}

	inbound.Options, err = json.MarshalIndent(options, "", "  ")
	if err != nil {
		return err
	}
	config, err := parseMasqueInbound(inbound)
	if err != nil {
		return err
	}
	candidate, err := parseMasqueClientSubnet(config.ClientSubnet)
	if err != nil {
		return err
	}
	var others []model.Inbound
	if err := tx.Where("type = ? AND id <> ?", "masque", inbound.Id).Find(&others).Error; err != nil {
		return err
	}
	for index := range others {
		other, err := parseMasqueInbound(&others[index])
		if err != nil {
			continue
		}
		prefix, err := parseMasqueClientSubnet(other.ClientSubnet)
		if err == nil && masqueSubnetOverlaps(candidate, []netip.Prefix{prefix}) {
			return fmt.Errorf("MASQUE 客户端地址池 %s 与入站 %s 冲突", candidate, others[index].Tag)
		}
	}
	return nil
}

func masqueTemplateDescription(config *masqueInboundConfig) string {
	return "https://cloudflareaccess.com"
}

func parseMasqueClientSubnet(raw string) (netip.Prefix, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Prefix{}, common.NewError("masque client subnet is required")
	}
	if !strings.Contains(raw, "/") {
		raw += "/24"
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("invalid masque client subnet: %w", err)
	}
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, common.NewError("masque client subnet must be IPv4")
	}
	if prefix.Bits() < 16 || prefix.Bits() > 30 {
		return netip.Prefix{}, common.NewError("masque client subnet prefix must be between /16 and /30")
	}
	return prefix.Masked(), nil
}

func loadMasqueSubnetsTx(tx *gorm.DB) ([]netip.Prefix, error) {
	var inbounds []model.Inbound
	if err := tx.Where("type = ?", "masque").Find(&inbounds).Error; err != nil {
		return nil, err
	}
	result := make([]netip.Prefix, 0, len(inbounds))
	for index := range inbounds {
		config, err := parseMasqueInbound(&inbounds[index])
		if err != nil {
			continue
		}
		prefix, err := parseMasqueClientSubnet(config.ClientSubnet)
		if err == nil {
			result = append(result, prefix)
		}
	}
	return result, nil
}

func masqueSubnetOverlaps(candidate netip.Prefix, existing []netip.Prefix) bool {
	for _, prefix := range existing {
		if candidate.Contains(prefix.Addr()) || prefix.Contains(candidate.Addr()) {
			return true
		}
	}
	return false
}

func allocateMasqueSubnet(existing []netip.Prefix) (netip.Prefix, error) {
	for second := 16; second <= 31; second++ {
		for third := 0; third <= 255; third++ {
			prefix := netip.PrefixFrom(netip.AddrFrom4([4]byte{172, byte(second), byte(third), 0}), 24)
			if !masqueSubnetOverlaps(prefix, existing) {
				return prefix, nil
			}
		}
	}
	return netip.Prefix{}, fmt.Errorf("no free private /24 subnet is available for MASQUE")
}

func masqueCredentialKey(inboundID uint) string {
	return strconv.FormatUint(uint64(inboundID), 10)
}
