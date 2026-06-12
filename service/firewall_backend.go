package service

import (
	"os/exec"
	"strings"
)

func detectFirewallBackend() string {
	if isUFWActive() {
		return "UFW"
	}
	if commandExists("nft") {
		return "nftables"
	}
	if commandExists("iptables") || commandExists("ip6tables") {
		return "iptables"
	}
	return "unknown"
}

func isUFWActive() bool {
	if !commandExists("ufw") {
		return false
	}
	output, err := exec.Command("ufw", "status").CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), "Status: active")
}

func commandExists(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
