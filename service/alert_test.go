package service

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
