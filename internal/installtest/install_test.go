package installtest

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
)

// macOS has flock(2) but no flock CLI. Use the inherited descriptor exactly as
// util-linux flock does; Linux CI additionally exercises its native executable.
func TestFlockHelper(t *testing.T) {
	if os.Getenv("NOVAS_FLOCK_HELPER") != "1" {
		return
	}
	if err := syscall.Flock(9, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func write(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func TestOfflineDeployment(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	helper := filepath.Join(filepath.Dir(source), "../../scripts/install-runtime.sh")
	if override := os.Getenv("NOVAS_INSTALL_HELPER"); override != "" {
		helper = override
	}
	installer := filepath.Join(filepath.Dir(source), "../../install.sh")
	for _, scenario := range []string{"fresh", "fresh-callback", "current", "concurrent-current", "validation-failure", "health-failure", "custom-db", "bad-package", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			fresh := scenario == "fresh" || scenario == "fresh-callback"
			if scenario == "concurrent-current" {
				// Observe fresh state, then simulate another installer completing
				// before this deployment acquires its real deployment lock.
				probe := exec.Command("bash", "-c", `source "$1"; installer_is_fresh "$2"`, "fixture", installer, root)
				probe.Env = []string{"PATH=" + filepath.Join(root, "empty-bin"), "NOVAS_DB_FOLDER="}
				if output, err := probe.CombinedOutput(); err != nil {
					t.Fatalf("initial freshness probe: %v\n%s", err, output)
				}
			}
			home := filepath.Join(root, "usr/local/novas")
			old := home
			oldName := "novas"
			units := filepath.Join(root, "etc/systemd/system")
			if !fresh {
				write(t, filepath.Join(old, "db/novas.db"), "user-data", 0600)
				write(t, filepath.Join(old, "cert/key.pem"), "preserve-key", 0600)
				write(t, filepath.Join(old, "novas"), "old-binary", 0755)
				write(t, filepath.Join(units, oldName+".service"), "[Service]\nExecStart=/usr/local/novas/novas\nWorkingDirectory=/usr/local/novas\nLimitNOFILE=65536\n", 0644)
				write(t, filepath.Join(units, oldName+".service.d/limits.conf"), "[Service]\nLimitNPROC=4096\n", 0644)
				write(t, filepath.Join(root, "usr/bin/"+oldName), "old-command", 0755)
				write(t, filepath.Join(root, "active-"+oldName), "", 0600)
				write(t, filepath.Join(root, "enabled-"+oldName), "", 0600)
				write(t, filepath.Join(root, "var/lib/novas/update.status"), "state=running", 0600)
			}
			if scenario == "custom-db" {
				write(t, filepath.Join(units, oldName+".service"), "[Service]\nEnvironment=NOVAS_DB_FOLDER=/external\n", 0644)
			}
			pkg := filepath.Join(root, "package")
			write(t, filepath.Join(pkg, "novas"), `#!/bin/bash
set -eu
case "$1" in
  -v) echo 'NovaPanel Panel 1.6.97';;
  checkdb) [[ "$SCENARIO" != validation-failure ]];;
  healthcheck) [[ "$SCENARIO" != health-failure ]];;
esac
`, 0755)
			for _, file := range []string{"novas.sh", "novas.service", "scripts/hy2-forward.sh", "scripts/login-guard.sh", "bin/mita"} {
				write(t, filepath.Join(pkg, file), "#!/bin/sh\nexit 0\n", 0755)
			}
			if scenario == "bad-package" {
				os.Remove(filepath.Join(pkg, "bin/mita"))
			}
			bin := filepath.Join(root, "test-bin")
			write(t, filepath.Join(bin, "sleep"), "#!/bin/sh\nexit 0\n", 0755)
			write(t, filepath.Join(bin, "systemctl"), `#!/bin/bash
set -eu
action="$1"; shift
name="${!#}"; name="${name%.service}"
case "$action" in
  cat) cat "$TEST_ROOT/etc/systemd/system/$name.service";;
  show) if [[ "$*" == *MainPID* ]]; then echo 42; fi;;
  is-active) test -f "$TEST_ROOT/active-$name";;
  is-enabled) test -f "$TEST_ROOT/enabled-$name";;
  stop) rm -f "$TEST_ROOT/active-$name";;
  start) touch "$TEST_ROOT/active-$name";;
  enable) touch "$TEST_ROOT/enabled-$name";;
  disable) rm -f "$TEST_ROOT/enabled-$name";;
  daemon-reload) :;;
  *) echo "unexpected systemctl $action" >&2; exit 1;;
esac
`, 0755)
			if _, err := exec.LookPath("flock"); err != nil {
				exe, _ := os.Executable()
				write(t, filepath.Join(bin, "flock"), "#!/bin/sh\nNOVAS_FLOCK_HELPER=1 exec '"+exe+"' -test.run=TestFlockHelper\n", 0755)
			}
			if scenario == "locked" {
				lockPath := filepath.Join(root, "var/lib/novas-install.lock")
				write(t, lockPath, "", 0600)
				lock, err := os.OpenFile(lockPath, os.O_RDWR, 0600)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
					t.Fatal(err)
				}
			}
			script := `source "$1"; novas_configure() { :; }; novas_deploy "$2" "$3"`
			if scenario == "concurrent-current" || scenario == "fresh-callback" {
				script = `source "$4"; FRESH_INSTALL=1
config_after_install() { echo settings >> "$TEST_ROOT/callback-actions"; }
install_fresh_ufw() {
  [[ ! -e "$TEST_ROOT/active-novas" ]] || return 90
  echo firewall >> "$TEST_ROOT/callback-actions"
}
source "$1"; novas_deploy "$2" "$3"`
			}
			command := exec.Command("bash", "-c", script, "fixture", helper, pkg, root, installer)
			command.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "SCENARIO="+scenario, "TEST_ROOT="+root, "NOVAS_DB_FOLDER=")
			output, err := command.CombinedOutput()
			success := fresh || scenario == "current" || scenario == "concurrent-current"
			if success && err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
			if !success && err == nil {
				t.Fatalf("failure was accepted\n%s", output)
			}
			if success {
				if scenario == "fresh-callback" || scenario == "concurrent-current" {
					actions, _ := os.ReadFile(filepath.Join(root, "callback-actions"))
					want := ""
					if fresh {
						want = "settings\nfirewall\n"
					}
					if string(actions) != want {
						t.Fatalf("unsafe deploy callback: %q want %q", actions, want)
					}
				}
				if _, err := os.Stat(filepath.Join(root, "active-novas")); err != nil {
					t.Fatal("new service not started", err)
				}
				if !fresh {
					data, _ := os.ReadFile(filepath.Join(home, "db/novas.db"))
					if string(data) != "user-data" {
						t.Fatal("lost database")
					}
					key, _ := os.ReadFile(filepath.Join(home, "cert/key.pem"))
					if string(key) != "preserve-key" {
						t.Fatal("lost certificate")
					}
					unit, _ := os.ReadFile(filepath.Join(units, "novas.service"))
					if !bytes.Contains(unit, []byte("/usr/local/novas/novas")) || !bytes.Contains(unit, []byte("LimitNOFILE=65536")) {
						t.Fatal("unit options not preserved", string(unit))
					}
				}

			} else {
				data, _ := os.ReadFile(filepath.Join(old, "db/novas.db"))
				if string(data) != "user-data" {
					t.Fatalf("rollback lost data\n%s", output)
				}
				if _, err := os.Stat(filepath.Join(root, "active-"+oldName)); err != nil {
					t.Fatalf("old service not recovered: %v\n%s", err, output)
				}
			}
			if strings.Contains(string(output), "preserve-key") {
				t.Fatal("secret leaked to log")
			}
		})
	}
}

