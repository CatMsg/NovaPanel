package service

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"gorm.io/gorm"
)

func TestOrderFleetTemplateTargetsCanaryRemoteAndLocalLast(t *testing.T) {
	got := orderFleetTemplateTargets([]string{"local", "b", "a", "b"}, "a")
	want := []string{"a", "b", "local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderFleetTemplateTargets() = %#v, want %#v", got, want)
	}
}

func TestOrderFleetTemplateTargetsIgnoresUnknownCanary(t *testing.T) {
	got := orderFleetTemplateTargets([]string{"b", "local", "a"}, "missing")
	want := []string{"b", "a", "local"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("orderFleetTemplateTargets() = %#v, want %#v", got, want)
	}
}

func TestReplaceFleetTemplateHost(t *testing.T) {
	input := json.RawMessage(`{"server_name":"la.mile.news","certificate_path":"/etc/la.mile.news/fullchain.pem"}`)
	got := replaceFleetTemplateHost(input, "la.mile.news", "tk.mile.news")
	want := `{"server_name":"tk.mile.news","certificate_path":"/etc/tk.mile.news/fullchain.pem"}`
	if string(got) != want {
		t.Fatalf("replaceFleetTemplateHost() = %s, want %s", got, want)
	}
}

func TestFleetTemplateHostname(t *testing.T) {
	for input, want := range map[string]string{
		"tk.mile.news:9999":  "tk.mile.news",
		"[2001:db8::1]:9999": "2001:db8::1",
		"la.mile.news":       "la.mile.news",
	} {
		if got := fleetTemplateHostname(input); got != want {
			t.Fatalf("fleetTemplateHostname(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestFleetTemplateSummaryDoesNotExposeTemplatePayload(t *testing.T) {
	template := FleetTemplate{
		ID:        "template-id",
		Name:      "production",
		CreatedAt: time.Unix(10, 0),
		Sections:  FleetTemplateSections{Clients: true},
		Clients: []model.Client{{
			Name:   "alice",
			Config: json.RawMessage(`{"vless":{"uuid":"secret-value"}}`),
		}},
		Outbounds: []FleetTemplateOutbound{{
			Type: "socks", Tag: "media", Options: json.RawMessage(`{"server":"private-endpoint"}`),
		}},
	}

	raw, err := json.Marshal(template.Summary())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-value") || strings.Contains(string(raw), "private-endpoint") {
		t.Fatalf("template summary exposed payload: %s", raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"clients", "inbounds", "outbounds", "tls", "config", "sourceHost"} {
		if _, exists := fields[key]; exists {
			t.Fatalf("template summary exposed %q payload: %s", key, raw)
		}
	}
}

func TestCaptureFleetTemplateOutboundsKeepsOptionsAfterStorage(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fleet-capture.db")); err != nil {
		t.Fatal(err)
	}
	options := json.RawMessage(`{"server":"la.mile.news","server_port":1080,"username":"stream"}`)
	if err := database.GetDB().Create(&model.Outbound{Type: "socks", Tag: "stream", Options: options}).Error; err != nil {
		t.Fatal(err)
	}
	template, err := (&FleetService{}).CaptureFleetTemplate("streaming", FleetTemplateSections{Outbounds: true}, "la.mile.news")
	if err != nil {
		t.Fatal(err)
	}
	stored, err := (&FleetService{}).FindFleetTemplate(template.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stored.Sections.Outbounds || len(stored.Outbounds) != 2 {
		t.Fatalf("unexpected stored outbounds: %#v", stored.Outbounds)
	}
	var found bool
	for _, outbound := range stored.Outbounds {
		if outbound.Tag == "stream" {
			found = true
			if outbound.Type != "socks" || !equalJSONBytes(outbound.Options, options) {
				t.Fatalf("outbound options changed after storage: %#v", outbound)
			}
		}
	}
	if !found {
		t.Fatal("captured template omitted stream outbound")
	}
}

func TestFleetTemplateOutboundPreviewAndMerge(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fleet-outbound.db")); err != nil {
		t.Fatal(err)
	}
	db := database.GetDB()
	if err := db.Create(&model.Outbound{
		Type: "socks", Tag: "media", Options: json.RawMessage(`{"server":"old.example","server_port":1080}`),
	}).Error; err != nil {
		t.Fatal(err)
	}
	template := FleetTemplate{
		SourceHost: "la.mile.news",
		Sections:   FleetTemplateSections{Outbounds: true, Route: true},
		Outbounds: []FleetTemplateOutbound{
			{Type: "socks", Tag: "media", Options: json.RawMessage(`{"server":"la.mile.news","server_port":1080}`)},
			{Type: "socks", Tag: "stream", Options: json.RawMessage(`{"server":"stream.example","server_port":1081}`)},
		},
		Config: map[string]json.RawMessage{"route": json.RawMessage(`{"final":"stream","rules":[]}`)},
	}
	svc := &ConfigService{}
	preview, err := svc.PreviewFleetTemplate(template, "tk.mile.news")
	if err != nil {
		t.Fatalf("preview template: %v", err)
	}
	if preview.OutboundAdd != 1 || preview.OutboundUpdate != 1 {
		t.Fatalf("unexpected outbound preview: %+v", preview)
	}
	var count int64
	if err := db.Model(&model.Outbound{}).Where("tag = ?", "stream").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("preview modified target database: count=%d, err=%v", count, err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return svc.applyFleetTemplateTx(tx, template, "tk.mile.news")
	}); err != nil {
		t.Fatalf("apply template: %v", err)
	}
	for tag, want := range map[string]string{"media": "la.mile.news", "stream": "stream.example"} {
		var outbound model.Outbound
		if err := db.Where("tag = ?", tag).First(&outbound).Error; err != nil {
			t.Fatal(err)
		}
		var options map[string]interface{}
		if err := json.Unmarshal(outbound.Options, &options); err != nil {
			t.Fatal(err)
		}
		if options["server"] != want {
			t.Fatalf("%s server = %v, want %s", tag, options["server"], want)
		}
	}
	if err := db.Model(&model.Outbound{}).Count(&count).Error; err != nil || count != 3 {
		t.Fatalf("unrelated direct outbound removed: count=%d, err=%v", count, err)
	}
}

func TestFleetTemplateRejectsInvalidOutbounds(t *testing.T) {
	for name, outbounds := range map[string][]FleetTemplateOutbound{
		"empty tag":       {{Type: "direct"}},
		"duplicate tag":   {{Type: "direct", Tag: "same"}, {Type: "direct", Tag: "same"}},
		"invalid options": {{Type: "direct", Tag: "bad", Options: json.RawMessage(`[]`)}},
		"overwritten tag": {{Type: "direct", Tag: "bad", Options: json.RawMessage(`{"tag":"other"}`)}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateFleetTemplateOutbounds(outbounds); err == nil {
				t.Fatal("expected invalid outbound template to be rejected")
			}
		})
	}
}

