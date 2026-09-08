package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// Validate an offline database without rewriting settings or creating a file.
// A missing database is valid only for a fresh installation.
func checkRuntimeDatabase(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return errors.New("database must be a non-empty regular file")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(absolute), RawQuery: "mode=ro&_busy_timeout=3000"}).String()
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var result string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("database integrity check failed: %s", result)
	}
	// An unrelated SQLite file must not be accepted as an existing panel DB.
	for _, query := range []string{"SELECT key, value FROM settings LIMIT 0", "SELECT username, password FROM users LIMIT 0"} {
		rows, err := db.QueryContext(ctx, query)
		if err != nil {
			return fmt.Errorf("invalid panel schema: %w", err)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}
