package service

import (
	"strings"
	"testing"

	"github.com/CatMsg/NovaPanel/database/model"
)

func TestParseSSHDPortsOutput(t *testing.T) {
	ports := parseSSHDPortsOutput(`
		permitrootlogin yes
		port 22
		port 2222
		port 22
	`)
	got := joinPorts(ports)
	want := "22,2222"
	if got != want {
		t.Fatalf("unexpected sshd ports: got %s want %s", got, want)
	}
}

func TestParseSSListenPorts(t *testing.T) {
	ports := parseSSListenPorts(`
LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=1,fd=3))
LISTEN 0 128 [::]:2222 [::]:* users:(("sshd",pid=2,fd=4))
LISTEN 0 128 127.0.0.1:8080 0.0.0.0:* users:(("nginx",pid=3,fd=5))
`)
	got := joinPorts(ports)
	want := "22,2222"
	if got != want {
		t.Fatalf("unexpected listen ports: got %s want %s", got, want)
	}
}

func TestValidateInboundPortsAgainstSSH(t *testing.T) {
	t.Cleanup(func() {
		_ = storeSSHListenPorts(nil, nil)
	})
	if err := storeSSHListenPorts([]int{22, 2222}, nil); err != nil {
		t.Fatalf("store ports: %v", err)
	}

	if err := validateInboundPortsAgainstSSH(&model.Inbound{Tag: "demo"}, []int{80, 443}); err != nil {
		t.Fatalf("unexpected conflict for safe ports: %v", err)
	}

	err := validateInboundPortsAgainstSSH(&model.Inbound{Tag: "hy2-demo"}, []int{80, 2222})
	if err == nil {
		t.Fatal("expected conflict for ssh port")
	}
	if !strings.Contains(err.Error(), "hy2-demo") || !strings.Contains(err.Error(), "2222") {
		t.Fatalf("unexpected conflict message: %v", err)
	}
}
