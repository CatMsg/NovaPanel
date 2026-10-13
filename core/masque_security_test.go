package core

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/sagernet/sing-box/adapter"
	native "github.com/sagernet/sing-box/protocol/masque"
	M "github.com/sagernet/sing/common/metadata"
)

func managedMASQUESecurityFixture(t *testing.T) (*Core, func(string, []string) adapter.Endpoint, httpProxyTestEcho) {
	t.Helper()
	if !http3AvailableInBuild() {
		t.Skip("requires with_quic")
	}
	initHTTPProxyTestDatabase(t)
	tls := makeHTTPProxyTestTLS(t)
	echo := startHTTPProxyEchoEndpoints(t)
	port := portFromAddress(t, reserveLoopbackPort(t))
	server := startHTTPProxyTestCore(t, marshalHTTPProxyTestConfig(map[string]any{
		"log":       map[string]any{"disabled": true},
		"endpoints": []any{map[string]any{"type": "masque-server", "tag": "test-http", "listen": "127.0.0.1", "listen_port": port, "version": []int{3}, "system": false, "address": []string{"172.29.0.1/29"}, "users": []map[string]string{{"username": httpProxyTestUser, "password": httpProxyTestPassword}, {"username": "bob", "password": "synthetic-bob-password"}}, "tls": map[string]any{"enabled": true, "certificate_path": tls.certificatePath, "key_path": tls.keyPath}}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": "direct"},
	}))
	client := func(user string, routes []string) adapter.Endpoint {
		password := httpProxyTestPassword
		if user == "bob" {
			password = "synthetic-bob-password"
		}
		config := map[string]any{"type": "masque-client", "tag": "native-client", "server": "127.0.0.1", "server_port": port, "version": 3, "disable_version_fallback": true, "system": false, "on_demand": true, "username": user, "password": password, "tls": map[string]any{"enabled": true, "server_name": "localhost", "certificate_path": tls.rootPath}}
		if routes != nil {
			config["advertise_routes"] = routes
		}
		client := startHTTPProxyTestBox(t, marshalHTTPProxyTestConfig(map[string]any{"endpoints": []any{config}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": "native-client"}}))
		value, _ := client.Endpoint().Get("native-client")
		return value
	}
	_, tcpPort, _ := net.SplitHostPort(echo.tcpAddress)
	return server, client, httpProxyTestEcho{tcpAddress: net.JoinHostPort("172.29.0.1", tcpPort), udpAddress: &net.UDPAddr{IP: net.ParseIP("172.29.0.1"), Port: echo.udpAddress.Port}}
}

func TestManagedMASQUEHTTP3ActuallyRateLimitsTCPAndUDP(t *testing.T) {
	server, newClient, targets := managedMASQUESecurityFixture(t)
	proxy := newClient(httpProxyTestUser, nil)
	for _, network := range []string{"tcp", "udp"} {
		t.Run(network, func(t *testing.T) {
			server.GetInstance().RateLimitTracker().SetLimits([]model.Client{{Name: httpProxyTestUser, UploadLimit: 2048, DownloadLimit: 2048}})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			destination := targets.tcpAddress
			if network == "udp" {
				destination = targets.udpAddress.String()
			}
			conn, err := proxy.DialContext(ctx, network, M.ParseSocksaddr(destination))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(10 * time.Second))
			payload := bytes.Repeat([]byte("r"), 4096)
			started := time.Now()
			if _, err := conn.Write(payload); err != nil {
				t.Fatal(err)
			}
			response := make([]byte, len(payload))
			if _, err := io.ReadFull(conn, response); err != nil {
				t.Fatal(err)
			}
			elapsed := time.Since(started)
			if !bytes.Equal(payload, response) || elapsed < time.Second {
				t.Fatalf("native %s traffic bypassed shared user rate buckets: %v", network, elapsed)
			}
			assertHTTPProxySession(t, server.GetInstance(), network)
			assertHTTPProxyUserTraffic(t, server.GetInstance(), network)
		})
	}
}

func TestManagedMASQUEHTTP3RejectsPeerRouteSpoofing(t *testing.T) {
	server, newClient, targets := managedMASQUESecurityFixture(t)
	alice := newClient(httpProxyTestUser, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	connection, err := alice.DialContext(ctx, "tcp", M.ParseSocksaddr(targets.tcpAddress))
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.Write([]byte("live"))
	if _, err := io.ReadFull(connection, make([]byte, 4)); err != nil {
		t.Fatal(err)
	}
	managed, _ := server.GetInstance().Endpoint().Get("test-http")
	identity := managed.(*managedMASQUEEndpoint).identity
	identity.mu.Lock()
	var victim netip.Addr
	for address, leases := range identity.leases {
		for _, lease := range leases {
			if lease.user == httpProxyTestUser {
				victim = address
			}
		}
	}
	identity.mu.Unlock()
	if !victim.IsValid() {
		t.Fatal("missing authenticated victim lease")
	}
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	bob := newClient("bob", []string{netip.PrefixFrom(victim, 32).String()})
	// Even if the client sees ADDRESS_ASSIGN before its forbidden route capsule
	// is rejected, no packet may acquire the victim's authenticated identity.
	probeCtx, probeCancel := context.WithTimeout(context.Background(), time.Second)
	defer probeCancel()
	if conn, err := bob.DialContext(probeCtx, "udp", M.ParseSocksaddr(targets.udpAddress.String())); err == nil {
		conn.Close()
	}
	packet := managedMASQUESpoofUDP(victim, netip.MustParseAddr("172.29.0.1"), uint16(listener.LocalAddr().(*net.UDPAddr).Port))
	_ = bob.(*native.ClientEndpoint).WritePackets([][]byte{packet})
	listener.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	if _, _, err := listener.ReadFrom(make([]byte, 128)); err == nil {
		t.Fatal("forbidden peer advertisement allowed a victim-source packet")
	}
	for _, session := range server.GetInstance().ConnTracker().Sessions() {
		if session.Network == "udp" && session.Destination == listener.LocalAddr().String() {
			t.Fatal("spoofed packet reached tracked router")
		}
	}
	connection.SetDeadline(time.Now().Add(3 * time.Second))
	connection.Write([]byte("safe"))
	if _, err := io.ReadFull(connection, make([]byte, 4)); err != nil {
		t.Fatalf("rejecting malicious peer broke another user's tunnel: %v", err)
	}
}

func managedMASQUESpoofUDP(source, destination netip.Addr, port uint16) []byte {
	payload := []byte("must-never-forward")
	packet := make([]byte, 28+len(payload))
	packet[0], packet[8], packet[9] = 0x45, 64, 17
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], source.AsSlice())
	copy(packet[16:20], destination.AsSlice())
	var sum uint32
	for i := 0; i < 20; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(packet[i : i+2]))
	}
	for sum>>16 != 0 {
		sum = sum&0xffff + sum>>16
	}
	binary.BigEndian.PutUint16(packet[10:12], ^uint16(sum))
	binary.BigEndian.PutUint16(packet[20:22], 42001)
	binary.BigEndian.PutUint16(packet[22:24], port)
	binary.BigEndian.PutUint16(packet[24:26], uint16(8+len(payload)))
	copy(packet[28:], payload)
	return packet
}

func TestManagedMASQUEHTTP3RejectsWrongTLSIdentity(t *testing.T) {
	server, _, targets := managedMASQUESecurityFixture(t)
	value, _ := server.GetInstance().Endpoint().Get("test-http")
	options := value.(*managedMASQUEEndpoint).options
	client := startHTTPProxyTestBox(t, marshalHTTPProxyTestConfig(map[string]any{
		"log": map[string]any{"disabled": true},
		"endpoints": []any{map[string]any{
			"type": "masque-client", "tag": "native-client", "server": "127.0.0.1", "server_port": options.ListenPort,
			"version": 3, "disable_version_fallback": true, "system": false, "on_demand": true,
			"username": httpProxyTestUser, "password": httpProxyTestPassword,
			"tls": map[string]any{"enabled": true, "server_name": "wrong.invalid", "certificate_path": options.TLS.CertificatePath},
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": "native-client"},
	}))
	proxy, _ := client.Endpoint().Get("native-client")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := proxy.DialContext(ctx, "tcp", M.ParseSocksaddr(targets.tcpAddress))
	if err == nil {
		conn.Close()
		t.Fatal("native H3 accepted a certificate for another hostname")
	}
	if !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("test did not reach TLS verification: %v", err)
	}
}
