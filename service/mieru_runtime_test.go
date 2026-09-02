package service

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/logger"
	"github.com/op/go-logging"
)

func TestStartMieruRuntimeWaitsForRunningStatus(t *testing.T) {
	workDir := t.TempDir()
	binary := filepath.Join(workDir, "mita")
	script := `#!/usr/bin/env bash
set -euo pipefail
case "${1:-}" in
  run)
    touch "${MITA_UDS_PATH}.ready"
    trap 'rm -f "${MITA_UDS_PATH}.ready"; exit 0' TERM INT
    while :; do sleep 1; done
    ;;
  status)
    [[ -f "${MITA_UDS_PATH}.ready" ]]
    printf 'RUNNING\n'
    ;;
esac
`
	if err := os.WriteFile(binary, []byte(script), 0755); err != nil {
		t.Fatalf("write fake mita: %v", err)
	}

	configPath := filepath.Join(workDir, "server.json")
	socketPath := filepath.Join(workDir, "mita.sock")
	runtimeState, err := startMieruRuntime(binary, configPath, socketPath, nil)
	if err != nil {
		t.Fatalf("start fake mita: %v", err)
	}
	if !runtimeState.running.Load() {
		t.Fatal("fake mita was not marked running after readiness check")
	}
	stopMieruRuntime(runtimeState)
	if runtimeState.running.Load() {
		t.Fatal("fake mita was still running after stop")
	}
}

func TestRequiresMitaRestartOnlyForNonReloadableFields(t *testing.T) {
	base := []byte(`{"portBindings":[{"port":20000,"protocol":"TCP"}],"users":[{"name":"one","hashedPassword":"abc"}],"loggingLevel":"INFO","mtu":1400,"dns":{"dualStack":"PREFER_IPv4"}}`)
	reloadable := []byte(`{"portBindings":[{"port":20000,"protocol":"TCP"}],"users":[{"name":"one","hashedPassword":"abc"},{"name":"two","hashedPassword":"def"}],"loggingLevel":"DEBUG","mtu":1400,"dns":{"dualStack":"PREFER_IPv4"}}`)
	if requiresMitaRestart(base, reloadable) {
		t.Fatal("reloadable Mieru changes unexpectedly require restart")
	}
	credentialChanged := []byte(`{"portBindings":[{"port":20001,"protocol":"TCP"}],"users":[{"name":"one","hashedPassword":"def"}],"loggingLevel":"INFO","mtu":1400,"dns":{"dualStack":"PREFER_IPv4"}}`)
	if !requiresMitaRestart(base, credentialChanged) {
		t.Fatal("Mieru credential changes should restart the service to close existing sessions")
	}
	userRemoved := []byte(`{"portBindings":[{"port":20001,"protocol":"TCP"}],"users":[],"loggingLevel":"INFO","mtu":1400,"dns":{"dualStack":"PREFER_IPv4"}}`)
	if !requiresMitaRestart(base, userRemoved) {
		t.Fatal("removing a Mieru user should restart the service to enforce access immediately")
	}
	trafficPattern := []byte(`{"portBindings":[{"port":20001,"protocol":"TCP"}],"users":[{"name":"one","hashedPassword":"def"}],"loggingLevel":"INFO","trafficPattern":{"seed":1031},"mtu":1400,"dns":{"dualStack":"PREFER_IPv4"}}`)
	if !requiresMitaRestart(base, trafficPattern) {
		t.Fatal("Mieru traffic pattern change should require restart")
	}
	portChanged := []byte(`{"portBindings":[{"port":20001,"protocol":"TCP"}],"users":[{"name":"one","hashedPassword":"abc"}],"loggingLevel":"INFO","mtu":1400,"dns":{"dualStack":"PREFER_IPv4"}}`)
	if !requiresMitaRestart(base, portChanged) {
		t.Fatal("Mieru listen port change should require restart")
	}
	mtuChanged := []byte(`{"portBindings":[{"port":20000,"protocol":"TCP"}],"users":[{"name":"one","hashedPassword":"abc"}],"loggingLevel":"INFO","mtu":1380,"dns":{"dualStack":"PREFER_IPv4"}}`)
	if !requiresMitaRestart(base, mtuChanged) {
		t.Fatal("Mieru MTU change should require restart")
	}
	egressChanged := []byte(`{"portBindings":[{"port":20000,"protocol":"TCP"}],"users":[{"name":"one","hashedPassword":"abc"}],"loggingLevel":"INFO","mtu":1400,"dns":{"dualStack":"PREFER_IPv4"},"egress":{"proxies":[{"name":"novapanel","protocol":"SOCKS5_PROXY_PROTOCOL","host":"127.0.0.1","port":39000}],"rules":[{"ipRanges":["*"],"domainNames":["*"],"action":"PROXY","proxyNames":["novapanel"]}]}}`)
	if !requiresMitaRestart(base, egressChanged) {
		t.Fatal("Mieru egress bridge change should require restart")
	}
}

