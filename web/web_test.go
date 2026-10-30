package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/gin-gonic/gin"
)

func TestPanelLoginResponseDisallowsFraming(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := database.InitDB(filepath.Join(t.TempDir(), "web-security.db")); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := database.CloseDB(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})

	settings := service.SettingService{}
	if _, err := settings.GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	server := NewServer()
	server.settingService = settings
	router, err := server.initRouter()
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/app/login", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /app/login status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", response.Header().Get("X-Frame-Options"))
	}
	if response.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" {
		t.Fatalf("Content-Security-Policy = %q, want frame-ancestors 'none'", response.Header().Get("Content-Security-Policy"))
	}

	for _, path := range []string{"/app/api/checkLogin", "/app/apiv2/status"} {
		apiResponse := httptest.NewRecorder()
		router.ServeHTTP(apiResponse, httptest.NewRequest(http.MethodGet, path, nil))
		if got := apiResponse.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("GET %s Cache-Control = %q, want no-store", path, got)
		}
		if got := apiResponse.Header().Get("Pragma"); got != "no-cache" {
			t.Errorf("GET %s Pragma = %q, want no-cache", path, got)
		}
	}
}
