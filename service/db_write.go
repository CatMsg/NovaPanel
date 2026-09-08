package service

import (
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"gorm.io/gorm"
)

const (
	writeRetryAttempts = 8
	writeRetryDelay    = 200 * time.Millisecond
)

func retryWrite(fn func(db *gorm.DB) error) error {
	return database.RetryOnLocked(writeRetryAttempts, writeRetryDelay, func() error {
		return fn(database.GetDB())
	})
}

func retryWriteTx(fn func(tx *gorm.DB) error) error {
	return database.WithRetryTx(writeRetryAttempts, writeRetryDelay, fn)
}
