package database

import (
	"bytes"
	"encoding/json"
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

func GetDb(exclude string) ([]byte, error) {
	exclude_changes, exclude_stats := false, false
	for _, table := range strings.Split(exclude, ",") {
		if table == "changes" {
			exclude_changes = true
		} else if table == "stats" {
			exclude_stats = true
		}
	}

	dir, err := filepath.Abs(filepath.Dir(os.Args[0]))
	if err != nil {
		return nil, err
	}
	dbPath := dir + config.GetName() + "_" + time.Now().Format("20060102-200203") + ".db"

	backupDb, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	defer os.Remove(dbPath)

	err = backupDb.AutoMigrate(
		&model.Setting{},
		&model.Tls{},
		&model.Inbound{},
		&model.Outbound{},
		&model.Endpoint{},
		&model.ManagedPortEntry{},
		&model.User{},
		&model.Stats{},
		&model.Client{},
		&model.Changes{},
	)
	if err != nil {
		return nil, err
	}

	var settings []model.Setting
	var tls []model.Tls
	var inbound []model.Inbound
	var outbound []model.Outbound
	var endpoint []model.Endpoint
	var users []model.User
	var clients []model.Client
	var stats []model.Stats
	var changes []model.Changes

	// Perform scans and handle errors
	if err := db.Model(&model.Setting{}).Scan(&settings).Error; err != nil {
		return nil, err
	} else if len(settings) > 0 {
		if err := backupDb.Save(settings).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.Tls{}).Scan(&tls).Error; err != nil {
		return nil, err
	} else if len(tls) > 0 {
		if err := backupDb.Save(tls).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.Inbound{}).Scan(&inbound).Error; err != nil {
		return nil, err
	} else if len(inbound) > 0 {
		if err := backupDb.Save(inbound).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.Outbound{}).Scan(&outbound).Error; err != nil {
		return nil, err
	} else if len(outbound) > 0 {
		if err := backupDb.Save(outbound).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.Endpoint{}).Scan(&endpoint).Error; err != nil {
		return nil, err
	} else if len(endpoint) > 0 {
		if err := backupDb.Save(endpoint).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.User{}).Scan(&users).Error; err != nil {
		return nil, err
	} else if len(users) > 0 {
		if err := backupDb.Save(users).Error; err != nil {
			return nil, err
		}
	}
	if err := db.Model(&model.Client{}).Scan(&clients).Error; err != nil {
		return nil, err
	} else if len(clients) > 0 {
		if err := backupDb.Save(clients).Error; err != nil {
			return nil, err
		}
	}

	if !exclude_stats {
		if err := db.Model(&model.Stats{}).Scan(&stats).Error; err != nil {
			return nil, err
		}
		if len(stats) > 0 {
			if err := backupDb.Save(stats).Error; err != nil {
				return nil, err
			}
		}
	}
	if !exclude_changes {
		if err := db.Model(&model.Changes{}).Scan(&changes).Error; err != nil {
			return nil, err
		}
		if len(changes) > 0 {
			if err := backupDb.Save(changes).Error; err != nil {
				return nil, err
			}
		}
	}

	// Update WAL
	err = backupDb.Exec("PRAGMA wal_checkpoint;").Error
	if err != nil {
		return nil, err
	}

	bdb, _ := backupDb.DB()
	bdb.Close()

	// Open the file for reading
	file, err := os.Open(dbPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Read the file contents
	fileContents, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}

	return fileContents, nil
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

	// Close old DB
	old_db, _ := db.DB()
	old_db.Close()

	// Save uploaded file to temporary file
	_, err = io.Copy(tempFile, file)
	if err != nil {
		return common.NewErrorf("Error saving db: %v", err)
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

	// Backup the current database for fallback
	fallbackPath := fmt.Sprintf("%s.backup", config.GetDBPath())
	// Remove the existing fallback file (if any)
	_, err = os.Stat(fallbackPath)
	if err == nil {
		errRemove := os.Remove(fallbackPath)
		if errRemove != nil {
			return common.NewErrorf("Error removing existing fallback db file: %v", errRemove)
		}
	}
	// Move the current database to the fallback location
	err = os.Rename(config.GetDBPath(), fallbackPath)
	if err != nil {
		return common.NewErrorf("Error backing up temporary db file: %v", err)
	}

	// Move temp to DB path
	err = os.Rename(tempPath, config.GetDBPath())
	if err != nil {
		errRename := os.Rename(fallbackPath, config.GetDBPath())
		if errRename != nil {
			return common.NewErrorf("Error moving db file and restoring fallback: %v", errRename)
		}
		return common.NewErrorf("Error moving db file: %v", err)
	}

	// Migrate DB
	migration.MigrateDb()
	err = InitDB(config.GetDBPath())
	if err != nil {
		errRename := os.Rename(fallbackPath, config.GetDBPath())
		if errRename != nil {
			return common.NewErrorf("Error migrating db and restoring fallback: %v", errRename)
		}
		return common.NewErrorf("Error migrating db: %v", err)
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
		if runtime.GOOS == "windows" {
			err = process.Kill()
		} else {
			err = process.Signal(syscall.SIGHUP)
		}
		if err != nil {
			logger.Error("send signal SIGHUP failed:", err)
		}
	}()
	return nil
}
