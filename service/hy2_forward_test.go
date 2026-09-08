package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestGetHy2ServerPorts(t *testing.T) {
	ranges, err := getHy2ServerPortRanges(json.RawMessage(`{"server_ports":[443," 8443 ",443,"500-502",12345]}`))
	if err != nil {
		t.Fatalf("getHy2ServerPortRanges returned error: %v", err)
	}

	got := fmt.Sprint(ranges)
	want := "[{443 443} {500 502} {8443 8443} {12345 12345}]"
	if got != want {
		t.Fatalf("unexpected ranges: got %s want %s", got, want)
	}

	if joinManagedPortRanges(ranges) != "443,500-502,8443,12345" {
		t.Fatalf("unexpected compact ranges: %s", joinManagedPortRanges(ranges))
	}
}

func TestGetHy2ServerPortsRejectsDirtyValues(t *testing.T) {
	_, err := getHy2ServerPortRanges(json.RawMessage(`{"server_ports":["500","bad","1000-1400"]}`))
	if err == nil {
		t.Fatal("expected getHy2ServerPortRanges to reject invalid token")
	}
	if _, err := getHy2ServerPortRanges(json.RawMessage(`{"server_ports":[70000]}`)); err == nil {
		t.Fatal("expected out-of-range port to be rejected")
	}
}

func TestGetHy2ServerPortsRejectsExcessiveFragments(t *testing.T) {
	values := make([]string, 0, maxManagedPortRangeSegments+1)
	for port := 1000; len(values) <= maxManagedPortRangeSegments; port += 2 {
		values = append(values, fmt.Sprintf("%d", port))
	}
	payload, err := json.Marshal(map[string]interface{}{"server_ports": values})
	if err != nil {
		t.Fatalf("marshal fragmented ports: %v", err)
	}
	if _, err := getHy2ServerPortRanges(payload); err == nil {
		t.Fatal("expected excessive fragmented ranges to be rejected")
	}
}

func TestHy2ForwardScriptAllowsPurgeWithoutTag(t *testing.T) {
	workDir := t.TempDir()
	binDir := filepath.Join(workDir, "bin")
	etcDir := filepath.Join(workDir, "etc", "ufw")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatalf("mkdir ufw dir: %v", err)
	}

	script, err := os.ReadFile(filepath.Join(mustRepoRoot(t), "scripts", "hy2-forward.sh"))
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	script = []byte(strings.NewReplacer(
		"/etc/ufw/before.rules", filepath.Join(etcDir, "before.rules"),
		"/etc/ufw/before6.rules", filepath.Join(etcDir, "before6.rules"),
		"/etc/ufw/user.rules", filepath.Join(etcDir, "user.rules"),
		"/etc/ufw/user6.rules", filepath.Join(etcDir, "user6.rules"),
	).Replace(string(script)))
	scriptPath := filepath.Join(workDir, "hy2-forward.sh")
	if err := os.WriteFile(scriptPath, script, 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}

	cmd := exec.Command("bash", scriptPath, "purge")
	cmd.Env = append(os.Environ(), "PATH="+binDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("purge without tag failed: %v\n%s", err, output)
	}
}

func TestRunHy2ForwardScriptIptables(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("hy2 forwarding script is only exercised on linux")
	}

	repoRoot := mustRepoRoot(t)
	oldWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}

	workDir := t.TempDir()
	binDir := filepath.Join(workDir, "bin")
	stateDir := filepath.Join(workDir, "state")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	logFile := filepath.Join(workDir, "iptables.log")
	mockIptables := mockIptablesScript(t)
	if err := os.WriteFile(filepath.Join(binDir, "iptables"), []byte(mockIptables), 0o755); err != nil {
		t.Fatalf("write mock iptables: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ip6tables"), []byte(mockIptables), 0o755); err != nil {
		t.Fatalf("write mock ip6tables: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ufw"), []byte("#!/usr/bin/env bash\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write unavailable ufw mock: %v", err)
	}

	env := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"HY2_FORWARD_BACKEND=iptables",
		"HY2_MOCK_LOG="+logFile,
		"HY2_MOCK_STATE_DIR="+stateDir,
	)

	tag := "hy2-demo"
	ports := []int{443, 8443, 443}
	wantChain := hy2ChainName(tag)

	if err := runHy2ForwardScriptWithEnv(repoRoot, env, "apply", tag, 12345, ports); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(stateDir, "iptables.jump")); err != nil {
		t.Fatalf("iptables jump rule not created: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(stateDir, "iptables.chain_name")); err != nil {
		t.Fatalf("read chain name: %v", err)
	} else if strings.TrimSpace(string(data)) != wantChain {
		t.Fatalf("unexpected chain name: %s", strings.TrimSpace(string(data)))
	}

	logData, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !bytes.Contains(logData, []byte("-p tcp --dport 443")) || !bytes.Contains(logData, []byte("-p udp --dport 8443")) {
		t.Fatalf("iptables apply did not emit expected redirect rules:\n%s", string(logData))
	}

	if err := runHy2ForwardScriptWithEnv(repoRoot, env, "remove", tag, 0, nil); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "iptables.jump")); !os.IsNotExist(err) {
		t.Fatalf("iptables jump rule still exists after remove: %v", err)
	}

	if err := runHy2ForwardScriptWithEnv(repoRoot, env, "purge", "", 0, nil); err != nil {
		t.Fatalf("purge failed: %v", err)
	}
}

