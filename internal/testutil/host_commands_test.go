package testutil

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func sandboxCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func TestHostCommandSandboxRecordsWithoutExecutingScripts(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "executed")
	scriptDir := filepath.Join(dir, "scripts")
	if err := os.Mkdir(scriptDir, 0700); err != nil {
		t.Fatal(err)
	}
	// Real-looking absolute paths must not cause either script body to execute.
	for _, name := range []string{"hy2-forward.sh", "login-guard.sh"} {
		body := "#!/bin/sh\nprintf executed > " + shellQuote(marker) + "\nexit 88\n"
		if err := os.WriteFile(filepath.Join(scriptDir, name), []byte(body), 0700); err != nil {
			t.Fatal(err)
		}
	}
	inboundCall := []string{"bash", "scripts/hy2-forward.sh", "apply", "stable-inbound", "12345", "12345", "tcp"}
	s := NewManagedPortSandbox(t, inboundCall)
	wantCalls := ManagedPortRebuildCalls(inboundCall)
	wantCalls = append(wantCalls, []string{"bash", "scripts/login-guard.sh", "sync", "2095", ""})
	for _, call := range wantCalls {
		args := append([]string(nil), call[1:]...)
		if call[0] == "bash" {
			args[0] = filepath.Join(scriptDir, filepath.Base(args[0]))
		}
		out, err := sandboxCommand(call[0], args...)
		if err != nil {
			t.Fatalf("stub %q: %v: %s", call, err, out)
		}
		if call[0] == "ufw" && string(out) != "Status: active\n" {
			t.Fatalf("ufw response = %q", out)
		}
	}
	s.AssertCalls(t, wantCalls)
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("script body executed or marker stat failed: %v", err)
	}
}

func TestHostCommandSandboxDeniesUnknownCommandsAndArguments(t *testing.T) {
	s := NewManagedPortSandbox(t)
	if got := os.Getenv("PATH"); got != filepath.Dir(s.logPath) || strings.Contains(got, string(os.PathListSeparator)) {
		t.Fatalf("PATH is not exclusive: %q", got)
	}
	entries, err := os.ReadDir(os.Getenv("PATH"))
	if err != nil {
		t.Fatal(err)
	}
	var wantDenied [][]string
	for _, entry := range entries {
		if entry.Name() == "calls" {
			continue
		}
		call := []string{entry.Name(), "unexpected"}
		out, err := sandboxCommand(call[0], call[1:]...)
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 97 || !strings.Contains(string(out), "sandbox denied command") {
			t.Fatalf("stub passed through %q: err=%v output=%s", call, err, out)
		}
		wantDenied = append(wantDenied, call)
	}
	for _, call := range [][]string{
		{"ufw", "allow", "2095"},
		{"bash", "-c", "exit 0"},
		{"bash", "scripts/unknown.sh", "purge"},
		{"bash", "scripts/hy2-forward.sh", "purge", "extra"},
		{"bash", "scripts/hy2-forward.sh", "apply", "panel-web-port", "22", "22", "tcp"},
		{"bash", "scripts/login-guard.sh", "install"},
	} {
		_, err := sandboxCommand(call[0], call[1:]...)
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) || exitErr.ExitCode() != 97 {
			t.Fatalf("unexpected bash/ufw tuple passed: %q: %v", call, err)
		}
		wantDenied = append(wantDenied, call)
	}
	for _, name := range []string{"go", "novapanel-unknown-test-command"} {
		if _, err := sandboxCommand(name); !errors.Is(err, exec.ErrNotFound) {
			t.Fatalf("unknown command %q found a fallback: %v", name, err)
		}
	}
	allowed, denied := s.records(t)
	if len(allowed) != 0 || !reflect.DeepEqual(denied, wantDenied) {
		t.Fatalf("unexpected records: allowed=%q denied=%q want=%q", allowed, denied, wantDenied)
	}
}

func TestHostCommandSandboxDoesNotEvaluateArguments(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "evaluated")
	arg := "'; printf escaped > " + shellQuote(marker) + "; # $(printf substitution)"
	s := NewHostCommandSandbox(t, []string{"scripts/login-guard.sh", "sync", "2095", arg})
	if out, err := sandboxCommand("bash", "scripts/login-guard.sh", "sync", "2095", arg); err != nil {
		t.Fatalf("literal argument: %v: %s", err, out)
	}
	s.AssertCalls(t, [][]string{{"bash", "scripts/login-guard.sh", "sync", "2095", arg}})
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("argument evaluated or marker stat failed: %v", err)
	}
}

func TestHostCommandSandboxRestoresEnvironment(t *testing.T) {
	previous := map[string]string{}
	for _, key := range []string{"PATH", "ENV", "BASH_ENV"} {
		previous[key] = os.Getenv(key)
	}
	t.Run("isolated", func(t *testing.T) {
		NewManagedPortSandbox(t).AssertCalls(t, nil)
	})
	for key, want := range previous {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s not restored: got %q, want %q", key, got, want)
		}
	}
}
