package service

import (
	"reflect"
	"testing"
	"time"
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
