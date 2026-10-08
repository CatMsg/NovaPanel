package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const modulePath = "github.com/sagernet/sing-box"

func main() {
	if err := checkSingBoxWindowsCompat(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// Retain the legacy script filename for existing build callers, but never edit
// the replacement or the global module cache. Alpha.4 already uses MyInterfaces.
func checkSingBoxWindowsCompat() error {
	verify := exec.Command("go", "run", "-mod=readonly", "./scripts/verify-sing-box.go")
	verify.Stdout = os.Stdout
	verify.Stderr = os.Stderr
	if err := verify.Run(); err != nil {
		return fmt.Errorf("verify pinned sing-box source: %w", err)
	}

	moduleDir, err := moduleDir(modulePath)
	if err != nil {
		return err
	}

	target := filepath.Join(moduleDir, "dns", "transport", "local", "systemconfig", "source_windows.go")
	if err := checkWindowsCompatFile(target); err != nil {
		return err
	}

	fmt.Println("verified sing-box Windows compatibility (read-only):", target)
	return nil
}

func moduleDir(path string) (string, error) {
	out, err := exec.Command("go", "list", "-mod=readonly", "-m", "-f", "{{.Dir}}", path).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("locate %s module dir: %w: %s", path, err, strings.TrimSpace(string(out)))
	}

	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("go list returned an empty module directory")
	}

	return dir, nil
}

func checkWindowsCompatFile(target string) error {
	data, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("read %s: %w", target, err)
	}

	content := string(data)
	if !strings.Contains(content, "s.interfaceMonitor.MyInterfaces()") || strings.Contains(content, ".MyInterface(") {
		return fmt.Errorf("unsupported Windows interface implementation in %s; refusing to mutate dependency source", target)
	}

	return nil
}