func TestRunHy2ForwardScriptNftables(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("hy2 forwarding script is only exercised on linux")
	}

	repoRoot := mustRepoRoot(t)
	oldWd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(oldWd) })
	if err := os.Chdir(repoRoot); err != nil {
		t.Fatalf("chdir repo root: %v", err)
	}

	workDir := t.TempDir()
	binDir := filepath.Join(workDir, "bin")
	stateDir := filepath.Join(workDir, "state")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}

	logFile := filepath.Join(workDir, "nft.log")
	mockNft := mockNftScript(t)
	if err := os.WriteFile(filepath.Join(binDir, "nft"), []byte(mockNft), 0o755); err != nil {
		t.Fatalf("write mock nft: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ufw"), []byte("#!/usr/bin/env bash\nexit 1\n"), 0o755); err != nil {
		t.Fatalf("write unavailable ufw mock: %v", err)
	}

	env := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"HY2_FORWARD_BACKEND=nftables",
		"HY2_MOCK_LOG="+logFile,
		"HY2_MOCK_STATE_DIR="+stateDir,
	)

	tag := "hy2-demo-nft"
	ports := []int{80, 443}

	if err := runHy2ForwardScriptWithEnv(repoRoot, env, "apply", tag, 12345, ports); err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	logData, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if !bytes.Contains(logData, []byte("add chain ip nat")) || !bytes.Contains(logData, []byte("tcp dport")) || !bytes.Contains(logData, []byte("udp dport")) || !bytes.Contains(logData, []byte("redirect to :12345")) {
		t.Fatalf("nft apply did not emit expected commands:\n%s", string(logData))
	}

	if err := runHy2ForwardScriptWithEnv(repoRoot, env, "remove", tag, 0, nil); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
}

