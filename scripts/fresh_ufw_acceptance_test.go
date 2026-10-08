package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func freshUFWScript(t *testing.T) string {
	t.Helper()
	script, err := os.ReadFile("test-fresh-ufw-linux.sh")
	if err != nil {
		t.Fatal(err)
	}
	return string(script)
}

// Load definitions only. None of the privileged native acceptance body runs.
func runFreshUFWHelper(t *testing.T, body string, env ...string) (string, error) {
	t.Helper()
	contents := freshUFWScript(t)
	main := strings.Index(contents, "\nrequire_github_hosted_release_job\n")
	if main < 0 {
		t.Fatal("cannot locate guarded acceptance entry point")
	}
	cmd := exec.Command("bash", "-c", contents[:main]+"\n"+body)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func TestFreshUFWAcceptanceUsesValidatedAbsoluteSSHD(t *testing.T) {
	contents := freshUFWScript(t)

	resolve := strings.Index(contents, `SSHD_BIN=$(command -v sshd) || die "OpenSSH daemon executable is unavailable"`)
	validate := strings.Index(contents, `[[ "$SSHD_BIN" == /* && -x "$SSHD_BIN" ]] || die "resolved sshd path must be absolute and executable"`)
	if resolve < 0 || validate <= resolve {
		t.Fatal("sshd must be resolved with command -v and validated as an absolute executable before use")
	}

	for _, invocation := range []string{
		`INITIAL_SSH_EFFECTIVE=$(LC_ALL=C "$SSHD_BIN" -T)`,
		`sshd_effective=$(LC_ALL=C "$SSHD_BIN" -T -f "$WORK_DIR/sshd_config")`,
		`"$SSHD_BIN" -t -f "$WORK_DIR/sshd_config"`,
		`"$SSHD_BIN" -D -f "$WORK_DIR/sshd_config" -E "$WORK_DIR/sshd.log" &`,
	} {
		if position := strings.Index(contents, invocation); position <= validate {
			t.Errorf("expected validated absolute sshd path invocation after guard: %s", invocation)
		}
	}

	for lineNumber, line := range strings.Split(contents, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && fields[0] == "sshd" && (fields[1] == "-T" || fields[1] == "-t" || fields[1] == "-D") {
			t.Errorf("line %d invokes sshd without the validated absolute path", lineNumber+1)
		}
	}
}

func TestFreshUFWBaselineOwnership(t *testing.T) {
	v4 := `LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=411,fd=3))`
	v6 := `LISTEN 0 128 [::]:22 [::]:* users:(("sshd",pid=411,fd=4))`
	config := "port 22\nlistenaddress 0.0.0.0:22\nlistenaddress [::]:22"
	tests := []struct {
		name, listeners, config, ports string
		ok                             bool
	}{
		{"hosted SSH22", v4 + "\n" + v6, config, "22", true},
		{"IPv4 only baseline", v4, config, "22", true},
		{"known socket", strings.ReplaceAll(v6, `"sshd",pid=411`, `"systemd",pid=1`), config, "22", true},
		{"socket and service coownership", strings.Replace(v6, "fd=4))", `fd=4),("systemd",pid=1,fd=9))`, 1), config, "22", true},
		{"known socket override", strings.ReplaceAll(strings.ReplaceAll(v6, ":22", ":2200"), `"sshd",pid=411`, `"systemd",pid=1`), config, "2200", true},
		{"multiple actual ports", v4 + "\n" + strings.ReplaceAll(v6, ":22", ":2200"), config + "\nport 2200", "22\n2200", true},
		{"unrelated listener", v4 + "\nLISTEN 0 128 127.0.0.1:8080 0.0.0.0:* users:((\"node\",pid=555,fd=3))", config, "22", true},
		{"unknown owner on SSH port", strings.ReplaceAll(v4, `"sshd"`, `"node"`), config, "22", false},
		{"unknown coowner", strings.Replace(v4, "fd=3))", `fd=3),("node",pid=555,fd=9))`, 1), config, "22", false},
		{"wrong daemon PID", strings.ReplaceAll(v4, "pid=411", "pid=555"), config, "22", false},
		{"non-init systemd", strings.ReplaceAll(v6, `"sshd",pid=411`, `"systemd",pid=555`), config, "22", false},
		{"unverified socket", strings.ReplaceAll(strings.ReplaceAll(v6, ":22", ":2201"), `"sshd",pid=411`, `"systemd",pid=1`), config, "2201", false},
		{"sshd-session is not service listener", strings.ReplaceAll(v4, `"sshd"`, `"sshd-session"`), config, "22", false},
		{"missing ownership", "LISTEN 0 128 0.0.0.0:22 0.0.0.0:*", config, "22", false},
		{"malformed ownership suffix", v4 + " unexpected", config, "22", false},
		{"unknown SSH port", strings.ReplaceAll(v4, ":22", ":2200"), config, "22", false},
		{"missing expected port", v4, config, "22\n2200", false},
		{"invalid config port", v4, "port invalid", "22", false},
		{"empty baseline", "", config, "22", false},
	}
	for _, port := range []string{"2222", "39095", "39096", "39998"} {
		tests = append(tests, struct {
			name, listeners, config, ports string
			ok                             bool
		}{"reserved " + port, v4 + "\nLISTEN 0 128 127.0.0.1:" + port + " 0.0.0.0:*", config, "22", false})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `
verify_baseline_sshd_pid() { [[ "$1" == 411 ]]; }
verify_baseline_socket() { [[ "$1" == '[::]:22' || "$1" == '[::]:2200' ]]; }
inspect_baseline_ssh "$FIXTURE_LISTENERS" "$FIXTURE_CONFIG" "$FIXTURE_PORTS"
`, "FIXTURE_LISTENERS="+tt.listeners, "FIXTURE_CONFIG="+tt.config, "FIXTURE_PORTS="+tt.ports)
			if (err == nil) != tt.ok {
				t.Fatalf("success=%v, want %v: %v\n%s", err == nil, tt.ok, err, output)
			}
		})
	}
}

