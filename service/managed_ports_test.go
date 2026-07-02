package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestValidateManagedPanelPorts(t *testing.T) {
	originalPorts := getSSHListenPorts()
	t.Cleanup(func() {
		_ = storeSSHListenPorts(originalPorts, nil)
	})

	if err := ValidateManagedPanelPorts(2095, 2096); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	if err := ValidateManagedPanelPorts(2095, 2095); err == nil {
		t.Fatal("expected duplicate panel ports to be rejected")
	}

	if err := storeSSHListenPorts([]int{2095}, nil); err != nil {
		t.Fatalf("store ssh listen ports: %v", err)
	}
	if err := ValidateManagedPanelPorts(2095, 2096); err == nil {
		t.Fatal("expected ssh conflict to be rejected")
	}
}

func TestSyncManagedPanelPortForwardingInvokesScript(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("panel port forwarding is only exercised on linux")
	}

	originalPorts := getSSHListenPorts()
	t.Cleanup(func() {
		_ = storeSSHListenPorts(originalPorts, nil)
	})
	if err := storeSSHListenPorts([]int{2222}, nil); err != nil {
		t.Fatalf("store ssh listen ports: %v", err)
	}

	workDir := t.TempDir()
	scriptsDir := filepath.Join(workDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}

	logFile := filepath.Join(workDir, "panel-forward.log")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${HY2_MOCK_LOG:?}"
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "hy2-forward.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir workdir: %v", err)
	}

	t.Setenv("HY2_MOCK_LOG", logFile)

	svc := &SettingService{}
	if err := svc.SyncManagedPanelPortForwarding(2095, 3000, 2096, 3001); err != nil {
		t.Fatalf("sync managed panel ports failed: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "apply panel-web-port 3000 3000 tcp") {
		t.Fatalf("web port forwarding was not applied:\n%s", log)
	}
	if strings.Contains(log, "udp") {
		t.Fatalf("panel port forwarding unexpectedly included udp:\n%s", log)
	}
	if !strings.Contains(log, "apply panel-sub-port 3001 3001 tcp") {
		t.Fatalf("sub port forwarding was not applied:\n%s", log)
	}
}

func TestSyncManagedEndpointPortForwardingInvokesScript(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("endpoint port forwarding is only exercised on linux")
	}

	originalPorts := getSSHListenPorts()
	t.Cleanup(func() {
		_ = storeSSHListenPorts(originalPorts, nil)
	})
	if err := storeSSHListenPorts([]int{2222}, nil); err != nil {
		t.Fatalf("store ssh listen ports: %v", err)
	}

	workDir := t.TempDir()
	scriptsDir := filepath.Join(workDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}

	logFile := filepath.Join(workDir, "endpoint-forward.log")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${HY2_MOCK_LOG:?}"
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "hy2-forward.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir workdir: %v", err)
	}

	t.Setenv("HY2_MOCK_LOG", logFile)

	oldEndpoint := &model.Endpoint{
		Type: "wireguard",
		Tag:  "endpoint-old",
		Options: json.RawMessage(`{
			"listen_port": 3000
		}`),
	}
	newEndpoint := &model.Endpoint{
		Type: "wireguard",
		Tag:  "endpoint-new",
		Options: json.RawMessage(`{
			"listen_port": 3001
		}`),
	}

	if err := syncManagedEndpointPortForwarding(oldEndpoint, newEndpoint); err != nil {
		t.Fatalf("sync managed endpoint ports failed: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "remove endpoint-old 3000 3000") {
		t.Fatalf("old endpoint port forwarding was not removed:\n%s", log)
	}
	if !strings.Contains(log, "apply endpoint-new 3001 3001") {
		t.Fatalf("new endpoint port forwarding was not applied:\n%s", log)
	}
}

func TestSyncManagedMasqueEndpointPortForwardingInvokesScript(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("endpoint port forwarding is only exercised on linux")
	}

	originalPorts := getSSHListenPorts()
	t.Cleanup(func() {
		_ = storeSSHListenPorts(originalPorts, nil)
	})
	if err := storeSSHListenPorts([]int{2222}, nil); err != nil {
		t.Fatalf("store ssh listen ports: %v", err)
	}

	workDir := t.TempDir()
	scriptsDir := filepath.Join(workDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}

	logFile := filepath.Join(workDir, "masque-forward.log")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${HY2_MOCK_LOG:?}"
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "hy2-forward.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir workdir: %v", err)
	}

	t.Setenv("HY2_MOCK_LOG", logFile)

	oldEndpoint := &model.Endpoint{
		Type: "masque",
		Tag:  "masque-old",
		Options: json.RawMessage(`{
			"port": 443,
			"server": "tk.mile.news"
		}`),
	}
	newEndpoint := &model.Endpoint{
		Type: "masque",
		Tag:  "masque-new",
		Options: json.RawMessage(`{
			"port": 8444,
			"server": "tk.mile.news"
		}`),
	}

	if err := syncManagedEndpointPortForwarding(oldEndpoint, newEndpoint); err != nil {
		t.Fatalf("sync managed masque endpoint ports failed: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "remove masque-old 443 443 udp") {
		t.Fatalf("old masque port forwarding was not removed:\n%s", log)
	}
	if !strings.Contains(log, "apply masque-new 8444 8444 udp") {
		t.Fatalf("new masque port forwarding was not applied:\n%s", log)
	}
	if strings.Contains(log, "tcp") {
		t.Fatalf("masque port forwarding unexpectedly included tcp:\n%s", log)
	}
}

