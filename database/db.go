package database

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

var schemaModels = []any{
	&model.Setting{},
	&model.Tls{},
	&model.Inbound{},
	&model.Outbound{},
	&model.Service{},
	&model.Endpoint{},
	&model.ManagedPortEntry{},
	&model.User{},
	&model.Tokens{},
	&model.Stats{},
	&model.TrafficBudgetSample{},
	&model.Client{},
	&model.Changes{},
}

type schemaIndex struct {
	model any
	name  string
	sql   string
}

var schemaIndexes = []schemaIndex{
	{&model.Setting{}, "idx_settings_key", `CREATE UNIQUE INDEX idx_settings_key ON settings(key)`},
	{&model.User{}, "idx_users_username", `CREATE UNIQUE INDEX idx_users_username ON users(username)`},
	{&model.Client{}, "idx_clients_name", `CREATE INDEX idx_clients_name ON clients(name)`},
	{&model.Client{}, "idx_clients_enable_name", `CREATE INDEX idx_clients_enable_name ON clients(enable, name)`},
	{&model.Inbound{}, "idx_inbounds_tag", `CREATE INDEX idx_inbounds_tag ON inbounds(tag)`},
	{&model.Outbound{}, "idx_outbounds_tag", `CREATE INDEX idx_outbounds_tag ON outbounds(tag)`},
	{&model.Endpoint{}, "idx_endpoints_tag", `CREATE INDEX idx_endpoints_tag ON endpoints(tag)`},
	{&model.ManagedPortEntry{}, "idx_managed_port_lookup", `CREATE INDEX idx_managed_port_lookup ON managed_port_entries(port, scope, owner_id)`},
	{&model.ManagedPortEntry{}, "idx_managed_port_range_lookup", `CREATE INDEX idx_managed_port_range_lookup ON managed_port_entries(port, end_port, scope, owner_id)`},
	{&model.ManagedPortEntry{}, "idx_managed_port_owner_port", `CREATE UNIQUE INDEX idx_managed_port_owner_port ON managed_port_entries(scope, owner_id, port)`},
	{&model.Service{}, "idx_services_tag", `CREATE INDEX idx_services_tag ON services(tag)`},
	{&model.Tokens{}, "idx_tokens_expiry", `CREATE INDEX idx_tokens_expiry ON tokens(expiry)`},
	{&model.Tokens{}, "idx_tokens_user_id", `CREATE INDEX idx_tokens_user_id ON tokens(user_id)`},
	{&model.Changes{}, "idx_changes_date_time", `CREATE INDEX idx_changes_date_time ON changes(date_time)`},
	{&model.Changes{}, "idx_changes_actor_key_date_time", `CREATE INDEX idx_changes_actor_key_date_time ON changes(actor, key, date_time)`},
	{&model.Stats{}, "idx_stats_lookup", `CREATE INDEX idx_stats_lookup ON stats(resource, tag, date_time)`},
	{&model.TrafficBudgetSample{}, "idx_traffic_budget_samples_period_time", `CREATE UNIQUE INDEX idx_traffic_budget_samples_period_time ON traffic_budget_samples(period_start, date_time)`},
	{&model.TrafficBudgetSample{}, "idx_traffic_budget_samples_time", `CREATE INDEX idx_traffic_budget_samples_time ON traffic_budget_samples(date_time)`},
}

func OpenDB(dbPath string) error {
	dir := path.Dir(dbPath)
	err := os.MkdirAll(dir, 0o750)
	if err != nil {
		return err
	}

	var gormLogger logger.Interface

	if config.IsDebug() {
		gormLogger = logger.Default
	} else {
		gormLogger = logger.Discard
	}

	c := &gorm.Config{
		Logger: gormLogger,
	}
	sep := "?"
	if strings.Contains(dbPath, "?") {
		sep = "&"
	}
	dsn := dbPath + sep + "_busy_timeout=10000&_journal_mode=WAL&_synchronous=NORMAL"
	db, err = gorm.Open(sqlite.Open(dsn), c)
	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if config.IsDebug() {
		db = db.Debug()
	}
	return nil
}

func InitDB(dbPath string) error {
	err := OpenDB(dbPath)
	if err != nil {
		return err
	}
	return db.Transaction(func(tx *gorm.DB) error {
		existingTables := 0
		for _, schemaModel := range schemaModels {
			if tx.Migrator().HasTable(schemaModel) {
				existingTables++
			}
		}
		if existingTables == 0 {
			var userTables int64
			if err := tx.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%'").Scan(&userTables).Error; err != nil {
				return fmt.Errorf("inspect database before initialization: %w", err)
			}
			if userTables != 0 {
				return errors.New("database is not empty and contains no NovaPanel schema; refusing to initialize it")
			}
			if err := tx.Migrator().CreateTable(schemaModels...); err != nil {
				return fmt.Errorf("create fresh database schema: %w", err)
			}
			if err := createIndexes(tx); err != nil {
				return err
			}
			return seedFreshDatabase(tx)
		}
		if existingTables != len(schemaModels) {
			return fmt.Errorf("database schema is incomplete (%d/%d tables); automatic schema migration is not supported", existingTables, len(schemaModels))
		}
		if err := validateSchema(tx); err != nil {
			return err
		}
		return validateIndexes(tx)
	})
}

// ValidateCurrentSchema opens an existing database read-only and verifies the
// complete current schema. It never creates tables, columns, or indexes.
func ValidateCurrentSchema(dbPath string) error {
	db, closeDB, err := openSchemaDatabase(dbPath, "mode=ro")
	if err != nil {
		return err
	}
	defer closeDB()
	return validateCurrentSchema(db)
}

