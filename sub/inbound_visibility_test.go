package sub

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/service"
	"github.com/gin-gonic/gin"
)

func TestExcludedInboundIsHiddenFromUserAndAggregateSubscriptions(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "inbound-visibility.db")); err != nil {
		t.Fatalf("initialize temporary database: %v", err)
	}
	db := database.GetDB()
	if sqlDB, err := db.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}

	inbounds := []model.Inbound{
		{
			Type:    "socks",
			Tag:     "visibility-visible-inbound",
			Options: json.RawMessage(`{"listen":"::","listen_port":1080}`),
			Addrs:   json.RawMessage(`[]`),
			OutJson: json.RawMessage(`{"type":"socks","tag":"visibility-visible-node","server":"visible.example","server_port":1080}`),
		},
		{
			Type:           "socks",
			Tag:            "visibility-hidden-inbound",
			ExcludeFromSub: true,
			Options:        json.RawMessage(`{"listen":"::","listen_port":2080}`),
			Addrs:          json.RawMessage(`[]`),
			OutJson:        json.RawMessage(`{"type":"socks","tag":"visibility-hidden-node","server":"hidden.example","server_port":2080}`),
		},
	}
	if err := db.Create(&inbounds).Error; err != nil {
		t.Fatalf("create test inbounds: %v", err)
	}
	inboundIDs, err := json.Marshal([]uint{inbounds[0].Id, inbounds[1].Id})
	if err != nil {
		t.Fatalf("encode test inbound IDs: %v", err)
	}
	links, err := json.Marshal([]Link{
		{Type: "local", Remark: inbounds[0].Tag, Uri: "socks://visible.example:1080#visibility-visible-local"},
		{Type: "local", Remark: inbounds[1].Tag, Uri: "socks://hidden.example:2080#visibility-hidden-local"},
		{Type: "local", Uri: "socks://legacy.example:1080#visibility-unknown-local"},
		{Type: "external", Remark: "external", Uri: "mierus://external-user:external-pass@external.example?profile=visibility-external-node&port=1080&protocol=TCP"},
	})
	if err != nil {
		t.Fatalf("encode test links: %v", err)
	}
	client := model.Client{
		Enable:   true,
		Name:     "visibility-user",
		Config:   json.RawMessage(`{"socks":{}}`),
		Inbounds: inboundIDs,
		Links:    links,
		Up:       123,
		Down:     456,
	}
	if err := db.Create(&client).Error; err != nil {
		t.Fatalf("create test client: %v", err)
	}
	runtimeInbounds, err := (&service.InboundService{}).GetAllConfig(db)
	if err != nil {
		t.Fatalf("build sing-box runtime inbounds: %v", err)
	}
	var hiddenRuntimeInbound map[string]interface{}
	for _, rawInbound := range runtimeInbounds {
		var runtimeInbound map[string]interface{}
		if err := json.Unmarshal(rawInbound, &runtimeInbound); err != nil {
			t.Fatalf("decode sing-box runtime inbound: %v", err)
		}
		if runtimeInbound["tag"] == inbounds[1].Tag {
			hiddenRuntimeInbound = runtimeInbound
		}
	}
	if hiddenRuntimeInbound == nil {
		t.Fatalf("subscription-only exclusion removed inbound %q from sing-box runtime config", inbounds[1].Tag)
	}
	if _, leaked := hiddenRuntimeInbound["exclude_from_sub"]; leaked {
		t.Fatalf("panel-only flag leaked into sing-box runtime inbound: %#v", hiddenRuntimeInbound)
	}
	if users, ok := hiddenRuntimeInbound["users"].([]interface{}); !ok || len(users) == 0 {
		t.Fatalf("hidden inbound lost its assigned client in runtime config: %#v", hiddenRuntimeInbound)
	}
	var before model.Client
	if err := db.First(&before, client.Id).Error; err != nil {
		t.Fatalf("read client before subscription generation: %v", err)
	}

	router := gin.New()
	NewSubHandler(router.Group("/sub"))
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	seedSubscriptionTestSettings(t,
		model.Setting{Key: "subEncode", Value: "false"},
		model.Setting{Key: "subShowInfo", Value: "false"},
		model.Setting{Key: "subUpdates", Value: "24"},
		model.Setting{Key: "subJsonExt", Value: "{}"},
		model.Setting{Key: "subClashExt", Value: ""},
		model.Setting{Key: "subMode", Value: "master"},
		model.Setting{Key: "subMasterSources", Value: server.URL + "/sub/" + client.Name},
		model.Setting{Key: "subURI", Value: server.URL + "/sub/"},
	)

	plain, _, err := (&SubService{}).GetSubs(client.Name)
	if err != nil {
		t.Fatalf("build plain subscription: %v", err)
	}
	assertSubscriptionContains(t, "plain", *plain, "visibility-visible-local", "visibility-external-node")
	assertSubscriptionOmits(t, "plain", *plain, "visibility-hidden-local", "visibility-unknown-local")

	jsonBody, _, err := (&JsonService{}).GetJson(client.Name, "json")
	if err != nil {
		t.Fatalf("build JSON subscription: %v", err)
	}
	assertSubscriptionContains(t, "JSON", *jsonBody, "visibility-visible-node", "visibility-external-node")
	assertSubscriptionOmits(t, "JSON", *jsonBody, "visibility-hidden-node", "visibility-hidden-inbound")

	clashBody, _, err := (&ClashService{}).GetClash(client.Name, "panel.example")
	if err != nil {
		t.Fatalf("build Clash subscription: %v", err)
	}
	assertSubscriptionContains(t, "Clash", *clashBody, "visibility-visible-node", "visibility-external-node")
	assertSubscriptionOmits(t, "Clash", *clashBody, "visibility-hidden-node", "visibility-hidden-inbound")

	for _, test := range []struct {
		format  string
		must    []string
		mustNot []string
	}{
		{format: "plain", must: []string{"visibility-visible-local", "visibility-external-node"}, mustNot: []string{"visibility-hidden-local", "visibility-unknown-local"}},
		{format: "json", must: []string{"visibility-visible-node", "visibility-external-node"}, mustNot: []string{"visibility-hidden-node", "visibility-hidden-inbound"}},
		{format: "clash", must: []string{"visibility-visible-node", "visibility-external-node"}, mustNot: []string{"visibility-hidden-node", "visibility-hidden-inbound"}},
	} {
		result, _, err := (&AggregateService{}).GetAggregate(test.format, "panel.example")
		if err != nil {
			t.Fatalf("build aggregate %s subscription: %v", test.format, err)
		}
		assertSubscriptionContains(t, "aggregate "+test.format, *result, test.must...)
		assertSubscriptionOmits(t, "aggregate "+test.format, *result, test.mustNot...)
	}

	if err := db.Model(&model.Inbound{}).Where("id = ?", inbounds[1].Id).Update("exclude_from_sub", false).Error; err != nil {
		t.Fatalf("make inbound visible: %v", err)
	}
	plain, _, err = (&SubService{}).GetSubs(client.Name)
	if err != nil {
		t.Fatalf("rebuild plain subscription after showing inbound: %v", err)
	}
	assertSubscriptionContains(t, "plain after show", *plain, "visibility-hidden-local")
	jsonBody, _, err = (&JsonService{}).GetJson(client.Name, "json")
	if err != nil {
		t.Fatalf("rebuild JSON subscription after showing inbound: %v", err)
	}
	assertSubscriptionContains(t, "JSON after show", *jsonBody, "visibility-hidden-node")
	clashBody, _, err = (&ClashService{}).GetClash(client.Name, "panel.example")
	if err != nil {
		t.Fatalf("rebuild Clash subscription after showing inbound: %v", err)
	}
	assertSubscriptionContains(t, "Clash after show", *clashBody, "visibility-hidden-node")
	if err := db.Model(&model.Inbound{}).Where("id = ?", inbounds[1].Id).Update("exclude_from_sub", true).Error; err != nil {
		t.Fatalf("hide inbound again: %v", err)
	}
	plain, _, err = (&SubService{}).GetSubs(client.Name)
	if err != nil {
		t.Fatalf("rebuild plain subscription after hiding inbound: %v", err)
	}
	assertSubscriptionOmits(t, "plain after hide", *plain, "visibility-hidden-local")
	jsonBody, _, err = (&JsonService{}).GetJson(client.Name, "json")
	if err != nil {
		t.Fatalf("rebuild JSON subscription after hiding inbound: %v", err)
	}
	assertSubscriptionOmits(t, "JSON after hide", *jsonBody, "visibility-hidden-node", "visibility-hidden-inbound")
	clashBody, _, err = (&ClashService{}).GetClash(client.Name, "panel.example")
	if err != nil {
		t.Fatalf("rebuild Clash subscription after hiding inbound: %v", err)
	}
	assertSubscriptionOmits(t, "Clash after hide", *clashBody, "visibility-hidden-node", "visibility-hidden-inbound")

	var after model.Client
	if err := db.First(&after, client.Id).Error; err != nil {
		t.Fatalf("read client after subscription generation: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("subscription generation mutated client identity or traffic: before=%+v after=%+v", before, after)
	}
	var stillRunning model.Inbound
	if err := db.First(&stillRunning, inbounds[1].Id).Error; err != nil {
		t.Fatalf("read excluded inbound after subscription generation: %v", err)
	}
	if !stillRunning.ExcludeFromSub || stillRunning.Tag != inbounds[1].Tag {
		t.Fatalf("subscription generation changed the inbound: %+v", stillRunning)
	}
}

