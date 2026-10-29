package api

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSameOriginAPIMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name           string
		method         string
		target         string
		origin         string
		forwarded      string
		forwardedSSL   string
		remoteAddr     string
		trustedProxies string
		secure         bool
		wantStatus     int
	}{
		{name: "same origin", method: http.MethodPost, target: "https://panel.example:9999/api/write", origin: "https://panel.example:9999", secure: true, wantStatus: http.StatusNoContent},
		{name: "same site sibling is cross origin", method: http.MethodPost, target: "https://panel.example:9999/api/write", origin: "https://evil.panel.example:9999", secure: true, wantStatus: http.StatusForbidden},
		{name: "scheme mismatch", method: http.MethodPost, target: "https://panel.example:9999/api/write", origin: "http://panel.example:9999", secure: true, wantStatus: http.StatusForbidden},
		{name: "port mismatch", method: http.MethodPost, target: "https://panel.example:9999/api/write", origin: "https://panel.example:443", secure: true, wantStatus: http.StatusForbidden},
		{name: "missing origin", method: http.MethodPost, target: "http://panel.example/api/write", wantStatus: http.StatusForbidden},
		{name: "null origin", method: http.MethodPost, target: "http://panel.example/api/write", origin: "null", wantStatus: http.StatusForbidden},
		{name: "origin path rejected", method: http.MethodPost, target: "http://panel.example/api/write", origin: "http://panel.example/attacker", wantStatus: http.StatusForbidden},
		{name: "default HTTPS port normalized", method: http.MethodPost, target: "https://panel.example/api/write", origin: "https://panel.example:443", secure: true, wantStatus: http.StatusNoContent},
		{name: "loopback proxy HTTPS", method: http.MethodPost, target: "http://panel.example:9999/api/write", origin: "https://panel.example:9999", forwarded: "https", remoteAddr: "127.0.0.1:8080", wantStatus: http.StatusNoContent},
		{name: "configured proxy HTTPS", method: http.MethodPost, target: "http://panel.example:9999/api/write", origin: "https://panel.example:9999", forwarded: "https", remoteAddr: "203.0.113.12:8080", trustedProxies: "203.0.113.0/24", wantStatus: http.StatusNoContent},
		{name: "untrusted forwarded proto cannot change scheme", method: http.MethodPost, target: "http://panel.example:9999/api/write", origin: "https://panel.example:9999", forwarded: "https", remoteAddr: "203.0.113.12:8080", wantStatus: http.StatusForbidden},
		{name: "untrusted forwarded ssl cannot change scheme", method: http.MethodPost, target: "http://panel.example:9999/api/write", origin: "https://panel.example:9999", forwardedSSL: "on", remoteAddr: "203.0.113.12:8080", wantStatus: http.StatusForbidden},
		{name: "safe GET does not require origin", method: http.MethodGet, target: "http://panel.example/api/read", wantStatus: http.StatusNoContent},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(sameOriginAPIMiddleware(func() (string, error) { return test.trustedProxies, nil }))
			router.Any("/api/*path", func(c *gin.Context) { c.Status(http.StatusNoContent) })

			request := httptest.NewRequest(test.method, test.target, nil)
			if test.remoteAddr != "" {
				request.RemoteAddr = test.remoteAddr
			}
			if test.origin != "" {
				request.Header.Set("Origin", test.origin)
			}
			if test.forwarded != "" {
				request.Header.Set("X-Forwarded-Proto", test.forwarded)
			}
			if test.forwardedSSL != "" {
				request.Header.Set("X-Forwarded-SSL", test.forwardedSSL)
			}
			if test.secure {
				request.TLS = &tls.ConnectionState{}
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestSessionCookieSecureUsesTrustedRequestScheme(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name           string
		method         string
		remoteAddr     string
		origin         string
		forwardedProto string
		trustedProxies string
		directTLS      bool
		wantSecure     bool
	}{
		{name: "direct TLS", remoteAddr: "203.0.113.12:8080", origin: "https://panel.example", directTLS: true, wantSecure: true},
		{name: "untrusted forwarded HTTPS", remoteAddr: "203.0.113.12:8080", origin: "http://panel.example", forwardedProto: "https", wantSecure: false},
		{name: "loopback proxy HTTPS", remoteAddr: "127.0.0.1:8080", origin: "https://panel.example", forwardedProto: "https", wantSecure: true},
		{name: "configured proxy HTTPS", remoteAddr: "203.0.113.12:8080", origin: "https://panel.example", forwardedProto: "https", trustedProxies: "203.0.113.0/24", wantSecure: true},
		{name: "safe request through configured HTTPS proxy", method: http.MethodGet, remoteAddr: "203.0.113.12:8080", forwardedProto: "https", trustedProxies: "203.0.113.0/24", wantSecure: true},
		{name: "safe request ignores untrusted HTTPS header", method: http.MethodGet, remoteAddr: "203.0.113.12:8080", forwardedProto: "https", wantSecure: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			router := gin.New()
			router.Use(sameOriginAPIMiddleware(func() (string, error) { return test.trustedProxies, nil }))
			router.Any("/", func(c *gin.Context) {
				if sessionOptions(c).Secure == test.wantSecure {
					c.Status(http.StatusNoContent)
					return
				}
				c.Status(http.StatusInternalServerError)
			})

			method := test.method
			if method == "" {
				method = http.MethodPost
			}
			request := httptest.NewRequest(method, "http://panel.example/", nil)
			request.RemoteAddr = test.remoteAddr
			request.Header.Set("Origin", test.origin)
			if test.forwardedProto != "" {
				request.Header.Set("X-Forwarded-Proto", test.forwardedProto)
			}
			if test.directTLS {
				request.TLS = &tls.ConnectionState{}
			}
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusNoContent {
				t.Fatalf("session Secure mismatch: status = %d", recorder.Code)
			}
		})
	}
}

func TestLogoutRequiresSameOriginPOST(t *testing.T) {
	router := backupAPIRouter(t, &backupAPIStub{}, false)
	for _, test := range []struct {
		method     string
		origin     string
		wantStatus int
	}{
		{method: http.MethodGet, origin: "http://example.com", wantStatus: http.StatusMethodNotAllowed},
		{method: http.MethodPost, origin: "https://evil.example", wantStatus: http.StatusForbidden},
		{method: http.MethodPost, origin: "http://example.com", wantStatus: http.StatusOK},
	} {
		request := httptest.NewRequest(test.method, "/api/logout", nil)
		request.Header.Set("Origin", test.origin)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		if recorder.Code != test.wantStatus {
			t.Errorf("%s logout with origin %q status = %d, want %d", test.method, test.origin, recorder.Code, test.wantStatus)
		}
	}
}