// MigrateMasqueIdentityColumns upgrades a database only when the two MASQUE
// identity columns are its sole schema difference. It does not inspect app
// versions or repair any other missing schema. Call on an isolated copy.
func MigrateMasqueIdentityColumns(dbPath string) (bool, error) {
	info, err := os.Lstat(dbPath)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return false, errors.New("database must be a non-empty regular file")
	}

	gormDB, closeDB, err := openSchemaDatabase(dbPath, "mode=rw")
	if err != nil {
		return false, err
	}
	defer closeDB()

	applied := false
	err = gormDB.Transaction(func(tx *gorm.DB) error {
		missing, err := missingSchemaColumns(tx)
		if err != nil {
			return err
		}
		if len(missing) == 0 {
			return validateIndexes(tx)
		}

		masqueColumns := map[string]string{
			"inbounds.masque_tls_certificate": "ALTER TABLE inbounds ADD COLUMN masque_tls_certificate TEXT NOT NULL DEFAULT ''",
			"inbounds.masque_tls_key":         "ALTER TABLE inbounds ADD COLUMN masque_tls_key TEXT NOT NULL DEFAULT ''",
		}
		for _, column := range missing {
			if _, supported := masqueColumns[column]; !supported {
				return fmt.Errorf("unsupported database schema; missing columns: %s (only the MASQUE identity column patch is supported)", strings.Join(missing, ", "))
			}
		}
		if err := validateIndexes(tx); err != nil {
			return err
		}
		for _, column := range missing {
			if err := tx.Exec(masqueColumns[column]).Error; err != nil {
				return fmt.Errorf("add MASQUE identity column %s: %w", column, err)
			}
		}
		if err := validateCurrentSchema(tx); err != nil {
			return err
		}
		applied = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("apply MASQUE identity column patch: %w", err)
	}
	return applied, nil
}

func openSchemaDatabase(dbPath, mode string) (*gorm.DB, func(), error) {
	absolute, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, nil, err
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     filepath.ToSlash(absolute),
		RawQuery: mode + "&_busy_timeout=3000",
	}).String()
	gormDB, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		return nil, nil, err
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		return nil, nil, err
	}
	return gormDB, func() { _ = sqlDB.Close() }, nil
}

func createIndexes(tx *gorm.DB) error {
	for _, index := range schemaIndexes {
		if err := tx.Exec(index.sql).Error; err != nil {
			return fmt.Errorf("create database index %s: %w", index.name, err)
		}
	}
	return nil
}

func validateSchema(tx *gorm.DB) error {
	missing, err := missingSchemaColumns(tx)
	if err != nil {
		return err
	}
	if len(missing) > 0 {
		return fmt.Errorf("database schema is missing column %s; automatic schema migration is not supported", missing[0])
	}
	return nil
}

func missingSchemaColumns(tx *gorm.DB) ([]string, error) {
	var missing []string
	for _, schemaModel := range schemaModels {
		statement := &gorm.Statement{DB: tx}
		if err := statement.Parse(schemaModel); err != nil {
			return nil, err
		}
		if !tx.Migrator().HasTable(schemaModel) {
			return nil, fmt.Errorf("database schema is missing table %s; automatic schema migration is not supported", statement.Schema.Table)
		}
		for _, field := range statement.Schema.Fields {
			if field.DBName != "" && !tx.Migrator().HasColumn(schemaModel, field.DBName) {
				missing = append(missing, statement.Schema.Table+"."+field.DBName)
			}
		}
	}
	return missing, nil
}

func validateCurrentSchema(tx *gorm.DB) error {
	if err := validateSchema(tx); err != nil {
		return err
	}
	return validateIndexes(tx)
}

func validateIndexes(tx *gorm.DB) error {
	for _, index := range schemaIndexes {
		if !tx.Migrator().HasIndex(index.model, index.name) {
			return fmt.Errorf("database schema is missing index %s; automatic schema migration is not supported", index.name)
		}
	}
	return nil
}

func seedFreshDatabase(tx *gorm.DB) error {
	if err := tx.Create(&model.Outbound{Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`)}).Error; err != nil {
		return fmt.Errorf("seed default direct outbound: %w", err)
	}
	if err := tx.Create(&model.User{Username: "admin", Password: "admin"}).Error; err != nil {
		return fmt.Errorf("seed initial administrator: %w", err)
	}
	return nil
}

func GetDB() *gorm.DB {
	return db
}

func CloseDB() error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	db = nil
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

func IsNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}

func IsLockedError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "database is locked") ||
		strings.Contains(message, "database table is locked") ||
		strings.Contains(message, "database is busy")
}

func RetryOnLocked(attempts int, delay time.Duration, fn func() error) error {
	if attempts < 1 {
		attempts = 1
	}

	var errs []error
	for attempt := 0; attempt < attempts; attempt++ {
		err := fn()
		if err == nil {
			return nil
		}
		errs = append(errs, err)
		if !IsLockedError(err) || attempt == attempts-1 {
			break
		}
		time.Sleep(delay)
	}

	return errors.Join(errs...)
}

func WithRetryTx(attempts int, delay time.Duration, fn func(tx *gorm.DB) error) error {
	return RetryOnLocked(attempts, delay, func() error {
		tx := GetDB().Begin()
		if tx.Error != nil {
			return tx.Error
		}

		if err := fn(tx); err != nil {
			tx.Rollback()
			return err
		}

		return tx.Commit().Error
	})
}
