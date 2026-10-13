package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"

	"github.com/CatMsg/NovaPanel/core"
	"github.com/CatMsg/NovaPanel/database/model"
	transport "github.com/sagernet/sing-box/transport/masque"
	"gorm.io/gorm"
)

type masqueMihomoCredential struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	IP         string `json:"ip"`
}

func masqueMihomoEnabled(inbound *model.Inbound) bool {
	var options struct {
		Enabled bool `json:"mihomo_compatible"`
	}
	return json.Unmarshal(inbound.Options, &options) == nil && options.Enabled
}

func masqueServerPublicKey(inbound *model.Inbound) (string, error) {
	if inbound.Tls == nil {
		return "", errors.New("compatibility with Mihomo requires an ECDSA TLS template")
	}
	var fields struct {
		Certificate          []string `json:"certificate"`
		CertificatePath      string   `json:"certificate_path"`
		ClientAuthentication string   `json:"client_authentication"`
	}
	if err := json.Unmarshal(inbound.Tls.Server, &fields); err != nil {
		return "", err
	}
	if fields.ClientAuthentication != "" && fields.ClientAuthentication != "no" {
		return "", errors.New("compatibility with Mihomo conflicts with this TLS client-authentication policy")
	}
	content := []byte(strings.Join(fields.Certificate, "\n"))
	if len(content) == 0 && fields.CertificatePath != "" {
		var err error
		content, err = os.ReadFile(fields.CertificatePath)
		if err != nil {
			return "", errors.New("cannot read Mihomo TLS certificate")
		}
	}
	block, _ := pem.Decode(content)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", errors.New("compatibility with Mihomo requires a readable ECDSA TLS certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", errors.New("invalid Mihomo TLS certificate")
	}
	if _, ok := certificate.PublicKey.(*ecdsa.PublicKey); !ok {
		return "", errors.New("compatibility with Mihomo MASQUE requires an ECDSA TLS certificate, not RSA")
	}
	der, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(der), nil
}

// Read the live certificate for subscriptions as ACME can replace it without
// saving the TLS template again. Never return a server private key.
func MasqueMihomoPublicKey(inbound *model.Inbound) (string, error) {
	return masqueServerPublicKey(inbound)
}

