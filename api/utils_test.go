package api

import "testing"

func TestResolveRemoteIPTrustBoundary(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		xff        string
		trusted    string
		want       string
	}{
		{name: "direct client ignores spoofed header", remoteAddr: "203.0.113.9:1234", xff: "198.51.100.7", want: "203.0.113.9"},
		{name: "local reverse proxy", remoteAddr: "127.0.0.1:1234", xff: "198.51.100.7", want: "198.51.100.7"},
		{name: "trusted proxy chain", remoteAddr: "127.0.0.1:1234", xff: "198.51.100.7, 10.2.3.4", trusted: "10.0.0.0/8", want: "198.51.100.7"},
		{name: "untrusted intermediate stops chain", remoteAddr: "127.0.0.1:1234", xff: "198.51.100.7, 192.0.2.5", trusted: "10.0.0.0/8", want: "192.0.2.5"},
		{name: "malformed header falls back to peer", remoteAddr: "127.0.0.1:1234", xff: "not-an-ip", want: "127.0.0.1"},
		{name: "ipv6", remoteAddr: "[::1]:1234", xff: "2001:db8::5", want: "2001:db8::5"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := resolveRemoteIP(test.remoteAddr, test.xff, test.trusted); got != test.want {
				t.Fatalf("resolveRemoteIP() = %q, want %q", got, test.want)
			}
		})
	}
}
