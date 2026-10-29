package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestCredentialChangeInvalidatesExistingSessionCookie(t *testing.T) {
	t.Setenv("NOVAS_DEVELOPMENT_DEFAULT_ADMIN", "")
	if err := database.InitDB(filepath.Join(t.TempDir(), "session-epoch.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}
	gin.SetMode(gin.TestMode)
	userService := &service.UserService{}
	if epoch, err := userService.GetAuthSessionEpoch(); err != nil || epoch != initialSessionEpoch {
		t.Fatalf("initial auth session epoch = %d, err=%v", epoch, err)
	}

	router := gin.New()
	router.Use(sessions.Sessions("session-epoch-test", cookie.NewStore([]byte("synthetic-session-epoch-key"))))
	router.GET("/seed-legacy-session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set(loginUser, "admin")
		session.Options(sessionOptions(c))
		if err := session.Save(); err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	router.GET("/seed-current-session", func(c *gin.Context) {
		epoch, err := userService.GetAuthSessionEpoch()
		if err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		if err := SetLoginUser(c, "admin", 0, epoch); err != nil {
			c.AbortWithError(http.StatusInternalServerError, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
	NewAPIHandler(router.Group("/api"), nil)

	seedRecorder := httptest.NewRecorder()
	seedRequest := httptest.NewRequest(http.MethodGet, "http://panel.example/seed-legacy-session", nil)
	router.ServeHTTP(seedRecorder, seedRequest)
	if seedRecorder.Code != http.StatusNoContent {
		t.Fatalf("seed legacy session status = %d", seedRecorder.Code)
	}
	cookies := seedRecorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected one session cookie, got %d", len(cookies))
	}

	callUsers := func() *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "http://panel.example/api/users", nil)
		request.AddCookie(cookies[0])
		request.Header.Set("X-Requested-With", "XMLHttpRequest")
		router.ServeHTTP(recorder, request)
		return recorder
	}
	var beforeChange Msg
	if recorder := callUsers(); json.Unmarshal(recorder.Body.Bytes(), &beforeChange) != nil || !beforeChange.Success {
		t.Fatalf("legacy session should remain valid before credential change: %s", recorder.Body.String())
	}

	if err := userService.UpdateFirstUser("admin", "replacement-password"); err != nil {
		t.Fatalf("reset administrator password: %v", err)
	}
	if epoch, err := userService.GetAuthSessionEpoch(); err != nil || epoch != initialSessionEpoch+1 {
		t.Fatalf("auth session epoch after reset = %d, err=%v", epoch, err)
	}

	afterChange := callUsers()
	var denied Msg
	if json.Unmarshal(afterChange.Body.Bytes(), &denied) != nil || denied.Success || denied.Msg != "Invalid login" {
		t.Fatalf("old session remained valid after credential reset: %s", afterChange.Body.String())
	}
	var cleared bool
	for _, responseCookie := range afterChange.Result().Cookies() {
		if responseCookie.Name == cookies[0].Name && responseCookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("stale login cookie was not cleared")
	}

	currentSessionRecorder := httptest.NewRecorder()
	currentSessionRequest := httptest.NewRequest(http.MethodGet, "http://panel.example/seed-current-session", nil)
	router.ServeHTTP(currentSessionRecorder, currentSessionRequest)
	currentCookies := currentSessionRecorder.Result().Cookies()
	if currentSessionRecorder.Code != http.StatusNoContent || len(currentCookies) != 1 {
		t.Fatalf("create current session: status=%d cookies=%d", currentSessionRecorder.Code, len(currentCookies))
	}
	currentRequest := httptest.NewRequest(http.MethodGet, "http://panel.example/api/users", nil)
	currentRequest.AddCookie(currentCookies[0])
	currentRequest.Header.Set("X-Requested-With", "XMLHttpRequest")
	currentRecorder := httptest.NewRecorder()
	router.ServeHTTP(currentRecorder, currentRequest)
	var currentMsg Msg
	if json.Unmarshal(currentRecorder.Body.Bytes(), &currentMsg) != nil || !currentMsg.Success {
		t.Fatalf("new-epoch session was rejected: %s", currentRecorder.Body.String())
	}
}
