package database

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func createCompleteTestSchema(t *testing.T, conn *gorm.DB) {
	t.Helper()
	if err := conn.Migrator().CreateTable(schemaModels...); err != nil {
		t.Fatal(err)
	}
	if err := createIndexes(conn); err != nil {
		t.Fatal(err)
	}
}

func TestInitDBCreatesAndValidatesFreshSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.db")
	if err := InitDB(path); err != nil {
		t.Fatalf("initialize fresh database: %v", err)
	}
	if err := InitDB(path); err != nil {
		t.Fatalf("validate initialized database: %v", err)
	}

	var outbound model.Outbound
	if err := GetDB().Where("tag = ?", "direct").First(&outbound).Error; err != nil {
		t.Fatalf("fresh database has no default direct outbound: %v", err)
	}
	var user model.User
	if err := GetDB().Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("fresh database has no initial administrator: %v", err)
	}
}

func TestInitDBRejectsPartialSchemaWithoutRepairing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.db")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("CREATE TABLE settings (id INTEGER PRIMARY KEY, key TEXT, value TEXT)").Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	err = InitDB(path)
	if err == nil || !strings.Contains(err.Error(), "automatic schema migration is not supported") {
		t.Fatalf("partial schema should fail explicitly, got %v", err)
	}
	if GetDB().Migrator().HasTable(&model.User{}) {
		t.Fatal("startup created missing tables in an existing database")
	}
}

func TestInitDBRejectsMissingIndexWithoutRepairing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-index.db")
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	if err := GetDB().Exec("DROP INDEX idx_settings_key").Error; err != nil {
		t.Fatal(err)
	}
	if err := InitDB(path); err == nil || !strings.Contains(err.Error(), "idx_settings_key") {
		t.Fatalf("missing index should fail explicitly, got %v", err)
	}
	if GetDB().Migrator().HasIndex(&model.Setting{}, "idx_settings_key") {
		t.Fatal("startup recreated a missing index in an existing database")
	}
}

func TestInitDBRejectsForeignDatabaseWithoutModifyingIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign.db")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("CREATE TABLE unrelated (id INTEGER PRIMARY KEY)").Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := InitDB(path); err == nil || !strings.Contains(err.Error(), "contains no NovaPanel schema") {
		t.Fatalf("foreign database should be rejected, got %v", err)
	}
	if GetDB().Migrator().HasTable(&model.Setting{}) {
		t.Fatal("initialization added NovaPanel tables to a foreign database")
	}
}

func TestInitDBRejectsMissingColumnWithoutRepairing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-column.db")
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	createCompleteTestSchema(t, conn)
	if err := conn.Exec("ALTER TABLE settings DROP COLUMN value").Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if err := InitDB(path); err == nil || !strings.Contains(err.Error(), "settings.value") {
		t.Fatalf("missing column should be rejected, got %v", err)
	}
	if GetDB().Migrator().HasColumn(&model.Setting{}, "value") {
		t.Fatal("initialization recreated a missing column")
	}
}

func TestMigrateMasqueIdentityColumnsPreservesData(t *testing.T) {
	path := filepath.Join(t.TempDir(), "masque-migration.db")
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	if err := CloseDB(); err != nil {
		t.Fatal(err)
	}
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("ALTER TABLE inbounds DROP COLUMN masque_tls_key").Error; err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("ALTER TABLE inbounds DROP COLUMN masque_tls_certificate").Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	if err := ValidateCurrentSchema(path); err == nil || !strings.Contains(err.Error(), "inbounds.masque_tls_certificate") {
		t.Fatalf("pre-upgrade schema should be rejected by validation, got %v", err)
	}
	applied, err := MigrateMasqueIdentityColumns(path)
	if err != nil || !applied {
		t.Fatalf("MASQUE identity migration did not run: applied=%v err=%v", applied, err)
	}
	if err := ValidateCurrentSchema(path); err != nil {
		t.Fatalf("migrated schema failed validation: %v", err)
	}
	if applied, err := MigrateMasqueIdentityColumns(path); err != nil || applied {
		t.Fatalf("migration is not idempotent: applied=%v err=%v", applied, err)
	}
	readOnly, err := sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer readOnly.Close()
	var users int
	if err := readOnly.QueryRow("SELECT COUNT(*) FROM users WHERE username = 'admin'").Scan(&users); err != nil || users != 1 {
		t.Fatalf("migration did not preserve existing user data: users=%d err=%v", users, err)
	}
}

func TestMigrateMasqueIdentityColumnsRepairsSingleMissingColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "partial.db")
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	if err := CloseDB(); err != nil {
		t.Fatal(err)
	}
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("ALTER TABLE inbounds DROP COLUMN masque_tls_key").Error; err != nil {
		t.Fatal(err)
	}
	sqlDB, err := conn.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}
	if applied, err := MigrateMasqueIdentityColumns(path); err != nil || !applied {
		t.Fatalf("single missing MASQUE column should be repaired: applied=%v err=%v", applied, err)
	}
	if err := ValidateCurrentSchema(path); err != nil {
		t.Fatalf("repaired schema did not validate: %v", err)
	}
}

func TestMigrateMasqueIdentityColumnsRejectsUnrelatedSchemaDifference(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unrelated-difference.db")
	if err := InitDB(path); err != nil {
		t.Fatal(err)
	}
	if err := CloseDB(); err != nil {
		t.Fatal(err)
	}
	conn, err := gorm.Open(sqlite.Open(path), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"ALTER TABLE inbounds DROP COLUMN masque_tls_key",
		"ALTER TABLE inbounds DROP COLUMN masque_tls_certificate",
		"ALTER TABLE settings DROP COLUMN value",
	} {
		if err := conn.Exec(statement).Error; err != nil {
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

	if applied, err := MigrateMasqueIdentityColumns(path); err == nil || applied || !strings.Contains(err.Error(), "settings.value") {
		t.Fatalf("unrelated schema difference should prevent migration: applied=%v err=%v", applied, err)
	}
	if err := ValidateCurrentSchema(path); err == nil || !strings.Contains(err.Error(), "settings.value") {
		t.Fatalf("rejected migration changed database schema: %v", err)
	}
}
