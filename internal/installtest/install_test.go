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
	for _, scenario := range []string{"fresh", "current", "validation-failure", "health-failure", "custom-db", "bad-package", "locked"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "usr/local/novas")
			old := home
			oldName := "novas"
			units := filepath.Join(root, "etc/systemd/system")
			if scenario != "fresh" {
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
			for _, file := range []string{"novas.sh", "novas.service", "scripts/hy2-forward.sh", "bin/mita"} {
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
			command := exec.Command("bash", "-c", `source "$1"; novas_configure() { :; }; novas_deploy "$2" "$3"`, "fixture", helper, pkg, root)
			command.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "SCENARIO="+scenario, "TEST_ROOT="+root, "NOVAS_DB_FOLDER=")
			output, err := command.CombinedOutput()
			success := scenario == "fresh" || scenario == "current"
			if success && err != nil {
				t.Fatalf("%v\n%s", err, output)
			}
			if !success && err == nil {
				t.Fatalf("failure was accepted\n%s", output)
			}
			if success {
				if _, err := os.Stat(filepath.Join(root, "active-novas")); err != nil {
					t.Fatal("new service not started", err)
				}
				if scenario != "fresh" {
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
