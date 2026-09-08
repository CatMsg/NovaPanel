package cmd

import (
	"bytes"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	env := append(os.Environ(), "NOVAS_DB_FOLDER="+dir, "NOVAS_LOG_LEVEL=error", "PATH="+dir+":"+os.Getenv("PATH"))
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
	run("admin", "-username", "runtime-smoke", "-password", "temporary-test-only")
	before := run("admin", "-show")
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
