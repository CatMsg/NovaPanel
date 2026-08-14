package service

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/util/common"
	"gorm.io/gorm"
)

type masqueClientCredential struct {
	PrivateKey string `json:"private_key"`
	PublicKey  string `json:"public_key"`
	IP         string `json:"ip"`
}

type masqueClientIdentity struct {
	Name      string
	PublicKey string
	Prefix    netip.Prefix
}

func newMasqueClientCredential() (masqueClientCredential, error) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return masqueClientCredential{}, fmt.Errorf("generate masque client key: %w", err)
	}
	privateDER, err := x509.MarshalECPrivateKey(privateKey)
	if err != nil {
		return masqueClientCredential{}, fmt.Errorf("marshal masque client private key: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		return masqueClientCredential{}, fmt.Errorf("marshal masque client public key: %w", err)
	}
	return masqueClientCredential{
		PrivateKey: base64.StdEncoding.EncodeToString(privateDER),
		PublicKey:  base64.StdEncoding.EncodeToString(publicDER),
	}, nil
}

func decodeMasqueCredentials(raw json.RawMessage) (map[string]masqueClientCredential, map[string]json.RawMessage, error) {
	root := make(map[string]json.RawMessage)
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &root); err != nil {
			return nil, nil, err
		}
	}
	credentials := make(map[string]masqueClientCredential)
	if payload := root["masque"]; len(payload) > 0 && string(payload) != "null" {
		if err := json.Unmarshal(payload, &credentials); err != nil {
			return nil, nil, fmt.Errorf("parse masque client credentials: %w", err)
		}
	}
	return credentials, root, nil
}

func encodeMasqueCredentials(root map[string]json.RawMessage, credentials map[string]masqueClientCredential) (json.RawMessage, error) {
	if root == nil {
		root = make(map[string]json.RawMessage)
	}
	if len(credentials) == 0 {
		delete(root, "masque")
	} else {
		payload, err := json.Marshal(credentials)
		if err != nil {
			return nil, err
		}
		root["masque"] = payload
	}
	return json.MarshalIndent(root, "", "  ")
}

