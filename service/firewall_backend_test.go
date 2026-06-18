package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFirewallBackendPriority(t *testing.T) {
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "ufw"), "#!/bin/sh\necho 'Status: inactive'\n")
	writeExecutable(t, filepath.Join(binDir, "iptables"), "#!/bin/sh\nexit 0\n")
	writeExecutable(t, filepath.Join(binDir, "nft"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir)

	if got := detectFirewallBackend(); got != "iptables" {
		t.Fatalf("inactive UFW should fall back to iptables before nftables: got %s", got)
	}

	writeExecutable(t, filepath.Join(binDir, "ufw"), "#!/bin/sh\necho 'Status: active'\n")
	if got := detectFirewallBackend(); got != "UFW" {
		t.Fatalf("active UFW should have highest priority: got %s", got)
	}
}

func writeExecutable(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}
