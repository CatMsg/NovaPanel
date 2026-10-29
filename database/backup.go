package database

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// MaxDatabaseBackupSize is the maximum size of an imported database snapshot.
const MaxDatabaseBackupSize int64 = 1 << 30
const restoreValidationTimeout = 3 * time.Minute

var requestRestartAfterDatabaseRestore = SendSighup

func CreateDBBackup(exclude string) (string, func(), error) {
	return CreateDBBackupContext(context.Background(), exclude)
}

func CreateDBBackupContext(ctx context.Context, exclude string) (string, func(), error) {
	exclude_changes, exclude_stats := false, false
	for _, table := range strings.Split(exclude, ",") {
		if table == "changes" {
			exclude_changes = true
		} else if table == "stats" {
			exclude_stats = true
		}
	}

	// VACUUM creates its output with the process umask. A private directory
	// protects the snapshot even before we can chmod the finished file.
	dir, err := os.MkdirTemp("", ".novapanel-backup-*")
	if err != nil {
		return "", nil, err
	}
	dbPath := filepath.Join(dir, "snapshot.db")
	cleanup := func() {
		_ = os.Remove(dbPath)
		removeSQLiteSidecars(dbPath)
		_ = os.Remove(dir)
	}
	if err := db.WithContext(ctx).Exec("VACUUM INTO ?", dbPath).Error; err != nil {
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
	backupDb = backupDb.WithContext(ctx)
	// Never export credentials capable of uploading or deleting remote backups.
	// VACUUM below also removes deleted values from SQLite free pages.
	if err := backupDb.Exec("UPDATE settings SET value = '' WHERE key LIKE 'remoteBackup%'").Error; err != nil {
		closeAndCleanup()
		return "", nil, err
	}
	if exclude_stats {
		if err := backupDb.Exec("DELETE FROM stats").Error; err != nil {
			closeAndCleanup()
			return "", nil, err
		}
		if err := backupDb.Exec("DELETE FROM traffic_budget_samples").Error; err != nil {
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
	{
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

// RehearseDB opens only the caller's isolated snapshot, read-only and immutable.
// Unlike ImportDB/ValidateDB it neither sanitizes settings nor initializes DBs.
func RehearseDB(ctx context.Context, path string) (map[string]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, restoreValidationTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&immutable=1"}
	conn, err := sql.Open("sqlite3", u.String())
	if err != nil {
		return nil, errors.New("cannot open rehearsal database")
	}
	defer conn.Close()
	conn.SetMaxOpenConns(1)
	var integrity string
	if err := conn.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&integrity); err != nil || integrity != "ok" {
		return nil, errors.New("SQLite quick_check failed")
	}
	// Restore checks index consistency too; quick_check alone can accept an
	// artifact that import rejects after it has displaced older recovery points.
	if err := conn.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return nil, errors.New("SQLite integrity_check failed")
	}
	schemaDB, err := gorm.Open(sqlite.Open(u.String()), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		return nil, errors.New("cannot open backup schema read-only")
	}
	schemaSQL, err := schemaDB.DB()
	if err != nil {
		return nil, errors.New("cannot inspect backup schema")
	}
	schemaSQL.SetMaxOpenConns(1)
	defer schemaSQL.Close()
	if err := validateSchema(schemaDB.WithContext(ctx)); err != nil {
		return nil, fmt.Errorf("backup schema is not current: %w", err)
	}
	if err := validateIndexes(schemaDB.WithContext(ctx)); err != nil {
		return nil, fmt.Errorf("backup indexes are not current: %w", err)
	}
	required := map[string]string{
		"settings": "id,key,value", "users": "id,username,password",
		"inbounds": "id,type,tag,options", "outbounds": "id,type,tag,options",
		"endpoints": "id,type,tag,options", "clients": "id,name,config,inbounds",
	}
	report := make(map[string]int64)
	for table, columns := range required {
		var kind string
		if err := conn.QueryRowContext(ctx, "SELECT type FROM sqlite_master WHERE name = ?", table).Scan(&kind); err != nil || kind != "table" {
			return nil, fmt.Errorf("backup schema missing table %s", table)
		}
		// All identifiers originate in the fixed allowlist above, never the file.
		rows, err := conn.QueryContext(ctx, "SELECT "+columns+" FROM "+table+" LIMIT 0")
		if err != nil {
			return nil, fmt.Errorf("backup schema incompatible table %s", table)
		}
		_ = rows.Close()
		// Names alone are insufficient: reject lookalike tables with incompatible
		// SQLite column declarations, including a non-primary-key id.
		info, err := conn.QueryContext(ctx, "PRAGMA table_info("+table+")")
		if err != nil {
			return nil, errors.New("backup schema inspection failed")
		}
		types := map[string]string{}
		primaryID := false
		for info.Next() {
			var cid, notNull, pk int
			var name, kind string
			var defaultValue interface{}
			if err := info.Scan(&cid, &name, &kind, &notNull, &defaultValue, &pk); err != nil {
				_ = info.Close()
				return nil, errors.New("backup schema inspection failed")
			}
			types[name] = strings.ToLower(kind)
			if name == "id" && pk == 1 {
				primaryID = true
			}
		}
		infoErr := info.Err()
		_ = info.Close()
		if infoErr != nil {
			return nil, errors.New("backup schema inspection failed")
		}
		for _, column := range strings.Split(columns, ",") {
			expected := "text"
			if column == "id" {
				expected = "integer"
			}
			if column == "options" || column == "config" || column == "inbounds" {
				expected = "blob"
			}
			if types[column] != expected || !primaryID {
				return nil, fmt.Errorf("backup schema incompatible table %s", table)
			}
		}
		var count int64
		if err := conn.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&count); err != nil {
			return nil, errors.New("backup row count failed")
		}
		report[table] = count
	}
	settingRows, err := conn.QueryContext(ctx, "SELECT key FROM settings")
	if err != nil {
		return nil, errors.New("backup settings inspection failed")
	}
	presentSettings := make(map[string]struct{})
	for settingRows.Next() {
		var key string
		if err := settingRows.Scan(&key); err != nil {
			_ = settingRows.Close()
			return nil, errors.New("backup settings inspection failed")
		}
		presentSettings[key] = struct{}{}
	}
	if err := settingRows.Err(); err != nil {
		_ = settingRows.Close()
		return nil, errors.New("backup settings inspection failed")
	}
	if err := settingRows.Close(); err != nil {
		return nil, errors.New("backup settings inspection failed")
	}
	for _, key := range []string{
		"remoteBackupConfig", "remoteBackupOwner", "remoteBackupState",
		"webCertFile", "webKeyFile", "webDomain", "subCertFile", "subKeyFile", "subDomain",
		"maintenanceMode",
	} {
		if _, exists := presentSettings[key]; !exists {
			return nil, fmt.Errorf("backup settings are incomplete: missing %s", key)
		}
	}
	rows, err := conn.QueryContext(ctx, "SELECT value FROM settings WHERE key = ?", "fleetServers")
	if err != nil {
		return nil, errors.New("backup settings inspection failed")
	}
	defer rows.Close()
	for rows.Next() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var raw sql.NullString
		if err := rows.Scan(&raw); err != nil {
			return nil, errors.New("backup settings inspection failed")
		}
		if value := strings.TrimSpace(raw.String); value != "" {
			var fleet []map[string]interface{}
			if err := json.Unmarshal([]byte(value), &fleet); err != nil {
				return nil, errors.New("backup fleet configuration is invalid")
			}
			if fleet == nil {
				return nil, errors.New("backup fleet configuration must be an array")
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, errors.New("backup settings inspection failed")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return report, nil
}

func ImportDB(file multipart.File) error {
	return ImportDBWithMaintenance(file, false)
}

// ImportDBWithMaintenance preserves an existing pause in the replacement
// before the live connection is closed. Only the API captures lifecycle state.
func ImportDBWithMaintenance(file multipart.File, preserveMaintenance bool) error {
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

	dbPath := config.GetDBPath()
	tempFile, err := createRestoreTempFile(dbPath)
	if err != nil {
		return common.NewErrorf("Error creating temporary db file: %v", err)
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
	}()

	// Save uploaded file to temporary file
	written, err := io.Copy(tempFile, io.LimitReader(file, MaxDatabaseBackupSize+1))
	if err != nil {
		return common.NewErrorf("Error saving db: %v", err)
	}
	if written > MaxDatabaseBackupSize {
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
	newSQLDB, err := newDb.DB()
	if err != nil {
		return common.NewErrorf("Error opening validated db: %v", err)
	}
	if preserveMaintenance {
		if err := preserveRestoreMaintenance(newDb); err != nil {
			_ = newSQLDB.Close()
			return common.NewError("Error preserving maintenance state in replacement db")
		}
	}
	if err := AdvanceRestoredAuthSessionEpoch(db, newDb); err != nil {
		_ = newSQLDB.Close()
		return common.NewError("Error advancing restored authentication session epoch")
	}
	if err := newSQLDB.Close(); err != nil {
		return common.NewErrorf("Error closing validated db: %v", err)
	}

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
	removeSQLiteSidecars(dbPath)

	// Backup the current database for fallback
	fallbackPath := fmt.Sprintf("%s.backup", dbPath)
	// Remove the existing fallback file (if any)
	_, err = os.Stat(fallbackPath)
	if err == nil {
		errRemove := os.Remove(fallbackPath)
		if errRemove != nil {
			reopenErr := InitDB(dbPath)
			return errors.Join(common.NewErrorf("Error removing existing fallback db file: %v", errRemove), reopenErr)
		}
	}
	removeSQLiteSidecars(fallbackPath)
	// Move the current database to the fallback location
	err = os.Rename(dbPath, fallbackPath)
	if err != nil {
		reopenErr := InitDB(dbPath)
		return errors.Join(common.NewErrorf("Error backing up current db file: %v", err), reopenErr)
	}

	// Move temp to DB path
	err = os.Rename(tempPath, dbPath)
	if err != nil {
		restoreErr := restoreFallbackDatabase(dbPath, fallbackPath)
		return errors.Join(common.NewErrorf("Error moving db file: %v", err), restoreErr)
	}

	err = InitDB(dbPath)
	if err != nil {
		restoreErr := restoreFallbackDatabase(dbPath, fallbackPath)
		return errors.Join(common.NewErrorf("Error opening restored db: %v", err), restoreErr)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return RotateAuthSessionEpoch(tx)
	}); err != nil {
		restoreErr := restoreFallbackDatabase(dbPath, fallbackPath)
		return errors.Join(common.NewError("Error rotating restored authentication session epoch"), restoreErr)
	}

	if err := cleanupRestoredInboundConflicts(); err != nil {
		logger.Warning("cleanup conflicted inbounds after restore failed:", err)
	}
	if err := cleanupRestoredEndpointConflicts(); err != nil {
		logger.Warning("cleanup conflicted endpoints after restore failed:", err)
	}
	logger.Info("database restore completed; fallback database kept at ", fallbackPath)

	// Restart app
	err = requestRestartAfterDatabaseRestore()
	if err != nil {
		return common.NewErrorf("Error restarting app: %v", err)
	}

	return nil
}

func createRestoreTempFile(dbPath string) (*os.File, error) {
	pattern := "." + filepath.Base(dbPath) + ".restore-*"
	return os.CreateTemp(filepath.Dir(dbPath), pattern)
}

func preserveRestoreMaintenance(conn *gorm.DB) error {
	return conn.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.Setting{}).Where("key = ?", "maintenanceMode").Update("value", "true")
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return errors.New("backup settings are incomplete: missing maintenanceMode")
		}
		return nil
	})
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
	ctx, cancel := context.WithTimeout(context.Background(), restoreValidationTimeout)
	defer cancel()
	// Import and rehearsal share the same read-only acceptance gate. Only a
	// real restore may subsequently sanitize machine-specific TLS paths.
	if _, err := RehearseDB(ctx, path); err != nil {
		return err
	}
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		return err
	}
	sqlDB, err := conn.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	conn = conn.WithContext(ctx)

	var settings []model.Setting
	if err := conn.Find(&settings).Error; err != nil {
		return err
	}
	values := make(map[string]string, len(settings))
	for _, setting := range settings {
		values[setting.Key] = setting.Value
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
				return fmt.Errorf("backup settings are incomplete: missing %s", key)
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
	tempFile, err := os.CreateTemp(filepath.Dir(config.GetDBPath()), ".novas-restore-validate-*.db")
	if err != nil {
		return nil, err
	}
	tempPath := tempFile.Name()
	defer os.Remove(tempPath)
	written, err := io.Copy(tempFile, io.LimitReader(file, MaxDatabaseBackupSize+1))
	if err != nil {
		_ = tempFile.Close()
		return nil, err
	}
	if written > MaxDatabaseBackupSize {
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
	for _, table := range []string{"settings", "tls", "inbounds", "outbounds", "endpoints", "managed_port_entries", "services", "users", "tokens", "clients", "stats", "traffic_budget_samples", "changes"} {
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
