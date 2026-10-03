package api

import (
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/gin-gonic/gin"
)

func TestGetDbPreventsCachingSensitiveBackup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := database.InitDB(filepath.Join(t.TempDir(), "backup.db")); err != nil {
		t.Fatalf("initialize database: %v", err)
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "/api/getdb", nil)

	(&ApiService{}).GetDb(context)

	if recorder.Code != 200 {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if got := recorder.Header().Get("Cache-Control"); got != "private, no-store" {
		t.Fatalf("Cache-Control = %q, want private, no-store", got)
	}
	if got := recorder.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
}
