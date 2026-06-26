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
	ports, err := getHy2ServerPorts(json.RawMessage(`{"server_ports":[443," 8443 ",443,"500-502",70000,12345]}`))
	if err != nil {
		t.Fatalf("getHy2ServerPorts returned error: %v", err)
	}

	got := fmt.Sprint(ports)
	want := "[443 8443 500 501 502 12345]"
	if got != want {
		t.Fatalf("unexpected ports: got %s want %s", got, want)
	}

	if joinPorts(ports) != "443,8443,500,501,502,12345" {
		t.Fatalf("unexpected joinPorts result: %s", joinPorts(ports))
	}
}

func TestGetHy2ServerPortsRejectsDirtyValues(t *testing.T) {
	_, err := getHy2ServerPorts(json.RawMessage(`{"server_ports":["500","bad","1000-1400"]}`))
	if err == nil {
		t.Fatal("expected getHy2ServerPorts to reject invalid token")
	}
}

func TestMergeHy2ForwardPorts(t *testing.T) {
	ports := mergeHy2ForwardPorts(500, []int{900, 500, 1000})
	if got := fmt.Sprint(ports); got != "[500 900 1000]" {
		t.Fatalf("unexpected merged ports: %s", got)
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
	if runtime.GOOS != "linux" {
		t.Skip("hy2 forwarding script is only exercised on linux")
	}

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

	scriptCopy, err := os.ReadFile(filepath.Join(mustRepoRoot(t), "scripts", "hy2-forward.sh"))
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	replaced := strings.NewReplacer(
		"/etc/ufw/before.rules", filepath.Join(etcDir, "before.rules"),
		"/etc/ufw/before6.rules", filepath.Join(etcDir, "before6.rules"),
	).Replace(string(scriptCopy))
	scriptPath := filepath.Join(workDir, "hy2-forward.sh")
	if err := os.WriteFile(scriptPath, []byte(replaced), 0o755); err != nil {
		t.Fatalf("write script copy: %v", err)
	}

	env := append(os.Environ(),
		"PATH="+binDir+":"+os.Getenv("PATH"),
		"HY2_MOCK_LOG="+filepath.Join(workDir, "ufw.log"),
	)

	cmd := exec.Command("bash", scriptPath, "apply", "hy2-demo-ufw", "12345", "443,8443")
	cmd.Env = env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("apply failed: %v\n%s", err, string(out))
	}

	beforeRules, err := os.ReadFile(filepath.Join(etcDir, "before.rules"))
	if err != nil {
		t.Fatalf("read before.rules: %v", err)
	}
	if !bytes.Contains(beforeRules, []byte("NOVAPANEL HY2 BEGIN")) || !bytes.Contains(beforeRules, []byte("-p tcp --dport 443")) || !bytes.Contains(beforeRules, []byte("-p udp --dport 8443")) || !bytes.Contains(beforeRules, []byte("REDIRECT --to-ports 12345")) {
		t.Fatalf("ufw apply did not write expected NAT block:\n%s", string(beforeRules))
	}
	ufwLog, err := os.ReadFile(filepath.Join(workDir, "ufw.log"))
	if err != nil {
		t.Fatalf("read ufw log: %v", err)
	}
	if !bytes.Contains(ufwLog, []byte("ufw allow 443 comment NovaPanel ")) || !bytes.Contains(ufwLog, []byte("ufw allow 8443 comment NovaPanel ")) {
		t.Fatalf("ufw apply did not add allow rules:\n%s", string(ufwLog))
	}

	cmd = exec.Command("bash", scriptPath, "remove", "hy2-demo-ufw", "12345", "443,8443")
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
	ufwLog, err = os.ReadFile(filepath.Join(workDir, "ufw.log"))
	if err != nil {
		t.Fatalf("read ufw log after remove: %v", err)
	}
	if !bytes.Contains(ufwLog, []byte("ufw --force delete allow 443")) || !bytes.Contains(ufwLog, []byte("ufw --force delete allow 8443")) {
		t.Fatalf("ufw remove did not delete allow rules:\n%s", string(ufwLog))
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
	args := []string{"bash", filepath.Join(repoRoot, "scripts", "hy2-forward.sh"), action, tag, fmt.Sprint(listenPort), joinPorts(ports)}
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

func writeMockBinary(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write mock %s: %v", name, err)
	}
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
printf '%s %s\n' "$(basename "$0")" "$*" >> "$log_file"

case "${1:-}" in
  status)
    printf 'Status: active\n'
    ;;
  reload)
    :
    ;;
esac
`
}
