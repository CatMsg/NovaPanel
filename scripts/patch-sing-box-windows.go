package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	modulePath = "github.com/sagernet/sing-box"
	oldSnippet = `networkManager.InterfaceMonitor().MyInterface()`
	newSnippet = `func() string { interfaces := networkManager.InterfaceMonitor().MyInterfaces(); if len(interfaces) == 0 { return "" }; return interfaces[0] }()`
)

func main() {
	if err := patchSingBoxWindowsCompat(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func patchSingBoxWindowsCompat() error {
	if err := exec.Command("go", "mod", "download", modulePath).Run(); err != nil {
		return fmt.Errorf("download %s: %w", modulePath, err)
	}

	moduleDir, err := moduleDir(modulePath)
	if err != nil {
		return err
	}

	target := filepath.Join(moduleDir, "dns", "transport", "local", "resolv_windows.go")
	updated, err := patchFile(target)
	if err != nil {
		return err
	}

	if updated {
		fmt.Println("patched sing-box Windows compatibility:", target)
		return nil
	}

	fmt.Println("sing-box Windows compatibility already patched:", target)
	return nil
}

func moduleDir(path string) (string, error) {
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", path).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("locate %s module dir: %w: %s", path, err, strings.TrimSpace(string(out)))
	}

	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", errors.New("go list returned an empty module directory")
	}

	return dir, nil
}

func patchFile(target string) (bool, error) {
	data, err := os.ReadFile(target)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", target, err)
	}

	content := string(data)
	switch {
	case strings.Contains(content, newSnippet):
		return false, nil
	case strings.Contains(content, oldSnippet):
		content = strings.ReplaceAll(content, oldSnippet, newSnippet)
	default:
		return false, fmt.Errorf("compatibility snippet not found in %s", target)
	}

	// Best effort: some module caches do not honor chmod the same way on every OS.
	_ = os.Chmod(target, 0o644)

	if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", target, err)
	}

	return true, nil
}
