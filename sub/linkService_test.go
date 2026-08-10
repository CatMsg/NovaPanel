package sub

import (
	"net/url"
	"strings"
	"testing"
)

func TestAddClientInfoKeepsMieruTransportValid(t *testing.T) {
	service := &LinkService{}
	result := service.addClientInfo(
		"mierus://alice:secret@proxy.example.com?port=8443&profile=cn2-mieru&protocol=TCP",
		" ♾",
	)

	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatalf("parse Mieru link: %v", err)
	}
	if got := parsed.Query().Get("protocol"); got != "TCP" {
		t.Fatalf("Mieru transport was polluted: %q", got)
	}
	if got := parsed.Query().Get("profile"); got != "cn2-mieru ♾" {
		t.Fatalf("unexpected Mieru profile: %q", got)
	}
	if got := parsed.RawQuery; got == "" || strings.Contains(got, "+") {
		t.Fatalf("Mieru profile space must use percent encoding: %q", got)
	}
}

func TestAddClientInfoUsesMieruFragmentFallback(t *testing.T) {
	service := &LinkService{}
	result := service.addClientInfo(
		"mierus://alice:secret@proxy.example.com?port=8443&protocol=UDP#cn2-mieru",
		" 2Days⏳",
	)

	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatalf("parse Mieru link: %v", err)
	}
	if got := parsed.Query().Get("protocol"); got != "UDP" {
		t.Fatalf("Mieru transport was polluted: %q", got)
	}
	if got := parsed.Fragment; got != "cn2-mieru 2Days⏳" {
		t.Fatalf("unexpected Mieru fragment: %q", got)
	}
}

func TestAddClientInfoNormalizesStoredMieruProfileWithoutClientInfo(t *testing.T) {
	service := &LinkService{}
	result := service.addClientInfo(
		"mierus://alice:secret@proxy.example.com?port=8443&profile=cn2+mieru&protocol=TCP",
		"",
	)

	parsed, err := url.Parse(result)
	if err != nil {
		t.Fatalf("parse Mieru link: %v", err)
	}
	if got := parsed.Query().Get("profile"); got != "cn2 mieru" {
		t.Fatalf("unexpected normalized Mieru profile: %q", got)
	}
	if got := parsed.RawQuery; strings.Contains(got, "+") || !strings.Contains(got, "profile=cn2%20mieru") {
		t.Fatalf("stored Mieru profile was not normalized: %q", got)
	}
}
