package service

import (
	"strings"
	"testing"
)

func TestNormalizeFleetURLRequiresHTTPSOutsideLoopback(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{name: "https domain", url: "https://fleet.example:9999"},
		{name: "https private address", url: "https://192.168.50.2:9999"},
		{name: "http localhost", url: "http://localhost:2095"},
		{name: "http localhost trailing dot", url: "http://localhost.:2095"},
		{name: "http ipv4 loopback", url: "http://127.0.0.1:2095"},
		{name: "http ipv6 loopback", url: "http://[::1]:2095"},
		{name: "http public domain", url: "http://fleet.example:9999", wantErr: true},
		{name: "http private address", url: "http://192.168.50.2:9999", wantErr: true},
		{name: "http public address", url: "http://203.0.113.10:9999", wantErr: true},
		{name: "lookalike localhost", url: "http://localhost.example:9999", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeFleetURL(test.url)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizeFleetURL(%q) error = %v, wantErr %v", test.url, err, test.wantErr)
			}
		})
	}
}

func TestFleetRequestsRefuseNonLoopbackHTTPBeforeSendingToken(t *testing.T) {
	fleet := &FleetService{}
	_, err := fleet.fetchFleetAPI("http://fleet.example:9999", "must-not-leak", "fleet-status", "")
	if err == nil || !strings.Contains(err.Error(), "必须使用 HTTPS") {
		t.Fatalf("fleet API request error = %v, want HTTPS policy rejection", err)
	}
	_, err = fleet.fetchFleetJSON("http://fleet.example:9999", "must-not-leak", "fleet-inbound-delete", struct{}{})
	if err == nil || !strings.Contains(err.Error(), "必须使用 HTTPS") {
		t.Fatalf("fleet JSON request error = %v, want HTTPS policy rejection", err)
	}
}
