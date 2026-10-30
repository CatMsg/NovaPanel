package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNoStoreAPIResponses(t *testing.T) {
	engine := gin.New()
	engine.Use(noStoreAPIResponses())
	engine.GET("/", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/", nil))
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", response.Header().Get("Cache-Control"))
	}
	if response.Header().Get("Pragma") != "no-cache" {
		t.Fatalf("Pragma = %q, want no-cache", response.Header().Get("Pragma"))
	}
}
