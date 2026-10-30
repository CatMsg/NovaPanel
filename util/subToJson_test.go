package util

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExternalSubscriptionHTTPSVerifiesCertificatesByDefault(t *testing.T) {
	allowLocalExternalSubscriptionServer(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("ss://node.example:443"))
	}))
	defer server.Close()

	data, _ := GetExternalLinkWithHeadersTimeout(server.URL, 2*time.Second)
	if data != "" {
		t.Fatalf("untrusted HTTPS certificate was accepted: %q", data)
	}
}

func TestExternalSubscriptionHTTPSAcceptsTrustedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("ss://node.example:443"))
	}))
	defer server.Close()

	original := externalSubscriptionTransport
	transport := original.Clone()
	transport.DialContext = (&net.Dialer{}).DialContext
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	externalSubscriptionTransport = transport
	t.Cleanup(func() {
		externalSubscriptionTransport = original
		transport.CloseIdleConnections()
	})

	data, _ := GetExternalLinkWithHeadersTimeout(server.URL, 2*time.Second)
	if data != "ss://node.example:443" {
		t.Fatalf("trusted HTTPS source response = %q, want the node URI", data)
	}
}

func TestHTTPSExternalSubscriptionCannotDowngradeRedirect(t *testing.T) {
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		_, _ = writer.Write([]byte("unencrypted target reached"))
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	original := externalSubscriptionTransport
	transport := original.Clone()
	transport.DialContext = (&net.Dialer{}).DialContext
	roots := x509.NewCertPool()
	roots.AddCert(source.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	externalSubscriptionTransport = transport
	t.Cleanup(func() {
		externalSubscriptionTransport = original
		transport.CloseIdleConnections()
	})

	data, _ := GetExternalLinkWithHeadersTimeout(source.URL, 2*time.Second)
	if data != "" {
		t.Fatalf("HTTPS downgrade unexpectedly returned body %q", data)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("HTTP downgrade target received %d requests", got)
	}
}

func TestExternalSubscriptionAllowsExplicitInsecureHTTPS(t *testing.T) {
	allowLocalExternalSubscriptionServer(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/start" {
			http.Redirect(writer, request, "/finish", http.StatusFound)
			return
		}
		_, _ = writer.Write([]byte("ss://node.example:443"))
	}))
	defer server.Close()

	url := strings.Replace(server.URL+"/start", "https://", "insecure+https://", 1)
	data, _ := GetExternalLinkWithHeadersTimeout(url, 2*time.Second)
	if data != "ss://node.example:443" {
		t.Fatalf("explicit insecure HTTPS source response = %q, want the node URI", data)
	}
}

func TestInsecureExternalSubscriptionCannotRedirectToAnotherAuthority(t *testing.T) {
	allowLocalExternalSubscriptionServer(t)
	var targetRequests atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		_, _ = writer.Write([]byte("target reached"))
	}))
	defer target.Close()

	source := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	url := strings.Replace(source.URL, "https://", "insecure+https://", 1)
	data, _ := GetExternalLinkWithHeadersTimeout(url, 2*time.Second)
	if data != "" {
		t.Fatalf("cross-authority redirect unexpectedly returned body %q", data)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("cross-authority redirect reached target %d times", got)
	}
}

func TestExternalSubscriptionBlocksPrivateNetworkTargets(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = writer.Write([]byte("private response"))
	}))
	defer server.Close()

	data, _ := GetExternalLinkWithHeadersTimeout(server.URL, 2*time.Second)
	if data != "" {
		t.Fatalf("private subscription target returned data: %q", data)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("private subscription target received %d requests", got)
	}
}

func TestInternalExternalSubscriptionRequiresOptInAndPinsRedirectOrigin(t *testing.T) {
	allowLocalExternalSubscriptionServer(t)
	var targetRequests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		targetRequests.Add(1)
		_, _ = writer.Write([]byte("target reached"))
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	defer source.Close()

	url := strings.Replace(source.URL, "http://", "internal+http://", 1)
	data, _ := GetExternalLinkWithHeadersTimeout(url, 2*time.Second)
	if data != "" {
		t.Fatalf("cross-origin internal redirect unexpectedly returned body %q", data)
	}
	if got := targetRequests.Load(); got != 0 {
		t.Fatalf("cross-origin internal redirect reached target %d times", got)
	}
}