func TestMieruWatchdogRequiresConsecutiveFailures(t *testing.T) {
	logger.InitLogger(logging.ERROR)
	service := NewMieruService()
	runtimeState := &mieruRuntime{}
	runtimeState.running.Store(true)
	service.active = runtimeState
	service.total = 1

	for attempt := 1; attempt < mieruWatchThreshold; attempt++ {
		if service.recordWatchdogResult(runtimeState, errors.New("probe failed")) {
			t.Fatalf("watchdog requested restart after only %d failures", attempt)
		}
	}
	if !service.recordWatchdogResult(runtimeState, errors.New("probe failed")) {
		t.Fatal("watchdog did not request restart at the failure threshold")
	}
	if service.recordWatchdogResult(runtimeState, nil) {
		t.Fatal("successful probe requested a restart")
	}
	if service.watchFailures != 0 {
		t.Fatalf("successful probe left %d failures", service.watchFailures)
	}
}

func TestProbeMieruBridgePerformsSOCKSHandshake(t *testing.T) {
	const (
		username = "alice"
		password = "0123456789abcdef"
	)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	serverErr := make(chan error, 1)
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer connection.Close()
		_ = connection.SetDeadline(time.Now().Add(time.Second))
		hello := make([]byte, 3)
		if _, err := io.ReadFull(connection, hello); err != nil {
			serverErr <- err
			return
		}
		if string(hello) != string([]byte{0x05, 0x01, 0x02}) {
			serverErr <- errors.New("unexpected SOCKS5 greeting")
			return
		}
		if _, err = connection.Write([]byte{0x05, 0x02}); err != nil {
			serverErr <- err
			return
		}
		authHeader := make([]byte, 2)
		if _, err = io.ReadFull(connection, authHeader); err != nil {
			serverErr <- err
			return
		}
		if authHeader[0] != 0x01 || int(authHeader[1]) != len(username) {
			serverErr <- errors.New("unexpected SOCKS5 authentication header")
			return
		}
		authPayload := make([]byte, len(username)+1)
		if _, err = io.ReadFull(connection, authPayload); err != nil {
			serverErr <- err
			return
		}
		if string(authPayload[:len(username)]) != username || int(authPayload[len(username)]) != len(password) {
			serverErr <- errors.New("unexpected SOCKS5 username")
			return
		}
		passwordPayload := make([]byte, len(password))
		if _, err = io.ReadFull(connection, passwordPayload); err != nil {
			serverErr <- err
			return
		}
		if string(passwordPayload) != password {
			serverErr <- errors.New("unexpected SOCKS5 password")
			return
		}
		_, err = connection.Write([]byte{0x01, 0x00})
		serverErr <- err
	}()

	address := listener.Addr().(*net.TCPAddr)
	payload, err := json.Marshal(mitaServerConfig{Users: []mitaUser{{
		Name: username, HashedPassword: password,
	}}, Egress: mitaEgress{
		Proxies: []mitaEgressProxy{{
			Name:     mieruBridgeProxyName,
			Protocol: "SOCKS5_PROXY_PROTOCOL",
			Host:     address.IP.String(),
			Port:     address.Port,
		}},
	}})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := probeMieruBridge(payload); err != nil {
		t.Fatalf("probe bridge: %v", err)
	}
	if err := <-serverErr; err != nil {
		t.Fatalf("SOCKS server: %v", err)
	}
}

func TestProbeMieruBridgeRejectsUnavailableBridge(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	payload := []byte(`{"users":[{"name":"alice","hashedPassword":"hash"}],"egress":{"proxies":[{"name":"novapanel","protocol":"SOCKS5_PROXY_PROTOCOL","host":"127.0.0.1","port":` + strconv.Itoa(port) + `}]}}`)
	if err := probeMieruBridge(payload); err == nil {
		t.Fatal("unavailable bridge passed the watchdog probe")
	}
}

func TestProbeMieruBridgeRequiresProbeUser(t *testing.T) {
	payload := []byte(`{"egress":{"proxies":[{"name":"novapanel","protocol":"SOCKS5_PROXY_PROTOCOL","host":"127.0.0.1","port":1080}]}}`)
	if err := probeMieruBridge(payload); err == nil {
		t.Fatal("bridge probe passed without an authenticated user")
	}
}

func TestBuildMieruAuthRequestRejectsOversizedCredentials(t *testing.T) {
	if _, err := buildMieruAuthRequest(strings.Repeat("u", 256), "password"); err == nil {
		t.Fatal("oversized username was accepted")
	}
	if _, err := buildMieruAuthRequest("username", strings.Repeat("p", 256)); err == nil {
		t.Fatal("oversized password was accepted")
	}
	request, err := buildMieruAuthRequest("user", "pass")
	if err != nil {
		t.Fatalf("valid credentials were rejected: %v", err)
	}
	if string(request) != string([]byte{0x01, 0x04, 'u', 's', 'e', 'r', 0x04, 'p', 'a', 's', 's'}) {
		t.Fatalf("unexpected authentication request: %x", request)
	}
}

func TestMieruBinaryPathRejectsRelativeOverride(t *testing.T) {
	t.Setenv("SUI_MITA_BIN", "bin/mita")
	if _, err := mieruBinaryPath(); err == nil {
		t.Fatal("relative mita binary override was accepted")
	}
}