func TestRunHy2ForwardScriptUFW(t *testing.T) {
	workDir := t.TempDir()
	binDir := filepath.Join(workDir, "bin")
	etcDir := filepath.Join(workDir, "etc", "ufw")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatalf("mkdir ufw dir: %v", err)
	}

	mockUfw := mockUfwScript(t)
	if err := os.WriteFile(filepath.Join(binDir, "ufw"), []byte(mockUfw), 0o755); err != nil {
		t.Fatalf("write mock ufw: %v", err)
	}
	for _, name := range []string{"chmod", "chown"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write mock %s: %v", name, err)
		}
	}

	scriptCopy, err := os.ReadFile(filepath.Join(mustRepoRoot(t), "scripts", "hy2-forward.sh"))
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	replaced := strings.NewReplacer(
		"/etc/ufw/before.rules", filepath.Join(etcDir, "before.rules"),
		"/etc/ufw/before6.rules", filepath.Join(etcDir, "before6.rules"),
		"/etc/ufw/user.rules", filepath.Join(etcDir, "user.rules"),
		"/etc/ufw/user6.rules", filepath.Join(etcDir, "user6.rules"),
	).Replace(string(scriptCopy))
	scriptPath := filepath.Join(workDir, "hy2-forward.sh")
	if err := os.WriteFile(scriptPath, []byte(replaced), 0o755); err != nil {
		t.Fatalf("write script copy: %v", err)
	}

	env := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"HY2_MOCK_LOG="+filepath.Join(workDir, "ufw.log"),
	)

	cmd := exec.Command("bash", scriptPath, "apply", "hy2-demo-ufw", "12345", "443,444,445,8443,20000-49999")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apply failed: %v\n%s", err, string(out))
	}

	beforeRules, err := os.ReadFile(filepath.Join(etcDir, "before.rules"))
	if err != nil {
		t.Fatalf("read before.rules: %v", err)
	}
	if !bytes.Contains(beforeRules, []byte("NOVAPANEL HY2 BEGIN")) || !bytes.Contains(beforeRules, []byte("-p tcp --dport 443:445")) || !bytes.Contains(beforeRules, []byte("-p udp --dport 8443")) || !bytes.Contains(beforeRules, []byte("-p udp --dport 20000:49999")) || !bytes.Contains(beforeRules, []byte("REDIRECT --to-ports 12345")) {
		t.Fatalf("ufw apply did not write expected NAT block:\n%s", string(beforeRules))
	}
	ufwLog, err := os.ReadFile(filepath.Join(workDir, "ufw.log"))
	if err != nil {
		t.Fatalf("read ufw log: %v", err)
	}
	if !bytes.Contains(ufwLog, []byte("ufw allow 443:445/tcp comment NovaPanel ")) ||
		!bytes.Contains(ufwLog, []byte("ufw allow 443:445/udp comment NovaPanel ")) ||
		!bytes.Contains(ufwLog, []byte("ufw allow 8443/tcp comment NovaPanel ")) ||
		!bytes.Contains(ufwLog, []byte("ufw allow 8443/udp comment NovaPanel ")) ||
		bytes.Count(ufwLog, []byte("ufw allow 20000:49999/udp comment NovaPanel ")) != 1 {
		t.Fatalf("ufw apply did not add allow rules:\n%s", string(ufwLog))
	}

	markerHex := fmt.Sprintf("%x", []byte("NovaPanel "+hy2ChainName("hy2-demo-ufw")))
	userRulesPath := filepath.Join(etcDir, "user.rules")
	userRules := fmt.Sprintf(`*filter
### RULES ###

### tuple ### allow udp 443 0.0.0.0/0 any 0.0.0.0/0 in comment=%s
-A ufw-user-input -p udp --dport 443 -j ACCEPT
### tuple ### allow tcp 9527 0.0.0.0/0 any 0.0.0.0/0 in
-A ufw-user-input -p tcp --dport 9527 -j ACCEPT

### END RULES ###
COMMIT
`, markerHex)
	if err := os.WriteFile(userRulesPath, []byte(userRules), 0o644); err != nil {
		t.Fatalf("seed UFW user rules: %v", err)
	}

	cmd = exec.Command("bash", scriptPath, "remove", "hy2-demo-ufw", "12345", "443,444,445,8443,20000-49999")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("remove failed: %v\n%s", err, string(out))
	}

	beforeRules, err = os.ReadFile(filepath.Join(etcDir, "before.rules"))
	if err != nil {
		t.Fatalf("read before.rules after remove: %v", err)
	}
	if bytes.Contains(beforeRules, []byte("NOVAPANEL HY2 BEGIN")) || bytes.Contains(beforeRules, []byte("REDIRECT --to-ports 12345")) {
		t.Fatalf("ufw remove did not clear NAT block:\n%s", string(beforeRules))
	}
	cleanedUserRules, err := os.ReadFile(userRulesPath)
	if err != nil {
		t.Fatalf("read UFW user rules after remove: %v", err)
	}
	if bytes.Contains(cleanedUserRules, []byte(markerHex)) || !bytes.Contains(cleanedUserRules, []byte("--dport 9527")) {
		t.Fatalf("ufw remove did not selectively clean managed allow rules:\n%s", string(cleanedUserRules))
	}
}

