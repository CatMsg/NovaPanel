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
	err := os.MkdirAll(dir, 01740)
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
	dsn := dbPath + sep + "_busy_timeout=10000&_journal_mode=WAL"
	db, err = gorm.Open(sqlite.Open(dsn), c)
	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(5)
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
		db.Migrator().CreateTable(&model.Outbound{})
		defaultOutbound := []model.Outbound{
			{Type: "direct", Tag: "direct", Options: json.RawMessage(`{}`)},
		}
		db.Create(&defaultOutbound)
	}

	err = db.AutoMigrate(
		&model.Setting{},
		&model.Tls{},
		&model.Inbound{},
		&model.Outbound{},
		&model.Service{},
		&model.Endpoint{},
		&model.User{},
		&model.Tokens{},
		&model.Stats{},
		&model.Client{},
		&model.Changes{},
	)
	if err != nil {
		return err
	}
	err = initUser()
	if err != nil {
		return err
	}

	return nil
}

func GetDB() *gorm.DB {
	return db
}

func IsNotFound(err error) bool {
	return err == gorm.ErrRecordNotFound
}

func IsLockedError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "database is locked")
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
