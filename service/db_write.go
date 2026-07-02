package service

import (
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"gorm.io/gorm"
)

const (
	writeRetryAttempts = 5
	writeRetryDelay    = 150 * time.Millisecond
)

func retryWrite(fn func(db *gorm.DB) error) error {
	return retryOnDatabaseLocked(writeRetryAttempts, writeRetryDelay, func() error {
		return fn(database.GetDB())
	})
}

func retryWriteTx(fn func(tx *gorm.DB) error) error {
	return retryWrite(func(db *gorm.DB) error {
		tx := db.Begin()
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