func TestSyncManagedTailscaleEndpointPortForwardingInvokesScript(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("endpoint port forwarding is only exercised on linux")
	}

	originalPorts := getSSHListenPorts()
	t.Cleanup(func() {
		_ = storeSSHListenPorts(originalPorts, nil)
	})
	if err := storeSSHListenPorts([]int{2222}, nil); err != nil {
		t.Fatalf("store ssh listen ports: %v", err)
	}

	workDir := t.TempDir()
	scriptsDir := filepath.Join(workDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}

	logFile := filepath.Join(workDir, "tailscale-forward.log")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${HY2_MOCK_LOG:?}"
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "hy2-forward.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir workdir: %v", err)
	}

	t.Setenv("HY2_MOCK_LOG", logFile)

	oldEndpoint := &model.Endpoint{
		Type: "tailscale",
		Tag:  "tailscale-old",
		Options: json.RawMessage(`{
			"relay_server_port": 41641
		}`),
	}
	newEndpoint := &model.Endpoint{
		Type: "tailscale",
		Tag:  "tailscale-new",
		Options: json.RawMessage(`{
			"relay_server_port": 41642
		}`),
	}

	if err := syncManagedEndpointPortForwarding(oldEndpoint, newEndpoint); err != nil {
		t.Fatalf("sync managed tailscale endpoint ports failed: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	log := string(data)
	if !strings.Contains(log, "remove tailscale-old 41641 41641") {
		t.Fatalf("old tailscale port forwarding was not removed:\n%s", log)
	}
	if !strings.Contains(log, "apply tailscale-new 41642 41642") {
		t.Fatalf("new tailscale port forwarding was not applied:\n%s", log)
	}
}

func TestRebuildAllManagedPortForwardingPurgesBeforeRebuild(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("managed port forwarding is only exercised on linux")
	}

	originalPorts := getSSHListenPorts()
	t.Cleanup(func() {
		_ = storeSSHListenPorts(originalPorts, nil)
	})
	if err := storeSSHListenPorts([]int{2222}, nil); err != nil {
		t.Fatalf("store ssh listen ports: %v", err)
	}

	workDir := t.TempDir()
	if err := database.InitDB(filepath.Join(workDir, "managed-ports.db")); err != nil {
		t.Fatalf("init db: %v", err)
	}

	scriptsDir := filepath.Join(workDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o755); err != nil {
		t.Fatalf("mkdir scripts dir: %v", err)
	}

	logFile := filepath.Join(workDir, "rebuild-all.log")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${HY2_MOCK_LOG:?}"
`
	if err := os.WriteFile(filepath.Join(scriptsDir, "hy2-forward.sh"), []byte(script), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	binDir := filepath.Join(workDir, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin dir: %v", err)
	}
	writeExecutable(t, filepath.Join(binDir, "iptables"), "#!/bin/sh\nexit 0\n")

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir workdir: %v", err)
	}

	t.Setenv("HY2_MOCK_LOG", logFile)
	t.Setenv("PATH", binDir)

	db := database.GetDB()
	inbound := model.Inbound{
		Type: "vless",
		Tag:  "inbound-a",
		Options: json.RawMessage(`{
			"listen_port": 4100
		}`),
	}
	if err := db.Create(&inbound).Error; err != nil {
		t.Fatalf("create inbound: %v", err)
	}

	endpoint := model.Endpoint{
		Type: "wireguard",
		Tag:  "endpoint-a",
		Options: json.RawMessage(`{
			"listen_port": 4200
		}`),
	}
	if err := db.Create(&endpoint).Error; err != nil {
		t.Fatalf("create endpoint: %v", err)
	}

	settingSvc := &SettingService{}
	inboundSvc := &InboundService{}
	endpointSvc := &EndpointService{}
	if err := settingSvc.RebuildAllManagedPortForwarding(inboundSvc, endpointSvc); err != nil {
		t.Fatalf("rebuild all managed port forwarding failed: %v", err)
	}

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	lines := strings.Fields(strings.TrimSpace(string(data)))
	if len(lines) < 1 || lines[0] != "purge" {
		t.Fatalf("expected first command to purge existing rules, got %q", string(data))
	}

	log := string(data)
	if !strings.Contains(log, "apply panel-web-port 2095 2095 tcp") {
		t.Fatalf("panel web port forwarding was not rebuilt:\n%s", log)
	}
	if !strings.Contains(log, "apply panel-sub-port 2096 2096 tcp") {
		t.Fatalf("panel sub port forwarding was not rebuilt:\n%s", log)
	}
	if !strings.Contains(log, "apply inbound-a 4100 4100 tcp,udp") {
		t.Fatalf("inbound port forwarding was not rebuilt:\n%s", log)
	}
	if !strings.Contains(log, "apply endpoint-a 4200 4200 tcp,udp") {
		t.Fatalf("endpoint port forwarding was not rebuilt:\n%s", log)
	}
}