func TestRunHy2ForwardScriptPurgeRemovesUfwLiveRedirects(t *testing.T) {
	workDir := t.TempDir()
	binDir := filepath.Join(workDir, "bin")
	etcDir := filepath.Join(workDir, "etc", "ufw")
	stateDir := filepath.Join(workDir, "state")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatalf("mkdir bin: %v", err)
	}
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		t.Fatalf("mkdir ufw dir: %v", err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(binDir, "ufw"), []byte(mockUfwScript(t)), 0o755); err != nil {
		t.Fatalf("write mock ufw: %v", err)
	}
	for _, name := range []string{"chmod", "chown"} {
		if err := os.WriteFile(filepath.Join(binDir, name), []byte("#!/usr/bin/env bash\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("write mock %s: %v", name, err)
		}
	}
	mockIptables := mockDirectRedirectIptablesScript(t)
	if err := os.WriteFile(filepath.Join(binDir, "iptables"), []byte(mockIptables), 0o755); err != nil {
		t.Fatalf("write mock iptables: %v", err)
	}
	if err := os.WriteFile(filepath.Join(binDir, "ip6tables"), []byte(mockIptables), 0o755); err != nil {
		t.Fatalf("write mock ip6tables: %v", err)
	}

	beforeRules := `*nat
:PREROUTING ACCEPT [0:0]
# NOVAPANEL HY2 BEGIN NPHY2_demo ip
-A PREROUTING -p tcp --dport 443 -j REDIRECT --to-ports 443
-A PREROUTING -p udp --dport 443 -j REDIRECT --to-ports 443
# NOVAPANEL HY2 END NPHY2_demo ip
COMMIT
`
	before6Rules := strings.ReplaceAll(beforeRules, " ip", " ip6")
	if err := os.WriteFile(filepath.Join(etcDir, "before.rules"), []byte(beforeRules), 0o644); err != nil {
		t.Fatalf("write before.rules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "before6.rules"), []byte(before6Rules), 0o644); err != nil {
		t.Fatalf("write before6.rules: %v", err)
	}
	managedMarkerHex := fmt.Sprintf("%x", []byte("NovaPanel NPHY2_orphan"))
	userRules := fmt.Sprintf(`*filter
### RULES ###

### tuple ### allow udp 443 0.0.0.0/0 any 0.0.0.0/0 in comment=%s
-A ufw-user-input -p udp --dport 443 -j ACCEPT
### tuple ### allow tcp 2222 0.0.0.0/0 any 0.0.0.0/0 in
-A ufw-user-input -p tcp --dport 2222 -j ACCEPT

### END RULES ###
COMMIT
`, managedMarkerHex)
	if err := os.WriteFile(filepath.Join(etcDir, "user.rules"), []byte(userRules), 0o644); err != nil {
		t.Fatalf("write user.rules: %v", err)
	}

	for _, bin := range []string{"iptables", "ip6tables"} {
		for _, protocol := range []string{"tcp", "udp"} {
			key := filepath.Join(stateDir, fmt.Sprintf("%s.%s.443.443.count", bin, protocol))
			if err := os.WriteFile(key, []byte("3"), 0o644); err != nil {
				t.Fatalf("seed live redirect count: %v", err)
			}
		}
	}

	scriptCopy, err := os.ReadFile(filepath.Join(mustRepoRoot(t), "scripts", "hy2-forward.sh"))
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	replaced := strings.NewReplacer(
		"/etc/ufw/before.rules", filepath.Join(etcDir, "before.rules"),
		"/etc/ufw/before6.rules", filepath.Join(etcDir, "before6.rules"),
		"/etc/ufw/user.rules", filepath.Join(etcDir, "user.rules"),
		"/etc/ufw/user6.rules", filepath.Join(etcDir, "user6.rules"),
	).Replace(string(scriptCopy))
	scriptPath := filepath.Join(workDir, "hy2-forward.sh")
	if err := os.WriteFile(scriptPath, []byte(replaced), 0o755); err != nil {
		t.Fatalf("write script copy: %v", err)
	}

	env := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"HY2_MOCK_STATE_DIR="+stateDir,
		"HY2_MOCK_LOG="+filepath.Join(workDir, "purge.log"),
	)
	cmd := exec.Command("bash", scriptPath, "purge")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("purge failed: %v\n%s", err, string(out))
	}

	for _, bin := range []string{"iptables", "ip6tables"} {
		for _, protocol := range []string{"tcp", "udp"} {
			key := filepath.Join(stateDir, fmt.Sprintf("%s.%s.443.443.count", bin, protocol))
			data, err := os.ReadFile(key)
			if err != nil {
				t.Fatalf("read live redirect count: %v", err)
			}
			if strings.TrimSpace(string(data)) != "0" {
				t.Fatalf("%s %s live redirect count was not cleared: %s", bin, protocol, string(data))
			}
		}
	}

	afterRules, err := os.ReadFile(filepath.Join(etcDir, "before.rules"))
	if err != nil {
		t.Fatalf("read before.rules after purge: %v", err)
	}
	after6Rules, err := os.ReadFile(filepath.Join(etcDir, "before6.rules"))
	if err != nil {
		t.Fatalf("read before6.rules after purge: %v", err)
	}
	if bytes.Contains(afterRules, []byte("NOVAPANEL HY2 BEGIN")) || bytes.Contains(after6Rules, []byte("NOVAPANEL HY2 BEGIN")) {
		t.Fatalf("purge did not strip UFW marker blocks:\n%s\n%s", string(afterRules), string(after6Rules))
	}
	cleanedUserRules, err := os.ReadFile(filepath.Join(etcDir, "user.rules"))
	if err != nil {
		t.Fatalf("read user.rules after purge: %v", err)
	}
	if bytes.Contains(cleanedUserRules, []byte(managedMarkerHex)) || !bytes.Contains(cleanedUserRules, []byte("--dport 2222")) {
		t.Fatalf("purge did not selectively remove NovaPanel UFW allow rules:\n%s", string(cleanedUserRules))
	}
}

func mustRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get wd: %v", err)
	}
	return filepath.Clean(filepath.Join(wd, ".."))
}

func runHy2ForwardScriptWithEnv(repoRoot string, env []string, action string, tag string, listenPort int, ports []int) error {
	workDir, err := os.MkdirTemp("", "hy2-forward-test-*")
	if err != nil {
		return fmt.Errorf("mktemp script dir: %w", err)
	}
	defer os.RemoveAll(workDir)

	etcDir := filepath.Join(workDir, "etc", "ufw")
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		return fmt.Errorf("mkdir ufw dir: %w", err)
	}

	scriptCopy, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "hy2-forward.sh"))
	if err != nil {
		return fmt.Errorf("read script: %w", err)
	}
	replaced := strings.NewReplacer(
		"/etc/ufw/before.rules", filepath.Join(etcDir, "before.rules"),
		"/etc/ufw/before6.rules", filepath.Join(etcDir, "before6.rules"),
		"/etc/ufw/user.rules", filepath.Join(etcDir, "user.rules"),
		"/etc/ufw/user6.rules", filepath.Join(etcDir, "user6.rules"),
	).Replace(string(scriptCopy))
	scriptPath := filepath.Join(workDir, "hy2-forward.sh")
	if err := os.WriteFile(scriptPath, []byte(replaced), 0o755); err != nil {
		return fmt.Errorf("write script copy: %w", err)
	}

	args := []string{scriptPath, action, tag, fmt.Sprint(listenPort), joinPorts(ports)}
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Env = env
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func hy2ChainName(tag string) string {
	sum := sha256.Sum256([]byte(tag))
	return "NPHY2_" + fmt.Sprintf("%x", sum)[:12]
}

func mockIptablesScript(t *testing.T) string {
	t.Helper()
	return `#!/usr/bin/env bash
set -euo pipefail

state_dir="${HY2_MOCK_STATE_DIR:?}"
log_file="${HY2_MOCK_LOG:?}"
cmd="$(basename "$0")"
printf '%s %s\n' "$cmd" "$*" >> "$log_file"

chain_name_file="${state_dir}/${cmd}.chain_name"
jump_file="${state_dir}/${cmd}.jump"
rules_file="${state_dir}/${cmd}.rules"

if [[ "${1:-}" == "-t" ]]; then
  shift 2
fi

case "${1:-}" in
  -N)
    printf '%s' "${2:-}" > "$chain_name_file"
    : > "$rules_file"
    ;;
  -F)
    : > "$rules_file"
    ;;
  -X)
    rm -f "$chain_name_file" "$jump_file" "$rules_file"
    ;;
  -C)
    [[ -f "$jump_file" ]]
    ;;
  -A)
    if [[ "${2:-}" == "PREROUTING" ]]; then
      touch "$jump_file"
    else
      printf '%s\n' "$*" >> "$rules_file"
    fi
    ;;
  -D)
    rm -f "$jump_file"
    ;;
  -S)
    if [[ -f "$chain_name_file" ]]; then
      chain="$(cat "$chain_name_file")"
      printf ':%s - [0:0]\n' "$chain"
      if [[ -f "$jump_file" ]]; then
        printf -- '-A PREROUTING -p udp -j %s\n' "$chain"
      fi
      if [[ -f "$rules_file" ]]; then
        cat "$rules_file"
      fi
    fi
    ;;
esac
`
}

