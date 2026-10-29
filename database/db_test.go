package database

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/security"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOpenDBCreatesPrivateDatabaseStorage(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "private-db")
	path := filepath.Join(dir, "novas.db")
	if err := InitDB(path); err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	if err := CloseDB(); err != nil {
		t.Fatalf("close database: %v", err)
	}
	if runtime.GOOS == "windows" {
		return // Windows ACLs are enforced by the installer, not chmod bits.
	}
	for target, want := range map[string]os.FileMode{dir: 0o700, path: 0o600} {
		info, err := os.Stat(target)
		if err != nil {
			t.Fatalf("stat %s: %v", target, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("permissions for %s = %04o, want %04o", target, got, want)
		}
	}

	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InitDB(path); err != nil {
		t.Fatalf("reopen database with permissive legacy mode: %v", err)
	}
	if err := CloseDB(); err != nil {
		t.Fatalf("close reopened database: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("legacy database permissions = %04o, want 0600", got)
	}
}

func TestOpenDBRejectsDatabaseSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires elevated Windows privileges in some environments")
	}
	target := filepath.Join(t.TempDir(), "target.db")
	if err := InitDB(target); err != nil {
		t.Fatalf("initialize target database: %v", err)
	}
	if err := CloseDB(); err != nil {
		t.Fatalf("close target database: %v", err)
	}
	link := filepath.Join(t.TempDir(), "linked.db")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if err := OpenDB(link); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatalf("expected database symlink rejection, got %v", err)
	}
}

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
	t.Setenv("NOVAS_DEVELOPMENT_DEFAULT_ADMIN", "1")
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
	if user.Password == "admin" {
		t.Fatal("fresh database stored the default administrator password in plaintext")
	}
	valid, needsUpgrade, err := security.VerifyPassword(user.Password, "admin")
	if err != nil || !valid || needsUpgrade || !security.IsDefaultUserPassword(user.Username, user.Password) {
		t.Fatalf("fresh administrator credential is not securely verifiable and detectable: valid=%v upgrade=%v err=%v", valid, needsUpgrade, err)
	}
}

func TestInitDBDoesNotSeedPublicAdminPasswordOutsideDevelopment(t *testing.T) {
	t.Setenv("NOVAS_DEVELOPMENT_DEFAULT_ADMIN", "")
	path := filepath.Join(t.TempDir(), "fresh-production.db")
	if err := InitDB(path); err != nil {
		t.Fatalf("initialize fresh database: %v", err)
	}
	var user model.User
	if err := GetDB().Where("username = ?", "admin").First(&user).Error; err != nil {
		t.Fatalf("fresh database has no initial administrator: %v", err)
	}
	if security.IsDefaultUserPassword(user.Username, user.Password) {
		t.Fatal("production database was seeded with the public default administrator credential")
	}
	valid, _, err := security.VerifyPassword(user.Password, "admin")
	if err != nil || valid {
		t.Fatalf("public default password still works: valid=%v err=%v", valid, err)
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
