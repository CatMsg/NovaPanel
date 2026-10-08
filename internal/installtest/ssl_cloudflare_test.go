package installtest

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCloudflareSSLMenuShell(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	script := filepath.Join(filepath.Dir(source), "ssl_cloudflare_test.sh")
	command := exec.Command("/bin/bash", script)
	root := t.TempDir()
	// No inherited PATH, shell startup files, or credentials reach the fixture.
	command.Env = []string{"PATH=", "HOME=" + root, "TMPDIR=" + root, "LC_ALL=C"}
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Cloudflare shell-menu tests failed: %v\n%s", err, output)
	}
}
