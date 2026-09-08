package api

import (
	"net/http/httptest"
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
