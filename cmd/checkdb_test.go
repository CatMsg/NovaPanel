package cmd

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCheckRuntimeDatabase(t *testing.T) {
	for _, kind := range []string{"missing", "valid", "wal", "corrupt", "empty", "unrelated", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "novas.db")
			switch kind {
			case "valid", "wal":
				if err := database.InitDB(path); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = database.CloseDB() }()
				if kind == "wal" {
					if err := database.GetDB().Exec("PRAGMA journal_mode=WAL").Error; err != nil {
						t.Fatal(err)
					}
				}
				if err := database.CloseDB(); err != nil {
					t.Fatal(err)
				}
			case "unrelated":
				db, err := sql.Open("sqlite3", path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if _, err := db.Exec("CREATE TABLE unrelated (id INTEGER)"); err != nil {
					t.Fatal(err)
				}
			case "corrupt", "empty":
				var content []byte
				if kind == "corrupt" {
					content = []byte("not sqlite")
				}
				if err := os.WriteFile(path, content, 0600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("missing.db", path); err != nil {
					t.Fatal(err)
				}
			}
			err := checkRuntimeDatabase(path)
			wantOK := kind == "missing" || kind == "valid" || kind == "wal"
			if (err == nil) != wantOK {
				t.Fatalf("unexpected validation: %v", err)
			}
			if kind == "missing" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("created a database")
				}
			}
		})
	}
}

func TestUpgradeRuntimeDatabaseAppliesStructuralMasquePatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "novas.db")
	if err := database.InitDB(path); err != nil {
		t.Fatal(err)
	}
	if err := database.CloseDB(); err != nil {
		t.Fatal(err)
	}
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, column := range []string{"masque_tls_key", "masque_tls_certificate"} {
		if err := conn.Exec("ALTER TABLE inbounds DROP COLUMN " + column).Error; err != nil {
			t.Fatal(err)
		}
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := upgradeRuntimeDatabase(path); err != nil {
		t.Fatalf("structural upgrade failed: %v", err)
	}
	if err := checkRuntimeDatabase(path); err != nil {
		t.Fatalf("upgraded database did not pass full validation: %v", err)
	}
}