// Only these harmless utilities are visible; firewall, SSH and package commands
// below are isolated fixtures, never the host's executables.
func installerFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	bin := filepath.Join(root, "test-bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bash", "awk", "sed", "sort", "grep", "cat", "cp", "rm"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(path, filepath.Join(bin, name)); err != nil {
			t.Fatal(err)
		}
	}
	write(t, filepath.Join(bin, "sshd"), `#!/bin/bash
[[ "$*" == -T ]] || exit 90
[[ "$SCENARIO" != sshd-failure ]] || exit 1
cat "$TEST_ROOT/sshd-config"
`, 0755)
	write(t, filepath.Join(bin, "ss"), `#!/bin/bash
[[ "$*" == '-H -ltnp' ]] || exit 90
[[ "$SCENARIO" != ss-failure ]] || exit 1
cat "$TEST_ROOT/listeners"
`, 0755)
	ssData, err := os.ReadFile(filepath.Join(bin, "ss"))
	if err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(bin, "ss-fixture"), string(ssData), 0755)
	write(t, filepath.Join(bin, "systemctl"), `#!/bin/bash
case "$*" in
  'is-active --quiet ssh.socket') [[ "$SCENARIO" == socket* ]];;
  'is-active --quiet sshd.socket') exit 1;;
  'show ssh.socket -p Triggers --value')
    if [[ "$SCENARIO" == socket-unknown-service ]]; then echo unrelated.service; else echo ssh.service; fi;;
  'show ssh.socket -p Listen --value') echo '0.0.0.0:2222 (Stream) [::]:2200 (Stream)';;
  *) exit 90;;
esac
`, 0755)
	write(t, filepath.Join(bin, "iptables"), "#!/bin/bash\necho iptables >> \"$TEST_ROOT/actions\"\nexit 91\n", 0755)
	write(t, filepath.Join(bin, "ufw-fixture"), `#!/bin/bash
set -eu
echo "ufw $*" >> "$TEST_ROOT/actions"
case "$1" in
  prepend)
    [[ "$2" == allow && "$3" == */tcp ]] || exit 90
    grep -qx IPV6=yes "$TEST_ROOT/etc/default/ufw" || exit 92
    [[ "$SCENARIO" != allow-failure ]] || exit 1
    echo "$3" >> "$TEST_ROOT/allowed";;
  --force)
    case "$2" in
      enable)
        echo active > "$TEST_ROOT/ufw-state"
        [[ "$SCENARIO" != *enable-failure ]];;
      disable)
        [[ "$SCENARIO" != recovery-failure ]] || exit 1
        echo inactive > "$TEST_ROOT/ufw-state";;
      *) exit 90;;
    esac;;
  reload) [[ "$SCENARIO" != *reload-failure && "$SCENARIO" != recovery-failure ]];;
  show)
    [[ "$2" == added && "$SCENARIO" != show-added-failure ]] || exit 1
    [[ "$SCENARIO" != shadowed-rule ]] || echo 'ufw deny 2222/tcp'
    while read -r port; do echo "ufw allow $port"; done < "$TEST_ROOT/allowed";;
  status)
    [[ "$SCENARIO" != snapshot-failure ]] || exit 1
    [[ "$SCENARIO" != status-failure || "$(cat "$TEST_ROOT/ufw-state")" != active ]] || exit 1
    [[ "$SCENARIO" != inactive ]] || { echo 'Status: inactive'; exit; }
    echo "Status: $(cat "$TEST_ROOT/ufw-state")"
    while read -r port; do
      echo "$port ALLOW IN Anywhere"
      [[ "$SCENARIO" == missing-v6 ]] || echo "$port (v6) ALLOW IN Anywhere (v6)"
    done < "$TEST_ROOT/allowed";;
  *) exit 90;;
esac
`, 0755)
	for _, name := range []string{"apt-get", "pacman", "zypper", "yum", "dnf"} {
		write(t, filepath.Join(bin, name), `#!/bin/bash
set -eu
echo "package ${0##*/} $*" >> "$TEST_ROOT/actions"
case "$*" in
  *ufw*)
    [[ "$SCENARIO" != *package-failure ]] || exit 1
    [[ "$SCENARIO" != *package-missing ]] || exit 0
    cp "$TEST_ROOT/test-bin/ufw-fixture" "$TEST_ROOT/test-bin/ufw";;
  *iproute*)
    [[ "$SCENARIO" != *ss-package-failure ]] || exit 1
    [[ "$SCENARIO" != *ss-package-missing ]] || exit 0
    cp "$TEST_ROOT/test-bin/ss-fixture" "$TEST_ROOT/test-bin/ss";;
  *) exit 90;;
esac
`, 0755)
	}
	write(t, filepath.Join(root, "sshd-config"), "port 2222\nport 2200\nlistenaddress [::]:2222\nlistenaddress 0.0.0.0:2200\n", 0600)
	write(t, filepath.Join(root, "listeners"), "LISTEN 0 128 0.0.0.0:2222 0.0.0.0:* users:((\"sshd\",pid=42,fd=3))\nLISTEN 0 128 [::]:2200 [::]:* users:((\"sshd\",pid=42,fd=4))\nLISTEN 0 128 *:9000 *:* users:((\"other\",pid=43,fd=5))\n", 0600)
	write(t, filepath.Join(root, "etc/default/ufw"), "# preserve config\nIPV6=no\nDEFAULT_INPUT_POLICY=\"DROP\"\n", 0600)
	write(t, filepath.Join(root, "etc/ufw/user.rules"), "existing rules including denies\n", 0600)
	write(t, filepath.Join(root, "etc/ufw/user6.rules"), "existing ipv6 rules\n", 0600)
	write(t, filepath.Join(root, "ufw-state"), "inactive\n", 0600)
	write(t, filepath.Join(root, "allowed"), "8443/tcp\n", 0600)
	home := filepath.Join(root, "usr/local/novas")
	write(t, filepath.Join(home, "novas"), `#!/bin/bash
if [[ "$SCENARIO" == interactive ]]; then
  case "$1 $2" in
    'setting -port')
      [[ "$(cat "$TEST_ROOT/ufw-state")" == active && "$NOVAS_DB_FOLDER" == '' ]] || exit 90
      echo cli-settings >> "$TEST_ROOT/actions"
      exit 0;;
    'admin -show') echo cli-admin-show >> "$TEST_ROOT/actions"; exit 0;;
  esac
fi
[[ "$*" == 'setting -show' && "$NOVAS_DB_FOLDER" == "$TEST_ROOT/usr/local/novas/db" ]] || exit 90
[[ "$SCENARIO" != setting-failure ]] || exit 1
if [[ "$SCENARIO" == malformed-settings ]]; then echo 'Panel port: invalid'; exit; fi
# Exact cmd.showSetting contract, also verified against local CLI setting -show.
printf '\tPanel port:\t %s\n' "${PANEL_PORT:-2095}"
printf '\tSub port:\t %s\n' "${SUB_PORT:-2096}"
`, 0755)
	_, source, _, _ := runtime.Caller(0)
	return root, bin, filepath.Join(filepath.Dir(source), "../../install.sh")
}

