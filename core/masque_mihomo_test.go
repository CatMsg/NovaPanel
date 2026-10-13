package core

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestManagedMASQUEMihomoBinaryTCPAndUDP(t *testing.T) {
	binary := os.Getenv("NOVAPANEL_MIHOMO_TEST_BINARY")
	if binary == "" || !http3AvailableInBuild() {
		t.Skip("requires with_quic and NOVAPANEL_MIHOMO_TEST_BINARY")
	}
	initHTTPProxyTestDatabase(t)
	tlsFiles := makeHTTPProxyTestTLS(t)
	echo := startHTTPProxyEchoEndpoints(t)
	port := portFromAddress(t, reserveLoopbackPort(t))
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	privateDER, _ := x509.MarshalECPrivateKey(key)
	publicDER, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	certificatePEM, err := os.ReadFile(tlsFiles.certificatePath)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(certificatePEM)
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	serverPublicDER, _ := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	server := startHTTPProxyTestCore(t, marshalHTTPProxyTestConfig(map[string]any{
		"log": map[string]any{"level": "debug"},
		"endpoints": []any{map[string]any{
			"type": "masque-server", "tag": "test-http", "listen": "127.0.0.1", "listen_port": port,
			"version": []int{3}, "system": false, "address": []string{"172.29.0.1/29"},
			"users":        []map[string]string{{"username": httpProxyTestUser, "password": httpProxyTestPassword}},
			"mihomo_users": []map[string]string{{"username": httpProxyTestUser, "public_key": base64.StdEncoding.EncodeToString(publicDER), "address": "172.29.0.2"}},
			"tls":          map[string]any{"enabled": true, "certificate_path": tlsFiles.certificatePath, "key_path": tlsFiles.keyPath},
		}},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": "direct"},
	}))
	nativeClient := startHTTPProxyTestBox(t, marshalHTTPProxyTestConfig(map[string]any{
		"endpoints": []any{map[string]any{
			"type": "masque-client", "tag": "native-client", "server": "127.0.0.1", "server_port": port,
			"version": 3, "system": false, "on_demand": true, "disable_version_fallback": true,
			"username": httpProxyTestUser, "password": httpProxyTestPassword,
			"headers": map[string]string{"X-Forwarded-For": "203.0.113.99", "Forwarded": "for=203.0.113.99"},
			"tls":     map[string]any{"enabled": true, "server_name": "localhost", "certificate_path": tlsFiles.rootPath},
		}}, "outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}}, "route": map[string]any{"final": "native-client"},
	}))
	_, tcpPort, _ := net.SplitHostPort(echo.tcpAddress)
	targets := httpProxyTestEcho{tcpAddress: net.JoinHostPort("172.29.0.1", tcpPort), udpAddress: &net.UDPAddr{IP: net.ParseIP("172.29.0.1"), Port: echo.udpAddress.Port}}
	nativeProxy, _ := nativeClient.Endpoint().Get("native-client")
	endpoint, _ := server.GetInstance().Endpoint().Get("test-http")
	identity := endpoint.(*managedMASQUEEndpoint).identity
	identity.mu.Lock()
	_, stolen := identity.leases[netip.MustParseAddr("172.29.0.2")]
	identity.mu.Unlock()
	if stolen {
		t.Fatal("native dynamic allocation stole the reserved Mihomo address")
	}
	socksPort := portFromAddress(t, reserveLoopbackPort(t))
	controller := reserveLoopbackPort(t)
	config := map[string]any{
		"external-controller": controller,
		"socks-port":          socksPort, "allow-lan": false, "bind-address": "127.0.0.1", "mode": "rule", "log-level": "debug",
		"dns": map[string]any{"enable": false},
		"proxies": []any{map[string]any{
			"name": "native-masque", "type": "masque", "server": "127.0.0.1", "port": port,
			"private-key": base64.StdEncoding.EncodeToString(privateDER), "public-key": base64.StdEncoding.EncodeToString(serverPublicDER),
			"ip": "172.29.0.2/32", "uri": "https://localhost/.well-known/masque/ip/*/*/", "sni": "localhost", "mtu": 1280, "udp": true,
		}}, "rules": []string{"MATCH,native-masque"},
	}
	directory := t.TempDir()
	content, _ := json.Marshal(config)
	configPath := filepath.Join(directory, "config.json")
	if err := os.WriteFile(configPath, content, 0600); err != nil {
		t.Fatal(err)
	}
	output, err := os.Create(filepath.Join(directory, "mihomo.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	command := exec.Command(binary, "-d", directory, "-f", configPath)
	command.Stdout, command.Stderr = output, output
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		command.Process.Signal(os.Interrupt)
		done := make(chan struct{})
		go func() { command.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			command.Process.Kill()
			<-done
		}
		if t.Failed() {
			content, _ := os.ReadFile(filepath.Join(directory, "mihomo.log"))
			t.Logf("Mihomo test log: %s", content)
		}
	})
	httpClient := &http.Client{Timeout: time.Second}
	waitHTTPProxyCondition(t, 5*time.Second, func() bool {
		response, err := httpClient.Get("http://" + controller + "/rules")
		if err != nil {
			return false
		}
		defer response.Body.Close()
		var result struct {
			Rules []struct {
				Proxy string `json:"proxy"`
			} `json:"rules"`
		}
		return response.StatusCode == http.StatusOK && json.NewDecoder(response.Body).Decode(&result) == nil && len(result.Rules) == 1 && result.Rules[0].Proxy == "native-masque"
	})
	waitHTTPProxyCondition(t, 5*time.Second, func() bool {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), 100*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	})
	client := startHTTPProxyTestBox(t, marshalHTTPProxyTestConfig(map[string]any{
		"outbounds": []any{map[string]any{"type": "socks", "tag": "proxy", "server": "127.0.0.1", "server_port": socksPort}},
		"route":     map[string]any{"final": "proxy"},
	}))
	proxy, _ := client.Outbound().Outbound("proxy")
	// Mihomo publishes listeners and rules before OnRunning. A successful
	// SOCKS handshake alone can precede its traffic handler being enabled.
	waitHTTPProxyCondition(t, 5*time.Second, func() bool {
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		conn, err := proxy.DialContext(ctx, "tcp", socksaddrForTest(t, targets.tcpAddress))
		if err != nil {
			return false
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(300 * time.Millisecond))
		if _, err := io.WriteString(conn, "ready"); err != nil {
			return false
		}
		response := make([]byte, 5)
		_, err = io.ReadFull(conn, response)
		return err == nil && string(response) == "ready"
	})
	assertHTTPProxyRoundTrip(t, proxy, server.GetInstance(), targets, 3, "Mihomo native MASQUE compatibility")
	assertHTTPProxyRoundTrip(t, nativeProxy, server.GetInstance(), targets, 3, "native with compatibility enabled")
	assertHTTPProxyHistory(t)
	identity.mu.Lock()
	_, assigned := identity.leases[netip.MustParseAddr("172.29.0.2")]
	leaseCount := len(identity.leases)
	trustedPeers := true
	for _, lease := range identity.leases {
		trustedPeers = trustedPeers && lease.peer.Addr.IsLoopback()
	}
	identity.mu.Unlock()
	if !assigned || leaseCount != 2 || !trustedPeers {
		t.Fatal("native and Mihomo tunnels did not retain independent addresses")
	}
	// Recreate the listener without the old public key. An existing Mihomo
	// process must not retain access after the panel revokes its credentials.
	options := endpoint.(*managedMASQUEEndpoint).options
	newKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	newDER, _ := x509.MarshalPKIXPublicKey(&newKey.PublicKey)
	options.MihomoUsers = append([]MihomoMASQUEUser(nil), options.MihomoUsers...)
	options.MihomoUsers[0].PublicKey = base64.StdEncoding.EncodeToString(newDER)
	raw, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	var replacement map[string]any
	if err := json.Unmarshal(raw, &replacement); err != nil {
		t.Fatal(err)
	}
	replacement["type"], replacement["tag"] = "masque-server", "test-http"
	raw, _ = json.Marshal(replacement)
	if err := server.RemoveEndpoint("test-http"); err != nil {
		t.Fatal(err)
	}
	if err := server.AddEndpoint(raw); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if conn, err := proxy.DialContext(ctx, "tcp", socksaddrForTest(t, targets.tcpAddress)); err == nil {
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Write([]byte("revoked")); err == nil {
			if _, err := io.ReadFull(conn, make([]byte, 7)); err == nil {
				t.Fatal("revoked Mihomo key still passed traffic")
			}
		}
	}
}