func TestFleetTemplatePreviewRollsBackInvalidOutboundReferences(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "fleet-invalid-outbound.db")); err != nil {
		t.Fatal(err)
	}
	template := FleetTemplate{
		Sections: FleetTemplateSections{Outbounds: true},
		Outbounds: []FleetTemplateOutbound{{
			Type: "selector", Tag: "media", Options: json.RawMessage(`{"outbounds":["missing"]}`),
		}},
	}
	if _, err := (&ConfigService{}).PreviewFleetTemplate(template, "tk.mile.news"); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("expected missing outbound reference error, got %v", err)
	}
	var count int64
	if err := database.GetDB().Model(&model.Outbound{}).Where("tag = ?", "media").Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("preview retained invalid outbound: count=%d, err=%v", count, err)
	}
}

func TestFleetTemplatePreviewRejectsOlderRemoteWithoutOutboundCounts(t *testing.T) {
	template := FleetTemplate{Outbounds: []FleetTemplateOutbound{{Type: "direct", Tag: "direct"}}}
	if err := validateFleetTemplatePreviewSupport(template, &FleetTemplatePreview{}); err == nil {
		t.Fatal("expected old remote preview to be rejected")
	}
	if err := validateFleetTemplatePreviewSupport(template, &FleetTemplatePreview{OutboundUpdate: 1}); err != nil {
		t.Fatalf("current remote preview was rejected: %v", err)
	}
}
