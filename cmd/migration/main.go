package migration

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/CatMsg/NovaPanel/config"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func MigrateDb() error {
	return migrateDB(config.GetDBPath())
}

func migrateDB(path string) (err error) {
	// void running on first install
	_, err = os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		println("Database not found")
		return nil
	}
	if err != nil {
		return err
	}

	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	tx := db.Begin()
	if tx.Error != nil {
		return tx.Error
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback().Error
		}
	}()
	currentVersion := config.GetVersion()
	dbVersion := ""
	if err = tx.Raw("SELECT value FROM settings WHERE key = ?", "version").Scan(&dbVersion).Error; err != nil {
		return err
	}
	dbVersion = strings.TrimSpace(dbVersion)
	fmt.Println("Current version:", currentVersion, "\nDatabase version:", dbVersion)

	if currentVersion == dbVersion {
		fmt.Println("Database is up to date, no need to migrate")
		if err = tx.Commit().Error; err != nil {
			return err
		}
		committed = true
		return nil
	}

	fmt.Println("Start migrating database...")

	// Before 1.2
	if dbVersion == "" {
		err = to1_1(tx)
		if err != nil {
			return fmt.Errorf("migration to 1.1 failed: %w", err)
		}
		err = to1_2(tx)
		if err != nil {
			return fmt.Errorf("migration to 1.2 failed: %w", err)
		}
		dbVersion = "1.2"
	}

	// Before 1.3
	if strings.HasPrefix(dbVersion, "1.2") {
		err = to1_3(tx)
		if err != nil {
			return fmt.Errorf("migration to 1.3 failed: %w", err)
		}
	}

	// Set version
	result := tx.Exec("UPDATE settings SET value = ? WHERE key = ?", currentVersion, "version")
	err = result.Error
	if err != nil {
		return fmt.Errorf("update version failed: %w", err)
	}
	if result.RowsAffected == 0 {
		if err = tx.Exec("INSERT INTO settings(key, value) VALUES(?, ?)", "version", currentVersion).Error; err != nil {
			return fmt.Errorf("insert version failed: %w", err)
		}
	}
	if err = tx.Commit().Error; err != nil {
		return err
	}
	committed = true
	fmt.Println("Migration done!")
	return nil
}