func TestExternalSubscriptionContextCancelsUpstreamRequest(t *testing.T) {
	allowLocalExternalSubscriptionServer(t)
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(started)
		<-request.Context().Done()
	}))
	t.Cleanup(server.Close)
	url := strings.Replace(server.URL, "http://", "internal+http://", 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan string, 1)
	go func() {
		data, _ := GetExternalLinkWithHeadersContext(ctx, url, 15*time.Second)
		done <- data
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream request did not start")
	}
	cancel()
	select {
	case data := <-done:
		if data != "" {
			t.Fatalf("canceled upstream unexpectedly returned data %q", data)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canceling the request context did not stop the upstream fetch")
	}
}

func TestValidateExternalSubscriptionIPsRejectsNonPublicAndReservedRanges(t *testing.T) {
	for _, value := range []string{
		"10.0.0.1",
		"100.64.0.1",
		"127.0.0.1",
		"169.254.1.1",
		"192.0.2.1",
		"198.18.0.1",
		"203.0.113.1",
		"::1",
		"fc00::1",
		"fe80::1",
		"2001:db8::1",
		"4000::1",
		"64:ff9b::a00:1",
	} {
		if err := validateExternalSubscriptionIPs([]netip.Addr{netip.MustParseAddr(value)}); err == nil {
			t.Errorf("non-public or reserved address %s was accepted", value)
		}
	}
	if err := validateExternalSubscriptionIPs([]netip.Addr{netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("10.0.0.1")}); err == nil {
		t.Fatal("mixed public/private DNS answers were accepted")
	}
	if err := validateExternalSubscriptionIPs([]netip.Addr{netip.MustParseAddr("1.1.1.1")}); err != nil {
		t.Fatalf("public address was rejected: %v", err)
	}
	if err := validateExternalSubscriptionIPs([]netip.Addr{netip.MustParseAddr("2606:4700:4700::1111")}); err != nil {
		t.Fatalf("public IPv6 address was rejected: %v", err)
	}
}

func TestExternalSubscriptionDialPinsValidatedResolvedIP(t *testing.T) {
	var lookedUpHost, dialedAddress string
	_, err := dialExternalSubscriptionWith(
		context.Background(),
		"tcp",
		"feed.example:443",
		func(_ context.Context, network, host string) ([]netip.Addr, error) {
			if network != "ip" {
				t.Fatalf("lookup network = %q, want ip", network)
			}
			lookedUpHost = host
			return []netip.Addr{netip.MustParseAddr("1.1.1.1")}, nil
		},
		func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" {
				t.Fatalf("dial network = %q, want tcp", network)
			}
			dialedAddress = address
			return nil, nil
		},
	)
	if err != nil {
		t.Fatalf("dial resolved public address: %v", err)
	}
	if lookedUpHost != "feed.example" || dialedAddress != "1.1.1.1:443" {
		t.Fatalf("lookup host=%q dial target=%q, want feed.example and pinned 1.1.1.1:443", lookedUpHost, dialedAddress)
	}
}

func TestExternalSubscriptionDialRejectsPrivateDNSAnswerBeforeDial(t *testing.T) {
	for _, address := range []string{"10.0.0.1", "4000::1"} {
		t.Run(address, func(t *testing.T) {
			dialed := false
			_, err := dialExternalSubscriptionWith(
				context.Background(),
				"tcp",
				"feed.example:443",
				func(context.Context, string, string) ([]netip.Addr, error) {
					return []netip.Addr{netip.MustParseAddr(address)}, nil
				},
				func(context.Context, string, string) (net.Conn, error) {
					dialed = true
					return nil, nil
				},
			)
			if err == nil || dialed {
				t.Fatalf("DNS answer %s err=%v dialed=%t, want rejection before dialing", address, err, dialed)
			}
		})
	}
}

func TestPrepareExternalSubscriptionURLRequiresAbsoluteHTTPURL(t *testing.T) {
	for _, raw := range []string{"", "/subscription", "ftp://example.com/subscription"} {
		if _, _, _, _, err := prepareExternalSubscriptionURL(raw); err == nil {
			t.Errorf("prepare %q unexpectedly succeeded", raw)
		}
	}
}

func allowLocalExternalSubscriptionServer(t *testing.T) {
	t.Helper()
	original := externalSubscriptionTransport
	transport := original.Clone()
	transport.DialContext = (&net.Dialer{}).DialContext
	externalSubscriptionTransport = transport
	t.Cleanup(func() {
		transport.CloseIdleConnections()
		externalSubscriptionTransport = original
	})
}
