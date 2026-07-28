package service

import (
	"errors"
	"fmt"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// configSnapshot is the database half of the save coordinator. SQLite cannot
// atomically commit firewall, listener and core changes, so failed external
// applies are compensated by restoring this before-image under saveConfigMu.
type configSnapshot struct {
	settings     []model.Setting
	tls          []model.Tls
	inbounds     []model.Inbound
	outbounds    []model.Outbound
	services     []model.Service
	endpoints    []model.Endpoint
	managedPorts []model.ManagedPortEntry
	clients      []model.Client
	stats        []model.Stats
	includeStats bool
}

func captureConfigSnapshot(tx *gorm.DB, includeStats bool) (*configSnapshot, error) {
	snapshot := &configSnapshot{includeStats: includeStats}
	queries := []struct {
		name string
		dest interface{}
	}{
		{"settings", &snapshot.settings},
		{"tls", &snapshot.tls},
		{"inbounds", &snapshot.inbounds},
		{"outbounds", &snapshot.outbounds},
		{"services", &snapshot.services},
		{"endpoints", &snapshot.endpoints},
		{"managed ports", &snapshot.managedPorts},
		{"clients", &snapshot.clients},
	}
	if includeStats {
		queries = append(queries, struct {
			name string
			dest interface{}
		}{"stats", &snapshot.stats})
	}
	for _, query := range queries {
		if err := tx.Find(query.dest).Error; err != nil {
			return nil, fmt.Errorf("snapshot %s: %w", query.name, err)
		}
	}
	return snapshot, nil
}

func (snapshot *configSnapshot) restore(tx *gorm.DB) error {
	if snapshot == nil {
		return nil
	}
	tables := []interface{}{
		&model.ManagedPortEntry{}, &model.Client{}, &model.Inbound{},
		&model.Service{}, &model.Endpoint{}, &model.Outbound{},
		&model.Tls{}, &model.Setting{},
	}
	if snapshot.includeStats {
		tables = append([]interface{}{&model.Stats{}}, tables...)
	}
	for _, table := range tables {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(table).Error; err != nil {
			return err
		}
	}

	rows := []struct {
		value interface{}
		count int
	}{
		{&snapshot.settings, len(snapshot.settings)},
		{&snapshot.tls, len(snapshot.tls)},
		{&snapshot.inbounds, len(snapshot.inbounds)},
		{&snapshot.outbounds, len(snapshot.outbounds)},
		{&snapshot.services, len(snapshot.services)},
		{&snapshot.endpoints, len(snapshot.endpoints)},
		{&snapshot.managedPorts, len(snapshot.managedPorts)},
		{&snapshot.clients, len(snapshot.clients)},
	}
	if snapshot.includeStats {
		rows = append(rows, struct {
			value interface{}
			count int
		}{&snapshot.stats, len(snapshot.stats)})
	}
	for _, row := range rows {
		if row.count == 0 {
			continue
		}
		if err := tx.Omit(clause.Associations).Create(row.value).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *ConfigService) compensateFailedSave(snapshot *configSnapshot, obj string, changeID uint64) error {
	var errs []error
	if err := retryWriteTx(func(tx *gorm.DB) error {
		if err := snapshot.restore(tx); err != nil {
			return err
		}
		if changeID > 0 {
			return tx.Unscoped().Delete(&model.Changes{}, changeID).Error
		}
		return nil
	}); err != nil {
		return fmt.Errorf("restore database snapshot: %w", err)
	}

	if err := s.SettingService.RebuildAllManagedPortForwarding(&s.InboundService, &s.EndpointService); err != nil {
		errs = append(errs, fmt.Errorf("restore managed ports: %w", err))
	}
	if obj == "settings" {
		if err := restartSubServer(); err != nil {
			errs = append(errs, fmt.Errorf("restore subscription listener: %w", err))
		}
	}
	if masquePtr != nil {
		if err := masquePtr.SyncFromDB(); err != nil {
			errs = append(errs, fmt.Errorf("restore masque service: %w", err))
		}
	}
	if mieruPtr != nil {
		if err := mieruPtr.SyncFromDB(); err != nil {
			errs = append(errs, fmt.Errorf("restore mieru service: %w", err))
		}
	}
	if corePtr != nil {
		var err error
		if corePtr.IsRunning() {
			err = s.RestartCore()
		} else {
			err = s.StartCore()
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("restore core: %w", err))
		}
	}
	return errors.Join(errs...)
}
