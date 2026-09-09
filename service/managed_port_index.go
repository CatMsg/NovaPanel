package service

import (
	"strings"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

const (
	managedPortScopeInbound  = "inbound"
	managedPortScopeEndpoint = "endpoint"
)

func RebuildManagedPortEntries() error {
	return retryWriteTx(func(tx *gorm.DB) error {
		return rebuildManagedPortEntriesTx(tx)
	})
}

func rebuildManagedPortEntriesTx(tx *gorm.DB) error {
	if err := tx.Where("1 = 1").Delete(&model.ManagedPortEntry{}).Error; err != nil {
		return err
	}

	var inbounds []*model.Inbound
	if err := tx.Model(model.Inbound{}).Find(&inbounds).Error; err != nil {
		return err
	}
	for _, inbound := range inbounds {
		if err := syncManagedPortEntriesForInboundTx(tx, inbound); err != nil {
			return err
		}
	}

	var endpoints []*model.Endpoint
	if err := tx.Model(model.Endpoint{}).Find(&endpoints).Error; err != nil {
		return err
	}
	for _, endpoint := range endpoints {
		if err := syncManagedPortEntriesForEndpointTx(tx, endpoint); err != nil {
			return err
		}
	}

	return nil
}

func syncManagedPortEntriesForInboundTx(tx *gorm.DB, inbound *model.Inbound) error {
	if inbound == nil {
		return nil
	}
	if err := deleteManagedPortEntriesTx(tx, managedPortScopeInbound, inbound.Id); err != nil {
		return err
	}
	spec, err := collectInboundForwardSpec(inbound)
	if err != nil {
		return err
	}
	if !spec.active {
		return nil
	}
	return createManagedPortRangeProtocolEntriesTx(tx, managedPortScopeInbound, inbound.Id, inbound.Tag, spec.portRanges, spec.protocols)
}

func syncManagedPortEntriesForEndpointTx(tx *gorm.DB, endpoint *model.Endpoint) error {
	if endpoint == nil {
		return nil
	}
	if err := deleteManagedPortEntriesTx(tx, managedPortScopeEndpoint, endpoint.Id); err != nil {
		return err
	}
	spec, err := collectEndpointForwardSpec(endpoint)
	if err != nil {
		return err
	}
	if !spec.active {
		return nil
	}
	return createManagedPortRangeProtocolEntriesTx(tx, managedPortScopeEndpoint, endpoint.Id, endpoint.Tag, spec.portRanges, spec.protocols)
}

func deleteManagedPortEntriesForInboundTx(tx *gorm.DB, inboundID uint) error {
	return deleteManagedPortEntriesTx(tx, managedPortScopeInbound, inboundID)
}

func deleteManagedPortEntriesForEndpointTx(tx *gorm.DB, endpointID uint) error {
	return deleteManagedPortEntriesTx(tx, managedPortScopeEndpoint, endpointID)
}

func deleteManagedPortEntriesTx(tx *gorm.DB, scope string, ownerID uint) error {
	if ownerID == 0 {
		return nil
	}
	return tx.Where("scope = ? AND owner_id = ?", scope, ownerID).Delete(&model.ManagedPortEntry{}).Error
}

func createManagedPortRangeProtocolEntriesTx(tx *gorm.DB, scope string, ownerID uint, ownerTag string, ranges []managedPortRange, protocols []string) error {
	ranges = normalizeManagedPortRanges(ranges)
	protocols = normalizeManagedProtocols(protocols)
	if ownerID == 0 || len(ranges) == 0 || len(protocols) == 0 {
		return nil
	}

	protocolList := strings.Join(protocols, ",")
	entries := make([]model.ManagedPortEntry, 0, len(ranges))
	for _, item := range ranges {
		entries = append(entries, model.ManagedPortEntry{
			Scope:     scope,
			OwnerId:   ownerID,
			OwnerTag:  ownerTag,
			Port:      item.start,
			EndPort:   item.end,
			Protocols: protocolList,
		})
	}
	return tx.Create(&entries).Error
}
