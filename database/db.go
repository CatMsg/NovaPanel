package database

import (
	"encoding/json"
	"errors"
	"os"
	"path"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var db *gorm.DB

func initUser() error {
	var count int64
	err := db.Model(&model.User{}).Count(&count).Error
	if err != nil {
		return err
	}
	if count == 0 {
		user := &model.User{
			Username: "admin",
			Password: "admin",
		}
		return db.Create(user).Error
	}
	return nil
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

	// Default Outbounds
	if !db.Migrator().HasTable(&model.Outbound{}) {
		if err := db.Migrator().CreateTable(&model.Outbound{}); err != nil {
			return err
		}
		defaultOutbound := []model.Outbound{
			{Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`)},
		}
		if err := db.Create(&defaultOutbound).Error; err != nil {
			return err
		}
	}

	err = db.AutoMigrate(
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
		&model.Client{},
		&model.Changes{},
	)
	if err != nil {
		return err
	}
	if err := ensureIndexes(); err != nil {
		return err
	}
	err = initUser()
	if err != nil {
		return err
	}

	return nil
}

func ensureIndexes() error {
	indexes := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_settings_key ON settings(key)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_users_username ON users(username)`,
		`CREATE INDEX IF NOT EXISTS idx_clients_name ON clients(name)`,
		`CREATE INDEX IF NOT EXISTS idx_clients_enable_name ON clients(enable, name)`,
		`CREATE INDEX IF NOT EXISTS idx_inbounds_tag ON inbounds(tag)`,
		`CREATE INDEX IF NOT EXISTS idx_outbounds_tag ON outbounds(tag)`,
		`CREATE INDEX IF NOT EXISTS idx_endpoints_tag ON endpoints(tag)`,
		`CREATE INDEX IF NOT EXISTS idx_managed_port_lookup ON managed_port_entries(port, scope, owner_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_managed_port_owner_port ON managed_port_entries(scope, owner_id, port)`,
		`CREATE INDEX IF NOT EXISTS idx_services_tag ON services(tag)`,
		`CREATE INDEX IF NOT EXISTS idx_tokens_expiry ON tokens(expiry)`,
		`CREATE INDEX IF NOT EXISTS idx_tokens_user_id ON tokens(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_changes_date_time ON changes(date_time)`,
		`CREATE INDEX IF NOT EXISTS idx_changes_actor_key_date_time ON changes(actor, key, date_time)`,
		`CREATE INDEX IF NOT EXISTS idx_stats_lookup ON stats(resource, tag, date_time)`,
	}

	for _, stmt := range indexes {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func GetDB() *gorm.DB {
	return db
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
