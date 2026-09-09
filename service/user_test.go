package service

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

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
	if user.Username != "alpha" || user.Password != "beta" {
		t.Fatalf("unexpected first user after create: %#v", user)
	}

	if err := svc.UpdateFirstUser("gamma", "delta"); err != nil {
		t.Fatalf("update first user: %v", err)
	}

	user, err = svc.GetFirstUser()
	if err != nil {
		t.Fatalf("get first user after update: %v", err)
	}
	if user.Username != "gamma" || user.Password != "delta" {
		t.Fatalf("unexpected first user after update: %#v", user)
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
