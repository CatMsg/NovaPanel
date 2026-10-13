package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	boxHTTP "github.com/sagernet/sing-box/transport/http"
	M "github.com/sagernet/sing/common/metadata"
)

const httpProxyTestUser = "test"
const httpProxyTestPassword = "proxy-test-password"

type httpProxyTestTLS struct {
	certificatePath string
	keyPath         string
	rootPath        string
}

func TestHTTPProxyLoopbackIntegration(t *testing.T) {
	initHTTPProxyTestDatabase(t)
	echo := startHTTPProxyEchoEndpoints(t)
	tlsFiles := makeHTTPProxyTestTLS(t)
	serverAddress := reserveLoopbackPort(t)
	serverPort := portFromAddress(t, serverAddress)
	versions := []int{1, 2}
	if http3AvailableInBuild() {
		versions = append(versions, 3)
	}
	runtime := startHTTPProxyTestCore(t, httpProxyServerConfig(serverPort, versions, tlsFiles))
	server := runtime.GetInstance()
	for _, version := range versions {
		version := version
		t.Run(fmt.Sprintf("http%d_tcp_and_connect_udp", version), func(t *testing.T) {
			client := startHTTPProxyTestBox(t, httpProxyClientConfig(serverPort, &version, httpProxyTestUser, httpProxyTestPassword, tlsFiles))
			proxy, ok := client.Outbound().Outbound("proxy")
			if !ok {
				t.Fatal("HTTP proxy outbound was not registered")
			}
			assertHTTPProxyRoundTrip(t, proxy, server, echo, version, "initial")
		})
	}
	if !http3AvailableInBuild() {
		t.Log("HTTP/3 integration skipped: the current test build does not include with_quic")
	}

	for _, version := range versions {
		version := version
		t.Run(fmt.Sprintf("wrong_authentication_http%d_is_rejected", version), func(t *testing.T) {
			client := startHTTPProxyTestBox(t, httpProxyClientConfig(serverPort, &version, httpProxyTestUser, "wrong-password", tlsFiles))
			proxy, ok := client.Outbound().Outbound("proxy")
			if !ok {
				t.Fatal("HTTP proxy outbound was not registered")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			conn, err := proxy.DialContext(ctx, "tcp", socksaddrForTest(t, echo.tcpAddress))
			if err == nil {
				_ = conn.Close()
				t.Fatalf("HTTP/%d CONNECT with incorrect credentials unexpectedly succeeded", version)
			}
		})
	}

	t.Run("replace_http_inbound_credentials_on_same_tcp_udp_port", func(t *testing.T) {
		oldInbound, ok := server.Inbound().Get("test-http")
		if !ok {
			t.Fatal("original HTTP inbound was not registered")
		}
		newPassword := "proxy-test-password-rotated"
		if err := runtime.AddInbound(httpProxyServerInboundConfig(serverPort, versions, tlsFiles, newPassword)); err != nil {
			t.Fatalf("replace running HTTP inbound on the same TCP/UDP port: %v", err)
		}
		newInbound, ok := server.Inbound().Get("test-http")
		if !ok || newInbound == oldInbound {
			t.Fatal("Core manager did not install a new HTTP inbound instance")
		}
		assertLoopbackPortBound(t, serverAddress, http3AvailableInBuild())

		replacementVersions := []int{1}
		if http3AvailableInBuild() {
			replacementVersions = append(replacementVersions, 3)
		}
		for _, version := range replacementVersions {
			version := version
			t.Run(fmt.Sprintf("old_credentials_http%d_rejected", version), func(t *testing.T) {
				client := startHTTPProxyTestBox(t, httpProxyClientConfig(serverPort, &version, httpProxyTestUser, httpProxyTestPassword, tlsFiles))
				proxy, ok := client.Outbound().Outbound("proxy")
				if !ok {
					t.Fatal("HTTP proxy outbound was not registered")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				conn, err := proxy.DialContext(ctx, "tcp", socksaddrForTest(t, echo.tcpAddress))
				if err == nil {
					_ = conn.Close()
					t.Fatalf("HTTP/%d accepted credentials from before the inbound replacement", version)
				}

				udpCtx, udpCancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer udpCancel()
				udpConn, err := proxy.ListenPacket(udpCtx, socksaddrForTest(t, echo.udpAddress.String()))
				if err == nil {
					_ = udpConn.Close()
					t.Fatalf("HTTP/%d CONNECT-UDP accepted credentials from before the inbound replacement", version)
				}
			})
			t.Run(fmt.Sprintf("new_credentials_http%d_tcp_and_connect_udp", version), func(t *testing.T) {
				client := startHTTPProxyTestBox(t, httpProxyClientConfig(serverPort, &version, httpProxyTestUser, newPassword, tlsFiles))
				proxy, ok := client.Outbound().Outbound("proxy")
				if !ok {
					t.Fatal("HTTP proxy outbound was not registered")
				}
				assertHTTPProxyRoundTrip(t, proxy, server, echo, version, "replacement")
			})
		}
	})

	t.Run("versionless_client_falls_back_to_http1", func(t *testing.T) {
		fallbackAddress := reserveLoopbackPort(t)
		fallbackPort := portFromAddress(t, fallbackAddress)
		fallbackServer := startHTTPProxyTestBox(t, httpProxyServerConfig(fallbackPort, []int{1}, tlsFiles))
		client := startHTTPProxyTestBox(t, httpProxyClientConfig(fallbackPort, nil, httpProxyTestUser, httpProxyTestPassword, tlsFiles))
		proxy, ok := client.Outbound().Outbound("proxy")
		if !ok {
			t.Fatal("versionless HTTP proxy outbound was not registered")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		conn, err := proxy.DialContext(ctx, "tcp", socksaddrForTest(t, echo.tcpAddress))
		if err != nil {
			t.Fatalf("versionless client did not fall back to HTTP/1 CONNECT: %v", err)
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		payload := []byte("versionless-fallback")
		if _, err := conn.Write(payload); err != nil {
			t.Fatal(err)
		}
		response := make([]byte, len(payload))
		if _, err := io.ReadFull(conn, response); err != nil {
			t.Fatal(err)
		}
		if string(response) != string(payload) {
			t.Fatalf("fallback echo = %q, want %q", response, payload)
		}
		assertHTTPProxySession(t, fallbackServer, "tcp")
		if err := client.Close(); err != nil {
			t.Fatalf("stop versionless client box: %v", err)
		}
		if err := fallbackServer.Close(); err != nil {
			t.Fatalf("stop HTTP/1 fallback server box: %v", err)
		}
		assertLoopbackPortReleased(t, fallbackAddress)
	})

	t.Run("http3_requires_tls", func(t *testing.T) {
		if !http3AvailableInBuild() {
			t.Skip("HTTP/3 is not available in this build")
		}
		ctx := Context(
			context.Background(),
			InboundRegistry(),
			OutboundRegistry(),
			EndpointRegistry(),
			DNSTransportRegistry(),
			ServiceRegistry(),
			CertificateProviderRegistry(),
		)
		var options option.Options
		config := httpProxyServerConfig(portFromAddress(t, reserveLoopbackPort(t)), []int{3}, httpProxyTestTLS{})
		if err := options.UnmarshalJSONContext(ctx, config); err != nil {
			t.Fatalf("parse HTTP/3-without-TLS fixture: %v", err)
		}
		_, err := NewBox(Options{Context: ctx, Options: options})
		if err == nil || !strings.Contains(err.Error(), "TLS is required for HTTP/3") {
			t.Fatalf("HTTP/3 without TLS error = %v, want TLS-required rejection", err)
		}
	})

	assertHTTPProxyHistory(t)
	if got := server.RateLimitTracker().limiter(httpProxyTestUser, true); got == nil {
		t.Fatal("synthetic upload limit was not loaded into the shared per-user tracker")
	} else if got.Limit() != 64*1024 {
		t.Fatalf("synthetic upload limit = %v bytes/s, want %d", got.Limit(), 64*1024)
	}
	if got := server.RateLimitTracker().limiter(httpProxyTestUser, false); got == nil {
		t.Fatal("synthetic download limit was not loaded into the shared per-user tracker")
	} else if got.Limit() != 64*1024 {
		t.Fatalf("synthetic download limit = %v bytes/s, want %d", got.Limit(), 64*1024)
	}

	if err := runtime.Stop(); err != nil {
		t.Fatalf("stop HTTP proxy Core runtime: %v", err)
	}
	assertLoopbackPortReleased(t, serverAddress)
}

type httpProxyTestEcho struct {
	tcpAddress string
	udpAddress *net.UDPAddr
}

func initHTTPProxyTestDatabase(t *testing.T) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "synthetic-http-proxy.db")
	if err := database.InitDB(dbPath); err != nil {
		t.Fatalf("initialize temporary synthetic SQLite database: %v", err)
	}
	sqlDB, err := database.GetDB().DB()
	if err != nil {
		t.Fatalf("get temporary SQLite connection: %v", err)
	}
	t.Cleanup(func() {
		if err := sqlDB.Close(); err != nil {
			t.Errorf("close temporary synthetic SQLite database: %v", err)
		}
	})
	if err := database.GetDB().Create(&model.Client{
		Enable:        true,
		Name:          httpProxyTestUser,
		UploadLimit:   64 * 1024,
		DownloadLimit: 64 * 1024,
	}).Error; err != nil {
		t.Fatalf("seed synthetic HTTP proxy client: %v", err)
	}
}

func startHTTPProxyEchoEndpoints(t *testing.T) httpProxyTestEcho {
	t.Helper()
	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start loopback TCP echo listener: %v", err)
	}
	var active sync.Map
	var workers sync.WaitGroup
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := tcpListener.Accept()
			if err != nil {
				return
			}
			active.Store(conn, struct{}{})
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer active.Delete(conn)
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	tcpAddress := tcpListener.Addr().String()
	t.Cleanup(func() {
		_ = tcpListener.Close()
		<-acceptDone
		active.Range(func(key, _ any) bool {
			_ = key.(net.Conn).Close()
			return true
		})
		workers.Wait()
	})

	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("start loopback UDP echo listener: %v", err)
	}
	udpDone := make(chan struct{})
	go func() {
		defer close(udpDone)
		buffer := make([]byte, 64*1024)
		for {
			n, source, err := udpConn.ReadFrom(buffer)
			if err != nil {
				return
			}
			if _, err := udpConn.WriteTo(buffer[:n], source); err != nil {
				return
			}
		}
	}()
	udpAddress, ok := udpConn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("UDP echo listener has unexpected address type %T", udpConn.LocalAddr())
	}
	t.Cleanup(func() {
		_ = udpConn.Close()
		<-udpDone
	})
	return httpProxyTestEcho{tcpAddress: tcpAddress, udpAddress: udpAddress}
}

