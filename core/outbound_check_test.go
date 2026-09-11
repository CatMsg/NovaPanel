package core

import "testing"

func TestParseCloudflareTrace(t *testing.T) {
	identity, err := parseCloudflareTrace([]byte("fl=29f83\nip=203.0.113.42\nloc=jp\ncolo=nrt\nwarp=off\n"))
	if err != nil {
		t.Fatalf("parse trace: %v", err)
	}
	if identity.PublicIP != "203.0.113.42" || identity.CountryCode != "JP" || identity.Colo != "NRT" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
}

func TestParseCloudflareTraceAcceptsIPv6AndMissingLocation(t *testing.T) {
	identity, err := parseCloudflareTrace([]byte("ip=2001:db8::5\nloc=\ncolo=\n"))
	if err != nil {
		t.Fatalf("parse trace: %v", err)
	}
	if identity.PublicIP != "2001:db8::5" || identity.CountryCode != "" || identity.Colo != "" {
		t.Fatalf("unexpected identity: %#v", identity)
	}
}

func TestParseCloudflareTraceRejectsInvalidIP(t *testing.T) {
	if _, err := parseCloudflareTrace([]byte("ip=not-an-ip\nloc=US\ncolo=SJC\n")); err == nil {
		t.Fatal("invalid trace was accepted")
	}
}
