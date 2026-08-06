package database

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/CatMsg/NovaPanel/cmd/migration"
	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const maxDatabaseBackupSize int64 = 1 << 30

func CreateDBBackup(exclude string) (string, func(), error) {
	exclude_changes, exclude_stats := false, false
	for _, table := range strings.Split(exclude, ",") {
		if table == "changes" {
			exclude_changes = true
		} else if table == "stats" {
			exclude_stats = true
		}
	}

	tempFile, err := os.CreateTemp("", "."+config.GetName()+"-backup-*.db")
	if err != nil {
		return "", nil, err
	}
	dbPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(dbPath)
		return "", nil, err
	}
	if err := os.Remove(dbPath); err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.Remove(dbPath)
		removeSQLiteSidecars(dbPath)
	}
	if err := db.Exec("VACUUM INTO ?", dbPath).Error; err != nil {
		cleanup()
		return "", nil, err
	}
	if err := os.Chmod(dbPath, 0o600); err != nil {
		cleanup()
		return "", nil, err
	}

	backupDb, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		cleanup()
		return "", nil, err
	}
	bdb, err := backupDb.DB()
	if err != nil {
		cleanup()
		return "", nil, err
	}
	closeAndCleanup := func() {
		_ = bdb.Close()
		cleanup()
	}
	if exclude_stats {
		if err := backupDb.Exec("DELETE FROM stats").Error; err != nil {
			closeAndCleanup()
			return "", nil, err
		}
	}
	if exclude_changes {
		if err := backupDb.Exec("DELETE FROM changes").Error; err != nil {
			closeAndCleanup()
			return "", nil, err
		}
	}
	if exclude_stats || exclude_changes {
		if err := backupDb.Exec("VACUUM").Error; err != nil {
			closeAndCleanup()
			return "", nil, err
		}
	}
	if err := bdb.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return dbPath, cleanup, nil
}

