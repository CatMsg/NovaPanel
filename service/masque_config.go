package service

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util"
	"gorm.io/gorm"
)

type masqueInboundConfig struct {
	Host    string
	Port    int
	Address netip.Prefix
}

func parseMasqueInbound(inbound *model.Inbound) (*masqueInboundConfig, error) {
	if inbound == nil {
		return nil, fmt.Errorf("missing MASQUE inbound")
	}
	var options struct {
		Server  string   `json:"server"`
		Port    int      `json:"listen_port"`
		Address []string `json:"address"`
		Version []int    `json:"version"`
		System  bool     `json:"system"`
		MTU     int      `json:"mtu"`
	}
	if err := json.Unmarshal(inbound.Options, &options); err != nil {
		return nil, err
	}
	if options.System || len(options.Version) != 1 || options.Version[0] != 3 {
		return nil, fmt.Errorf("MASQUE requires system:false and HTTP/3 only")
	}
	if options.Port < 1 || options.Port > 65535 || strings.TrimSpace(options.Server) == "" {
		return nil, fmt.Errorf("MASQUE server and valid listen_port are required")
	}
	if options.MTU < 1280 || options.MTU > 9000 {
		return nil, fmt.Errorf("MASQUE MTU must be between 1280 and 9000")
	}
	if len(options.Address) != 1 {
		return nil, fmt.Errorf("MASQUE requires one IPv4 server address/prefix")
	}
	address, err := netip.ParsePrefix(options.Address[0])
	if err != nil || !address.Addr().Is4() || address.Bits() < 16 || address.Bits() > 30 || address.Addr() == address.Masked().Addr() {
		return nil, fmt.Errorf("MASQUE address must be a server IPv4 address in a /16 through /30 pool (for example 172.16.0.1/24)")
	}
	return &masqueInboundConfig{Host: strings.TrimSpace(options.Server), Port: options.Port, Address: address}, nil
}

func prepareMasqueInbound(tx *gorm.DB, inbound, oldInbound *model.Inbound, hostname string) error {
	if oldInbound != nil && oldInbound.Type == "masque" {
		inbound.MasqueTLSCertificate = oldInbound.MasqueTLSCertificate
		inbound.MasqueTLSKey = oldInbound.MasqueTLSKey
	}
	// MASQUE uses its own generated ECDSA identity, not a shared TLS template.
	inbound.TlsId = 0
	inbound.Tls = nil
	var options map[string]any
	if err := json.Unmarshal(inbound.Options, &options); err != nil {
		return err
	}
	if options == nil {
		options = make(map[string]any)
	}
	for _, key := range []string{"private_key", "public_key", "client_subnet", "keepalive", "remote_dns_resolve", "network", "udp", "sni"} {
		if _, present := options[key]; present {
			return fmt.Errorf("legacy MASQUE option %s is not supported; use native server options", key)
		}
	}
	if host, _ := options["server"].(string); strings.TrimSpace(host) == "" {
		options["server"] = hostname
	}
	if _, present := options["system"]; !present {
		options["system"] = false
	}
	if _, present := options["version"]; !present {
		options["version"] = []int{3}
	}
	if _, present := options["mtu"]; !present {
		options["mtu"] = 1280
	}
	if _, present := options["listen"]; !present {
		options["listen"] = "::"
	}
	if _, present := options["address"]; !present {
		var others []model.Inbound
		if err := tx.Where("type = ? AND id <> ?", "masque", inbound.Id).Find(&others).Error; err != nil {
			return err
		}
		for second := 16; second < 32; second++ {
			candidate := netip.MustParsePrefix(fmt.Sprintf("172.%d.0.1/24", second))
			conflict := false
			for _, other := range others {
				var fields struct {
					Address []netip.Prefix `json:"address"`
				}
				if err := json.Unmarshal(other.Options, &fields); err != nil {
					return err
				}
				for _, address := range fields.Address {
					conflict = conflict || candidate.Overlaps(address)
				}
			}
			if !conflict {
				options["address"] = []string{candidate.String()}
				break
			}
		}
	}
	var err error
	inbound.Options, err = json.MarshalIndent(options, "", "  ")
	if err != nil {
		return err
	}
	config, err := parseMasqueInbound(inbound)
	if err != nil {
		return err
	}
	if _, err := ensureMasqueTLSMaterial(inbound, config.Host); err != nil {
		return err
	}
	if masqueMihomoEnabled(inbound) {
		if _, err := masqueServerPublicKey(inbound); err != nil {
			return err
		}
	}
	var others []model.Inbound
	if err := tx.Where("type = ? AND id <> ?", "masque", inbound.Id).Find(&others).Error; err != nil {
		return err
	}
	for _, other := range others {
		var fields struct {
			Address []netip.Prefix `json:"address"`
		}
		if err := json.Unmarshal(other.Options, &fields); err != nil {
			return err
		}
		for _, address := range fields.Address {
			if config.Address.Overlaps(address) {
				return fmt.Errorf("MASQUE address pool overlaps inbound %s", other.Tag)
			}
		}
	}
	return nil
}

func buildMasqueEndpointConfig(tx *gorm.DB, inbound *model.Inbound) (json.RawMessage, error) {
	users, err := loadMasqueUsers(tx, inbound)
	if err != nil {
		return nil, err
	}
	// Upstream users:[] disables authentication. Never instantiate that config.
	if len(users) == 0 {
		return nil, nil
	}
	config, err := parseMasqueInbound(inbound)
	if err != nil {
		return nil, err
	}
	raw, err := inbound.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var options map[string]any
	if err := json.Unmarshal(raw, &options); err != nil {
		return nil, err
	}
	options["type"], options["users"] = "masque-server", users
	delete(options, "server")
	delete(options, "mihomo_compatible")
	tlsOptions, err := masqueServerTLSOptions(inbound, config.Host)
	if err != nil {
		return nil, err
	}
	options["tls"] = tlsOptions
	if masqueMihomoEnabled(inbound) {
		enabled := make(map[string]bool)
		for _, user := range users {
			enabled[user.Username] = true
		}
		compatible, err := loadMasqueMihomoUsers(tx, inbound, enabled)
		if err != nil {
			return nil, err
		}
		options["mihomo_users"] = compatible
	}
	return json.Marshal(options)
}

func buildMasqueClientTemplate(inbound *model.Inbound) (json.RawMessage, error) {
	config, err := parseMasqueInbound(inbound)
	if err != nil {
		return nil, err
	}
	tlsOptions, err := masqueServerTLSOptions(inbound, config.Host)
	if err != nil {
		return nil, err
	}
	delete(tlsOptions, "key")
	copy := *inbound
	copy.OutJson = json.RawMessage(`{}`)
	copy.TlsId = 1
	copy.Tls = &model.Tls{Server: mustJSONRaw(tlsOptions), Client: json.RawMessage(`{}`)}
	if err := util.FillOutJson(&copy, config.Host); err != nil {
		return nil, err
	}
	if masqueMihomoEnabled(inbound) {
		compatible, err := masqueMihomoTemplate(inbound)
		if err != nil {
			return nil, err
		}
		var fields map[string]any
		if err := json.Unmarshal(copy.OutJson, &fields); err != nil {
			return nil, err
		}
		fields["nova_mihomo"] = compatible
		return json.Marshal(fields)
	}
	return copy.OutJson, nil
}

func mustJSONRaw(value any) json.RawMessage {
	raw, _ := json.Marshal(value)
	return raw
}