func makeHTTPProxyTestTLS(t *testing.T) httpProxyTestTLS {
	t.Helper()
	dir := t.TempDir()
	now := time.Now()
	rootKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test TLS root key: %v", err)
	}
	rootTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "NovaPanel loopback test root"},
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	rootDER, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &rootKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create test TLS root certificate: %v", err)
	}
	rootPath := filepath.Join(dir, "root.pem")
	writeHTTPProxyTestFile(t, rootPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rootDER}), 0o600)
	rootCert, err := x509.ParseCertificate(rootDER)
	if err != nil {
		t.Fatalf("parse test TLS root certificate: %v", err)
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate test TLS server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    now.Add(-time.Minute),
		NotAfter:     now.Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, rootCert, &serverKey.PublicKey, rootKey)
	if err != nil {
		t.Fatalf("create test TLS server certificate: %v", err)
	}
	serverCertPath := filepath.Join(dir, "server.pem")
	writeHTTPProxyTestFile(t, serverCertPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: serverDER}), 0o600)
	serverKeyDER, err := x509.MarshalPKCS8PrivateKey(serverKey)
	if err != nil {
		t.Fatalf("marshal test TLS server key: %v", err)
	}
	serverKeyPath := filepath.Join(dir, "server-key.pem")
	writeHTTPProxyTestFile(t, serverKeyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: serverKeyDER}), 0o600)
	return httpProxyTestTLS{certificatePath: serverCertPath, keyPath: serverKeyPath, rootPath: rootPath}
}

