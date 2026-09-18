package service

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
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
	}

	raw, err := json.Marshal(template.Summary())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-value") {
		t.Fatalf("template summary exposed payload: %s", raw)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"clients", "inbounds", "tls", "config", "sourceHost"} {
		if _, exists := fields[key]; exists {
			t.Fatalf("template summary exposed %q payload: %s", key, raw)
		}
	}
}