func mockNftScript(t *testing.T) string {
	t.Helper()
	return `#!/usr/bin/env bash
set -euo pipefail

state_dir="${HY2_MOCK_STATE_DIR:?}"
log_file="${HY2_MOCK_LOG:?}"
cmd="$(basename "$0")"
printf '%s %s\n' "$cmd" "$*" >> "$log_file"

family="${2:-}"
chain_name_file="${state_dir}/${family}.chain_name"
rules_file="${state_dir}/${family}.rules"

case "${1:-}" in
  add)
    case "${2:-}" in
      table)
        : > /dev/null
        ;;
      chain)
        printf '%s' "${4:-}" > "$chain_name_file"
        : > "$rules_file"
        ;;
      rule)
        printf '%s\n' "$*" >> "$rules_file"
        ;;
    esac
    ;;
  delete)
    rm -f "$chain_name_file" "$rules_file"
    ;;
  -a)
    if [[ "${2:-}" == "list" && "${3:-}" == "table" ]]; then
      if [[ -f "$chain_name_file" ]]; then
        printf 'chain %s {\n' "$(cat "$chain_name_file")"
      fi
    fi
    ;;
esac
`
}

func mockUfwScript(t *testing.T) string {
	t.Helper()
	return `#!/usr/bin/env bash
set -euo pipefail

log_file="${HY2_MOCK_LOG:?}"
state_file="${log_file}.state"
printf '%s %s\n' "$(basename "$0")" "$*" >> "$log_file"

case "${1:-}" in
  status)
    printf 'Status: active\n'
    if [[ "${2:-}" == "numbered" && -f "$state_file" ]]; then
      number=0
      while IFS='|' read -r target marker; do
        [[ -n "$target" ]] || continue
        number=$((number + 1))
        printf '[ %d] %s ALLOW IN Anywhere # %s\n' "$number" "$target" "$marker"
      done < "$state_file"
    fi
    ;;
  allow)
    printf '%s|%s\n' "${2:-}" "${4:-}" >> "$state_file"
    ;;
  --force)
    if [[ "${2:-}" == "delete" && "${3:-}" =~ ^[0-9]+$ && -f "$state_file" ]]; then
      awk -v number="${3}" 'NR != number' "$state_file" > "${state_file}.tmp"
      mv "${state_file}.tmp" "$state_file"
    fi
    ;;
  reload)
    :
    ;;
esac
`
}

func mockDirectRedirectIptablesScript(t *testing.T) string {
	t.Helper()
	return `#!/usr/bin/env bash
set -euo pipefail

state_dir="${HY2_MOCK_STATE_DIR:?}"
log_file="${HY2_MOCK_LOG:?}"
cmd="$(basename "$0")"
printf '%s %s\n' "$cmd" "$*" >> "$log_file"

if [[ "${1:-}" == "-t" ]]; then
  shift 2
fi

extract_arg() {
  local name="$1"
  shift
  while [[ "$#" -gt 0 ]]; do
    if [[ "$1" == "$name" && "$#" -gt 1 ]]; then
      printf '%s' "$2"
      return 0
    fi
    shift
  done
}

protocol="$(extract_arg "-p" "$@")"
port="$(extract_arg "--dport" "$@")"
target="$(extract_arg "--to-ports" "$@")"
count_file="${state_dir}/${cmd}.${protocol}.${port}.${target}.count"

case "${1:-}" in
  -C)
    count=0
    if [[ -f "$count_file" ]]; then
      count="$(cat "$count_file")"
    fi
    [[ "$count" -gt 0 ]]
    ;;
  -D)
    count=0
    if [[ -f "$count_file" ]]; then
      count="$(cat "$count_file")"
    fi
    if [[ "$count" -gt 0 ]]; then
      printf '%s' "$((count - 1))" > "$count_file"
    fi
    ;;
  -S)
    :
    ;;
esac
`
}