func writeHTTPProxyTestFile(t *testing.T, path string, content []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, content, mode); err != nil {
		t.Fatalf("write temporary TLS test material: %v", err)
	}
}

func httpProxyServerConfig(port int, versions []int, tlsFiles httpProxyTestTLS) []byte {
	inbound := httpProxyServerInboundOptions(port, versions, tlsFiles, httpProxyTestPassword)
	return marshalHTTPProxyTestConfig(map[string]any{
		"inbounds":  []any{inbound},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	})
}

func httpProxyServerInboundConfig(port int, versions []int, tlsFiles httpProxyTestTLS, password string) []byte {
	return marshalHTTPProxyTestConfig(httpProxyServerInboundOptions(port, versions, tlsFiles, password))
}

func httpProxyServerInboundOptions(port int, versions []int, tlsFiles httpProxyTestTLS, password string) map[string]any {
	inbound := map[string]any{
		"type":        "http",
		"tag":         "test-http",
		"listen":      "127.0.0.1",
		"listen_port": port,
		"version":     versions,
		"users":       []map[string]string{{"username": httpProxyTestUser, "password": password}},
	}
	if tlsFiles.certificatePath != "" {
		inbound["tls"] = map[string]any{
			"enabled":          true,
			"certificate_path": tlsFiles.certificatePath,
			"key_path":         tlsFiles.keyPath,
		}
	}
	return inbound
}

