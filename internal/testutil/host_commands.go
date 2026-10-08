// Package testutil contains fixtures for tests, never production command hooks.
package testutil

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// HostCommandSandbox isolates PATH for the audited managed-port/login-guard
// test paths. It is not an OS sandbox: callers must rule out absolute commands
// and direct runtime mutations. Do not use it with parallel tests.
type HostCommandSandbox struct {
	logPath string
}

// NewHostCommandSandbox allows only ufw status and the exact bash argument
// tuples supplied by the test. Even executable-relative scripts are only
// recorded, never read, sourced, or executed. All stubs use /bin/sh builtins.
func NewHostCommandSandbox(t *testing.T, bashCalls ...[]string) *HostCommandSandbox {
	t.Helper()
	dir := t.TempDir()
	s := &HostCommandSandbox{logPath: filepath.Join(dir, "calls")}
	if err := os.WriteFile(s.logPath, nil, 0600); err != nil {
		t.Fatal(err)
	}
	var allow strings.Builder
	allow.WriteString(`if [ "$command" = ufw ] && [ "$#" -eq 1 ] && [ "$1" = status ]; then
    record ALLOW "$command" "$@"
    printf 'Status: active\n'
    exit 0
fi
if [ "$command" = bash ] && [ "$#" -gt 0 ]; then
    case "$1" in
        scripts/hy2-forward.sh|/*/scripts/hy2-forward.sh) script=scripts/hy2-forward.sh ;;
        scripts/login-guard.sh|/*/scripts/login-guard.sh) script=scripts/login-guard.sh ;;
        *) script=unknown ;;
    esac
`)
	for _, call := range bashCalls {
		if len(call) < 2 || (call[0] != "scripts/hy2-forward.sh" && call[0] != "scripts/login-guard.sh") {
			t.Fatalf("invalid sandbox script tuple: %q", call)
		}
		fmt.Fprintf(&allow, "    if [ \"$#\" -eq %d ] && [ \"$script\" = %s ]", len(call), shellQuote(call[0]))
		for i, arg := range call[1:] {
			if strings.ContainsAny(arg, "\t\r\n") {
				t.Fatal("sandbox arguments must not contain record delimiters")
			}
			fmt.Fprintf(&allow, " && [ \"${%d}\" = %s ]", i+2, shellQuote(arg))
		}
		allow.WriteString("; then\n        shift\n        record ALLOW bash \"$script\" \"$@\"\n        exit 0\n    fi\n")
	}
	allow.WriteString("fi\nrecord DENY \"$command\" \"$@\"\nprintf 'sandbox denied command: %s\\n' \"$command\" >&2\nexit 97\n")
	stub := "#!/bin/sh\n" + "log=" + shellQuote(s.logPath) + `
command=${0##*/}
record() {
    # One builtin append preserves empty arguments and never evaluates input.
    line=$1
    shift
    for arg do line="$line	$arg"; done
    printf '%s\n' "$line" >> "$log"
}
` + allow.String()
	for _, name := range []string{
		"bash", "ufw", "sh", "systemctl", "service", "sudo", "iptables", "ip6tables",
		"iptables-save", "iptables-restore", "ip6tables-save", "ip6tables-restore", "nft",
		"firewall-cmd", "fail2ban-client", "apt", "apt-get", "yum", "dnf", "pacman",
		"zypper", "apk", "ip", "ss", "sshd", "mita", "curl", "wget",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(stub), 0700); err != nil {
			t.Fatal(err)
		}
	}
	// No inherited directory, empty entry, or cwd entry can find a real command.
	t.Setenv("PATH", dir)
	t.Setenv("ENV", "")
	t.Setenv("BASH_ENV", "")
	return s
}

// NewManagedPortSandbox permits only the fixed panel fixture and extra inbound
// tuples. Login sync may be absent on Linux without systemd; script bodies
// are never run.
func NewManagedPortSandbox(t *testing.T, inboundCalls ...[]string) *HostCommandSandbox {
	t.Helper()
	var scripts [][]string
	for _, call := range ManagedPortRebuildCalls(inboundCalls...) {
		if call[0] == "bash" {
			scripts = append(scripts, call[1:])
		}
	}
	scripts = append(scripts, []string{"scripts/login-guard.sh", "sync", "2095", ""})
	return NewHostCommandSandbox(t, scripts...)
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

func (s *HostCommandSandbox) records(t *testing.T) (allowed, denied [][]string) {
	t.Helper()
	raw, err := os.ReadFile(s.logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		switch fields[0] {
		case "ALLOW":
			allowed = append(allowed, fields[1:])
		case "DENY":
			denied = append(denied, fields[1:])
		default:
			t.Fatalf("invalid sandbox record: %q", line)
		}
	}
	return
}

// AssertCalls also fails if a denied command was attempted but its error was
// swallowed by the runtime. Call only after the tested goroutines finish.
func (s *HostCommandSandbox) AssertCalls(t *testing.T, want [][]string) {
	t.Helper()
	got, denied := s.records(t)
	if len(denied) != 0 {
		t.Errorf("unexpected host command attempts: %q", denied)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("sandbox calls = %q, want %q", got, want)
	}
}

// ManagedPortRebuildCalls describes one rebuild of the fixed test panel ports.
func ManagedPortRebuildCalls(inboundCalls ...[]string) [][]string {
	calls := [][]string{
		{"ufw", "status"},
		{"bash", "scripts/hy2-forward.sh", "purge"},
		{"ufw", "status"},
		{"bash", "scripts/hy2-forward.sh", "apply", "panel-web-port", "2095", "2095", "tcp"},
		{"bash", "scripts/hy2-forward.sh", "apply", "panel-sub-port", "2096", "2096", "tcp"},
	}
	return append(calls, inboundCalls...)
}

// SystemdPresent mirrors the production read-only support check, not a skip.
func SystemdPresent() bool {
	info, err := os.Stat("/run/systemd/system")
	return err == nil && info.IsDir()
}
