package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-contrib/gzip"
	ginSessions "github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestLoginFailureTrackerUsesRollingWindow(t *testing.T) {
	const remoteIP = "192.0.2.10"
	if loginFailureWindow != 10*time.Minute || loginFailureLimit != 3 {
		t.Fatalf("login protection threshold = %d failures/%s, want 3 failures/10m", loginFailureLimit, loginFailureWindow)
	}
	loginFailures.Lock()
	loginFailures.entries = map[string]loginFailureState{
		remoteIP: {attempts: []time.Time{time.Now().Add(-loginFailureWindow - time.Second)}},
	}
	loginFailures.Unlock()
	t.Cleanup(func() { clearLoginFailures(remoteIP) })

	for range loginFailureLimit - 1 {
		recordLoginFailure(remoteIP, nil)
	}
	loginFailures.Lock()
	state := loginFailures.entries[remoteIP]
	loginFailures.Unlock()
	if len(state.attempts) != loginFailureLimit-1 || state.notified {
		t.Fatalf("unexpected state before threshold: %#v", state)
	}

	recordLoginFailure(remoteIP, nil)
	loginFailures.Lock()
	state = loginFailures.entries[remoteIP]
	loginFailures.Unlock()
	if len(state.attempts) != loginFailureLimit || !state.notified {
		t.Fatalf("unexpected state at threshold: %#v", state)
	}
}

func TestLoginVerificationLimiterBoundsWorkAndReleasesOnce(t *testing.T) {
	limiter := newLoginVerificationLimiter(2)

	releaseFirst, ok := limiter.tryAcquire()
	if !ok {
		t.Fatal("first login verification was rejected")
	}
	releaseSecond, ok := limiter.tryAcquire()
	if !ok {
		t.Fatal("second login verification was rejected")
	}
	if release, ok := limiter.tryAcquire(); ok {
		release()
		t.Fatal("login verification exceeded configured capacity")
	}

	releaseFirst()
	releaseFirst()
	release, ok := limiter.tryAcquire()
	if !ok {
		t.Fatal("released verification slot was not reusable")
	}
	release()
	releaseSecond()
}

func TestOverloadedLoginResponseIsRetryable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/login", nil)

	rejectOverloadedLogin(context)

	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("overloaded login status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
	if recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q, want 1", recorder.Header().Get("Retry-After"))
	}
	if !strings.Contains(recorder.Body.String(), "login temporarily unavailable") {
		t.Fatalf("overloaded login response is unclear: %s", recorder.Body.String())
	}
}

func TestLoginHandlerRejectsBeforeReadingBodyWhenAtCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousGate := loginVerificationGate
	loginVerificationGate = newLoginVerificationLimiter(1)
	t.Cleanup(func() { loginVerificationGate = previousGate })
	release, ok := loginVerificationGate.tryAcquire()
	if !ok {
		t.Fatal("failed to occupy login verification slot")
	}
	defer release()

	body := &loginBodyReadTracker{}
	request := httptest.NewRequest(http.MethodPost, "/api/login", nil)
	request.Body = io.NopCloser(body)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request

	(&ApiService{}).Login(context)

	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "1" {
		t.Fatalf("overloaded handler response: status=%d headers=%v", recorder.Code, recorder.Header())
	}
	if body.reads != 0 {
		t.Fatalf("overloaded handler read request body %d times", body.reads)
	}
}