func TestLegacyLocalLinkRemainsWhenNoInboundIsExcluded(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "legacy-local-link.db")); err != nil {
		t.Fatalf("initialize temporary database: %v", err)
	}
	seedSubscriptionTestSettings(t)
	if sqlDB, err := database.GetDB().DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}
	inbound := model.Inbound{Type: "socks", Tag: "legacy-visible", Options: json.RawMessage(`{"listen_port":1080}`)}
	if err := database.GetDB().Create(&inbound).Error; err != nil {
		t.Fatalf("create visible inbound: %v", err)
	}
	ids, err := json.Marshal([]uint{inbound.Id})
	if err != nil {
		t.Fatalf("encode inbound IDs: %v", err)
	}
	links := json.RawMessage(`[{"type":"local","uri":"socks://legacy.example:1080#legacy-local"}]`)
	client := model.Client{Inbounds: ids, Links: links}
	if err := filterExcludedInboundLinks(&client); err != nil {
		t.Fatalf("filter legacy local links: %v", err)
	}
	if string(client.Links) != string(links) {
		t.Fatalf("legacy local links changed without an excluded inbound: got %s, want %s", client.Links, links)
	}
}

func assertSubscriptionContains(t *testing.T, format, body string, values ...string) {
	t.Helper()
	for _, value := range values {
		if !strings.Contains(body, value) {
			t.Errorf("%s subscription is missing %q: %s", format, value, body)
		}
	}
}

func assertSubscriptionOmits(t *testing.T, format, body string, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(body, value) {
			t.Errorf("%s subscription unexpectedly contains %q: %s", format, value, body)
		}
	}
}