func runInstallerFixture(t *testing.T, root, bin, installer, script string, env ...string) ([]byte, error) {
	t.Helper()
	command := exec.Command("bash", "-c", `set -euo pipefail; source "$1"; `+script, "fixture", installer, root)
	// Do not inherit host SSH, storage, startup scripts, or package-manager state.
	command.Env = append([]string{"PATH=" + bin, "HOME=" + root, "TEST_ROOT=" + root,
		"SCENARIO=default", "NOVAS_DB_FOLDER=", "SSH_CONNECTION=192.0.2.1 54321 192.0.2.2 2222"}, env...)
	return command.CombinedOutput()
}

func TestFreshInstallUFW(t *testing.T) {
	for _, scenario := range []string{"default", "custom-ports", "already-active", "without-session", "arch", "opensuse", "preinstalled-unsupported", "socket", "socket-default-config",
		"fedora", "centos", "almalinux", "rocky", "oracle", "centos-yum", "ss-missing", "fedora-ss-missing", "ss-package-failure", "ss-package-missing",
		"armbian", "armbian-package-failure", "armbian-package-missing", "armbian-ss-missing", "armbian-ss-package-failure", "armbian-ss-package-missing",
		"fedora-package-failure", "rocky-package-failure", "centos-yum-package-failure", "fedora-package-missing", "rocky-package-missing",
		"unsupported", "package-failure", "package-missing", "sshd-failure", "ss-failure", "no-listener", "config-mismatch", "empty-config",
		"bad-config-port", "bad-listener-port", "session-mismatch", "bad-session", "setting-failure", "malformed-settings", "bad-panel-port",
		"missing-ipv6-config", "allow-failure", "show-added-failure", "shadowed-rule", "enable-failure", "reload-failure", "snapshot-failure", "status-failure", "inactive", "missing-v6",
		"active-enable-failure", "active-reload-failure", "recovery-failure", "socket-unknown-service", "socket-mismatch"} {
		t.Run(scenario, func(t *testing.T) {
			root, bin, installer := installerFixture(t)
			env := []string{"SCENARIO=" + scenario}
			release := "ubuntu"
			success := false
			switch scenario {
			case "default", "custom-ports", "already-active", "without-session", "arch", "opensuse", "preinstalled-unsupported", "socket", "socket-default-config",
				"fedora", "centos", "almalinux", "rocky", "oracle", "centos-yum", "ss-missing", "fedora-ss-missing", "armbian", "armbian-ss-missing":
				success = true
			}
			switch scenario {
			case "custom-ports":
				env = append(env, "PANEL_PORT=32095", "SUB_PORT=32096")
			case "already-active", "preinstalled-unsupported", "active-enable-failure", "active-reload-failure":
				data, _ := os.ReadFile(filepath.Join(bin, "ufw-fixture"))
				write(t, filepath.Join(bin, "ufw"), string(data), 0755)
				write(t, filepath.Join(root, "ufw-state"), "active\n", 0600)
				if scenario == "preinstalled-unsupported" {
					release = "gentoo"
				}
			case "without-session":
				env = append(env, "SSH_CONNECTION=")
			case "arch":
				release = "arch"
			case "opensuse":
				release = "opensuse-tumbleweed"
			case "unsupported":
				release = "gentoo"
			case "fedora", "centos", "almalinux", "rocky", "oracle":
				release = scenario
			case "centos-yum", "centos-yum-package-failure":
				release = "centos"
				if err := os.Remove(filepath.Join(bin, "dnf")); err != nil {
					t.Fatal(err)
				}
			case "no-listener":
				write(t, filepath.Join(root, "listeners"), "LISTEN 0 128 *:9000 *:* users:((\"other\",pid=43,fd=5))\n", 0600)
			case "config-mismatch":
				write(t, filepath.Join(root, "sshd-config"), "port 22\n", 0600)
			case "empty-config":
				write(t, filepath.Join(root, "sshd-config"), "passwordauthentication yes\n", 0600)
			case "bad-config-port":
				write(t, filepath.Join(root, "sshd-config"), "port 65536\n", 0600)
			case "bad-listener-port":
				write(t, filepath.Join(root, "listeners"), "LISTEN 0 128 *:invalid *:* users:((\"sshd\",pid=42,fd=3))\n", 0600)
			case "session-mismatch":
				env = append(env, "SSH_CONNECTION=192.0.2.1 54321 192.0.2.2 2223")
			case "bad-session":
				env = append(env, "SSH_CONNECTION=192.0.2.1 54321 192.0.2.2 invalid")
			case "bad-panel-port":
				env = append(env, "PANEL_PORT=0")
			case "missing-ipv6-config":
				if err := os.Remove(filepath.Join(root, "etc/default/ufw")); err != nil {
					t.Fatal(err)
				}
			}
			if strings.HasPrefix(scenario, "fedora-") {
				release = "fedora"
			}
			if strings.HasPrefix(scenario, "rocky-") {
				release = "rocky"
			}
			if scenario == "armbian" || strings.HasPrefix(scenario, "armbian-") {
				release = "armbian"
			}
			if strings.Contains(scenario, "ss-missing") || strings.Contains(scenario, "ss-package-") {
				if err := os.Remove(filepath.Join(bin, "ss")); err != nil {
					t.Fatal(err)
				}
				if scenario == "fedora-ss-missing" {
					release = "fedora"
				}
			}
			if strings.HasPrefix(scenario, "socket") {
				write(t, filepath.Join(root, "listeners"), "LISTEN 0 128 0.0.0.0:2222 *:* users:((\"systemd\",pid=1,fd=3))\nLISTEN 0 128 [::]:2200 [::]:* users:((\"systemd\",pid=1,fd=4))\nLISTEN 0 128 *:9000 *:* users:((\"systemd\",pid=1,fd=5))\n", 0600)
				if scenario == "socket-default-config" {
					write(t, filepath.Join(root, "sshd-config"), "port 22\nlistenaddress [::]:22\n", 0600)
				}
				if scenario == "socket-mismatch" {
					write(t, filepath.Join(root, "listeners"), "LISTEN 0 128 *:2223 *:* users:((\"systemd\",pid=1,fd=3))\n", 0600)
				}
			}
			output, err := runInstallerFixture(t, root, bin, installer, `release=`+release+`; install_fresh_ufw "$2/usr/local/novas" "$2"`, env...)
			if success != (err == nil) {
				t.Fatalf("success=%v err=%v\n%s", success, err, output)
			}
			actions, _ := os.ReadFile(filepath.Join(root, "actions"))
			log := string(actions)
			if strings.Contains(log, "iptables") || strings.Contains(log, "nftables") || strings.Contains(log, "reset") || strings.Contains(log, "delete") {
				t.Fatal("unsafe/fallback command", log)
			}
			if strings.Contains(log, "epel-release") || strings.Contains(log, "http") {
				t.Fatal("added a repository", log)
			}
			if release == "armbian" {
				want := "package apt-get install -y -q ufw\n"
				if strings.Contains(scenario, "-ss-") {
					want = "package apt-get install -y -q iproute2\n"
					if success {
						want += "package apt-get install -y -q ufw\n"
					}
				}
				var packages strings.Builder
				for _, line := range strings.Split(log, "\n") {
					if strings.HasPrefix(line, "package ") {
						packages.WriteString(line + "\n")
					}
				}
				if packages.String() != want {
					t.Fatalf("unexpected Armbian package operations: %q want %q", packages.String(), want)
				}
				if strings.Contains(scenario, "-ss-package-") && strings.Contains(log, "ufw ") {
					t.Fatal("touched UFW without SSH listener inspection", log)
				}
				if strings.HasSuffix(scenario, "package-failure") && !bytes.Contains(output, []byte("from configured repositories on armbian")) {
					t.Fatal("configured-repository failure not reported", string(output))
				}
				if strings.HasSuffix(scenario, "package-missing") {
					want := "UFW installation failed"
					if strings.Contains(scenario, "-ss-") {
						want = "SSH listener inspection requires ss"
					}
					if !bytes.Contains(output, []byte(want)) {
						t.Fatal("missing installed command not reported", string(output))
					}
				}
			}
			if strings.Contains(log, "package ") {
				manager := "apt-get"
				switch release {
				case "fedora", "centos", "almalinux", "rocky", "oracle":
					manager = "dnf"
				case "arch":
					manager = "pacman"
				case "opensuse-tumbleweed":
					manager = "zypper"
				}
				if strings.HasPrefix(scenario, "centos-yum") {
					manager = "yum"
				}
				if !strings.Contains(log, "package "+manager+" ") {
					t.Fatal("wrong distribution package manager", log)
				}
			}
			if success {
				panel, sub := "2095", "2096"
				if scenario == "custom-ports" {
					panel, sub = "32095", "32096"
				}
				enable := strings.Index(log, "ufw --force enable")
				for _, port := range []string{"2200", "2222", panel, sub} {
					allow := strings.Index(log, "ufw prepend allow "+port+"/tcp")
					if allow < 0 || enable < allow {
						t.Fatal("missing allow before activation", port, log)
					}
				}
				if strings.Contains(log, "allow 22/tcp\n") || strings.Contains(log, "9000/tcp") || strings.Contains(log, "/udp") {
					t.Fatal("unrelated port opened", log)
				}
				config, _ := os.ReadFile(filepath.Join(root, "etc/default/ufw"))
				if !strings.Contains(string(config), "IPV6=yes") || !strings.Contains(string(config), `DEFAULT_INPUT_POLICY="DROP"`) {
					t.Fatal("lost IPv6/other configuration", string(config))
				}
			} else if scenario != "enable-failure" && scenario != "reload-failure" && scenario != "status-failure" && scenario != "inactive" && scenario != "missing-v6" && !strings.HasPrefix(scenario, "active-") && scenario != "recovery-failure" {
				if strings.Contains(log, "ufw --force enable") {
					t.Fatal("activated UFW despite unsafe discovery/setup", log)
				}
			}
			state, _ := os.ReadFile(filepath.Join(root, "ufw-state"))
			if !success && strings.Contains(log, "ufw --force enable") {
				if strings.HasPrefix(scenario, "active-") {
					if string(state) != "active\n" || strings.Contains(log, "ufw --force disable") {
						t.Fatal("disabled previously active UFW", log)
					}
				} else if scenario == "recovery-failure" {
					if !bytes.Contains(output, []byte("CRITICAL: UFW recovery failed")) {
						t.Fatal("recovery failure not reported", string(output))
					}
				} else if string(state) != "inactive\n" || !strings.Contains(log, "ufw --force disable") {
					t.Fatal("failed activation not restored", log, string(state))
				}
			}
			for path, want := range map[string]string{"etc/ufw/user.rules": "existing rules including denies\n", "etc/ufw/user6.rules": "existing ipv6 rules\n"} {
				data, _ := os.ReadFile(filepath.Join(root, path))
				if string(data) != want {
					t.Fatal("existing rules changed", path)
				}
			}
		})
	}
}

