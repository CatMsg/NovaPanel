package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	panelLogger "github.com/CatMsg/NovaPanel/logger"
	"github.com/sagernet/sing-box/adapter"
)

const (
	snellV6TestPSK     = "snell-v6-integration-server-psk"
	snellV6TestUserKey = "snell-v6-integration-user-key"
	snellV6TestBobKey  = "snell-v6-integration-bob-key"
)

func TestSnellV6LoopbackIntegration(t *testing.T) {
	initHTTPProxyTestDatabase(t)
	if err := database.GetDB().Create(&model.Client{
		Enable: true, Name: "bob", UploadLimit: 64 * 1024, DownloadLimit: 64 * 1024,
	}).Error; err != nil {
		t.Fatal(err)
	}
	echo := startHTTPProxyEchoEndpoints(t)
	address := reserveLoopbackPort(t)
	port := portFromAddress(t, address)
	users := []map[string]string{
		{"name": httpProxyTestUser, "userkey": snellV6TestUserKey},
		{"name": "bob", "userkey": snellV6TestBobKey},
	}
	for _, mode := range []string{"default", "unshaped", "unsafe-raw"} {
		t.Run(mode, func(t *testing.T) {
			runtime := startHTTPProxyTestCore(t, snellV6ServerConfig(port, mode, users))
			server := runtime.GetInstance()
			if err := server.RateLimitTracker().Reload(); err != nil {
				t.Fatal(err)
			}

			tcpClient := startHTTPProxyTestBox(t, snellV6ClientConfig(port, mode, snellV6TestUserKey))
			tcpOutbound := getTestSnellOutbound(t, tcpClient)
			assertSnellV6TCPRoundTrip(t, tcpOutbound, echo.tcpAddress, mode, server, httpProxyTestUser)
			assertSnellV6UserTraffic(t, server, httpProxyTestUser)

			udpClient := startHTTPProxyTestBox(t, snellV6ClientConfig(port, mode, snellV6TestBobKey))
			udpOutbound := getTestSnellOutbound(t, udpClient)
			assertSnellV6UDPRoundTrip(t, udpOutbound, echo.udpAddress.String(), mode, server, "bob")
			assertSnellV6UserTraffic(t, server, "bob")

			for _, user := range []string{httpProxyTestUser, "bob"} {
				for _, upload := range []bool{true, false} {
					limiter := server.RateLimitTracker().limiter(user, upload)
					if limiter == nil || limiter.Limit() != 64*1024 {
						t.Fatalf("user %q upload=%v limiter=%v; want 64 KiB/s", user, upload, limiter)
					}
				}
			}

			if mode == "default" {
				wrongClient := startHTTPProxyTestBox(t, snellV6ClientConfig(port, mode, "incorrect-user-key"))
				wrongOutbound := getTestSnellOutbound(t, wrongClient)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				conn, err := wrongOutbound.DialContext(ctx, "tcp", socksaddrForTest(t, echo.tcpAddress))
				if err == nil {
					_ = conn.SetDeadline(time.Now().Add(time.Second))
					_, _ = conn.Write([]byte("wrong-key"))
					response := make([]byte, len("wrong-key"))
					if n, readErr := io.ReadFull(conn, response); readErr == nil && string(response[:n]) == "wrong-key" {
						_ = conn.Close()
						t.Fatal("Snell v6 server accepted an incorrect userkey")
					}
					_ = conn.Close()
				}
			}
			assertSnellV6History(t, httpProxyTestUser, "tcp")
			assertSnellV6History(t, "bob", "udp")
		})
	}
}

