package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-contrib/sessions/cookie"
	"github.com/gin-gonic/gin"
)

func TestRemovedFleetTemplateActionsCannotBeDispatched(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(sessions.Sessions("fleet-test", cookie.NewStore([]byte("synthetic-fleet-session-test-key"))))
	panel := &APIHandler{}
	remote := &APIv2Handler{}
	router.GET("/api/:getAction", panel.getHandler)
	router.POST("/api/:postAction", panel.postHandler)
	router.POST("/apiv2/:postAction", remote.postHandler)

	for _, item := range []struct{ method, path string }{
		{"GET", "/api/fleetTemplates"},
		{"POST", "/api/fleetTemplateCapture"},
		{"POST", "/api/fleetTemplateDelete"},
		{"POST", "/api/fleetTemplatePreview"},
		{"POST", "/api/fleetTemplateDeploy"},
		{"POST", "/apiv2/fleet-template-preview"},
		{"POST", "/apiv2/fleet-template-apply"},
	} {
		t.Run(item.path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(item.method, item.path, strings.NewReader(`{"id":"retired-template"}`))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			var response Msg
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatalf("invalid response: %v", err)
			}
			if response.Success || !strings.Contains(response.Msg, "unknown action") {
				t.Fatalf("removed action was accepted: %s", recorder.Body.String())
			}
		})
	}
}