func TestFreshUFWBaselineNormalizesQueuesAndOwnerOrder(t *testing.T) {
	output, err := runFreshUFWHelper(t, `
verify_baseline_sshd_pid() { [[ "$1" == 411 ]]; }
verify_baseline_socket() { [[ "$1" == '[::]:22' ]]; }
config='port 22'
before='LISTEN 0 128 [::]:22 [::]:* users:(("sshd",pid=411,fd=4),("systemd",pid=1,fd=9))'
after='LISTEN 1 256 [::]:22 [::]:* users:(("systemd",pid=1,fd=9),("sshd",pid=411,fd=4))'
baseline=$(inspect_baseline_ssh "$before" "$config" 22)
[[ "$baseline" == "$(inspect_baseline_ssh "$after" "$config" 22)" ]]
changed_fd="${after/fd=4/fd=5}"
[[ "$baseline" != "$(inspect_baseline_ssh "$changed_fd" "$config" 22)" ]]
`)
	if err != nil {
		t.Fatalf("normalized listener identity comparison failed: %v\n%s", err, output)
	}
}

func TestFreshUFWBaselineRejectsDaemonOverrides(t *testing.T) {
	for _, tt := range []struct {
		command string
		ok      bool
	}{
		{"/usr/sbin/sshd -D", true},
		{"/usr/sbin/sshd -D -e", true},
		{"sshd: /usr/sbin/sshd -D [listener] 0 of 10-100 startups", true},
		{"sshd: /usr/sbin/sshd -D -e [listener] 1 of 10-100 startups", true},
		{"/usr/sbin/sshd -D -f /tmp/config", false},
		{"/usr/sbin/sshd -D -p 22", false},
		{"/usr/sbin/sshd -D -o Port=22", false},
		{"sshd: /usr/sbin/sshd -D -f /tmp/config [listener] 0 of 10-100 startups", false},
		{"sshd: runner [priv]", false},
		{"/unknown/sshd -D", false},
	} {
		t.Run(tt.command, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `SSHD_BIN=/usr/sbin/sshd; baseline_sshd_command_valid "$FIXTURE_COMMAND"`, "FIXTURE_COMMAND="+tt.command)
			if (err == nil) != tt.ok {
				t.Fatalf("success=%v, want %v: %v\n%s", err == nil, tt.ok, err, output)
			}
		})
	}
}

