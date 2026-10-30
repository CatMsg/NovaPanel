package service

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchPublicIPAcceptsIPv6OnlyServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("2001:4860:4860::8888\n"))
	}))
	defer server.Close()

	got := fetchPublicIP(server.Client(), []string{server.URL})
	if got != "2001:4860:4860::8888" {
		t.Fatalf("fetchPublicIP() = %q, want public IPv6 literal", got)
	}
}

func TestNormalizePublicIPAcceptsIPv4AndRejectsNonPublicValues(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "public IPv4", input: " 1.1.1.1\n", want: "1.1.1.1"},
		{name: "public IPv6", input: "2001:4860:4860::8888", want: "2001:4860:4860::8888"},
		{name: "private IPv4", input: "10.0.0.1"},
		{name: "unique local IPv6", input: "fd12:3456::1"},
		{name: "documentation IPv6", input: "2001:db8::1"},
		{name: "malformed", input: "not an IP"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := normalizePublicIP(test.input); got != test.want {
				t.Fatalf("normalizePublicIP(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestFetchPublicIPRejectsNonPublicResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("fd12:3456::1\n"))
	}))
	defer server.Close()

	if got := fetchPublicIP(server.Client(), []string{server.URL}); got != "" {
		t.Fatalf("fetchPublicIP() = %q, want empty for a non-public address", got)
	}
}

func TestFetchPublicIPDoesNotFollowRedirects(t *testing.T) {
	destinationRequests := 0
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinationRequests++
		_, _ = w.Write([]byte("1.1.1.1\n"))
	}))
	defer destination.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer redirector.Close()

	if got := fetchPublicIP(newPublicIPHTTPClient(), []string{redirector.URL}); got != "" {
		t.Fatalf("fetchPublicIP() = %q, want empty for a redirect response", got)
	}
	if destinationRequests != 0 {
		t.Fatalf("redirect destination received %d requests", destinationRequests)
	}
}
