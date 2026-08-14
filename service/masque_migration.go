package service

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util"
	"gorm.io/gorm"
)

// MigrateLegacyMasqueEndpoints moves the old single-client endpoint model into
// the multi-user inbound model. It is idempotent because migrated endpoints are
// removed in the same transaction that creates the inbound.
func MigrateLegacyMasqueEndpoints() error {
	db := database.GetDB()
	if db == nil {
		return nil
	}
	var count int64
	if err := db.Model(&model.Endpoint{}).Where("type = ?", "masque").Count(&count).Error; err != nil || count == 0 {
		return err
	}

	migrated := 0
	err := retryWriteTx(func(tx *gorm.DB) error {
		var endpoints []model.Endpoint
		if err := tx.Where("type = ?", "masque").Find(&endpoints).Error; err != nil {
			return err
		}
		usedSubnets, err := loadMasqueSubnetsTx(tx)
		if err != nil {
			return err
		}
		clientIDs, err := allClientIDsTx(tx)
		if err != nil {
			return err
		}
		clientIDList := joinUintIDs(clientIDs)

		for index := range endpoints {
			endpoint := &endpoints[index]
			var existing model.Inbound
			if err := tx.Where("tag = ? AND type = ?", endpoint.Tag, "masque").First(&existing).Error; err == nil {
				if err := deleteManagedPortEntriesForEndpointTx(tx, endpoint.Id); err != nil {
					return err
				}
				if err := tx.Delete(endpoint).Error; err != nil {
					return err
				}
				migrated++
				continue
			} else if err != gorm.ErrRecordNotFound {
				return err
			}

			legacy, err := parseMasqueEndpoint(endpoint)
			if err != nil {
				return fmt.Errorf("parse legacy MASQUE endpoint %s: %w", endpoint.Tag, err)
			}
			if _, err := parseMasquePrivateKey(legacy.PrivateKey); err != nil {
				keys, keyErr := newMasqueClientCredential()
				if keyErr != nil {
					return keyErr
				}
				legacy.PrivateKey = keys.PrivateKey
				legacy.PublicKey = keys.PublicKey
			} else {
				legacy.PublicKey, err = masquePublicKeyFromPrivate(legacy.PrivateKey)
				if err != nil {
					return err
				}
			}
			subnet, err := parseMasqueClientSubnet(legacy.ClientSubnet)
			if err != nil || masqueSubnetOverlaps(subnet, usedSubnets) {
				subnet, err = allocateMasqueMigrationSubnet(usedSubnets)
				if err != nil {
					return err
				}
			}
			usedSubnets = append(usedSubnets, subnet)
			if legacy.MTU <= 0 {
				legacy.MTU = 1380
			}
			if legacy.KeepAlive <= 0 {
				legacy.KeepAlive = 25
			}
			options, err := json.Marshal(map[string]interface{}{
				"listen": "::", "listen_port": legacy.Port, "server": legacy.Host,
				"network": "quic", "private_key": legacy.PrivateKey, "public_key": legacy.PublicKey,
				"client_subnet": subnet.String(), "mtu": legacy.MTU, "keepalive": legacy.KeepAlive,
				"sni": legacy.SNI, "remote_dns_resolve": legacy.RemoteDNSResolve, "udp": legacy.UDP,
			})
			if err != nil {
				return err
			}
			inbound := &model.Inbound{
				Type: "masque", Tag: endpoint.Tag, Options: options,
				Addrs: json.RawMessage("[]"), OutJson: json.RawMessage("{}"),
			}
			if err := util.FillOutJson(inbound, legacy.Host); err != nil {
				return err
			}
			if err := tx.Create(inbound).Error; err != nil {
				return err
			}
			if err := syncManagedPortEntriesForInboundTx(tx, inbound); err != nil {
				return err
			}
			if clientIDList != "" {
				if err := (&ClientService{}).UpdateClientsOnInboundAdd(tx, clientIDList, inbound.Id, legacy.Host); err != nil {
					return err
				}
			}
			if err := deleteManagedPortEntriesForEndpointTx(tx, endpoint.Id); err != nil {
				return err
			}
			if err := tx.Delete(endpoint).Error; err != nil {
				return err
			}
			migrated++
		}
		return nil
	})
	if err != nil {
		return err
	}
	if migrated > 0 {
		markDataUpdated()
		logger.Info("migrated legacy MASQUE endpoints to inbounds: ", migrated)
	}
	return nil
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

func allocateMasqueMigrationSubnet(existing []netip.Prefix) (netip.Prefix, error) {
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

func allClientIDsTx(tx *gorm.DB) ([]uint, error) {
	var ids []uint
	if err := tx.Model(&model.Client{}).Order("id ASC").Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func joinUintIDs(ids []uint) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if id > 0 {
			parts = append(parts, strconv.FormatUint(uint64(id), 10))
		}
	}
	return strings.Join(parts, ",")
}