func TestFreshUFWPortDeclarationRetainsImplicitAndExplicitDefaults(t *testing.T) {
	for _, tt := range []struct{ config, want string }{
		{"port 22\nlistenaddress 0.0.0.0:22\nlistenaddress [::]:22", "Port 22\nPort 2222\n"},
		{"port 2200\nlistenaddress [::]:2200", "Port 2200\nPort 2222\n"},
		{"port 22\nport 2200\nlistenaddress [::]:2255", "Port 22\nPort 2200\nPort 2222\nPort 2255\n"},
	} {
		output, err := runFreshUFWHelper(t, `
{ ssh_config_ports <<<"$FIXTURE_CONFIG"; printf '%s\n' "$SSH_PORT"; } | sort -nu | sed 's/^/Port /'
`, "FIXTURE_CONFIG="+tt.config)
		if err != nil || output != tt.want {
			t.Fatalf("ports=%q, want %q: %v", output, tt.want, err)
		}
	}
}

func TestFreshUFWBaselineSocketBinding(t *testing.T) {
	for _, tt := range []struct {
		name, endpoint, binding, trigger string
		ok                               bool
	}{
		{"Ubuntu wildcard socket22", "*:22", "[::]:22 (Stream)", "ssh.service", true},
		{"bare socket22", "[::]:22", "22 (Stream)", "ssh.service", true},
		{"explicit v4 socket", "0.0.0.0:22", "0.0.0.0:22 (Stream)", "ssh.service", true},
		{"dualstack socket", "[::]:22", "0.0.0.0:22 (Stream) [::]:22 (Stream)", "ssh.service", true},
		{"wrong endpoint", "127.0.0.1:22", "[::]:22 (Stream)", "ssh.service", false},
		{"wrong port", "[::]:22", "[::]:2200 (Stream)", "ssh.service", false},
		{"unknown trigger", "[::]:22", "[::]:22 (Stream)", "unknown.service", false},
		{"ambiguous triggers", "[::]:22", "[::]:22 (Stream)", "ssh.service unknown.service", false},
		{"datagram", "[::]:22", "[::]:22 (Datagram)", "ssh.service", false},
		{"empty binding", "[::]:22", "", "ssh.service", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `
systemctl() {
    case "$1" in
        is-active) [[ "$3" == ssh.socket ]] ;;
        show)
            [[ "$2" == ssh.socket ]] || return 3
            case "$4" in
                Triggers) printf '%s\n' "$FIXTURE_TRIGGER" ;;
                Listen) printf '%s\n' "$FIXTURE_BINDING" ;;
                *) return 1 ;;
            esac ;;
        *) return 1 ;;
    esac
}
verify_baseline_socket "$FIXTURE_ENDPOINT"
`, "FIXTURE_ENDPOINT="+tt.endpoint, "FIXTURE_BINDING="+tt.binding, "FIXTURE_TRIGGER="+tt.trigger)
			if (err == nil) != tt.ok {
				t.Fatalf("success=%v, want %v: %v\n%s", err == nil, tt.ok, err, output)
			}
		})
	}
}

func TestFreshUFWOwnedListenerMatching(t *testing.T) {
	v4 := `LISTEN 0 128 127.0.0.1:2222 0.0.0.0:* users:(("sshd",pid=999999,fd=3))`
	v6 := `LISTEN 0 128 [::1]:2222 [::]:* users:(("sshd",pid=999999,fd=4))`
	for _, tt := range []struct {
		name, listeners string
		ok              bool
	}{
		{"owned dualstack", v4 + "\n" + v6, true},
		{"missing v6", v4, false},
		{"duplicate v4", v4 + "\n" + v4, false},
		{"wrong PID", v4 + "\n" + strings.ReplaceAll(v6, "999999", "123"), false},
		{"public bind", strings.ReplaceAll(v4, "127.0.0.1", "0.0.0.0") + "\n" + v6, false},
		{"mixed owners", strings.Replace(v4, "fd=3))", `fd=3),("node",pid=555,fd=9))`, 1) + "\n" + v6, false},
		{"extra listener", v4 + "\n" + v6 + "\n" + strings.ReplaceAll(v4, "127.0.0.1", "127.0.0.2"), false},
		{"unknown owner", strings.ReplaceAll(v4, `"sshd"`, `"sshd-session"`) + "\n" + v6, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `SSHD_PID=999999; owned_ssh_listeners_match <<<"$FIXTURE_LISTENERS"`, "FIXTURE_LISTENERS="+tt.listeners)
			if (err == nil) != tt.ok {
				t.Fatalf("success=%v, want %v: %v\n%s", err == nil, tt.ok, err, output)
			}
		})
	}
}

