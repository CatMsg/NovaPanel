package core

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

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

func TestOutboundIdentityHTTPClientDoesNotFollowRedirects(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	baseTransport, ok := source.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatal("test server client transport has unexpected type")
	}
	transport := baseTransport.Clone()
	defer transport.CloseIdleConnections()
	client := newOutboundIdentityHTTPClient(transport)

	response, err := client.Get(source.URL)
	if err != nil {
		t.Fatalf("request identity endpoint: %v", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusFound {
		t.Fatalf("response status = %d, want redirect status %d", response.StatusCode, http.StatusFound)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("redirect target received %d requests", got)
	}
}