func TestMihomoSnellV6SharedPSKLoopback(t *testing.T) {
	binary := os.Getenv("NOVAPANEL_MIHOMO_TEST_BINARY")
	if binary == "" {
		t.Skip("Mihomo integration binary not configured")
	}
	t.Run("unshaped", func(t *testing.T) {
		mode := "unshaped"
		initHTTPProxyTestDatabase(t)
		echo := startHTTPProxyEchoEndpoints(t)
		serverAddress := reserveLoopbackPort(t)
		serverPort := portFromAddress(t, serverAddress)
		serverRuntime := startHTTPProxyTestCore(t, snellV6ServerConfig(serverPort, mode, nil))
		server := serverRuntime.GetInstance()
		proxyAddress := reserveLoopbackPort(t)
		proxyPort := portFromAddress(t, proxyAddress)

		temporary := t.TempDir()
		configPath := filepath.Join(temporary, "mihomo.yaml")
		config := fmt.Sprintf(`socks-port: %d
bind-address: 127.0.0.1
allow-lan: false
mode: rule
log-level: debug
proxies:
  - name: snell
    type: snell
    server: 127.0.0.1
    port: %d
    version: 6
    psk: %q
    mode: %s
rules:
  - IP-CIDR,127.0.0.1/32,snell,no-resolve
  - MATCH,snell
`, proxyPort, serverPort, snellV6TestPSK, mode)
		if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(binary, "-d", filepath.Join(temporary, "home"), "-f", configPath)
		var processOutput bytes.Buffer
		cmd.Stdout, cmd.Stderr = &processOutput, &processOutput
		if err := cmd.Start(); err != nil {
			t.Fatalf("start Mihomo Alpha: %v", err)
		}
		t.Cleanup(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
			_ = cmd.Wait()
		})

		var proxyConn net.Conn
		waitHTTPProxyCondition(t, 10*time.Second, func() bool {
			if cmd.ProcessState != nil {
				return false
			}
			conn, err := net.DialTimeout("tcp", proxyAddress, 150*time.Millisecond)
			if err != nil {
				return false
			}
			proxyConn = conn
			return true
		})
		time.Sleep(200 * time.Millisecond)
		defer proxyConn.Close()
		_ = proxyConn.SetDeadline(time.Now().Add(8 * time.Second))
		if _, err := proxyConn.Write([]byte{5, 1, 0}); err != nil {
			t.Fatalf("send SOCKS5 greeting to Mihomo: %v", err)
		}
		greeting := make([]byte, 2)
		if _, err := io.ReadFull(proxyConn, greeting); err != nil || !bytes.Equal(greeting, []byte{5, 0}) {
			t.Fatalf("Mihomo SOCKS5 greeting = %v, err=%v", greeting, err)
		}
		host, portText, err := net.SplitHostPort(echo.tcpAddress)
		if err != nil {
			t.Fatal(err)
		}
		ip := net.ParseIP(host).To4()
		port, err := strconv.Atoi(portText)
		if err != nil || ip == nil {
			t.Fatalf("parse echo address %q: ip=%v port=%q err=%v", echo.tcpAddress, ip, portText, err)
		}
		request := []byte{5, 1, 0, 1, ip[0], ip[1], ip[2], ip[3], byte(port >> 8), byte(port)}
		if _, err := proxyConn.Write(request); err != nil {
			t.Fatalf("send SOCKS5 CONNECT to Mihomo: %v", err)
		}
		responseHeader := make([]byte, 4)
		if _, err := io.ReadFull(proxyConn, responseHeader); err != nil || !bytes.Equal(responseHeader[:2], []byte{5, 0}) {
			t.Fatalf("Mihomo SOCKS5 CONNECT response = %v, err=%v; process output: %s", responseHeader, err, processOutput.String())
		}
		responseAddrLen := 4
		if responseHeader[3] == 4 {
			responseAddrLen = 16
		} else if responseHeader[3] == 3 {
			length := []byte{0}
			if _, err := io.ReadFull(proxyConn, length); err != nil {
				t.Fatal(err)
			}
			responseAddrLen = int(length[0])
		}
		if _, err := io.CopyN(io.Discard, proxyConn, int64(responseAddrLen+2)); err != nil {
			t.Fatalf("read SOCKS5 bound address: %v", err)
		}
		payload := []byte("mihomo-snell-v6-shared-psk")
		if _, err := proxyConn.Write(payload); err != nil {
			t.Fatalf("write through Mihomo Snell: %v", err)
		}
		response := make([]byte, len(payload))
		if _, err := io.ReadFull(proxyConn, response); err != nil || !bytes.Equal(response, payload) {
			t.Fatalf("Mihomo Snell round-trip = %q, err=%v; process output: %s; server logs: %s", response, err, processOutput.String(), strings.Join(panelLogger.GetLogs(80, "debug"), "\n"))
		}

		waitHTTPProxyCondition(t, 5*time.Second, func() bool {
			for _, session := range server.ConnTracker().Sessions() {
				if session.Inbound == "test-snell" && session.Outbound == "direct" && session.User == "" && session.Network == "tcp" {
					return true
				}
			}
			return false
		})
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var inboundBytes, outboundBytes int64
			for _, stat := range *server.StatsTracker().GetStats() {
				if stat.Traffic <= 0 {
					continue
				}
				switch stat.Resource {
				case "inbound":
					inboundBytes += stat.Traffic
				case "outbound":
					outboundBytes += stat.Traffic
				}
			}
			if inboundBytes > 0 && outboundBytes > 0 {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatal("shared-PSK Snell traffic was not counted in inbound and outbound totals")
	})
}