func TestFreshUFWBaselinePreservation(t *testing.T) {
	for _, changed := range []string{"none", "config", "ports", "listeners", "units", "fingerprint"} {
		t.Run(changed, func(t *testing.T) {
			output, err := runFreshUFWHelper(t, `
INITIAL_SSH_EFFECTIVE='port 22'
INITIAL_SSH_PORTS=22
INITIAL_SSH_LISTENERS='baseline-listeners'
INITIAL_SSH_UNITS='baseline-units'
INITIAL_SSH_FINGERPRINT='baseline-fingerprint'
SSHD_BIN=fixture_sshd
fixture_value() {
    if [[ "$FIXTURE_CHANGED" == "$1" ]]; then printf 'changed\n'; else printf '%s\n' "$2"; fi
}
fixture_sshd() { fixture_value config "$INITIAL_SSH_EFFECTIVE"; }
ss() { printf 'fixture-listeners\n'; }
installer_ssh_ports() { fixture_value ports "$INITIAL_SSH_PORTS"; }
inspect_baseline_ssh() { fixture_value listeners "$INITIAL_SSH_LISTENERS"; }
ssh_unit_state() { fixture_value units "$INITIAL_SSH_UNITS"; }
ssh_config_fingerprint() { fixture_value fingerprint "$INITIAL_SSH_FINGERPRINT"; }
verify_ssh_baseline_unchanged
`, "FIXTURE_CHANGED="+changed)
			if (err == nil) != (changed == "none") {
				t.Fatalf("changed=%s: %v\n%s", changed, err, output)
			}
			if changed != "none" && !strings.Contains(output, "FAIL: SSH inspection:") {
				t.Fatalf("missing safe stage diagnostic: %s", output)
			}
		})
	}
}

func TestFreshUFWCleanupOwnsOnlyAddedRulesAndPID(t *testing.T) {
	for _, identityMatches := range []bool{true, false} {
		name := "owned PID"
		identity := "1234"
		if !identityMatches {
			name, identity = "reused PID", "different"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			work := filepath.Join(dir, "work")
			if err := os.Mkdir(work, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(work, "ufw-rules-before-test"), []byte("ufw allow 22/tcp\n"), 0600); err != nil {
				t.Fatal(err)
			}
			output, err := runFreshUFWHelper(t, `
SSHD_BIN=/fixture/sshd
SSHD_PID=999999
SSHD_IDENTITY=1234
process_identity() { printf '%s\n' "$FIXTURE_IDENTITY"; }
kill() { printf 'kill %s\n' "$*" >>"$FIXTURE_LOG"; return 0; }
wait() { return 0; }
ufw() {
    printf 'ufw %s\n' "$*" >>"$FIXTURE_LOG"
    if [[ "$1" == status ]]; then
        if [[ -f "$FIXTURE_STATE/inactive" ]]; then printf 'Status: inactive\n'; else printf 'Status: active\n'; fi
    fi
    if [[ "${1:-} ${2:-}" == '--force disable' ]]; then touch "$FIXTURE_STATE/inactive"; fi
    if [[ "${1:-} ${2:-} ${3:-}" == '--force delete allow' ]]; then
        touch "$FIXTURE_STATE/${4%/tcp}"
    fi
}
ufw_user_rules() {
    printf 'ufw allow 22/tcp\n'
    for port in 2222 39095 39096; do
        [[ -f "$FIXTURE_STATE/$port" ]] || printf 'ufw allow %s/tcp\n' "$port"
    done
}
UFW_ACTIVATION_OWNED=1
ALLOW_RULES_OWNED=1
OWNED_ALLOW_PORTS=(2222 39095 39096)
DENY_RULE_OWNED=0
UFW_BASELINE_READY=1
cleanup
`, "WORK_DIR="+work, "FIXTURE_STATE="+dir, "FIXTURE_LOG="+filepath.Join(dir, "commands"), "FIXTURE_IDENTITY="+identity)
			if (err == nil) != identityMatches {
				t.Fatalf("cleanup success=%v, want %v: %v\n%s", err == nil, identityMatches, err, output)
			}
			log, readErr := os.ReadFile(filepath.Join(dir, "commands"))
			if readErr != nil {
				t.Fatal(readErr)
			}
			commands := string(log)
			if strings.Contains(commands, "delete allow 22/tcp") || strings.Contains(commands, "systemctl") {
				t.Fatalf("cleanup touched host SSH: %s", commands)
			}
			if strings.Contains(commands, "kill 999999\n") != identityMatches {
				t.Fatalf("PID signal ownership not respected: %s", commands)
			}
			for _, port := range []string{"2222", "39095", "39096"} {
				if !strings.Contains(commands, "ufw --force delete allow "+port+"/tcp") {
					t.Errorf("owned rule not removed: %s", port)
				}
			}
		})
	}
}

