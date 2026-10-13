package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	certificateRegistry "github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	serviceRegistry "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	"github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	native "github.com/sagernet/sing-box/protocol/masque"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

// These tests exercise the native endpoint's default behavior without a session
// observer, a panel database or a host TUN. They document the identity gap that
// the managed observer must close, not an unmodified upstream source tree.
func TestMASQUENativeCONNECTIP(t *testing.T) {
	observed := make(chan adapter.InboundContext, 8)
	port := masqueNativeFreePort(t)
	masqueNativeBox(t, []map[string]any{{
		"type": "masque-server", "tag": "native-server", "listen": "127.0.0.1", "listen_port": port,
		"system": false, "version": []int{1}, "address": []string{"172.29.0.1/29"},
		"users": []map[string]string{{"username": "alice", "password": "local-test-only"}},
	}}, observed)
	clientBox := masqueNativeBox(t, []map[string]any{masqueNativeClientConfig(port, "alice", "local-test-only")}, nil)
	client, ok := clientBox.Endpoint().Get("native-client")
	if !ok {
		t.Fatal("missing native client endpoint")
	}
	masqueNativeRoundTrip(t, client, "tcp")
	masqueNativeRoundTrip(t, client, "udp")
	for _, network := range []string{"tcp", "udp"} {
		select {
		case metadata := <-observed:
			if metadata.Inbound != "native-server" || metadata.User != "" || !netip.MustParsePrefix("172.29.0.0/29").Contains(metadata.Source.Addr) {
				t.Fatalf("%s unexpected upstream metadata: %+v", network, metadata)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not reach the native router", network)
		}
	}
	wrongBox := masqueNativeBox(t, []map[string]any{masqueNativeClientConfig(port, "alice", "wrong-password")}, nil)
	wrong, _ := wrongBox.Endpoint().Get("native-client")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if conn, err := wrong.DialContext(ctx, "tcp", M.ParseSocksaddr("172.29.0.1:1")); err == nil {
		conn.Close()
		t.Fatal("native endpoint accepted a wrong password")
	}
}

func TestMASQUENativePeerRouteCanSpoofAssignedSource(t *testing.T) {
	observed := make(chan adapter.InboundContext, 8)
	port := masqueNativeFreePort(t)
	masqueNativeBox(t, []map[string]any{{
		"type": "masque-server", "tag": "native-server", "listen": "127.0.0.1", "listen_port": port,
		"system": false, "version": []int{1}, "address": []string{"172.29.0.1/29"},
		"users": []map[string]string{
			{"username": "alice", "password": "alice-test-only"},
			{"username": "bob", "password": "bob-test-only"},
		},
	}}, observed)
	aliceBox := masqueNativeBox(t, []map[string]any{masqueNativeClientConfig(port, "alice", "alice-test-only")}, nil)
	alice, _ := aliceBox.Endpoint().Get("native-client")
	masqueNativeRoundTrip(t, alice, "tcp")
	victim := (<-observed).Source.Addr
	bobConfig := masqueNativeClientConfig(port, "bob", "bob-test-only")
	bobConfig["advertise_routes"] = []string{netip.PrefixFrom(victim, 32).String()}
	bobBox := masqueNativeBox(t, []map[string]any{bobConfig}, nil)
	bob, _ := bobBox.Endpoint().Get("native-client")
	masqueNativeRoundTrip(t, bob, "tcp")
	bobSource := (<-observed).Source.Addr
	if bobSource == victim {
		t.Fatal("test requires two independently assigned native users")
	}
	listener, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	destinationPort := uint16(listener.LocalAddr().(*net.UDPAddr).Port)
	payload := []byte("bob-packet-with-alice-source")
	packet := masqueNativeUDPPacket(victim, netip.MustParseAddr("172.29.0.1"), 42001, destinationPort, payload)
	if err := bob.(*native.ClientEndpoint).WritePackets([][]byte{packet}); err != nil {
		t.Fatal(err)
	}
	listener.SetReadDeadline(time.Now().Add(5 * time.Second))
	buffer := make([]byte, 128)
	n, _, err := listener.ReadFrom(buffer)
	if err != nil {
		t.Fatalf("spoofed source did not reach local native destination: %v", err)
	}
	if !bytes.Equal(buffer[:n], payload) {
		t.Fatalf("unexpected payload: %q", buffer[:n])
	}
	select {
	case metadata := <-observed:
		if metadata.Source.Addr != victim || metadata.User != "" {
			t.Fatalf("unexpected spoofed native metadata: %+v", metadata)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("missing spoofed native router metadata")
	}
}

type masqueNativeObservedRouter struct {
	adapter.Router
	observed chan<- adapter.InboundContext
}

func (r *masqueNativeObservedRouter) RouteConnectionEx(ctx context.Context, conn net.Conn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.observed <- metadata
	r.Router.RouteConnectionEx(ctx, conn, metadata, onClose)
}

func (r *masqueNativeObservedRouter) RoutePacketConnectionEx(ctx context.Context, conn N.PacketConn, metadata adapter.InboundContext, onClose N.CloseHandlerFunc) {
	r.observed <- metadata
	r.Router.RoutePacketConnectionEx(ctx, conn, metadata, onClose)
}

func masqueNativeBox(t *testing.T, endpoints []map[string]any, observed chan<- adapter.InboundContext) *box.Box {
	t.Helper()
	endpointRegistry := endpoint.NewRegistry()
	native.RegisterEndpoint(endpointRegistry)
	if observed != nil {
		endpoint.Register[option.MASQUEServerEndpointOptions](endpointRegistry, "masque-server", func(ctx context.Context, router adapter.Router, logger log.ContextLogger, tag string, options option.MASQUEServerEndpointOptions) (adapter.Endpoint, error) {
			return native.NewServerEndpoint(ctx, &masqueNativeObservedRouter{Router: router, observed: observed}, logger, tag, options)
		})
	}
	outbounds := outbound.NewRegistry()
	direct.RegisterOutbound(outbounds)
	dnsRegistry := dns.NewTransportRegistry()
	local.RegisterTransport(dnsRegistry)
	ctx := box.Context(context.Background(), inbound.NewRegistry(), outbounds, endpointRegistry, dnsRegistry, serviceRegistry.NewRegistry(), certificateRegistry.NewRegistry())
	final := "direct"
	for _, config := range endpoints {
		if config["type"] == "masque-client" {
			final = config["tag"].(string)
		}
	}
	content, err := json.Marshal(map[string]any{
		"log": map[string]any{"disabled": true}, "endpoints": endpoints,
		"outbounds": []map[string]any{{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": final},
	})
	if err != nil {
		t.Fatal(err)
	}
	var options option.Options
	if err := options.UnmarshalJSONContext(ctx, content); err != nil {
		t.Fatal(err)
	}
	runtime, err := box.New(box.Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Start(); err != nil {
		runtime.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close isolated native endpoint: %v", err)
		}
	})
	return runtime
}

func masqueNativeClientConfig(port int, user, password string) map[string]any {
	return map[string]any{
		"type": "masque-client", "tag": "native-client", "server": "127.0.0.1", "server_port": port,
		"system": false, "version": 1, "disable_version_fallback": true, "on_demand": true,
		"username": user, "password": password,
	}
}

func masqueNativeFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port
}

func masqueNativeRoundTrip(t *testing.T, client adapter.Endpoint, network string) {
	t.Helper()
	var port int
	if network == "tcp" {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		port = listener.Addr().(*net.TCPAddr).Port
		go func() {
			conn, err := listener.Accept()
			if err == nil {
				defer conn.Close()
				io.Copy(conn, conn)
			}
		}()
	} else {
		listener, err := net.ListenPacket("udp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		port = listener.LocalAddr().(*net.UDPAddr).Port
		go func() {
			buffer := make([]byte, 256)
			if n, source, err := listener.ReadFrom(buffer); err == nil {
				listener.WriteTo(buffer[:n], source)
			}
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := client.DialContext(ctx, network, M.ParseSocksaddr(fmt.Sprintf("172.29.0.1:%d", port)))
	if err != nil {
		t.Fatalf("native %s CONNECT-IP: %v", network, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	payload := []byte("native-" + network + "-roundtrip")
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatalf("native %s echo: %v", network, err)
	}
	if !bytes.Equal(response, payload) {
		t.Fatalf("native %s echo mismatch: %q", network, response)
	}
}

func masqueNativeUDPPacket(source, destination netip.Addr, sourcePort, destinationPort uint16, payload []byte) []byte {
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
	binary.BigEndian.PutUint16(packet[20:22], sourcePort)
	binary.BigEndian.PutUint16(packet[22:24], destinationPort)
	binary.BigEndian.PutUint16(packet[24:26], uint16(8+len(payload)))
	copy(packet[28:], payload)
	return packet
}
