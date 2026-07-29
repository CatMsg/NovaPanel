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