func TestFreshUFWCleanupRetainsSafetyRulesIfDisableFails(t *testing.T) {
	dir := t.TempDir()
	output, err := runFreshUFWHelper(t, `
SSHD_PID=''
UFW_ACTIVATION_OWNED=1
ALLOW_RULES_OWNED=1
OWNED_ALLOW_PORTS=(22 2222)
DENY_RULE_OWNED=1
ufw() {
    printf '%s\n' "$*" >>"$FIXTURE_LOG"
    if [[ "$1" == status ]]; then printf 'Status: active\n'; return 0; fi
    if [[ "${1:-} ${2:-}" == '--force disable' ]]; then return 1; fi
    return 0
}
cleanup
`, "FIXTURE_LOG="+filepath.Join(dir, "commands"))
	if err == nil || !strings.Contains(output, "retaining all safety rules") {
		t.Fatalf("failed disable was not surfaced: %v\n%s", err, output)
	}
	log, readErr := os.ReadFile(filepath.Join(dir, "commands"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(log), "delete") {
		t.Fatalf("safety rules removed with UFW still active: %s", log)
	}
}

func TestFreshUFWHarnessPreservesNativeSafetyBoundaries(t *testing.T) {
	contents := freshUFWScript(t)
	for _, required := range []string{
		`NOVAS_FRESH_UFW_OPT_IN`, `github-hosted`, `workflow_dispatch`,
		`INITIAL_SSH_PORTS=$(installer_ssh_ports)`,
		`INITIAL_SSH_LISTENERS=$(inspect_baseline_ssh`,
		`verify_ssh_baseline_unchanged || die "package installation changed host SSH`,
		`apt-get install -y --no-upgrade --no-install-recommends ufw openssh-client iproute2 cron`,
		`ssh_config_ports <<<"$INITIAL_SSH_EFFECTIVE"; printf '%s\n' "$SSH_PORT"`,
		`cmp -s "$WORK_DIR/ssh-global-drop-in" "$SSH_DROP_IN"`,
		`OWNED_ALLOW_PORTS+=("$port")`,
		`for port in "${EXPECTED_ALLOW_PORTS[@]}"; do`,
		`install_fresh_ufw "$WORK_DIR/panel-home" "" "$PANEL_PORT" "$SUB_PORT"`,
		`timeout 10 ssh-keyscan -T 5 -p "$SSH_PORT"`,
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("missing safety/acceptance requirement: %s", required)
		}
	}
	for _, forbidden := range []string{
		"systemctl stop ssh", "systemctl disable ssh", "systemctl restart ssh", "systemctl reload ssh",
		"ufw --force reset", "ufw reset", "apt-get install -y --no-install-recommends ufw openssh-server",
	} {
		if strings.Contains(contents, forbidden) {
			t.Errorf("host mutation or bypass introduced: %s", forbidden)
		}
	}
	preflight := strings.Index(contents, "INITIAL_SSH_LISTENERS=$(inspect_baseline_ssh")
	mutation := strings.Index(contents, "WORK_DIR=$(mktemp")
	if preflight < 0 || mutation <= preflight {
		t.Fatal("read-only SSH ownership preflight must precede every host mutation")
	}
}