func httpProxyClientConfig(port int, version *int, username string, password string, tlsFiles httpProxyTestTLS) []byte {
	outbound := map[string]any{
		"type":        "http",
		"tag":         "proxy",
		"server":      "127.0.0.1",
		"server_port": port,
		"username":    username,
		"password":    password,
		"tls": map[string]any{
			"enabled":          true,
			"server_name":      "localhost",
			"certificate_path": tlsFiles.rootPath,
		},
	}
	if version != nil {
		outbound["version"] = *version
		outbound["disable_version_fallback"] = true
	}
	return marshalHTTPProxyTestConfig(map[string]any{
		"outbounds": []any{outbound},
		"route":     map[string]any{"final": "proxy"},
	})
}

func marshalHTTPProxyTestConfig(config map[string]any) []byte {
	content, err := json.Marshal(config)
	if err != nil {
		panic(fmt.Sprintf("marshal static HTTP proxy test config: %v", err))
	}
	return content
}

func startHTTPProxyTestBox(t *testing.T, config []byte) *Box {
	t.Helper()
	ctx := Context(
		context.Background(),
		InboundRegistry(),
		OutboundRegistry(),
		EndpointRegistry(),
		DNSTransportRegistry(),
		ServiceRegistry(),
		CertificateProviderRegistry(),
	)
	var options option.Options
	if err := options.UnmarshalJSONContext(ctx, config); err != nil {
		t.Fatalf("parse sing-box test config: %v", err)
	}
	box, err := NewBox(Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatalf("construct sing-box test box: %v", err)
	}
	if err := box.Start(); err != nil {
		_ = box.Close()
		t.Fatalf("start sing-box test box: %v", err)
	}
	t.Cleanup(func() {
		if err := box.Close(); err != nil {
			t.Errorf("stop sing-box test box: %v", err)
		}
	})
	return box
}