func normalizeMasqueMihomo(tx *gorm.DB, clients []*model.Client) error {
	if len(clients) == 0 {
		return nil
	}
	var inbounds []*model.Inbound
	if err := tx.Where("type = ?", "masque").Find(&inbounds).Error; err != nil {
		return err
	}
	compatible := make(map[uint]*model.Inbound)
	for _, inbound := range inbounds {
		if masqueMihomoEnabled(inbound) {
			compatible[inbound.Id] = inbound
		}
	}
	if len(compatible) == 0 {
		return nil
	}
	replaced := make(map[uint]bool)
	for _, client := range clients {
		if client.Id != 0 {
			replaced[client.Id] = true
		}
	}
	reserved := make(map[string]map[netip.Addr]bool)
	reserve := func(id string, value masqueMihomoCredential) error {
		prefix, err := netip.ParsePrefix(value.IP)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
			return errors.New("invalid stored Mihomo client address")
		}
		if reserved[id] == nil {
			reserved[id] = make(map[netip.Addr]bool)
		}
		if reserved[id][prefix.Addr()] {
			return errors.New("duplicate Mihomo client address")
		}
		reserved[id][prefix.Addr()] = true
		return nil
	}
	var existing []model.Client
	if err := tx.Find(&existing).Error; err != nil {
		return err
	}
	for _, client := range existing {
		if replaced[client.Id] {
			continue
		}
		credential, _, err := decodeMasqueCredential(client.Config)
		if err != nil {
			continue
		}
		bound, err := decodeClientInboundIDs(client.Inbounds)
		if err != nil {
			return err
		}
		for id, value := range credential.Mihomo {
			inboundID, _ := strconv.ParseUint(id, 10, 64)
			assigned := false
			for _, boundID := range bound {
				assigned = assigned || boundID == uint(inboundID)
			}
			if assigned && compatible[uint(inboundID)] != nil {
				if err := reserve(id, value); err != nil {
					return err
				}
			}
		}
	}
	for _, client := range clients {
		credential, _, err := decodeMasqueCredential(client.Config)
		if err != nil {
			return err
		}
		ids, err := decodeClientInboundIDs(client.Inbounds)
		if err != nil {
			return err
		}
		for _, id := range ids {
			key := strconv.FormatUint(uint64(id), 10)
			if value, exists := credential.Mihomo[key]; exists && compatible[id] != nil && value.IP != "" {
				if err := reserve(key, value); err != nil {
					return err
				}
			}
		}
	}
	for _, client := range clients {
		ids, err := decodeClientInboundIDs(client.Inbounds)
		if err != nil {
			return err
		}
		credential, root, err := decodeMasqueCredential(client.Config)
		if err != nil {
			return err
		}
		if credential.Mihomo == nil {
			credential.Mihomo = make(map[string]masqueMihomoCredential)
		}
		touched := false
		for key := range credential.Mihomo {
			id, _ := strconv.ParseUint(key, 10, 64)
			assigned := false
			for _, bound := range ids {
				assigned = assigned || bound == uint(id)
			}
			if !assigned {
				delete(credential.Mihomo, key)
				touched = true
			}
		}
		for _, id := range ids {
			inbound := compatible[id]
			if inbound == nil {
				continue
			}
			touched = true
			key := strconv.FormatUint(uint64(id), 10)
			value := credential.Mihomo[key]
			hadAddress := value.IP != ""
			if value.PrivateKey == "" {
				privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if err != nil {
					return err
				}
				privateDER, err := x509.MarshalECPrivateKey(privateKey)
				if err != nil {
					return err
				}
				publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
				if err != nil {
					return err
				}
				value.PrivateKey, value.PublicKey = base64.StdEncoding.EncodeToString(privateDER), base64.StdEncoding.EncodeToString(publicDER)
			}
			privateDER, err := base64.StdEncoding.DecodeString(value.PrivateKey)
			if err != nil {
				return errors.New("invalid Mihomo client private key")
			}
			privateKey, err := x509.ParseECPrivateKey(privateDER)
			if err != nil {
				return errors.New("invalid Mihomo client private key")
			}
			publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
			if err != nil || value.PublicKey != base64.StdEncoding.EncodeToString(publicDER) {
				return errors.New("client key pair for Mihomo does not match")
			}
			var options struct {
				Address []netip.Prefix `json:"address"`
			}
			if err := json.Unmarshal(inbound.Options, &options); err != nil || len(options.Address) != 1 {
				return errors.New("invalid Mihomo address pool")
			}
			pool := options.Address[0]
			if value.IP == "" {
				for candidate := pool.Masked().Addr().Next(); pool.Contains(candidate) && pool.Contains(candidate.Next()); candidate = candidate.Next() {
					if candidate != pool.Addr() && !reserved[key][candidate] {
						value.IP = netip.PrefixFrom(candidate, 32).String()
						break
					}
				}
			}
			prefix, err := netip.ParsePrefix(value.IP)
			if err != nil || !pool.Contains(prefix.Addr()) || pool.Addr() == prefix.Addr() || pool.Masked().Addr() == prefix.Addr() || !pool.Contains(prefix.Addr().Next()) {
				return errors.New("client pool for Mihomo exhausted or stored address is invalid")
			}
			if !hadAddress {
				if err := reserve(key, value); err != nil {
					return err
				}
			}
			credential.Mihomo[key] = value
		}
		if !touched {
			continue
		}
		root["masque"], err = json.Marshal(credential)
		if err != nil {
			return err
		}
		client.Config, err = json.MarshalIndent(root, "", "  ")
		if err != nil {
			return err
		}
	}
	return nil
}

func loadMasqueMihomoUsers(tx *gorm.DB, inbound *model.Inbound, enabled map[string]bool) ([]core.MihomoMASQUEUser, error) {
	var clients []model.Client
	if err := tx.Where("enable = true AND ? IN (SELECT json_each.value FROM json_each(clients.inbounds))", inbound.Id).Order("id").Find(&clients).Error; err != nil {
		return nil, err
	}
	var result []core.MihomoMASQUEUser
	for _, client := range clients {
		if !enabled[client.Name] {
			continue
		}
		credential, _, err := decodeMasqueCredential(client.Config)
		if err != nil {
			return nil, err
		}
		value := credential.Mihomo[strconv.FormatUint(uint64(inbound.Id), 10)]
		if value.PublicKey == "" {
			return nil, fmt.Errorf("credentials for Mihomo missing for user %s", client.Name)
		}
		prefix, err := netip.ParsePrefix(value.IP)
		if err != nil || value.PublicKey == "" {
			return nil, fmt.Errorf("credentials for Mihomo missing for user %s", client.Name)
		}
		result = append(result, core.MihomoMASQUEUser{Username: client.Name, PublicKey: value.PublicKey, Address: prefix.Addr()})
	}
	return result, nil
}

func masqueMihomoTemplate(inbound *model.Inbound) (map[string]any, error) {
	publicKey, err := masqueServerPublicKey(inbound)
	if err != nil {
		return nil, err
	}
	config, err := parseMasqueInbound(inbound)
	if err != nil {
		return nil, err
	}
	var fields struct {
		Path string `json:"path"`
		MTU  int    `json:"mtu"`
	}
	if err := json.Unmarshal(inbound.Options, &fields); err != nil {
		return nil, err
	}
	template, err := transport.ParseTemplate(fields.Path)
	if err != nil {
		return nil, err
	}
	return map[string]any{"public-key": publicKey, "uri": "https://" + net.JoinHostPort(strings.Trim(config.Host, "[]"), strconv.Itoa(config.Port)) + template.Expand(), "network": "quic", "mtu": fields.MTU, "udp": true}, nil
}