func TestLoginBodyReadDeadlineBoundsSlowRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	readResult := make(chan error, 1)
	router := gin.New()
	router.Use(loginRequestBodyDeadlineMiddleware(100 * time.Millisecond))
	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.POST("/api/login", func(c *gin.Context) {
		_, err := io.ReadAll(c.Request.Body)
		readResult <- err
		c.Status(http.StatusNoContent)
	})
	server := httptest.NewServer(router)
	defer server.Close()

	connection, err := net.Dial("tcp", strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if _, err := io.WriteString(connection, "POST /api/login HTTP/1.1\r\nHost: test\r\nAccept-Encoding: gzip\r\nContent-Length: 2\r\nConnection: close\r\n\r\na"); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatalf("read response to slow request: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		t.Fatalf("slow request status = %d, want %d", response.StatusCode, http.StatusNoContent)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("slow request was not bounded by its read deadline: %s", elapsed)
	}
	select {
	case readErr := <-readResult:
		if readErr == nil {
			t.Fatal("slow request body read unexpectedly completed")
		}
	case <-time.After(time.Second):
		t.Fatal("handler remained blocked reading the slow request body")
	}
}

func TestLoginBodyReadDeadlineSupportsHTTP2(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(loginRequestBodyDeadlineMiddleware(time.Second))
	router.Use(gzip.Gzip(gzip.DefaultCompression))
	router.POST("/api/login", func(c *gin.Context) {
		_, _ = io.Copy(io.Discard, c.Request.Body)
		c.Status(http.StatusNoContent)
	})

	server := httptest.NewUnstartedServer(router)
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()

	request, err := http.NewRequest(http.MethodPost, server.URL+"/api/login", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("send HTTP/2 login request: %v", err)
	}
	defer response.Body.Close()
	if response.ProtoMajor != 2 || response.StatusCode != http.StatusNoContent {
		t.Fatalf("HTTP/2 login response = %s %d, want HTTP/2 204", response.Proto, response.StatusCode)
	}
}

type loginBodyReadTracker struct {
	reads int
}

func (r *loginBodyReadTracker) Read([]byte) (int, error) {
	r.reads++
	return 0, errors.New("body should not be read")
}

func TestBindLoginPayloadEnforcesBodyLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var payload struct {
		User string `form:"user" json:"user"`
		Pass string `form:"pass" json:"pass"`
	}
	body := `{"user":"admin","pass":"secret"}` + strings.Repeat(" ", maxLoginRequestBodyBytes-len(`{"user":"admin","pass":"secret"}`)+1)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	context.Request.Header.Set("Content-Type", "application/json")

	err := bindLoginPayload(context, &payload)
	var maxBytesErr *http.MaxBytesError
	if !errors.As(err, &maxBytesErr) {
		t.Fatalf("oversized login body error = %v, want *http.MaxBytesError", err)
	}
}

func TestBindLoginPayloadAcceptsJSONAndForm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name        string
		contentType string
		body        string
	}{
		{name: "json", contentType: "application/json", body: `{"user":"admin","pass":"secret"}`},
		{name: "form", contentType: "application/x-www-form-urlencoded", body: "user=admin&pass=secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload struct {
				User string `form:"user" json:"user"`
				Pass string `form:"pass" json:"pass"`
			}
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(tt.body))
			context.Request.Header.Set("Content-Type", tt.contentType)

			if err := bindLoginPayload(context, &payload); err != nil {
				t.Fatalf("bind login payload: %v", err)
			}
			if payload.User != "admin" || payload.Pass != "secret" {
				t.Fatalf("bound credentials = (%q, %q), want (admin, secret)", payload.User, payload.Pass)
			}
		})
	}
}

func TestLoginSessionSaveFailureDoesNotReportSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := cookie.NewStore([]byte(""))
	router := gin.New()
	router.Use(ginSessions.Sessions("login-session-test", store))
	router.POST("/login", func(c *gin.Context) {
		completeLogin(c, "admin", 0, initialSessionEpoch)
	})

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "http://panel.example/login", nil)
	router.ServeHTTP(recorder, request)

	var response Msg
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode login response: %v; body=%s", err, recorder.Body.String())
	}
	if recorder.Code != http.StatusOK || response.Success || response.Msg != "login temporarily unavailable" {
		t.Fatalf("session-save failure response = status %d, %#v", recorder.Code, response)
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("failed session save emitted cookies: %#v", cookies)
	}
}