func startHTTPProxyTestCore(t *testing.T, config []byte) *Core {
	t.Helper()
	runtime := NewCore()
	t.Cleanup(func() {
		if err := runtime.Stop(); err != nil {
			t.Errorf("stop HTTP proxy Core runtime: %v", err)
		}
	})
	if err := runtime.Start(config); err != nil {
		t.Fatalf("start HTTP proxy Core runtime: %v", err)
	}
	return runtime
}

func assertHTTPProxyRoundTrip(t *testing.T, proxy adapter.Outbound, server *Box, echo httpProxyTestEcho, version int, phase string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	label := fmt.Sprintf("%s HTTP/%d", phase, version)

	tcpConn, err := proxy.DialContext(ctx, "tcp", socksaddrForTest(t, echo.tcpAddress))
	if err != nil {
		t.Fatalf("%s TCP CONNECT failed: %v", label, err)
	}
	defer tcpConn.Close()
	_ = tcpConn.SetDeadline(time.Now().Add(5 * time.Second))
	tcpPayload := []byte(fmt.Sprintf("%s-tcp-roundtrip", label))
	if _, err := tcpConn.Write(tcpPayload); err != nil {
		t.Fatalf("%s TCP payload write failed: %v", label, err)
	}
	tcpResponse := make([]byte, len(tcpPayload))
	if _, err := io.ReadFull(tcpConn, tcpResponse); err != nil {
		t.Fatalf("%s TCP payload read failed: %v", label, err)
	}
	if string(tcpResponse) != string(tcpPayload) {
		t.Fatalf("%s TCP echo = %q, want %q", label, tcpResponse, tcpPayload)
	}
	assertHTTPProxySession(t, server, "tcp")
	assertHTTPProxyUserTraffic(t, server, "tcp")

	udpDestination := socksaddrForTest(t, echo.udpAddress.String())
	packetConn, err := proxy.ListenPacket(ctx, udpDestination)
	if err != nil {
		t.Fatalf("%s CONNECT-UDP setup failed: %v", label, err)
	}
	defer packetConn.Close()
	_ = packetConn.SetDeadline(time.Now().Add(5 * time.Second))
	udpPayload := []byte(fmt.Sprintf("%s-udp-roundtrip", label))
	udpTarget, err := net.ResolveUDPAddr("udp", echo.udpAddress.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := packetConn.WriteTo(udpPayload, udpTarget); err != nil {
		t.Fatalf("%s UDP payload write failed: %v", label, err)
	}
	// SOCKS-based external-client harnesses also receive an encapsulation header.
	udpResponse := make([]byte, 64*1024)
	n, _, err := packetConn.ReadFrom(udpResponse)
	if err != nil {
		t.Fatalf("%s UDP payload read failed: %v", label, err)
	}
	if string(udpResponse[:n]) != string(udpPayload) {
		t.Fatalf("%s UDP echo = %q, want %q", label, udpResponse[:n], udpPayload)
	}
	assertHTTPProxySession(t, server, "udp")
	assertHTTPProxyUserTraffic(t, server, "udp")
}

func http3AvailableInBuild() bool {
	return boxHTTP.ConfigureHTTP3ListenerFunc != nil && boxHTTP.NewHTTP3Client != nil
}

func reserveLoopbackPort(t *testing.T) string {
	t.Helper()
	for attempt := 0; attempt < 5; attempt++ {
		tcp, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("reserve loopback TCP port: %v", err)
		}
		address := tcp.Addr().String()
		udp, err := net.ListenPacket("udp", address)
		if err == nil {
			_ = udp.Close()
			_ = tcp.Close()
			return address
		}
		_ = tcp.Close()
	}
	t.Fatal("could not reserve a matching loopback TCP/UDP port")
	return ""
}

func portFromAddress(t *testing.T, address string) int {
	t.Helper()
	_, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split loopback address %q: %v", address, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("parse loopback port %q: %v", portText, err)
	}
	return port
}