func snellV6ServerConfig(port int, mode string, users []map[string]string) []byte {
	inbound := map[string]any{
		"type": "snell", "tag": "test-snell", "listen": "127.0.0.1", "listen_port": port,
		"version": 6, "psk": snellV6TestPSK, "mode": mode,
	}
	if users != nil {
		inbound["users"] = users
	}
	return marshalHTTPProxyTestConfig(map[string]any{
		"log":       map[string]any{"level": "debug"},
		"inbounds":  []any{inbound},
		"outbounds": []any{map[string]any{"type": "direct", "tag": "direct"}},
		"route":     map[string]any{"final": "direct"},
	})
}

func snellV6ClientConfig(port int, mode, userkey string) []byte {
	return marshalHTTPProxyTestConfig(map[string]any{
		"log": map[string]any{"disabled": true},
		"outbounds": []any{
			map[string]any{
				"type": "snell", "tag": "proxy", "server": "127.0.0.1", "server_port": port,
				"version": 6, "psk": snellV6TestPSK, "userkey": userkey, "mode": mode, "reuse": true,
			},
			map[string]any{"type": "direct", "tag": "direct"},
		},
		"route": map[string]any{"final": "direct"},
	})
}

func getTestSnellOutbound(t *testing.T, client *Box) adapter.Outbound {
	t.Helper()
	outbound, ok := client.Outbound().Outbound("proxy")
	if !ok {
		t.Fatal("Snell v6 client outbound was not registered")
	}
	return outbound
}

func assertSnellV6TCPRoundTrip(t *testing.T, outbound adapter.Outbound, destination, mode string, server *Box, user string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := outbound.DialContext(ctx, "tcp", socksaddrForTest(t, destination))
	if err != nil {
		t.Fatalf("Snell v6 %s TCP connection failed: %v", mode, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	payload := []byte(fmt.Sprintf("snell-v6-%s-tcp", mode))
	if _, err := conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, response); err != nil || string(response) != string(payload) {
		t.Fatalf("Snell v6 TCP echo = %q, err=%v; want %q", response, err, payload)
	}
	assertSnellV6Session(t, server, user, "tcp")
}

func assertSnellV6UDPRoundTrip(t *testing.T, outbound adapter.Outbound, destination, mode string, server *Box, user string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	address := socksaddrForTest(t, destination)
	packetConn, err := outbound.ListenPacket(ctx, address)
	if err != nil {
		t.Fatalf("Snell v6 %s UDP-over-TCP setup failed: %v", mode, err)
	}
	defer packetConn.Close()
	_ = packetConn.SetDeadline(time.Now().Add(3 * time.Second))
	udpTarget, err := net.ResolveUDPAddr("udp", destination)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(fmt.Sprintf("snell-v6-%s-udp", mode))
	if _, err := packetConn.WriteTo(payload, udpTarget); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 256)
	n, _, err := packetConn.ReadFrom(response)
	if err != nil || string(response[:n]) != string(payload) {
		t.Fatalf("Snell v6 UDP echo = %q, err=%v; want %q", response[:n], err, payload)
	}
	assertSnellV6Session(t, server, user, "udp")
}

func assertSnellV6Session(t *testing.T, server *Box, user, network string) {
	t.Helper()
	waitHTTPProxyCondition(t, 5*time.Second, func() bool {
		for _, session := range server.ConnTracker().Sessions() {
			if session.Inbound == "test-snell" && session.Outbound == "direct" && session.User == user && session.Network == network {
				return true
			}
		}
		return false
	})
}

func assertSnellV6UserTraffic(t *testing.T, server *Box, user string) {
	t.Helper()
	waitHTTPProxyCondition(t, 5*time.Second, func() bool {
		var uploaded, downloaded bool
		for _, stat := range *server.StatsTracker().GetStats() {
			if stat.Resource == "user" && stat.Tag == user && stat.Traffic > 0 {
				if stat.Direction {
					uploaded = true
				} else {
					downloaded = true
				}
			}
		}
		return uploaded && downloaded
	})
}

func assertSnellV6History(t *testing.T, user, network string) {
	t.Helper()
	var client model.Client
	if err := database.GetDB().Where("name = ?", user).First(&client).Error; err != nil {
		t.Fatal(err)
	}
	var history []model.ClientHistoryEntry
	if err := json.Unmarshal(client.History, &history); err != nil {
		t.Fatal(err)
	}
	for _, entry := range history {
		if entry.Inbound == "test-snell" && entry.Outbound == "direct" && entry.Network == network {
			return
		}
	}
	t.Fatalf("Snell history for user %q network %q missing: %+v", user, network, history)
}
