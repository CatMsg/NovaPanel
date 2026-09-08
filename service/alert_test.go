package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
)

func TestSaveAlertSettingsRequiresTelegramAndPreservesToken(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "alerts.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	svc := &AlertService{}
	if err := svc.SaveAlertSettings(AlertSettings{
		Enabled:         true,
		IntervalMinutes: 5,
		CooldownMinutes: 60,
	}); err == nil {
		t.Fatal("expected enabled alerts without Telegram settings to fail")
	}

	if err := svc.SaveAlertSettings(AlertSettings{
		Enabled:         true,
		TelegramToken:   "test-token",
		TelegramChatID:  "123456",
		IntervalMinutes: 5,
		CooldownMinutes: 60,
	}); err != nil {
		t.Fatalf("save Telegram settings: %v", err)
	}

	if err := svc.SaveAlertSettings(AlertSettings{
		Enabled:         true,
		TelegramChatID:  "654321",
		IntervalMinutes: 10,
		CooldownMinutes: 30,
	}); err != nil {
		t.Fatalf("preserve existing Telegram token: %v", err)
	}

	settings, err := svc.GetAlertSettings()
	if err != nil {
		t.Fatalf("read Telegram settings: %v", err)
	}
	if !settings.Enabled || !settings.TelegramTokenSet || settings.TelegramChatID != "654321" {
		t.Fatalf("unexpected Telegram settings: %#v", settings)
	}
}

func TestPostAlertJSON(t *testing.T) {
	var payload map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	if err := postAlertJSON(server.URL, map[string]string{"title": "NovaPanel", "message": "test"}); err != nil {
		t.Fatalf("post alert: %v", err)
	}
	if payload["message"] != "test" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestPostAlertJSONRejectsFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()

	if err := postAlertJSON(server.URL, map[string]string{"message": "test"}); err == nil {
		t.Fatal("expected HTTP failure")
	}
}