func normalizeMasqueClientConfigs(tx *gorm.DB, clients []*model.Client) error {
	if len(clients) == 0 {
		return nil
	}

	selectedIDs := make(map[uint]struct{})
	clientInboundIDs := make([][]uint, len(clients))
	for index, client := range clients {
		ids, err := decodeClientInboundIDs(client.Inbounds)
		if err != nil {
			return err
		}
		clientInboundIDs[index] = ids
		for _, id := range ids {
			selectedIDs[id] = struct{}{}
		}
	}

	configs := make(map[uint]*masqueInboundConfig)
	if len(selectedIDs) > 0 {
		ids := make([]uint, 0, len(selectedIDs))
		for id := range selectedIDs {
			ids = append(ids, id)
		}
		var inbounds []model.Inbound
		if err := tx.Model(model.Inbound{}).Where("id IN ? AND type = ?", ids, "masque").Find(&inbounds).Error; err != nil {
			return err
		}
		for index := range inbounds {
			config, err := parseMasqueInbound(&inbounds[index])
			if err != nil {
				return err
			}
			configs[inbounds[index].Id] = config
		}
	}

	currentIDs := make(map[uint]struct{})
	for _, client := range clients {
		if client.Id > 0 {
			currentIDs[client.Id] = struct{}{}
		}
	}
	used := make(map[uint]map[netip.Addr]struct{})
	var existing []model.Client
	if err := tx.Model(model.Client{}).Find(&existing).Error; err != nil {
		return err
	}
	for index := range existing {
		if _, replaced := currentIDs[existing[index].Id]; replaced {
			continue
		}
		credentials, _, err := decodeMasqueCredentials(existing[index].Config)
		if err != nil {
			continue
		}
		reserveMasqueCredentialIPs(used, credentials)
	}

	for index, client := range clients {
		credentials, root, err := decodeMasqueCredentials(client.Config)
		if err != nil {
			return fmt.Errorf("parse client %s config: %w", client.Name, err)
		}
		selectedMasque := make(map[uint]struct{})
		for _, inboundID := range clientInboundIDs[index] {
			if _, ok := configs[inboundID]; ok {
				selectedMasque[inboundID] = struct{}{}
			}
		}
		for key := range credentials {
			id, err := strconv.ParseUint(key, 10, 64)
			if err != nil {
				delete(credentials, key)
				continue
			}
			if _, selected := selectedMasque[uint(id)]; !selected {
				delete(credentials, key)
			}
		}

		ids := make([]uint, 0, len(selectedMasque))
		for id := range selectedMasque {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, inboundID := range ids {
			config := configs[inboundID]
			subnet, err := parseMasqueClientSubnet(config.ClientSubnet)
			if err != nil {
				return err
			}
			key := masqueCredentialKey(inboundID)
			credential := credentials[key]
			if strings.TrimSpace(credential.PrivateKey) == "" {
				credential, err = newMasqueClientCredential()
				if err != nil {
					return err
				}
			}
			publicKey, err := masquePublicKeyFromPrivate(credential.PrivateKey)
			if err != nil {
				return fmt.Errorf("invalid masque key for client %s: %w", client.Name, err)
			}
			credential.PublicKey = publicKey
			prefix, prefixOK := validMasqueCredentialPrefix(credential.IP, subnet)
			if prefixOK {
				if _, exists := masqueUsedSet(used, inboundID)[prefix.Addr()]; exists {
					prefixOK = false
				}
			}
			if !prefixOK {
				prefix, err = allocateMasqueClientPrefix(subnet, masqueUsedSet(used, inboundID))
				if err != nil {
					return fmt.Errorf("allocate masque address for client %s: %w", client.Name, err)
				}
			}
			credential.IP = prefix.String()
			masqueUsedSet(used, inboundID)[prefix.Addr()] = struct{}{}
			credentials[key] = credential
		}

		client.Config, err = encodeMasqueCredentials(root, credentials)
		if err != nil {
			return err
		}
	}
	return nil
}

func masqueUsedSet(used map[uint]map[netip.Addr]struct{}, inboundID uint) map[netip.Addr]struct{} {
	if used[inboundID] == nil {
		used[inboundID] = make(map[netip.Addr]struct{})
	}
	return used[inboundID]
}

func reserveMasqueCredentialIPs(used map[uint]map[netip.Addr]struct{}, credentials map[string]masqueClientCredential) {
	for key, credential := range credentials {
		id, err := strconv.ParseUint(key, 10, 64)
		if err != nil {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(credential.IP))
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		masqueUsedSet(used, uint(id))[prefix.Addr()] = struct{}{}
	}
}

func validMasqueCredentialPrefix(raw string, subnet netip.Prefix) (netip.Prefix, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
	if err != nil || prefix.Bits() != 32 || !prefix.Addr().Is4() || !subnet.Contains(prefix.Addr()) {
		return netip.Prefix{}, false
	}
	addr := prefix.Addr()
	if addr == subnet.Addr() || !subnet.Contains(addr.Next()) {
		return netip.Prefix{}, false
	}
	return prefix, true
}

func allocateMasqueClientPrefix(subnet netip.Prefix, used map[netip.Addr]struct{}) (netip.Prefix, error) {
	addr := subnet.Addr().Next()
	if subnet.Contains(addr.Next()) {
		addr = addr.Next()
	}
	for subnet.Contains(addr) && subnet.Contains(addr.Next()) {
		if _, exists := used[addr]; !exists {
			return netip.PrefixFrom(addr, 32), nil
		}
		addr = addr.Next()
	}
	return netip.Prefix{}, common.NewError("masque client subnet has no free address")
}

func loadMasqueClientIdentities(tx *gorm.DB, inbound *model.Inbound) (map[string]masqueClientIdentity, error) {
	if inbound == nil {
		return nil, common.NewError("missing masque inbound")
	}
	config, err := parseMasqueInbound(inbound)
	if err != nil {
		return nil, err
	}
	subnet, err := parseMasqueClientSubnet(config.ClientSubnet)
	if err != nil {
		return nil, err
	}
	var clients []model.Client
	if err := tx.Model(model.Client{}).
		Where("enable = true AND ? IN (SELECT json_each.value FROM json_each(clients.inbounds))", inbound.Id).
		Find(&clients).Error; err != nil {
		return nil, err
	}
	now := time.Now().Unix()
	identities := make(map[string]masqueClientIdentity, len(clients))
	for index := range clients {
		client := &clients[index]
		if (client.Volume > 0 && client.Up+client.Down >= client.Volume) || (client.Expiry > 0 && client.Expiry < now) {
			continue
		}
		credentials, _, err := decodeMasqueCredentials(client.Config)
		if err != nil {
			return nil, err
		}
		credential, ok := credentials[masqueCredentialKey(inbound.Id)]
		if !ok {
			continue
		}
		prefix, ok := validMasqueCredentialPrefix(credential.IP, subnet)
		if !ok {
			return nil, fmt.Errorf("invalid masque address for client %s", client.Name)
		}
		publicKey, err := masquePublicKeyFromPrivate(credential.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("invalid masque key for client %s: %w", client.Name, err)
		}
		if credential.PublicKey != "" && credential.PublicKey != publicKey {
			return nil, fmt.Errorf("masque key mismatch for client %s", client.Name)
		}
		if _, exists := identities[publicKey]; exists {
			return nil, fmt.Errorf("duplicate masque client public key for %s", client.Name)
		}
		identities[publicKey] = masqueClientIdentity{Name: client.Name, PublicKey: publicKey, Prefix: prefix}
	}
	return identities, nil
}
