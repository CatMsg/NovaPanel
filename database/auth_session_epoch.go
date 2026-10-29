package database

import (
	"errors"
	"math"
	"strconv"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

const (
	AuthSessionEpochSettingKey = "authSessionEpoch"
	InitialAuthSessionEpoch    = uint64(1)
)

func GetAuthSessionEpoch(db *gorm.DB) (uint64, error) {
	epoch, _, err := readAuthSessionEpoch(db)
	return epoch, err
}

func RotateAuthSessionEpoch(tx *gorm.DB) error {
	epoch, exists, err := readAuthSessionEpoch(tx)
	if err != nil {
		return err
	}
	if epoch == math.MaxUint64 {
		return errors.New("authentication session epoch exhausted")
	}
	next := strconv.FormatUint(epoch+1, 10)
	if !exists {
		return tx.Create(&model.Setting{Key: AuthSessionEpochSettingKey, Value: next}).Error
	}
	return tx.Model(model.Setting{}).Where("key = ?", AuthSessionEpochSettingKey).Update("value", next).Error
}

// AdvanceRestoredAuthSessionEpoch makes every pre-restore session stale even
// when the imported snapshot predates epoch tracking or has an older epoch.
func AdvanceRestoredAuthSessionEpoch(liveDB, restoredDB *gorm.DB) error {
	liveEpoch, _, err := readAuthSessionEpoch(liveDB)
	if err != nil {
		return err
	}
	restoredEpoch, _, err := readAuthSessionEpoch(restoredDB)
	if err != nil {
		return err
	}
	if restoredEpoch > liveEpoch {
		liveEpoch = restoredEpoch
	}
	if liveEpoch == math.MaxUint64 {
		return errors.New("authentication session epoch exhausted")
	}
	next := strconv.FormatUint(liveEpoch+1, 10)
	return restoredDB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Setting{}).Where("key = ?", AuthSessionEpochSettingKey).Update("value", next)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected > 0 {
			return nil
		}
		return tx.Create(&model.Setting{Key: AuthSessionEpochSettingKey, Value: next}).Error
	})
}

func readAuthSessionEpoch(db *gorm.DB) (uint64, bool, error) {
	if db == nil {
		return 0, false, errors.New("database is unavailable")
	}
	var setting model.Setting
	err := db.Model(model.Setting{}).Where("key = ?", AuthSessionEpochSettingKey).First(&setting).Error
	if IsNotFound(err) {
		return InitialAuthSessionEpoch, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	epoch, err := strconv.ParseUint(setting.Value, 10, 64)
	if err != nil || epoch == 0 {
		return 0, false, errors.New("invalid authentication session epoch")
	}
	return epoch, true, nil
}
