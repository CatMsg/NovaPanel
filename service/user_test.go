package service

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/security"
)

func TestAdminPasswordIsNeverSerialized(t *testing.T) {
	user := model.User{Username: "admin", Password: "$novapanel$argon2id$sensitive-hash"}
	encoded, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), user.Password) {
		t.Fatalf("administrator password field leaked through JSON: %s", encoded)
	}
}

func TestUpdateFirstUserCreatesAndUpdatesUser(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "user-create-update.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	svc := &UserService{}
	if err := svc.UpdateFirstUser("alpha", "beta"); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	user, err := svc.GetFirstUser()
	if err != nil {
		t.Fatalf("get first user: %v", err)
	}
	valid, needsUpgrade, err := security.VerifyPassword(user.Password, "beta")
	if user.Username != "alpha" || user.Password == "beta" || err != nil || !valid || needsUpgrade {
		t.Fatalf("first user password was not securely stored: user=%#v valid=%v upgrade=%v err=%v", user, valid, needsUpgrade, err)
	}

	if err := svc.UpdateFirstUser("gamma", "delta"); err != nil {
		t.Fatalf("update first user: %v", err)
	}

	user, err = svc.GetFirstUser()
	if err != nil {
		t.Fatalf("get first user after update: %v", err)
	}
	valid, needsUpgrade, err = security.VerifyPassword(user.Password, "delta")
	if user.Username != "gamma" || user.Password == "delta" || err != nil || !valid || needsUpgrade {
		t.Fatalf("updated password was not securely stored: user=%#v valid=%v upgrade=%v err=%v", user, valid, needsUpgrade, err)
	}
}

func TestAuthSessionEpochRotatesWithCredentialChanges(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "user-session-epoch.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	svc := &UserService{}
	if epoch, err := svc.GetAuthSessionEpoch(); err != nil || epoch != initialAuthSessionEpoch {
		t.Fatalf("initial auth session epoch = %d, err=%v", epoch, err)
	}
	if err := svc.UpdateFirstUser("admin", "first-password"); err != nil {
		t.Fatalf("set first password: %v", err)
	}
	if epoch, err := svc.GetAuthSessionEpoch(); err != nil || epoch != 2 {
		t.Fatalf("epoch after password reset = %d, err=%v", epoch, err)
	}
	loginName, loginEpoch, err := svc.LoginWithSessionEpoch("admin", "first-password", "192.0.2.1")
	if err != nil || loginName != "admin" || loginEpoch != 2 {
		t.Fatalf("login epoch snapshot = %q/%d, err=%v", loginName, loginEpoch, err)
	}

	user, err := svc.GetFirstUser()
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ChangePass(strconv.FormatUint(uint64(user.Id), 10), "first-password", "admin", "second-password"); err != nil {
		t.Fatalf("change password: %v", err)
	}
	if epoch, err := svc.GetAuthSessionEpoch(); err != nil || epoch != 3 {
		t.Fatalf("epoch after password change = %d, err=%v", epoch, err)
	}
	if _, _, err := svc.LoginWithSessionEpoch("admin", "first-password", "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password remained valid: %v", err)
	}
	loginName, loginEpoch, err = svc.LoginWithSessionEpoch("admin", "second-password", "192.0.2.1")
	if err != nil || loginName != "admin" || loginEpoch != 3 {
		t.Fatalf("new login epoch snapshot = %q/%d, err=%v", loginName, loginEpoch, err)
	}

	settings, err := (&SettingService{}).GetAllSetting()
	if err != nil {
		t.Fatal(err)
	}
	if _, exposed := (*settings)[authSessionEpochKey]; exposed {
		t.Fatal("internal auth session epoch was exposed in panel settings")
	}
}

func TestAuthSessionEpochRejectsCorruptValue(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "user-session-epoch-corrupt.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	if err := database.GetDB().Create(&model.Setting{Key: authSessionEpochKey, Value: "not-a-number"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := (&UserService{}).GetAuthSessionEpoch(); err == nil {
		t.Fatal("corrupt auth session epoch was accepted")
	}
}

func TestLoginDistinguishesInvalidCredentials(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "user-login.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	svc := &UserService{}
	if err := svc.UpdateFirstUser("alpha", "beta"); err != nil {
		t.Fatalf("update first user: %v", err)
	}
	if _, err := svc.Login("alpha", "wrong", "192.0.2.1"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
	username, err := svc.Login("alpha", "beta", "192.0.2.1")
	if err != nil {
		t.Fatalf("login: %v", err)
	}
	if username != "alpha" {
		t.Fatalf("unexpected username: %q", username)
	}
	var stored model.User
	if err := database.GetDB().Where("username = ?", "alpha").First(&stored).Error; err != nil {
		t.Fatalf("read stored password: %v", err)
	}
	if stored.Password == "beta" {
		t.Fatal("login left the administrator password in plaintext")
	}
}

func TestLoginUpgradesLegacyPlaintextPassword(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "user-legacy-password.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	svc := &UserService{}
	if err := svc.UpdateFirstUser("legacy", "will-be-replaced"); err != nil {
		t.Fatalf("create test user: %v", err)
	}
	if err := database.GetDB().Model(&model.User{}).Where("username = ?", "legacy").Update("password", "legacy-cleartext").Error; err != nil {
		t.Fatalf("install legacy password fixture: %v", err)
	}
	if username, err := svc.Login("legacy", "legacy-cleartext", "192.0.2.1"); err != nil || username != "legacy" {
		t.Fatalf("legacy password login failed: username=%q err=%v", username, err)
	}
	var stored model.User
	if err := database.GetDB().Where("username = ?", "legacy").First(&stored).Error; err != nil {
		t.Fatalf("read upgraded password: %v", err)
	}
	valid, needsUpgrade, err := security.VerifyPassword(stored.Password, "legacy-cleartext")
	if err != nil || !valid || needsUpgrade || stored.Password == "legacy-cleartext" {
		t.Fatalf("legacy password was not migrated after login: valid=%v upgrade=%v err=%v", valid, needsUpgrade, err)
	}
}

func TestDefaultCredentialHealthSurvivesHashingAndPasswordChange(t *testing.T) {
	t.Setenv("NOVAS_DEVELOPMENT_DEFAULT_ADMIN", "1")
	if err := database.InitDB(filepath.Join(t.TempDir(), "user-default-health.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	health := &HealthService{}
	if check := health.checkDefaultCredentials(); check.Status != "error" {
		t.Fatalf("hashed default credentials should be flagged, got %#v", check)
	}
	svc := &UserService{}
	if err := svc.UpdateFirstUser("admin", "changed-admin-password"); err != nil {
		t.Fatalf("change default credentials: %v", err)
	}
	if check := health.checkDefaultCredentials(); check.Status != "ok" {
		t.Fatalf("changed credentials should pass the default credential check, got %#v", check)
	}
}

func TestAddTokenRejectsMissingUser(t *testing.T) {
	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "token-missing-user.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	svc := &UserService{}
	if _, err := svc.AddToken("does-not-exist", 0, "demo"); err == nil {
		t.Fatal("expected AddToken to fail for missing user")
	}

	var count int64
	if err := database.GetDB().Model(model.Tokens{}).Count(&count).Error; err != nil {
		t.Fatalf("count tokens: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no token rows for missing user, got %d", count)
	}
}
