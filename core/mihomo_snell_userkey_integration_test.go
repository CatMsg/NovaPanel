package core

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
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
)

func TestMihomoSnellV6MultiUserSinglePort(t *testing.T) {
	binary := os.Getenv("NOVAPANEL_MIHOMO_USERKEY_TEST_BINARY")
	if binary == "" {
		t.Skip("userkey-enabled Mihomo test binary not configured")
	}
	initHTTPProxyTestDatabase(t)
	if err := database.GetDB().Create(&model.Client{
		Enable: true, Name: "bob", UploadLimit: 64 * 1024, DownloadLimit: 64 * 1024,
	}).Error; err != nil {
		t.Fatal(err)
	}
	echo := startHTTPProxyEchoEndpoints(t)
	serverAddress := reserveLoopbackPort(t)
	serverPort := portFromAddress(t, serverAddress)
	users := []map[string]string{
		{"name": httpProxyTestUser, "userkey": snellV6TestUserKey},
		{"name": "bob", "userkey": snellV6TestBobKey},
	}
	serverRuntime := startHTTPProxyTestCore(t, snellV6ServerConfig(serverPort, "default", users))
	server := serverRuntime.GetInstance()
	if err := server.RateLimitTracker().Reload(); err != nil {
		t.Fatal(err)
	}

	clients := []struct {
		name    string
		userKey string
	}{
		{name: httpProxyTestUser, userKey: snellV6TestUserKey},
		{name: "bob", userKey: snellV6TestBobKey},
	}
	for _, client := range clients {
		client := client
		t.Run(client.name, func(t *testing.T) {
			proxyAddress := reserveLoopbackPort(t)
			proxyPort := portFromAddress(t, proxyAddress)
			controllerAddress := reserveLoopbackPort(t)
			controllerPort := portFromAddress(t, controllerAddress)
			for controllerPort == proxyPort {
				controllerAddress = reserveLoopbackPort(t)
				controllerPort = portFromAddress(t, controllerAddress)
			}
			temporary := t.TempDir()
			configPath := filepath.Join(temporary, "mihomo.yaml")
			config := strings.Join([]string{
				fmt.Sprintf("socks-port: %d", proxyPort),
				"bind-address: 127.0.0.1",
				"allow-lan: false",
				"mode: global",
				"log-level: debug",
				fmt.Sprintf("external-controller: 127.0.0.1:%d", controllerPort),
				"proxies:",
				"  - name: snell-user",
				"    type: snell",
				"    server: 127.0.0.1",
				fmt.Sprintf("    port: %d", serverPort),
				"    version: 6",
				fmt.Sprintf("    psk: %q", snellV6TestPSK),
				"    mode: default",
				"    udp: true",
				fmt.Sprintf("    userkey: %q", client.userKey),
				"proxy-groups:",
				"  - name: GLOBAL",
				"    type: select",
				"    proxies:",
				"      - snell-user",
			}, "\n")
			if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "-d", filepath.Join(temporary, "home"), "-f", configPath)
			var processOutput bytes.Buffer
			cmd.Stdout, cmd.Stderr = &processOutput, &processOutput
			if err := cmd.Start(); err != nil {
				t.Fatalf("start userkey-enabled Mihomo: %v", err)
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
			defer proxyConn.Close()
			selectionRequest, err := http.NewRequest(http.MethodPut, "http://"+controllerAddress+"/proxies/GLOBAL", strings.NewReader(`{"name":"snell-user"}`))
			if err != nil {
				t.Fatalf("build Mihomo proxy selection request: %v", err)
			}
			selectionRequest.Header.Set("Content-Type", "application/json")
			selection, err := (&http.Client{Timeout: 2 * time.Second}).Do(selectionRequest)
			if err != nil {
				t.Fatalf("select Snell through Mihomo controller: %v; output: %s", err, processOutput.String())
			}
			_ = selection.Body.Close()
			if selection.StatusCode != http.StatusNoContent && selection.StatusCode != http.StatusOK {
				t.Fatalf("select Snell through Mihomo controller: HTTP %s; output: %s", selection.Status, processOutput.String())
			}
			_ = proxyConn.SetDeadline(time.Now().Add(8 * time.Second))
			if _, err := proxyConn.Write([]byte{5, 1, 0}); err != nil {
				t.Fatalf("send SOCKS5 greeting: %v", err)
			}
			greeting := make([]byte, 2)
			if _, err := io.ReadFull(proxyConn, greeting); err != nil || !bytes.Equal(greeting, []byte{5, 0}) {
				t.Fatalf("SOCKS5 greeting = %v, err=%v", greeting, err)
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
				t.Fatalf("send SOCKS5 CONNECT: %v", err)
			}
			responseHeader := make([]byte, 4)
			if _, err := io.ReadFull(proxyConn, responseHeader); err != nil || !bytes.Equal(responseHeader[:2], []byte{5, 0}) {
				t.Fatalf("SOCKS5 CONNECT response = %v, err=%v; Mihomo output: %s", responseHeader, err, processOutput.String())
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
			payload := []byte("same-port-per-user-auth:" + client.name)
			if _, err := proxyConn.Write(payload); err != nil {
				t.Fatalf("write through Snell: %v", err)
			}
			response := make([]byte, len(payload))
			if _, err := io.ReadFull(proxyConn, response); err != nil || !bytes.Equal(response, payload) {
				t.Fatalf("Snell echo = %q, err=%v; Mihomo output: %s; server logs: %s", response, err, processOutput.String(), strings.Join(panelLogger.GetLogs(80, "debug"), "\n"))
			}
			assertSnellV6Session(t, server, client.name, "tcp")
			_ = proxyConn.Close()
			assertMihomoSnellUserUDPRoundTrip(t, proxyPort, echo.udpAddress, client.name, server, processOutput.String())
			assertSnellV6UserTraffic(t, server, client.name)
			assertSnellV6History(t, client.name, "tcp")
			assertSnellV6History(t, client.name, "udp")
		})
	}
}

func assertMihomoSnellUserUDPRoundTrip(t *testing.T, socksPort int, destination *net.UDPAddr, user string, server *Box, processOutput string) {
	t.Helper()
	control, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), 2*time.Second)
	if err != nil {
		t.Fatalf("open Mihomo SOCKS5 UDP control connection: %v", err)
	}
	defer control.Close()
	_ = control.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := control.Write([]byte{5, 1, 0}); err != nil {
		t.Fatal(err)
	}
	method := make([]byte, 2)
	if _, err := io.ReadFull(control, method); err != nil || !bytes.Equal(method, []byte{5, 0}) {
		t.Fatalf("Mihomo SOCKS5 method response = %v, err=%v", method, err)
	}
	if _, err := control.Write([]byte{5, 3, 0, 1, 127, 0, 0, 1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	response := make([]byte, 4)
	if _, err := io.ReadFull(control, response); err != nil || response[0] != 5 || response[1] != 0 {
		t.Fatalf("Mihomo SOCKS5 UDP ASSOCIATE response = %v, err=%v", response, err)
	}
	var relayIP net.IP
	switch response[3] {
	case 1:
		address := make([]byte, 4)
		if _, err := io.ReadFull(control, address); err != nil {
			t.Fatal(err)
		}
		relayIP = net.IP(address)
	case 4:
		address := make([]byte, 16)
		if _, err := io.ReadFull(control, address); err != nil {
			t.Fatal(err)
		}
		relayIP = net.IP(address)
	case 3:
		length := []byte{0}
		if _, err := io.ReadFull(control, length); err != nil {
			t.Fatal(err)
		}
		host := make([]byte, int(length[0]))
		if _, err := io.ReadFull(control, host); err != nil {
			t.Fatal(err)
		}
		relayIP = net.ParseIP(string(host))
	default:
		t.Fatalf("unsupported SOCKS5 relay address type: %d", response[3])
	}
	portBytes := make([]byte, 2)
	if _, err := io.ReadFull(control, portBytes); err != nil {
		t.Fatal(err)
	}
	if relayIP == nil || relayIP.IsUnspecified() {
		relayIP = net.ParseIP("127.0.0.1")
	}
	relay := &net.UDPAddr{IP: relayIP, Port: int(binary.BigEndian.Uint16(portBytes))}
	packetConn, err := net.DialUDP("udp", nil, relay)
	if err != nil {
		t.Fatalf("dial Mihomo SOCKS5 UDP relay: %v", err)
	}
	defer packetConn.Close()
	_ = packetConn.SetDeadline(time.Now().Add(5 * time.Second))
	targetIP := destination.IP.To4()
	if targetIP == nil {
		t.Fatalf("UDP echo target must be IPv4 in this test: %v", destination)
	}
	payload := []byte("mihomo-snell-user-udp:" + user)
	packet := []byte{0, 0, 0, 1}
	packet = append(packet, targetIP...)
	packet = append(packet, byte(destination.Port>>8), byte(destination.Port))
	packet = append(packet, payload...)
	if _, err := packetConn.Write(packet); err != nil {
		t.Fatalf("write SOCKS5 UDP packet: %v", err)
	}
	buffer := make([]byte, 2048)
	n, err := packetConn.Read(buffer)
	if err != nil {
		t.Fatalf("read SOCKS5 UDP response: %v; Mihomo output: %s", err, processOutput)
	}
	if n < 10 || buffer[0] != 0 || buffer[1] != 0 || buffer[2] != 0 {
		t.Fatalf("invalid SOCKS5 UDP response header %x", buffer[:n])
	}
	addressLength := 4
	switch buffer[3] {
	case 1:
		addressLength = 4
	case 4:
		addressLength = 16
	case 3:
		if n < 5 {
			t.Fatalf("short SOCKS5 UDP domain response %x", buffer[:n])
		}
		addressLength = int(buffer[4])
	default:
		t.Fatalf("invalid SOCKS5 UDP address type %d", buffer[3])
	}
	payloadOffset := 4 + addressLength + 2
	if buffer[3] == 3 {
		payloadOffset++
	}
	if n < payloadOffset || !bytes.Equal(buffer[payloadOffset:n], payload) {
		t.Fatalf("Mihomo Snell UDP echo = %q, want %q; output: %s", buffer[payloadOffset:n], payload, processOutput)
	}
	assertSnellV6Session(t, server, user, "udp")
}
