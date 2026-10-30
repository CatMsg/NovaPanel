package cmd

import (
	"bytes"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Opt-in end-to-end check against the actual release-feature binary, never the
// developer's database. Run with NOVAS_TEST_BINARY=/absolute/path/to/novas.
func TestRuntimeBinarySmoke(t *testing.T) {
	binary := os.Getenv("NOVAS_TEST_BINARY")
	if binary == "" {
		t.Skip("set NOVAS_TEST_BINARY to exercise the built runtime")
	}
	dir := t.TempDir()
	// Isolate the external firewall boundary even on Linux CI. The real binary
	// runs, but this lifecycle test must never mutate its host's network rules.
	source, err := os.Open(binary)
	if err != nil {
		t.Fatal(err)
	}
	binary = filepath.Join(dir, "novas")
	destination, err := os.OpenFile(binary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		source.Close()
		t.Fatal(err)
	}
	_, copyErr := io.Copy(destination, source)
	source.Close()
	closeErr := destination.Close()
	if copyErr != nil || closeErr != nil {
		t.Fatal(copyErr, closeErr)
	}
	if err := os.Mkdir(filepath.Join(dir, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts/hy2-forward.sh"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ufw"), []byte("#!/bin/sh\necho 'Status: active'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "NOVAS_DB_FOLDER="+dir, "NOVAS_LOG_LEVEL=error", "NOVAS_DEVELOPMENT_DEFAULT_ADMIN=", "PATH="+dir+":"+os.Getenv("PATH"))
	run := func(args ...string) []byte {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Env = env
		cmd.Dir = dir
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("runtime command %s failed: %v", args[0], err)
		}
		return output
	}
	freePort := func() string {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	}
	panelPort, subPort := freePort(), freePort()
	for panelPort == subPort {
		subPort = freePort()
	}
	run("setting", "-port", panelPort, "-path", "/runtime-test/", "-subPort", subPort)
	setAdmin := exec.Command(binary, "admin", "-username", "runtime-smoke", "-password-stdin")
	setAdmin.Env, setAdmin.Dir = env, dir
	setAdmin.Stdin = strings.NewReader("temporary-test-only\n")
	if output, err := setAdmin.CombinedOutput(); err != nil {
		t.Fatalf("runtime admin password update failed: %v: %s", err, output)
	}
	plaintextPassword := exec.Command(binary, "admin", "-username", "runtime-smoke", "-password", "smoke-only-secret")
	plaintextPassword.Env, plaintextPassword.Dir = env, dir
	if output, err := plaintextPassword.CombinedOutput(); err == nil || !strings.Contains(string(output), "plaintext -password is disabled") {
		t.Fatalf("runtime accepted plaintext password argument: err=%v output=%s", err, output)
	}
	blockedDBFolder := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blockedDBFolder, []byte("block"), 0600); err != nil {
		t.Fatal(err)
	}
	failureEnv := make([]string, 0, len(env)+1)
	for _, item := range env {
		if !strings.HasPrefix(item, "NOVAS_DB_FOLDER=") {
			failureEnv = append(failureEnv, item)
		}
	}
	failureEnv = append(failureEnv, "NOVAS_DB_FOLDER="+blockedDBFolder)
	failureCases := []struct {
		name  string
		args  []string
		stdin string
	}{
		{name: "missing subcommand"},
		{name: "admin update", args: []string{"admin", "-username", "runtime-smoke", "-password-stdin"}, stdin: "temporary-test-only\n"},
		{name: "admin reset", args: []string{"admin", "-reset"}},
		{name: "admin show", args: []string{"admin", "-show"}},
		{name: "setting update", args: []string{"setting", "-path", "/runtime-test/"}},
		{name: "setting reset", args: []string{"setting", "-reset"}},
		{name: "setting show", args: []string{"setting", "-show"}},
		{name: "panel uri", args: []string{"uri"}},
	}
	for _, test := range failureCases {
		failedCommand := exec.Command(binary, test.args...)
		failedCommand.Env, failedCommand.Dir = failureEnv, dir
		failedCommand.Stdin = strings.NewReader(test.stdin)
		if output, err := failedCommand.CombinedOutput(); err == nil {
			t.Errorf("%s command reported success when its database could not open: %s", test.name, output)
		}
	}
	before := run("admin", "-show")
	run("upgrade-db")
	run("checkdb")
	if after := run("admin", "-show"); !bytes.Equal(before, after) {
		t.Fatal("administrator changed during validation")
	}
	var logs bytes.Buffer
	server := exec.Command(binary)
	server.Env, server.Dir = env, dir
	server.Stdout, server.Stderr = &logs, &logs
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Wait() }()
	defer func() {
		_ = server.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(15 * time.Second):
			_ = server.Process.Kill()
			<-done
		}
	}()
	for attempt := 0; attempt < 40; attempt++ {
		probe := exec.Command(binary, "healthcheck")
		probe.Env = env
		if probe.Run() == nil {
			if after := run("admin", "-show"); !bytes.Equal(before, after) {
				t.Fatal("administrator changed after startup")
			}
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatal("runtime never became HTTP-healthy")
}
