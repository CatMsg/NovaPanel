package service

import (
	"os"
	"path/filepath"
	"testing"
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
