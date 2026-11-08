package service

import (
	"reflect"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestSourceIPFromSessionSource(t *testing.T) {
	tests := map[string]string{
		"203.0.113.9:44321":   "203.0.113.9",
		"[2001:db8::5]:44321": "2001:db8::5",
		"198.51.100.7":        "198.51.100.7",
		"::ffff:192.0.2.9":    "192.0.2.9",
		"example.com:443":     "",
		"":                    "",
		"not-an-address":      "",
	}
	for source, want := range tests {
		if got := sourceIPFromSessionSource(source); got != want {
			t.Fatalf("sourceIPFromSessionSource(%q) = %q, want %q", source, got, want)
		}
	}
}

func TestApplyMieruSourceIPsMarksUserLevelAttribution(t *testing.T) {
	runtimeState := &mieruRuntime{}
	runtimeState.running.Store(true)
	mieru := &MieruService{
		inboundTag:       "mieru-in",
		active:           runtimeState,
		sourceIPs:        map[string][]string{"alice": {"198.51.100.8", "203.0.113.8"}},
		sourceIPsUpdated: time.Now(),
	}

	view := SessionView{Inbound: "mieru-in", User: "alice"}
	applyMieruSourceIPs(&view, mieru)
	if view.SourceIP != "" || view.SourceIPScope != "user" || !reflect.DeepEqual(view.SourceIPs, []string{"198.51.100.8", "203.0.113.8"}) {
		t.Fatalf("unexpected multi-device attribution: %#v", view)
	}

	mieru.sourceIPs = map[string][]string{"alice": {"203.0.113.8"}}
	view = SessionView{Inbound: "mieru-in", User: "alice"}
	applyMieruSourceIPs(&view, mieru)
	if view.SourceIP != "203.0.113.8" || view.SourceIPScope != "user" {
		t.Fatalf("unexpected single-device attribution: %#v", view)
	}

	view = SessionView{Inbound: "mieru-in", User: "alice", SourceIP: "192.0.2.4"}
	applyMieruSourceIPs(&view, mieru)
	if view.SourceIP != "192.0.2.4" || len(view.SourceIPs) != 0 {
		t.Fatalf("overwrote exact source IP attribution: %#v", view)
	}
}

func TestCombineSessionDNSResolvers(t *testing.T) {
	tests := []struct {
		name string
		a    string
		aaaa string
		want string
	}{
		{name: "same resolver", a: "dns-media", aaaa: "dns-media", want: "dns-media"},
		{name: "different resolvers", a: "dns-v4", aaaa: "dns-v6", want: "A: dns-v4 · AAAA: dns-v6"},
		{name: "IPv4 only", a: "dns-v4", want: "dns-v4"},
		{name: "IPv6 only", aaaa: "dns-v6", want: "dns-v6"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := combineSessionDNSResolvers(test.a, test.aaaa); got != test.want {
				t.Fatalf("combineSessionDNSResolvers() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestApplySessionUserDescriptions(t *testing.T) {
	initFleetInboundTestDB(t)
	alice := addFleetInboundTestClient(t, "alice", true, `{}`)
	alice.Desc = "主手机"
	if err := database.GetDB().Model(&model.Client{}).Where("id = ?", alice.Id).Update("desc", alice.Desc).Error; err != nil {
		t.Fatal(err)
	}
	bob := addFleetInboundTestClient(t, "bob", false, `{}`)
	bob.Desc = "备用设备"
	if err := database.GetDB().Model(&model.Client{}).Where("id = ?", bob.Id).Update("desc", bob.Desc).Error; err != nil {
		t.Fatal(err)
	}

	sessions := []SessionView{
		{User: "alice"},
		{User: "bob"},
		{User: "unknown"},
	}
	applySessionUserDescriptions(sessions)

	if sessions[0].UserDesc != "主手机" || sessions[1].UserDesc != "备用设备" || sessions[2].UserDesc != "" {
		t.Fatalf("unexpected session user descriptions: %#v", sessions)
	}
}
