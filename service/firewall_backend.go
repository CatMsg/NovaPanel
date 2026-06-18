package service

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func detectFirewallBackend() string {
	if isUFWActive() {
		return "UFW"
	}
	if commandExists("iptables") || commandExists("ip6tables") {
		return "iptables"
	}
	if commandExists("nft") {
		return "nftables"
	}
	return "unknown"
}

func ensureFirewallBackend() (string, error) {
	backend := detectFirewallBackend()
	if backend != "unknown" {
		return backend, nil
	}

	if err := installFirewallBackend(); err != nil {
		return "", err
	}

	backend = detectFirewallBackend()
	if backend == "unknown" {
		return "", fmt.Errorf("firewall backend still unavailable after installation")
	}

	return backend, nil
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

func installFirewallBackend() error {
	release, err := osReleaseID()
	if err != nil {
		return err
	}

	run := func(name string, args ...string) error {
		cmd := exec.Command(name, args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			trimmed := strings.TrimSpace(string(output))
			if trimmed != "" {
				return fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(args, " "), err, trimmed)
			}
			return fmt.Errorf("%s %s failed: %w", name, strings.Join(args, " "), err)
		}
		return nil
	}

	switch release {
	case "centos", "almalinux", "rocky", "oracle":
		if err := run("yum", "install", "-y", "-q", "iptables"); err == nil {
			return nil
		}
		return run("yum", "install", "-y", "-q", "nftables")
	case "fedora":
		if err := run("dnf", "install", "-y", "-q", "iptables"); err == nil {
			return nil
		}
		return run("dnf", "install", "-y", "-q", "nftables")
	case "arch", "manjaro", "parch":
		if err := run("pacman", "-S", "--noconfirm", "--needed", "iptables"); err == nil {
			return nil
		}
		return run("pacman", "-S", "--noconfirm", "--needed", "nftables")
	case "opensuse-tumbleweed":
		if err := run("zypper", "-q", "install", "-y", "iptables"); err == nil {
			return nil
		}
		return run("zypper", "-q", "install", "-y", "nftables")
	default:
		if err := run("apt-get", "install", "-y", "-q", "iptables"); err == nil {
			return nil
		}
		return run("apt-get", "install", "-y", "-q", "nftables")
	}
}

func osReleaseID() (string, error) {
	for _, file := range []string{"/etc/os-release", "/usr/lib/os-release"} {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "ID=") {
				id := strings.TrimSpace(strings.TrimPrefix(line, "ID="))
				id = strings.Trim(id, `"`)
				if id != "" {
					return id, nil
				}
			}
		}
	}
	return "", fmt.Errorf("unable to determine os release id")
}