func socksaddrForTest(t *testing.T, address string) M.Socksaddr {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatalf("split test endpoint %q: %v", address, err)
	}
	portNumber, err := strconv.ParseUint(port, 10, 16)
	if err != nil {
		t.Fatalf("parse test endpoint port %q: %v", port, err)
	}
	return M.ParseSocksaddrHostPort(host, uint16(portNumber))
}

func assertHTTPProxySession(t *testing.T, server *Box, network string) {
	t.Helper()
	var found SessionSnapshot
	waitHTTPProxyCondition(t, 5*time.Second, func() bool {
		for _, session := range server.ConnTracker().Sessions() {
			if session.Inbound == "test-http" && session.Outbound == "direct" && session.User == httpProxyTestUser && session.Network == network {
				found = session
				return true
			}
		}
		return false
	})
	if found.Source == "" {
		t.Fatalf("%s session has no source address: %+v", network, found)
	}
	host, _, err := net.SplitHostPort(found.Source)
	if err != nil {
		t.Fatalf("%s session source %q is not host:port: %v", network, found.Source, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() {
		t.Fatalf("%s session source %q is not a loopback IP", network, found.Source)
	}
}

func assertHTTPProxyUserTraffic(t *testing.T, server *Box, network string) {
	t.Helper()
	var uploadSeen, downloadSeen bool
	waitHTTPProxyCondition(t, 5*time.Second, func() bool {
		stats := server.StatsTracker().GetStats()
		for _, stat := range *stats {
			if stat.Resource == "user" && stat.Tag == httpProxyTestUser && stat.Traffic > 0 {
				if stat.Direction {
					uploadSeen = true
				} else {
					downloadSeen = true
				}
			}
		}
		return uploadSeen && downloadSeen
	})
	if !uploadSeen || !downloadSeen {
		t.Fatalf("%s user stats did not include both upload and download traffic", network)
	}
}

func waitHTTPProxyCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case <-ticker.C:
		case <-deadline.C:
			t.Fatal("timed out waiting for loopback proxy state")
		}
	}
}

func assertHTTPProxyHistory(t *testing.T) {
	t.Helper()
	var client model.Client
	if err := database.GetDB().Where("name = ?", httpProxyTestUser).First(&client).Error; err != nil {
		t.Fatalf("read synthetic client history: %v", err)
	}
	var history []model.ClientHistoryEntry
	if err := json.Unmarshal(client.History, &history); err != nil {
		t.Fatalf("decode synthetic client history: %v", err)
	}
	networks := make(map[string]bool)
	for _, entry := range history {
		if entry.Inbound == "test-http" && entry.Outbound == "direct" {
			networks[entry.Network] = true
		}
	}
	if !networks["tcp"] || !networks["udp"] {
		t.Fatalf("HTTP inbound history missing TCP/UDP records: %+v", history)
	}
}

func assertLoopbackPortReleased(t *testing.T, address string) {
	t.Helper()
	tcp, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("TCP listener port %s was not released: %v", address, err)
	}
	defer tcp.Close()
	udp, err := net.ListenPacket("udp", address)
	if err != nil {
		t.Fatalf("UDP listener port %s was not released: %v", address, err)
	}
	defer udp.Close()
}

func assertLoopbackPortBound(t *testing.T, address string, expectUDP bool) {
	t.Helper()
	if listener, err := net.Listen("tcp", address); err == nil {
		_ = listener.Close()
		t.Fatalf("expected TCP listener port %s to remain bound after replacement", address)
	} else if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("check replacement TCP listener port %s: %v", address, err)
	}
	if !expectUDP {
		return
	}
	if packetConn, err := net.ListenPacket("udp", address); err == nil {
		_ = packetConn.Close()
		t.Fatalf("expected UDP listener port %s to remain bound after replacement", address)
	} else if !errors.Is(err, syscall.EADDRINUSE) {
		t.Fatalf("check replacement UDP listener port %s: %v", address, err)
	}
}