func TestFreshInstallDetection(t *testing.T) {
	for _, artifact := range []string{"none", "usr/local/novas", "usr/bin/novas", "usr/local/bin/novas", "etc/systemd/system/novas.service",
		"etc/systemd/system/novas.service.d", "usr/lib/systemd/system/novas.service", "lib/systemd/system/novas.service", "var/lib/novas", "dangling-link", "custom-db", "path-command"} {
		t.Run(artifact, func(t *testing.T) {
			root, bin, installer := installerFixture(t)
			// The fixture's mock installed binary must not influence pre-install detection.
			if err := os.RemoveAll(filepath.Join(root, "usr/local/novas")); err != nil {
				t.Fatal(err)
			}
			env := []string{}
			switch artifact {
			case "none":
			case "custom-db":
				env = append(env, "NOVAS_DB_FOLDER=/external")
			case "path-command":
				write(t, filepath.Join(bin, "novas"), "#!/bin/bash\nexit 90\n", 0755)
			case "dangling-link":
				if err := os.Symlink("missing", filepath.Join(root, "usr/local/novas")); err != nil {
					t.Fatal(err)
				}
			default:
				write(t, filepath.Join(root, artifact), "existing artifact", 0600)
			}
			output, err := runInstallerFixture(t, root, bin, installer, `installer_is_fresh "$2"`, env...)
			if (artifact == "none") != (err == nil) {
				t.Fatalf("artifact=%s err=%v\n%s", artifact, err, output)
			}
		})
	}
}

