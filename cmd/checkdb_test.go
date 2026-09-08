package cmd

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckRuntimeDatabase(t *testing.T) {
	for _, kind := range []string{"missing", "valid", "wal", "corrupt", "empty", "unrelated", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "novas.db")
			switch kind {
			case "valid", "wal", "unrelated":
				db, err := sql.Open("sqlite3", path)
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				if kind == "wal" {
					if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
						t.Fatal(err)
					}
				}
				statement := "CREATE TABLE unrelated (id INTEGER)"
				if kind != "unrelated" {
					statement = "CREATE TABLE settings (key TEXT, value TEXT); CREATE TABLE users (username TEXT, password TEXT); INSERT INTO users VALUES ('test', 'test-only');"
				}
				if _, err := db.Exec(statement); err != nil {
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
