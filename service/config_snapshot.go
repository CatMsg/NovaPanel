package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Keep the rollback history bound aligned with core.maxClientHistoryEntries.
const maxConfigSnapshotHistoryEntries = 200

// configSnapshot is the database half of the save coordinator. SQLite cannot
// atomically commit firewall, listener and core changes, so failed external
// applies are compensated by restoring this before-image under saveConfigMu.
type configSnapshot struct {
	settings      []model.Setting
	tls           []model.Tls
	inbounds      []model.Inbound
	outbounds     []model.Outbound
	services      []model.Service
	endpoints     []model.Endpoint
	managedPorts  []model.ManagedPortEntry
	clients       []model.Client
	afterClients  []model.Client
	stats         []model.Stats
	afterStats    []model.Stats
	includeStats  bool
	afterCaptured bool
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

// Capture in the configuration write transaction, before commit. A later read
// cannot distinguish a requested reset from concurrent traffic.
func (snapshot *configSnapshot) captureAfterImage(tx *gorm.DB) error {
	var clients []model.Client
	if err := tx.Find(&clients).Error; err != nil {
		return fmt.Errorf("snapshot after-image clients: %w", err)
	}
	var stats []model.Stats
	if snapshot.includeStats {
		if err := tx.Find(&stats).Error; err != nil {
			return fmt.Errorf("snapshot after-image stats: %w", err)
		}
	}
	snapshot.afterClients = clients
	snapshot.afterStats = stats
	snapshot.afterCaptured = true
	return nil
}

func (snapshot *configSnapshot) restore(tx *gorm.DB) error {
	if snapshot == nil {
		return nil
	}
	if !snapshot.afterCaptured {
		return errors.New("configuration snapshot is missing its transactional after-image")
	}
	if err := snapshot.restoreClients(tx); err != nil {
		return err
	}
	if snapshot.includeStats {
		if err := snapshot.restoreStats(tx); err != nil {
			return err
		}
	}
	tables := []interface{}{
		&model.ManagedPortEntry{}, &model.Inbound{},
		&model.Service{}, &model.Endpoint{}, &model.Outbound{},
		&model.Tls{}, &model.Setting{},
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

func (snapshot *configSnapshot) restoreClients(tx *gorm.DB) error {
	var currentClients []model.Client
	if err := tx.Find(&currentClients).Error; err != nil {
		return err
	}
	current := make(map[uint]model.Client, len(currentClients))
	for _, client := range currentClients {
		current[client.Id] = client
	}
	after := make(map[uint]model.Client, len(snapshot.afterClients))
	for _, client := range snapshot.afterClients {
		after[client.Id] = client
	}
	beforeIDs := make(map[uint]bool, len(snapshot.clients))
	// Copy the baseline so a rolled-back restore attempt can be retried safely.
	restored := append([]model.Client(nil), snapshot.clients...)
	for index := range restored {
		before := &restored[index]
		beforeIDs[before.Id] = true
		committed, existedAfter := after[before.Id]
		live, existsNow := current[before.Id]
		if !existedAfter || !existsNow {
			continue
		}
		if live.NextReset != committed.NextReset &&
			(before.Up != committed.Up || before.Down != committed.Down ||
				before.TotalUp != committed.TotalUp || before.TotalDown != committed.TotalDown) {
			// A reset intent followed by a separate cycle transition cannot be
			// undone with per-column deltas without assigning usage to the wrong
			// cycle. Leave persisted data intact and report the conflict instead.
			return fmt.Errorf("restore client %d: usage intent conflicts with concurrent reset cycle", before.Id)
		}
		counters := []struct {
			name           string
			dest           *int64
			after, current int64
		}{
			{"up", &before.Up, committed.Up, live.Up},
			{"down", &before.Down, committed.Down, live.Down},
			{"total_up", &before.TotalUp, committed.TotalUp, live.TotalUp},
			{"total_down", &before.TotalDown, committed.TotalDown, live.TotalDown},
		}
		for _, counter := range counters {
			value, err := compensateClientCounter(*counter.dest, counter.after, counter.current)
			if err != nil {
				return fmt.Errorf("restore client %d %s: %w", before.Id, counter.name, err)
			}
			*counter.dest = value
		}
		history, err := compensateClientHistory(before.History, committed.History, live.History)
		if err != nil {
			return fmt.Errorf("restore client %d history: %w", before.Id, err)
		}
		before.History = history
		// Reset/depletion jobs also change these fields after the save commits.
		if live.NextReset != committed.NextReset {
			before.NextReset = live.NextReset
		}
		if live.DelayStart != committed.DelayStart {
			before.DelayStart = live.DelayStart
		}
		if live.Expiry != committed.Expiry {
			before.Expiry = live.Expiry
		}
		if live.Enable != committed.Enable {
			before.Enable = live.Enable
		}
	}
	var addedIDs []uint
	for _, client := range snapshot.afterClients {
		if !beforeIDs[client.Id] {
			addedIDs = append(addedIDs, client.Id)
		}
	}
	if len(addedIDs) > 0 {
		if err := tx.Where("id IN ?", addedIDs).Delete(&model.Client{}).Error; err != nil {
			return err
		}
	}
	if len(restored) == 0 {
		return nil
	}
	return tx.Omit(clause.Associations).Clauses(clause.OnConflict{UpdateAll: true}).Create(&restored).Error
}

// Stats are append-only samples apart from retention. Undo only the rows changed
// by this save, never replace the table or resurrect unrelated retention deletes.
func (snapshot *configSnapshot) restoreStats(tx *gorm.DB) error {
	var rows []model.Stats
	if err := tx.Find(&rows).Error; err != nil {
		return err
	}
	current := make(map[uint64]model.Stats, len(rows))
	for _, row := range rows {
		current[row.Id] = row
	}
	after := make(map[uint64]model.Stats, len(snapshot.afterStats))
	for _, row := range snapshot.afterStats {
		after[row.Id] = row
	}
	beforeIDs := make(map[uint64]bool, len(snapshot.stats))
	var restored []model.Stats
	for _, before := range snapshot.stats {
		beforeIDs[before.Id] = true
		committed, existedAfter := after[before.Id]
		if existedAfter && committed == before {
			continue
		}
		live, existsNow := current[before.Id]
		if existsNow {
			if live == before {
				continue
			}
			if !existedAfter || live != committed {
				return fmt.Errorf("restore stats %d: concurrent row conflict", before.Id)
			}
		} else if existedAfter {
			continue
		}
		restored = append(restored, before)
	}
	var addedIDs []uint64
	for _, committed := range snapshot.afterStats {
		if beforeIDs[committed.Id] {
			continue
		}
		if live, existsNow := current[committed.Id]; existsNow {
			if live != committed {
				return fmt.Errorf("restore stats %d: concurrent row conflict", committed.Id)
			}
			addedIDs = append(addedIDs, committed.Id)
		}
	}
	if len(addedIDs) > 0 {
		if err := tx.Where("id IN ?", addedIDs).Delete(&model.Stats{}).Error; err != nil {
			return err
		}
	}
	if len(restored) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&restored).Error
}

func compensateClientCounter(before, after, current int64) (int64, error) {
	if before < 0 || after < 0 || current < 0 {
		return 0, errors.New("negative traffic counter")
	}
	if current >= after {
		delta := current - after
		if delta > math.MaxInt64-before {
			return 0, errors.New("traffic compensation overflow")
		}
		return before + delta, nil
	}
	delta := after - current
	if delta > before {
		return 0, errors.New("traffic compensation would be negative")
	}
	return before - delta, nil
}

func compensateClientHistory(before, after, current json.RawMessage) (json.RawMessage, error) {
	if bytes.Equal(after, current) {
		return before, nil
	}
	decode := func(raw json.RawMessage) ([]json.RawMessage, error) {
		var entries []json.RawMessage
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &entries); err != nil {
				return nil, err
			}
		}
		return entries, nil
	}
	beforeEntries, err := decode(before)
	if err != nil {
		return nil, err
	}
	afterEntries, err := decode(after)
	if err != nil {
		return nil, err
	}
	currentEntries, err := decode(current)
	if err != nil {
		return nil, err
	}
	// Canonicalize only for identity; retain raw entries and their unknown fields.
	key := func(entry json.RawMessage) (string, error) {
		var value interface{}
		decoder := json.NewDecoder(bytes.NewReader(entry))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return "", err
		}
		raw, err := json.Marshal(value)
		return string(raw), err
	}
	committed := make(map[string]bool, len(afterEntries))
	for _, entry := range afterEntries {
		identity, err := key(entry)
		if err != nil {
			return nil, err
		}
		committed[identity] = true
	}
	seen := make(map[string]bool)
	merged := make([]json.RawMessage, 0, len(currentEntries)+len(beforeEntries))
	for group, entries := range [][]json.RawMessage{currentEntries, beforeEntries} {
		for _, entry := range entries {
			identity, err := key(entry)
			if err != nil {
				return nil, err
			}
			if seen[identity] || (group == 0 && committed[identity]) {
				continue
			}
			seen[identity] = true
			merged = append(merged, entry)
		}
	}
	// Both images are newest-first; current-only entries precede the older
	// before-image. Apply the same bound as the live history tracker.
	if len(merged) > maxConfigSnapshotHistoryEntries {
		merged = merged[:maxConfigSnapshotHistoryEntries]
	}
	return json.Marshal(merged)
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
		if err := (&LoginGuardService{}).SyncLoginProtection(); err != nil {
			errs = append(errs, fmt.Errorf("restore login protection: %w", err))
		}
		if err := restartSubServer(); err != nil {
			errs = append(errs, fmt.Errorf("restore subscription listener: %w", err))
		}
		if err := GetTrafficBudgetService().Reconcile(); err != nil {
			errs = append(errs, fmt.Errorf("restore traffic budget protection: %w", err))
		}
	}
	if IsTrafficBudgetBlocked() {
		return errors.Join(errs...)
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