func ImportDB(file multipart.File) error {
	// Check if the file is a SQLite database
	isValidDb, err := IsSQLiteDB(file)
	if err != nil {
		return common.NewErrorf("Error checking db file format: %v", err)
	}
	if !isValidDb {
		return common.NewError("Invalid db file format")
	}

	// Reset the file reader to the beginning
	_, err = file.Seek(0, 0)
	if err != nil {
		return common.NewErrorf("Error resetting file reader: %v", err)
	}

	// Save the file as temporary file
	tempPath := fmt.Sprintf("%s.temp", config.GetDBPath())
	// Remove the existing fallback file (if any) before creating one
	_, err = os.Stat(tempPath)
	if err == nil {
		errRemove := os.Remove(tempPath)
		if errRemove != nil {
			return common.NewErrorf("Error removing existing temporary db file: %v", errRemove)
		}
	}
	// Create the temporary file
	tempFile, err := os.Create(tempPath)
	if err != nil {
		return common.NewErrorf("Error creating temporary db file: %v", err)
	}
	defer tempFile.Close()

	// Remove temp file before returning
	defer os.Remove(tempPath)

	// Save uploaded file to temporary file
	written, err := io.Copy(tempFile, io.LimitReader(file, maxDatabaseBackupSize+1))
	if err != nil {
		return common.NewErrorf("Error saving db: %v", err)
	}
	if written > maxDatabaseBackupSize {
		return common.NewError("Database backup exceeds 1 GiB limit")
	}
	if err := tempFile.Sync(); err != nil {
		return common.NewErrorf("Error syncing temporary db: %v", err)
	}
	if err := tempFile.Close(); err != nil {
		return common.NewErrorf("Error closing temporary db: %v", err)
	}

	if err := validateAndSanitizeRestoreDB(tempPath); err != nil {
		return common.NewErrorf("备份校验失败: %v", err)
	}

	// Check if we can init db or not
	newDb, err := gorm.Open(sqlite.Open(tempPath), &gorm.Config{})
	if err != nil {
		return common.NewErrorf("Error checking db: %v", err)
	}
	newDb_db, _ := newDb.DB()
	newDb_db.Close()

	// Flush and close the live DB only after the replacement has passed all validation.
	if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
		return common.NewErrorf("Error checkpointing current db: %v", err)
	}
	oldDB, oldDBErr := db.DB()
	if oldDBErr != nil {
		return common.NewErrorf("Error opening current db: %v", oldDBErr)
	}
	if err := oldDB.Close(); err != nil {
		return common.NewErrorf("Error closing current db: %v", err)
	}
	removeSQLiteSidecars(config.GetDBPath())

	// Backup the current database for fallback
	fallbackPath := fmt.Sprintf("%s.backup", config.GetDBPath())
	// Remove the existing fallback file (if any)
	_, err = os.Stat(fallbackPath)
	if err == nil {
		errRemove := os.Remove(fallbackPath)
		if errRemove != nil {
			reopenErr := InitDB(config.GetDBPath())
			return errors.Join(common.NewErrorf("Error removing existing fallback db file: %v", errRemove), reopenErr)
		}
	}
	removeSQLiteSidecars(fallbackPath)
	// Move the current database to the fallback location
	err = os.Rename(config.GetDBPath(), fallbackPath)
	if err != nil {
		reopenErr := InitDB(config.GetDBPath())
		return errors.Join(common.NewErrorf("Error backing up current db file: %v", err), reopenErr)
	}

	// Move temp to DB path
	err = os.Rename(tempPath, config.GetDBPath())
	if err != nil {
		restoreErr := restoreFallbackDatabase(config.GetDBPath(), fallbackPath)
		return errors.Join(common.NewErrorf("Error moving db file: %v", err), restoreErr)
	}

	// Migrate DB
	if err := migration.MigrateDb(); err != nil {
		restoreErr := restoreFallbackDatabase(config.GetDBPath(), fallbackPath)
		return errors.Join(common.NewErrorf("Error migrating db: %v", err), restoreErr)
	}
	err = InitDB(config.GetDBPath())
	if err != nil {
		restoreErr := restoreFallbackDatabase(config.GetDBPath(), fallbackPath)
		return errors.Join(common.NewErrorf("Error opening restored db: %v", err), restoreErr)
	}

	if err := cleanupRestoredInboundConflicts(); err != nil {
		logger.Warning("cleanup conflicted inbounds after restore failed:", err)
	}
	if err := cleanupRestoredEndpointConflicts(); err != nil {
		logger.Warning("cleanup conflicted endpoints after restore failed:", err)
	}
	logger.Info("database restore completed; fallback database kept at ", fallbackPath)

	// Restart app
	err = SendSighup()
	if err != nil {
		return common.NewErrorf("Error restarting app: %v", err)
	}

	return nil
}

func removeSQLiteSidecars(path string) {
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warning("remove sqlite sidecar failed:", err)
		}
	}
}

func restoreFallbackDatabase(dbPath, fallbackPath string) error {
	if db != nil {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	}
	if err := os.Remove(dbPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return common.NewErrorf("Error removing failed restored db: %v", err)
	}
	removeSQLiteSidecars(dbPath)
	if err := os.Rename(fallbackPath, dbPath); err != nil {
		return common.NewErrorf("Error restoring fallback db: %v", err)
	}
	if err := InitDB(dbPath); err != nil {
		return common.NewErrorf("Error reopening fallback db: %v", err)
	}
	return nil
}

