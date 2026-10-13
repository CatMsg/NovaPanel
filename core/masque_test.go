package core

import (
	"context"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/sagernet/sing-box/adapter"
	transport "github.com/sagernet/sing-box/transport/masque"
	M "github.com/sagernet/sing/common/metadata"
)

func TestManagedMASQUEIdentityLease(t *testing.T) {
	router := &masqueRouter{
		users:  map[string]struct{}{"alice": {}},
		leases: make(map[netip.Addr]*masqueLease),
	}
	address := netip.MustParseAddr("172.29.0.2")
	peer := M.ParseSocksaddr("203.0.113.9:54321")
	if _, err := router.OnSessionOpened(context.Background(), "unknown", peer, []netip.Addr{address}); err == nil {
		t.Fatal("unconfigured identity accepted")
	}
	release, err := router.OnSessionOpened(context.Background(), "alice", peer, []netip.Addr{address})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if _, err := router.OnSessionOpened(context.Background(), "alice", peer, []netip.Addr{address}); err == nil {
		t.Fatal("active address was reassigned")
	}
	conn, client := net.Pipe()
	defer client.Close()
	metadata := adapter.InboundContext{Source: M.SocksaddrFrom(address, 10000), Network: "tcp"}
	ctx, bound, _, err := router.bind(metadata, conn)
	if err != nil || bound.User != "alice" || bound.Source != peer {
		t.Fatalf("authenticated metadata not preserved: user=%q source=%s err=%v", bound.User, bound.Source, err)
	}
	if got := router.PreMatch(metadata, nil).Action; got != adapter.PreMatchContinue {
		t.Fatalf("managed TCP bypasses ordinary accounting: %v", got)
	}
	if err := router.AllowPeerRoutes(ctx, "alice", []transport.AddressRange{{}}); err == nil {
		t.Fatal("peer route advertisement accepted")
	}
	if err := router.AllowPeerForwarding(ctx, "alice", address); err == nil {
		t.Fatal("tunnel-to-tunnel accounting bypass accepted")
	}
	release()
	if ctx.Err() == nil {
		t.Fatal("closed lease did not cancel its connection context")
	}
	if _, err := client.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("lease did not close active connection: %v", err)
	}
	if _, _, _, err := router.bind(metadata, nil); err == nil {
		t.Fatal("released source identity remained usable")
	}
	if got := router.PreMatch(metadata, nil).Action; got != adapter.PreMatchReject {
		t.Fatalf("unknown source not rejected: %v", got)
	}
	second, err := router.OnSessionOpened(context.Background(), "alice", peer, []netip.Addr{address})
	if err != nil {
		t.Fatal(err)
	}
	defer second()
	release()
	if _, _, _, err := router.bind(metadata, nil); err != nil {
		t.Fatal("stale cleanup removed a newer lease")
	}
}

func TestManagedMASQUEHTTP3Loopback(t *testing.T) {
	if !http3AvailableInBuild() {
		t.Skip("requires with_quic; release race gate must run this case")
	}
	initHTTPProxyTestDatabase(t)
	tlsFiles := makeHTTPProxyTestTLS(t)
	echo := startHTTPProxyEchoEndpoints(t)
	port := portFromAddress(t, reserveLoopbackPort(t))
	server := startHTTPProxyTestCore(t, marshalHTTPProxyTestConfig(map[string]any{
		"endpoints": []any{map[string]any{
			"type": "masque-server", "tag": "test-http", "listen": "127.0.0.1", "listen_port": port,
			"version": []int{3}, "system": false, "address": []string{"172.29.0.1/29"},
			"users": []map[string]string{{"username": httpProxyTestUser, "password": httpProxyTestPassword}},
			"tls":   map[string]any{"enabled": true, "certificate_path": tlsFiles.certificatePath, "key_path": tlsFiles.keyPath},
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	}))
	clientConfig := func(password string) []byte {
		return marshalHTTPProxyTestConfig(map[string]any{
			"endpoints": []any{map[string]any{
				"type": "masque-client", "tag": "native-client", "server": "127.0.0.1", "server_port": port,
				"version": 3, "disable_version_fallback": true, "system": false, "on_demand": true,
				"username": httpProxyTestUser, "password": password,
				"tls": map[string]any{"enabled": true, "server_name": "localhost", "certificate_path": tlsFiles.rootPath},
			}},
			"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
			"route":     map[string]any{"final": "native-client"},
		})
	}
	client := startHTTPProxyTestBox(t, clientConfig(httpProxyTestPassword))
	proxy, ok := client.Endpoint().Get("native-client")
	if !ok {
		t.Fatal("native client endpoint missing")
	}
	// The server's tunnel address maps to its own loopback; a client-side
	// loopback destination would instead stay inside the client's IP stack.
	_, tcpPort, err := net.SplitHostPort(echo.tcpAddress)
	if err != nil {
		t.Fatal(err)
	}
	targets := httpProxyTestEcho{
		tcpAddress: net.JoinHostPort("172.29.0.1", tcpPort),
		udpAddress: &net.UDPAddr{IP: net.ParseIP("172.29.0.1"), Port: echo.udpAddress.Port},
	}
	assertHTTPProxyRoundTrip(t, proxy, server.GetInstance(), targets, 3, "native MASQUE")
	assertHTTPProxyHistory(t)
	if server.GetInstance().RateLimitTracker().limiter(httpProxyTestUser, true) == nil || server.GetInstance().RateLimitTracker().limiter(httpProxyTestUser, false) == nil {
		t.Fatal("native authenticated user has no shared upload/download limiter")
	}
	wrong := startHTTPProxyTestBox(t, clientConfig("incorrect-test-password"))
	wrongProxy, _ := wrong.Endpoint().Get("native-client")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if conn, err := wrongProxy.DialContext(ctx, "tcp", socksaddrForTest(t, targets.tcpAddress)); err == nil {
		conn.Close()
		t.Fatal("native HTTP/3 accepted an incorrect password")
	}
	if err := server.RemoveEndpoint("test-http"); err != nil {
		t.Fatal(err)
	}
	waitHTTPProxyCondition(t, 5*time.Second, func() bool {
		return len(server.GetInstance().ConnTracker().Sessions()) == 0
	})
}
