package service

import (
	"encoding/json"
	"strings"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"gorm.io/gorm"
)

const mieruStandardHandshakeMigrationKey = "migration.mieru-standard-handshake-v1"

// migrateMieruHandshakeModeOnce moves legacy 0-RTT inbounds to the stable
// handshake mode. The marker lets users explicitly select 0-RTT again later.
func migrateMieruHandshakeModeOnce() error {
	db := database.GetDB()
	if db == nil {
		return nil
	}

	var count int64
	if err := db.Model(&model.Setting{}).Where("key = ?", mieruStandardHandshakeMigrationKey).Count(&count).Error; err != nil || count > 0 {
		return err
	}

	migrated := 0
	err := retryWriteTx(func(tx *gorm.DB) error {
		migratedInAttempt := 0
		var markerCount int64
		if err := tx.Model(&model.Setting{}).Where("key = ?", mieruStandardHandshakeMigrationKey).Count(&markerCount).Error; err != nil {
			return err
		}
		if markerCount > 0 {
			return nil
		}

		var inbounds []model.Inbound
		if err := tx.Where("type = ?", "mieru").Find(&inbounds).Error; err != nil {
			return err
		}
		for index := range inbounds {
			inbound := &inbounds[index]
			var options map[string]interface{}
			if err := json.Unmarshal(inbound.Options, &options); err != nil {
				return err
			}
			mode := strings.ToUpper(strings.TrimSpace(mieruString(options["handshake_mode"])))
			if mode != "HANDSHAKE_NO_WAIT" {
				continue
			}
			options["handshake_mode"] = "HANDSHAKE_STANDARD"
			payload, err := json.Marshal(options)
			if err != nil {
				return err
			}
			if err := tx.Model(&model.Inbound{}).Where("id = ?", inbound.Id).Update("options", payload).Error; err != nil {
				return err
			}
			migratedInAttempt++
		}

		if err := tx.Create(&model.Setting{Key: mieruStandardHandshakeMigrationKey, Value: "done"}).Error; err != nil {
			return err
		}
		migrated = migratedInAttempt
		return nil
	})
	if err != nil {
		return err
	}
	if migrated > 0 {
		markDataUpdated()
		logger.Info("migrated Mieru inbounds to stable handshake mode: ", migrated)
	}
	return nil
}
