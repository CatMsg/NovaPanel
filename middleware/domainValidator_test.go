package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestDomainValidatorNormalizesDNSHost(t *testing.T) {
	engine := gin.New()
	engine.Use(DomainValidator("panel.example"))
	engine.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name string
		host string
		want int
	}{
		{name: "canonical", host: "panel.example", want: http.StatusNoContent},
		{name: "case insensitive", host: "PANEL.EXAMPLE", want: http.StatusNoContent},
		{name: "trailing dot", host: "panel.example.", want: http.StatusNoContent},
		{name: "port", host: "panel.example:9999", want: http.StatusNoContent},
		{name: "case and trailing dot with port", host: "PANEL.EXAMPLE.:9999", want: http.StatusNoContent},
		{name: "foreign suffix", host: "panel.example.attacker.test", want: http.StatusForbidden},
		{name: "invalid port", host: "panel.example:bad", want: http.StatusForbidden},
		{name: "zero port", host: "panel.example:0", want: http.StatusForbidden},
		{name: "out of range port", host: "panel.example:65536", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://panel.example/", nil)
			request.Host = tt.host
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != tt.want {
				t.Fatalf("status = %d, want %d", response.Code, tt.want)
			}
		})
	}
}

func TestDomainValidatorBracketedIPv6Host(t *testing.T) {
	engine := gin.New()
	engine.Use(DomainValidator("2001:db8::1"))
	engine.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	tests := []struct {
		name string
		host string
		want int
	}{
		{name: "bracketed literal", host: "[2001:db8::1]", want: http.StatusNoContent},
		{name: "bracketed literal with port", host: "[2001:db8::1]:9999", want: http.StatusNoContent},
		{name: "invalid bracketed hostname", host: "[panel.example]", want: http.StatusForbidden},
		{name: "invalid port", host: "[2001:db8::1]:bad", want: http.StatusForbidden},
		{name: "unbracketed literal", host: "2001:db8::1", want: http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://panel.example/", nil)
			request.Host = tt.host
			response := httptest.NewRecorder()
			engine.ServeHTTP(response, request)
			if response.Code != tt.want {
				t.Fatalf("status = %d, want %d", response.Code, tt.want)
			}
		})
	}
}
