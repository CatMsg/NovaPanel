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
		users:  map[string]struct{}{"alice": {}, "bob": {}},
		leases: make(map[netip.Addr][]*masqueLease),
		flows:  make(map[masqueFlowKey]masqueFlowEntry),
	}
	address := netip.MustParseAddr("172.29.0.2")
	peer1 := M.ParseSocksaddr("203.0.113.9:54321")
	if _, _, err := router.OnSessionOpened(context.Background(), "unknown", peer1, []netip.Addr{address}); err == nil {
		t.Fatal("unconfigured identity accepted")
	}
	ctx1, release1, err := router.OnSessionOpened(context.Background(), "alice", peer1, []netip.Addr{address})
	if err != nil {
		t.Fatal(err)
	}
	defer release1()
	peer2 := M.ParseSocksaddr("203.0.113.10:54322")
	ctx2, release2, err := router.OnSessionOpened(context.Background(), "alice", peer2, []netip.Addr{address})
	if err != nil {
		t.Fatalf("same user could not open a second session on its assigned address: %v", err)
	}
	defer release2()
	source1 := M.SocksaddrFrom(address, 10001)
	source2 := M.SocksaddrFrom(address, 10002)
	originalDestination := M.ParseSocksaddr("172.29.0.1:443")
	loopbackDestination := M.ParseSocksaddr("127.0.0.1:443")
	router.OnSessionPacket(ctx1, "alice", peer1, 6, source1, originalDestination)
	router.OnSessionPacket(ctx2, "alice", peer2, 6, source2, originalDestination)
	conn1, client1 := net.Pipe()
	defer client1.Close()
	metadata1 := adapter.InboundContext{Source: source1, Destination: loopbackDestination, OriginDestination: originalDestination, Network: "tcp"}
	boundCtx1, bound1, _, err := router.bind(metadata1, conn1)
	if err != nil || bound1.User != "alice" || bound1.Source != peer1 {
		t.Fatalf("first authenticated flow was not pinned: user=%q source=%s err=%v", bound1.User, bound1.Source, err)
	}
	conn2, client2 := net.Pipe()
	defer client2.Close()
	metadata2 := adapter.InboundContext{Source: source2, Destination: loopbackDestination, OriginDestination: originalDestination, Network: "tcp"}
	_, bound2, _, err := router.bind(metadata2, conn2)
	if err != nil || bound2.User != "alice" || bound2.Source != peer2 {
		t.Fatalf("second authenticated flow was attributed to the wrong device: user=%q source=%s err=%v", bound2.User, bound2.Source, err)
	}
	if got := router.PreMatch(metadata1, nil).Action; got != adapter.PreMatchContinue {
		t.Fatalf("managed TCP bypasses ordinary accounting: %v", got)
	}
	if err := router.AllowPeerRoutes(boundCtx1, "alice", []transport.AddressRange{{}}); err == nil {
		t.Fatal("peer route advertisement accepted")
	}
	if err := router.AllowPeerForwarding(boundCtx1, "alice", address); err == nil {
		t.Fatal("tunnel-to-tunnel accounting bypass accepted")
	}
	release1()
	if boundCtx1.Err() == nil {
		t.Fatal("closed lease did not cancel its connection context")
	}
	if _, err := client1.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("lease did not close active connection: %v", err)
	}
	if _, bound, _, err := router.bind(metadata2, nil); err != nil || bound.Source != peer2 {
		t.Fatalf("closing one device disconnected the other device: source=%s err=%v", bound.Source, err)
	}
	if _, _, err := router.OnSessionOpened(context.Background(), "bob", peer1, []netip.Addr{address}); err == nil {
		t.Fatal("a different user was allowed to reuse the assigned address")
	}
	secondCtx, secondRelease, err := router.OnSessionOpened(context.Background(), "alice", peer1, []netip.Addr{address})
	if err != nil {
		t.Fatal(err)
	}
	defer secondRelease()
	udpSource := M.SocksaddrFrom(address, 53001)
	udpDestination := M.ParseSocksaddr("203.0.113.53:53")
	router.OnSessionPacket(secondCtx, "alice", peer1, 17, udpSource, udpDestination)
	metadata3 := adapter.InboundContext{Source: udpSource, Destination: udpDestination, Network: "udp"}
	if got := router.PreMatch(metadata3, nil).Action; got != adapter.PreMatchContinue {
		t.Fatalf("active shared address was invalidated by stale cleanup: %v", got)
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