func TestInstallerConfigureFirewallGate(t *testing.T) {
	for _, scenario := range []string{"fresh", "upgrade", "concurrent-upgrade", "ambiguous", "settings-fail", "ufw-fail"} {
		t.Run(scenario, func(t *testing.T) {
			root, bin, installer := installerFixture(t)
			output, err := runInstallerFixture(t, root, bin, installer, `
config_after_install() { echo settings >> "$TEST_ROOT/actions"; [[ "$SCENARIO" != settings-fail ]]; }
install_fresh_ufw() { echo firewall >> "$TEST_ROOT/actions"; [[ "$SCENARIO" != ufw-fail ]]; }
FRESH_INSTALL=1; old=""
case "$SCENARIO" in
  upgrade) FRESH_INSTALL=0; old=novas;;
  concurrent-upgrade) old=novas;;
  ambiguous) FRESH_INSTALL=0;;
esac
novas_configure "$2/usr/local/novas" "$old"
`, "SCENARIO="+scenario)
			if (scenario == "settings-fail" || scenario == "ufw-fail") != (err != nil) {
				t.Fatalf("%v\n%s", err, output)
			}
			actions, _ := os.ReadFile(filepath.Join(root, "actions"))
			want := ""
			switch scenario {
			case "fresh", "ufw-fail":
				want = "settings\nfirewall\n"
			case "ambiguous", "settings-fail":
				want = "settings\n"
			}
			if string(actions) != want {
				t.Fatalf("unexpected configure order/actions: %q want %q", actions, want)
			}
		})
	}
}

