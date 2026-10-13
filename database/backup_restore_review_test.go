package database

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func restoreReviewSnapshot(t *testing.T, fleet string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "snapshot.db")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	createCompleteTestSchema(t, conn)
	if err := conn.Create(&[]model.Setting{
		{Key: "fleetServers", Value: fleet},
		{Key: "remoteBackupConfig", Value: ""},
		{Key: "remoteBackupOwner", Value: ""},
		{Key: "remoteBackupState", Value: ""},
		{Key: "webCertFile", Value: ""},
		{Key: "webKeyFile", Value: ""},
		{Key: "webDomain", Value: ""},
		{Key: "subCertFile", Value: ""},
		{Key: "subKeyFile", Value: ""},
		{Key: "subDomain", Value: ""},
		{Key: "maintenanceMode", Value: "false"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRestoreReviewFleetValidationParity(t *testing.T) {
	for _, test := range []struct {
		name, value string
		valid       bool
	}{
		{"empty", "", true}, {"whitespace", "  ", true},
		{"array", "[]", true}, {"objects", `[{"id":"test-only"}]`, true},
		{"malformed", "synthetic-private-invalid-fleet", false},
		{"null", "null", false}, {"object", "{}", false},
		{"scalar", "1", false}, {"non_object_array", `["synthetic-private"]`, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := restoreReviewSnapshot(t, test.value)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			counts, rehearsalErr := RehearseDB(context.Background(), path)
			if (rehearsalErr == nil) != test.valid {
				t.Errorf("rehearsal acceptance differs from restore rules: accepted=%t", rehearsalErr == nil)
			}
			if rehearsalErr != nil && strings.Contains(rehearsalErr.Error(), "synthetic-private") {
				t.Error("validation error exposed private setting content")
			}
			if test.valid && counts["users"] != 0 {
				t.Error("rehearsal initialized a missing default user")
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("rehearsal modified snapshot bytes")
			}
			if restoreErr := validateAndSanitizeRestoreDB(path); (restoreErr == nil) != test.valid {
				t.Errorf("actual restore acceptance differs from rehearsal: accepted=%t", restoreErr == nil)
			}
		})
	}
}

func TestRestoreRejectsMissingCurrentSettingsWithoutBackfill(t *testing.T) {
	path := restoreReviewSnapshot(t, "[]")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Where("key = ?", "remoteBackupState").Delete(&model.Setting{}).Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := conn.DB()
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := RehearseDB(context.Background(), path); err == nil || !strings.Contains(err.Error(), "settings are incomplete") {
		t.Fatalf("missing required setting should be rejected during rehearsal, got %v", err)
	}
	if err := validateAndSanitizeRestoreDB(path); err == nil {
		t.Fatal("restore accepted a snapshot missing a required current setting")
	}
	check, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := check.Model(&model.Setting{}).Where("key = ?", "remoteBackupState").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	checkSQL, _ := check.DB()
	_ = checkSQL.Close()
	if count != 0 {
		t.Fatal("restore validation silently recreated a missing setting")
	}
}

func TestRestoreReviewRehearsalDoesNotSanitizeTLS(t *testing.T) {
	path := restoreReviewSnapshot(t, "[]")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.pem")
	for key, value := range map[string]string{"webCertFile": missing, "webKeyFile": missing, "webDomain": "test.invalid"} {
		if err := conn.Model(&model.Setting{}).Where("key = ?", key).Update("value", value).Error; err != nil {
			_ = sqlDB.Close()
			t.Fatal(err)
		}
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RehearseDB(context.Background(), path); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rehearsal sanitized machine-specific settings")
	}
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(path + suffix); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("rehearsal created a database sidecar")
		}
	}
	if err := validateAndSanitizeRestoreDB(path); err != nil {
		t.Fatal(err)
	}
	afterRestore, err := os.ReadFile(path)
	if err != nil || bytes.Equal(before, afterRestore) {
		t.Fatal("actual restore did not perform its TLS sanitization")
	}
}

func TestRestoreReviewRehearsalHonorsCancellation(t *testing.T) {
	path := restoreReviewSnapshot(t, "[]")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RehearseDB(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatal("rehearsal ignored cancellation")
	}
	ctx, cancel = context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if _, err := RehearseDB(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("rehearsal ignored the caller's deadline")
	}
}
