package sub

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
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

func TestRequestedUserSubscriptionFormatNegotiatesBrowserAccept(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name       string
		accept     string
		userAgent  string
		wantFormat string
		selected   bool
	}{
		{name: "browser", accept: "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", userAgent: "Mozilla/5.0", wantFormat: "info", selected: true},
		{name: "html refused", accept: "text/html;q=0,*/*;q=1", userAgent: "curl/8.7.1"},
		{name: "cli wildcard", accept: "*/*", userAgent: "curl/8.7.1"},
		{name: "clash wins over browser accept", accept: "text/html", userAgent: "Mihomo/1.19", wantFormat: "clash", selected: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context, _ := gin.CreateTestContext(httptest.NewRecorder())
			context.Request = httptest.NewRequest(http.MethodGet, "/sub/alice", nil)
			context.Request.Header.Set("Accept", test.accept)
			context.Request.Header.Set("User-Agent", test.userAgent)

			format, selected, negotiated := requestedUserSubscriptionFormat(context)
			if format != test.wantFormat || selected != test.selected || !negotiated {
				t.Fatalf("unexpected result: %q %v %v", format, selected, negotiated)
			}
		})
	}
}

func TestRequestedUserSubscriptionFormatExplicitRawAndInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, test := range []struct {
		query string
		want  string
	}{
		{query: "raw", want: "raw"},
		{query: "info", want: "info"},
		{query: "html", want: "html"},
	} {
		context, _ := gin.CreateTestContext(httptest.NewRecorder())
		context.Request = httptest.NewRequest(http.MethodGet, "/sub/alice?format="+test.query, nil)
		context.Request.Header.Set("Accept", "text/html")
		context.Request.Header.Set("User-Agent", "Mihomo/1.19")

		format, selected, negotiated := requestedUserSubscriptionFormat(context)
		if format != test.want || !selected || negotiated {
			t.Fatalf("explicit %q must win: %q %v %v", test.query, format, selected, negotiated)
		}
	}
}

func TestSubscriptionHandlerBrowserAndLegacyFormats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	if err := database.InitDB(filepath.Join(t.TempDir(), "subscription-handler.db")); err != nil {
		t.Fatalf("init database: %v", err)
	}
	client := model.Client{
		Enable:   true,
		Name:     "alice",
		Config:   []byte(`{}`),
		Inbounds: []byte(`[]`),
		Links:    []byte(`[{"type":"local","uri":"test://legacy"}]`),
		Volume:   10 << 30,
		Up:       1 << 30,
		Down:     2 << 30,
	}
	if err := database.GetDB().Create(&client).Error; err != nil {
		t.Fatalf("create client: %v", err)
	}
	if err := database.GetDB().Create(&[]model.Setting{{Key: "subEncode", Value: "false"}, {Key: "subShowInfo", Value: "false"}}).Error; err != nil {
		t.Fatalf("create subscription settings: %v", err)
	}
	disabled := model.Client{Enable: false, Name: "disabled", Config: []byte(`{}`), Inbounds: []byte(`[]`), Links: []byte(`[]`)}
	if err := database.GetDB().Create(&disabled).Error; err != nil {
		t.Fatalf("create disabled client: %v", err)
	}

	router := gin.New()
	NewSubHandler(router.Group("/sub"))

	request := func(target, accept, userAgent string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header.Set("Accept", accept)
		req.Header.Set("User-Agent", userAgent)
		router.ServeHTTP(recorder, req)
		return recorder
	}

	browser := request("/sub/alice", "text/html,application/xhtml+xml", "Mozilla/5.0")
	if browser.Code != http.StatusOK || !strings.HasPrefix(browser.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("browser response: status=%d content-type=%q body=%q", browser.Code, browser.Header().Get("Content-Type"), browser.Body.String())
	}
	if !strings.Contains(browser.Body.String(), `data-testid="client-name">alice`) {
		t.Fatalf("browser response did not render user center")
	}
	if browser.Header().Get("Cache-Control") != "private, no-store" || browser.Header().Get("Content-Security-Policy") == "" || browser.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("user center security headers are incomplete: %v", browser.Header())
	}
	if got := browser.Header().Get("Vary"); !strings.Contains(got, "Accept") || !strings.Contains(got, "User-Agent") {
		t.Fatalf("browser negotiation must vary on Accept and User-Agent: %q", got)
	}

	raw := request("/sub/alice?format=raw", "text/html", "Mozilla/5.0")
	if raw.Code != http.StatusOK || raw.Body.String() != "test://legacy" {
		t.Fatalf("explicit raw response regressed: status=%d body=%q", raw.Code, raw.Body.String())
	}

	legacy := request("/sub/alice", "*/*", "curl/8.7.1")
	if legacy.Code != http.StatusOK || legacy.Body.String() != raw.Body.String() {
		t.Fatalf("legacy response differs from explicit raw: legacy=%q raw=%q", legacy.Body.String(), raw.Body.String())
	}
	if got := legacy.Header().Get("Vary"); !strings.Contains(got, "Accept") || !strings.Contains(got, "User-Agent") {
		t.Fatalf("legacy negotiated response must vary on Accept and User-Agent: %q", got)
	}

	disabledInfo := request("/sub/disabled?format=info", "*/*", "curl/8.7.1")
	if disabledInfo.Code != http.StatusOK || !strings.Contains(disabledInfo.Body.String(), `data-testid="enabled-state"><span class="dot"></span>已停用`) {
		t.Fatalf("disabled client info did not render: status=%d body=%q", disabledInfo.Code, disabledInfo.Body.String())
	}

	jsonResponse := request("/sub/alice?format=json", "*/*", "sing-box/1.13")
	if jsonResponse.Code != http.StatusOK || !strings.Contains(jsonResponse.Body.String(), `"outbounds"`) {
		t.Fatalf("json response regressed: status=%d body=%q", jsonResponse.Code, jsonResponse.Body.String())
	}

	clash := request("/sub/alice?format=clash", "*/*", "curl/8.7.1")
	if clash.Code != http.StatusOK || !strings.Contains(clash.Body.String(), "proxies:") {
		t.Fatalf("clash response regressed: status=%d body=%q", clash.Code, clash.Body.String())
	}

	head := httptest.NewRecorder()
	headRequest := httptest.NewRequest(http.MethodHead, "/sub/alice", nil)
	router.ServeHTTP(head, headRequest)
	if head.Code != http.StatusOK || head.Header().Get("Subscription-Userinfo") != "upload=1073741824; download=2147483648; total=10737418240; expire=0" {
		t.Fatalf("HEAD response regressed: status=%d headers=%v", head.Code, head.Header())
	}
}