func TestInstallerUFWLateDeploymentFailure(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "new-activation", true: "already-active"}[active], func(t *testing.T) {
			root, bin, installer := installerFixture(t)
			if active {
				data, _ := os.ReadFile(filepath.Join(bin, "ufw-fixture"))
				write(t, filepath.Join(bin, "ufw"), string(data), 0755)
				write(t, filepath.Join(root, "ufw-state"), "active\n", 0600)
			}
			// Only archive/network plumbing and the deployment outcome are stubbed.
			// Real installer UFW setup and its outer deployment EXIT trap run here.
			staging := filepath.Join(root, "staging")
			write(t, filepath.Join(staging, "novas/novas"), "#!/bin/bash\nexit 90\n", 0755)
			write(t, filepath.Join(staging, "novas/scripts/install-runtime.sh"), `novas_deploy() {
  install_fresh_ufw "$TEST_ROOT/usr/local/novas" "$TEST_ROOT" || return 1
  echo simulated-late-health-failure >&2
  return 7
}
`, 0600)
			output, err := runInstallerFixture(t, root, bin, installer, `
release=ubuntu
mktemp() { printf '%s\n' "$TEST_ROOT/staging"; }
arch() { echo amd64; }
download_to_file() { :; }
verify_archive_checksum() { :; }
tar() { if [[ "$1" == -tzf ]]; then echo novas/; fi; }
install_novas v1.6.128
`)
			if err == nil || !bytes.Contains(output, []byte("simulated-late-health-failure")) || bytes.Contains(output, []byte("安装成功")) {
				t.Fatalf("late deployment failure was accepted: %v\n%s", err, output)
			}
			state, _ := os.ReadFile(filepath.Join(root, "ufw-state"))
			want := "inactive\n"
			if active {
				want = "active\n"
			}
			if string(state) != want {
				t.Fatalf("activation state not recovered: %q want %q\n%s", state, want, output)
			}
			actions, _ := os.ReadFile(filepath.Join(root, "actions"))
			if active && bytes.Contains(actions, []byte("ufw --force disable")) {
				t.Fatal("disabled preexisting firewall", string(actions))
			}
			if _, err := os.Stat(staging); !os.IsNotExist(err) {
				t.Fatal("installer staging was not cleaned", err)
			}
		})
	}
}

