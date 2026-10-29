package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

type backupAPIStub struct {
	calls int
	saved service.RemoteBackupSettings
}

func (s *backupAPIStub) Status() (service.RemoteBackupStatus, error) {
	s.calls++
	return service.RemoteBackupStatus{Settings: service.RemoteBackupSettings{HasPassword: true}}, nil
}
func (s *backupAPIStub) Save(in service.RemoteBackupSettings) error {
	s.calls++
	s.saved = in
	return nil
}
func (s *backupAPIStub) Run(context.Context, string, string) (*service.RemoteBackupResult, error) {
	s.calls++
	return nil, errors.New("backup size or SHA-256 mismatch")
}

func backupAPIRouter(t *testing.T, stub *backupAPIStub, authenticated bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	old := remoteBackupServiceForAPI
	remoteBackupServiceForAPI = func() remoteBackupAPI { return stub }
	t.Cleanup(func() { remoteBackupServiceForAPI = old })
	r := gin.New()
	r.Use(sessions.Sessions("backup-test", cookie.NewStore([]byte("synthetic-session-key-only-for-tests"))))
	if authenticated {
		r.Use(func(c *gin.Context) { sessions.Default(c).Set(loginUser, "test-admin"); c.Next() })
	}
	NewAPIHandler(r.Group("/api"), nil)
	return r
}

func TestRemoteBackupAPIRequiresAuthentication(t *testing.T) {
	stub := &backupAPIStub{}
	r := backupAPIRouter(t, stub, false)
	for _, item := range []struct{ method, path, body string }{
		{"GET", "/api/remote-backup-status", ""},
		{"GET", "/api/getdb", ""},
		{"POST", "/api/importdb", ""},
		{"POST", "/api/remoteBackupSave", `{"password":"synthetic-secret"}`},
		{"POST", "/api/remoteBackupRun", `{"action":"download","artifact":"x.db"}`},
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
		req.Header.Set("X-Requested-With", "XMLHttpRequest")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://example.com")
		r.ServeHTTP(w, req)
		if w.Header().Get("Content-Disposition") != "" {
			t.Fatalf("unauthenticated endpoint returned attachment: %s", item.path)
		}
		var msg Msg
		if json.Unmarshal(w.Body.Bytes(), &msg) != nil || msg.Success || !strings.Contains(msg.Msg, "Invalid login") {
			t.Fatalf("unauthenticated endpoint: %s", w.Body.String())
		}
	}
	if stub.calls != 0 {
		t.Fatal("unauthenticated request reached backup service")
	}
}

func TestDatabaseImportRejectsCrossOriginSessionRequest(t *testing.T) {
	r := backupAPIRouter(t, &backupAPIStub{}, true)
	request := httptest.NewRequest(http.MethodPost, "http://example.com/api/importdb", nil)
	request.Header.Set("Origin", "https://evil.example")
	request.Header.Set("Content-Type", "multipart/form-data")
	recorder := httptest.NewRecorder()
	r.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden || !strings.Contains(recorder.Body.String(), "cross-origin request rejected") {
		t.Fatalf("cross-origin database import status=%d body=%s", recorder.Code, recorder.Body.String())
	}
}

func TestRemoteBackupAPIMasksSettingsAndRejectsCorruptDownload(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "live-test.db")); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := database.GetDB().DB()
	defer sqlDB.Close()
	if err := database.GetDB().Create(&model.Setting{Key: "test-marker", Value: "untouched"}).Error; err != nil {
		t.Fatal(err)
	}
	stub := &backupAPIStub{}
	r := backupAPIRouter(t, stub, true)
	request := func(method, path, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Origin", "http://example.com")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	w := request("POST", "/api/remoteBackupSave", `{"enabled":false,"password":"synthetic-secret"}`)
	if w.Code != http.StatusOK || stub.saved.Password == nil || *stub.saved.Password != "synthetic-secret" {
		t.Fatalf("save request: %s", w.Body.String())
	}
	if strings.Contains(w.Body.String(), "synthetic-secret") || strings.Contains(w.Body.String(), `"password"`) {
		t.Fatal("API echoed secret")
	}
	w = request("GET", "/api/remote-backup-status", "")
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("status response cache/security headers missing")
	}
	w = request("POST", "/api/remoteBackupRun", `{"action":"download","artifact":"npb.db"}`)
	var msg Msg
	if json.Unmarshal(w.Body.Bytes(), &msg) != nil || msg.Success || !strings.Contains(msg.Msg, "SHA-256") {
		t.Fatalf("corrupt download error: %s", w.Body.String())
	}
	if w.Header().Get("Content-Disposition") != "" {
		t.Fatal("failed validation returned a download")
	}
	var marker model.Setting
	if err := database.GetDB().Where("key = ?", "test-marker").First(&marker).Error; err != nil || marker.Value != "untouched" {
		t.Fatal("failed recovery download mutated live database")
	}
	for _, invalid := range []string{`{"password":"never-echo", "unexpected":true}`, `{"password":"never-echo"} {}`, strings.Repeat("x", (16<<10)+1)} {
		w = request("POST", "/api/remoteBackupSave", invalid)
		if strings.Contains(w.Body.String(), "never-echo") || !strings.Contains(w.Body.String(), "invalid remote backup request") {
			t.Fatal("invalid request reflected secret or was accepted")
		}
	}
}
