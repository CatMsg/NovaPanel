package sub

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRequestedSubscriptionFormatNegotiatesClashClients(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, userAgent := range []string{
		"clash.meta",
		"mihomo/1.19.28",
		"OpenClash",
		"Shadowrocket/3000",
		"Stash/2.6",
	} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest("GET", "/sub/alice", nil)
		context.Request.Header.Set("User-Agent", userAgent)

		format, selected, negotiated := requestedSubscriptionFormat(context)
		if format != "clash" || !selected || !negotiated {
			t.Fatalf("unexpected result for %q: %q %v %v", userAgent, format, selected, negotiated)
		}
	}
}

func TestRequestedSubscriptionFormatPreservesExplicitFormat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/sub/alice?format=json", nil)
	context.Request.Header.Set("User-Agent", "clash.meta")

	format, selected, negotiated := requestedSubscriptionFormat(context)
	if format != "json" || !selected || negotiated {
		t.Fatalf("explicit format must win: %q %v %v", format, selected, negotiated)
	}
}

func TestRequestedSubscriptionFormatKeepsLegacyPlainOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/sub/alice", nil)
	context.Request.Header.Set("User-Agent", "curl/8.7.1")

	format, selected, negotiated := requestedSubscriptionFormat(context)
	if format != "" || selected || negotiated {
		t.Fatalf("legacy client must keep plain subscription: %q %v %v", format, selected, negotiated)
	}
}