func TestFreshInteractiveUsesUFWBeforeSettingCLI(t *testing.T) {
	root, bin, installer := installerFixture(t)
	output, err := runInstallerFixture(t, root, bin, installer, `
release=ubuntu; FRESH_INSTALL=1
config_after_install "$2/usr/local/novas" "$2" <<'INPUT'
y
32095

32096

n
INPUT
`, "SCENARIO=interactive")
	if err != nil {
		t.Fatalf("interactive fresh install: %v\n%s", err, output)
	}
	actions, _ := os.ReadFile(filepath.Join(root, "actions"))
	log := string(actions)
	enable, settings := strings.Index(log, "ufw --force enable"), strings.Index(log, "cli-settings")
	if enable < 0 || settings < enable {
		t.Fatal("setting CLI could choose a non-UFW backend", log)
	}
	for _, port := range []string{"2222", "2200", "32095", "32096"} {
		allow := strings.Index(log, "ufw prepend allow "+port+"/tcp")
		if allow < 0 || allow > enable {
			t.Fatal("requested access missing before enable", port, log)
		}
	}
	if strings.Contains(log, "allow 2095/tcp") || strings.Contains(log, "allow 2096/tcp") || strings.Contains(log, "iptables") {
		t.Fatal("opened unused defaults or used iptables", log)
	}
}