// validateAndSanitizeRestoreDB checks the uploaded database before it can
// replace the live database. This keeps a bad backup from taking the panel
// offline and removes machine-specific TLS paths that cannot work locally.
func validateAndSanitizeRestoreDB(path string) error {
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, err := conn.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	var integrity string
	if err := conn.Raw("PRAGMA integrity_check").Scan(&integrity).Error; err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(integrity), "ok") {
		return fmt.Errorf("sqlite integrity_check: %s", integrity)
	}

	for _, table := range []string{"settings", "inbounds", "outbounds", "endpoints", "users", "clients"} {
		var count int64
		if err := conn.Raw("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", table).Scan(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("缺少数据表 %s", table)
		}
	}

	var settings []model.Setting
	if err := conn.Find(&settings).Error; err != nil {
		return err
	}
	values := make(map[string]string, len(settings))
	for _, setting := range settings {
		values[setting.Key] = setting.Value
	}
	if raw := strings.TrimSpace(values["fleetServers"]); raw != "" {
		var fleet []map[string]interface{}
		if err := json.Unmarshal([]byte(raw), &fleet); err != nil {
			return fmt.Errorf("服务器集合配置损坏: %w", err)
		}
		if fleet == nil {
			return fmt.Errorf("服务器集合配置必须是数组")
		}
	}

	updates := make(map[string]string)
	for _, pair := range [][2]string{
		{"webCertFile", "webKeyFile"},
		{"subCertFile", "subKeyFile"},
	} {
		cert, key := strings.TrimSpace(values[pair[0]]), strings.TrimSpace(values[pair[1]])
		if cert == "" && key == "" {
			continue
		}
		certInfo, certErr := os.Stat(cert)
		keyInfo, keyErr := os.Stat(key)
		if certErr != nil || keyErr != nil || certInfo.IsDir() || keyInfo.IsDir() || certInfo.Size() == 0 || keyInfo.Size() == 0 {
			updates[pair[0]], updates[pair[1]] = "", ""
			if pair[0] == "webCertFile" {
				updates["webDomain"] = ""
			} else {
				updates["subDomain"] = ""
			}
		}
	}
	if len(updates) == 0 {
		return nil
	}
	return conn.Transaction(func(tx *gorm.DB) error {
		for key, value := range updates {
			result := tx.Model(&model.Setting{}).Where("key = ?", key).Update("value", value)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				if err := tx.Create(&model.Setting{Key: key, Value: value}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ValidateDB copies and validates a backup without touching the live database.
// It is used by the restore preview so the user can inspect the backup first.
func ValidateDB(file multipart.File) (map[string]interface{}, error) {
	if valid, err := IsSQLiteDB(file); err != nil {
		return nil, common.NewErrorf("Error checking db file format: %v", err)
	} else if !valid {
		return nil, common.NewError("Invalid db file format")
	}
	if _, err := file.Seek(0, 0); err != nil {
		return nil, common.NewErrorf("Error resetting file reader: %v", err)
	}
	tempFile, err := os.CreateTemp(filepath.Dir(config.GetDBPath()), ".sui-restore-validate-*.db")
	if err != nil {
		return nil, err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	written, err := io.Copy(tempFile, io.LimitReader(file, maxDatabaseBackupSize+1))
	if err != nil {
		_ = tempFile.Close()
		return nil, err
	}
	if written > maxDatabaseBackupSize {
		_ = tempFile.Close()
		return nil, common.NewError("Database backup exceeds 1 GiB limit")
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return nil, err
	}
	if err := tempFile.Close(); err != nil {
		return nil, err
	}
	if err := validateAndSanitizeRestoreDB(tempPath); err != nil {
		return nil, common.NewErrorf("备份校验失败: %v", err)
	}

	conn, err := gorm.Open(sqlite.Open(tempPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := conn.DB()
	if err != nil {
		return nil, err
	}
	defer sqlDB.Close()
	report := map[string]interface{}{"valid": true}
	for _, table := range []string{"settings", "tls", "inbounds", "outbounds", "endpoints", "managed_port_entries", "services", "users", "tokens", "clients", "stats", "changes"} {
		var count int64
		if err := conn.Table(table).Count(&count).Error; err != nil {
			continue
		}
		report[table] = count
	}
	var fleetSetting model.Setting
	if err := conn.Where("key = ?", "fleetServers").First(&fleetSetting).Error; err == nil {
		var fleet []interface{}
		if json.Unmarshal([]byte(fleetSetting.Value), &fleet) == nil {
			report["fleet_servers"] = len(fleet)
		}
	}
	return report, nil
}

func IsSQLiteDB(file io.Reader) (bool, error) {
	signature := []byte("SQLite format 3\x00")
	buf := make([]byte, len(signature))
	_, err := file.Read(buf)
	if err != nil {
		return false, err
	}
	return bytes.Equal(buf, signature), nil
}

func SendSighup() error {
	// Get the current process
	process, err := os.FindProcess(os.Getpid())
	if err != nil {
		return err
	}

	// Send SIGHUP to the current process
	go func() {
		time.Sleep(3 * time.Second)
		var signalErr error
		if runtime.GOOS == "windows" {
			signalErr = process.Kill()
		} else {
			signalErr = process.Signal(syscall.SIGHUP)
		}
		if signalErr != nil {
			logger.Error("send signal SIGHUP failed:", signalErr)
		}
	}()
	return nil
}
