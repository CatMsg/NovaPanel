package api

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestFindUsernameConcurrentAndExpiredTokenCleanup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &APIv2Handler{tokens: []TokenInMemory{
		{Token: "expired", Username: "old", Expiry: time.Now().Add(-time.Hour).Unix()},
		{Token: "valid", Username: "admin"},
	}}

	const readers = 32
	var waitGroup sync.WaitGroup
	waitGroup.Add(readers)
	for range readers {
		go func() {
			defer waitGroup.Done()
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("GET", "/", nil)
			context.Request.Header.Set("Token", "valid")
			if username := handler.findUsername(context); username != "admin" {
				t.Errorf("username = %q, want admin", username)
			}
		}()
	}
	waitGroup.Wait()

	handler.tokensMu.Lock()
	defer handler.tokensMu.Unlock()
	if len(handler.tokens) != 1 || handler.tokens[0].Token != "valid" {
		t.Fatalf("unexpected token cache: %#v", handler.tokens)
	}
}

func TestTokenExpiryBoundary(t *testing.T) {
	for _, test := range []struct {
		name   string
		expiry int64
		now    int64
		want   bool
	}{
		{name: "no expiry", expiry: 0, now: 100, want: false},
		{name: "not yet expired", expiry: 101, now: 100, want: false},
		{name: "expires at current second", expiry: 100, now: 100, want: true},
		{name: "expired earlier", expiry: 99, now: 100, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := tokenExpired(test.expiry, test.now); got != test.want {
				t.Fatalf("tokenExpired(%d, %d) = %t, want %t", test.expiry, test.now, got, test.want)
			}
		})
	}
}

func TestAPITokenMatchingRequiresExactValue(t *testing.T) {
	for _, test := range []struct {
		stored    string
		candidate string
		want      bool
	}{
		{stored: "synthetic-token", candidate: "synthetic-token", want: true},
		{stored: "synthetic-token", candidate: "synthetic-toke"},
		{stored: "synthetic-token", candidate: "synthetic-token-extra"},
		{stored: "synthetic-token", candidate: "prefix-synthetic-token"},
		{stored: "synthetic-token", candidate: ""},
	} {
		if got := apiTokenMatches(test.stored, test.candidate); got != test.want {
			t.Errorf("apiTokenMatches(%q, %q) = %t, want %t", test.stored, test.candidate, got, test.want)
		}
	}
}

func TestReloadTokensFailsClosed(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		name string
		load func() ([]byte, error)
	}{
		{name: "database error", load: func() ([]byte, error) { return nil, errors.New("database unavailable") }},
		{name: "malformed token data", load: func() ([]byte, error) { return []byte("{"), nil }},
	} {
		t.Run(test.name, func(t *testing.T) {
			handler := &APIv2Handler{tokens: []TokenInMemory{{Token: "old-token", Username: "admin"}}}
			handler.reloadTokens(test.load)

			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest("GET", "/", nil)
			context.Request.Header.Set("Token", "old-token")
			if username := handler.findUsername(context); username != "" {
				t.Fatalf("stale token still authenticated as %q", username)
			}
		})
	}
}

func TestAPIv2TokenCannotDownloadDatabaseBackup(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &APIv2Handler{tokens: []TokenInMemory{{Token: "synthetic-token", Username: "admin"}}}
	router := gin.New()
	handler.initRouter(router.Group("/apiv2"))

	for _, test := range []struct {
		token       string
		wantMessage string
	}{
		{token: "", wantMessage: "invalid token"},
		{token: "synthetic-token", wantMessage: "getdb"},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest("GET", "/apiv2/getdb", nil)
		if test.token != "" {
			request.Header.Set("Token", test.token)
		}
		router.ServeHTTP(recorder, request)
		if recorder.Header().Get("Content-Disposition") != "" {
			t.Fatalf("token %q unexpectedly received a database attachment", test.token)
		}
		var message Msg
		if err := json.Unmarshal(recorder.Body.Bytes(), &message); err != nil {
			t.Fatalf("token %q response is not JSON: %s", test.token, recorder.Body.String())
		}
		if message.Success || !strings.Contains(message.Msg, test.wantMessage) {
			t.Fatalf("token %q response = %#v, want message containing %q", test.token, message, test.wantMessage)
		}
	}
}

func TestAPIv2TokenCannotImportDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &APIv2Handler{tokens: []TokenInMemory{{Token: "synthetic-token", Username: "admin"}}}
	router := gin.New()
	handler.initRouter(router.Group("/apiv2"))

	request := httptest.NewRequest("POST", "/apiv2/importdb", nil)
	request.Header.Set("Token", "synthetic-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	var message Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &message); err != nil {
		t.Fatalf("response is not JSON: %s", recorder.Body.String())
	}
	if message.Success || !strings.Contains(message.Msg, "unknown action:") || !strings.Contains(message.Msg, "importdb") {
		t.Fatalf("APIv2 importdb response = %#v, want an unsupported-action error", message)
	}
}
