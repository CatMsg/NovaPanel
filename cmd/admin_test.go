package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/security"
	"github.com/CatMsg/NovaPanel/service"
)

func TestReadAdminPassword(t *testing.T) {
	for _, test := range []struct {
		name  string
		input string
		want  string
		valid bool
	}{
		{name: "newline", input: "  secret  \n", want: "  secret  ", valid: true},
		{name: "carriage return newline", input: "secret\r\n", want: "secret", valid: true},
		{name: "empty", input: "\n", valid: false},
		{name: "oversized", input: strings.Repeat("x", maxAdminPasswordInput+1), valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := readAdminPassword(strings.NewReader(test.input))
			if (err == nil) != test.valid {
				t.Fatalf("readAdminPassword() error = %v, valid = %v", err, test.valid)
			}
			if test.valid && got != test.want {
				t.Fatalf("readAdminPassword() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestAdminCommandsReturnInitializationErrors(t *testing.T) {
	blockedDBFolder := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blockedDBFolder, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOVAS_DB_FOLDER", blockedDBFolder)

	for name, run := range map[string]func() error{
		"reset":  func() error { return resetAdmin() },
		"update": func() error { return updateAdmin("admin", "secret") },
		"show":   func() error { return showAdmin() },
	} {
		t.Run(name, func(t *testing.T) {
			if err := run(); err == nil {
				t.Fatal("command succeeded despite database initialization failure")
			}
		})
	}
}

func TestResetAdminCredentialsUsesOneTimeRandomPassword(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "admin-reset.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.CloseDB() })

	password, err := resetAdminCredentials(&service.UserService{})
	if err != nil {
		t.Fatalf("reset admin credentials: %v", err)
	}
	if len(password) != 48 || password == "admin" {
		t.Fatalf("unexpected reset password format: %q", password)
	}

	var user model.User
	if err := database.GetDB().First(&user).Error; err != nil {
		t.Fatalf("read reset administrator: %v", err)
	}
	valid, needsUpgrade, err := security.VerifyPassword(user.Password, password)
	if err != nil || !valid || needsUpgrade || security.IsDefaultUserPassword(user.Username, user.Password) {
		t.Fatalf("reset credential was not securely stored: valid=%v upgrade=%v err=%v", valid, needsUpgrade, err)
	}
	if valid, _, err := security.VerifyPassword(user.Password, "admin"); err != nil || valid {
		t.Fatalf("default password still works after reset: valid=%v err=%v", valid, err)
	}
}
