package service

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	loginGuardScript  = "scripts/login-guard.sh"
	loginGuardJail    = "novapanel"
	loginGuardTimeout = 30 * time.Second
)

type LoginProtectionStatus struct {
	Supported bool     `json:"supported"`
	Installed bool     `json:"installed"`
	Active    bool     `json:"active"`
	Jail      string   `json:"jail"`
	BannedIPs []string `json:"bannedIps"`
	Error     string   `json:"error,omitempty"`
}

type LoginGuardService struct {
	SettingService
}

func loginGuardScriptPath() string {
	executable, err := os.Executable()
	if err == nil {
		candidate := filepath.Join(filepath.Dir(executable), loginGuardScript)
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate
		}
	}
	return loginGuardScript
}

func loginProtectionSupported() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	info, err := os.Stat("/run/systemd/system")
	return err == nil && info.IsDir()
}

func normalizeNetworkList(raw string) (string, error) {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	seen := make(map[string]struct{}, len(fields))
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		value := strings.TrimSpace(field)
		if value == "" {
			continue
		}
		if ip := net.ParseIP(value); ip != nil {
			value = ip.String()
		} else if _, network, err := net.ParseCIDR(value); err == nil {
			value = network.String()
		} else {
			return "", fmt.Errorf("invalid IP or CIDR: %s", value)
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return strings.Join(values, " "), nil
}

func (s *LoginGuardService) SyncLoginProtection() error {
	if !loginProtectionSupported() {
		return nil
	}
	port, err := s.GetPort()
	if err != nil {
		return err
	}
	allowlist, err := s.getString("loginBanAllowlist")
	if err != nil {
		return err
	}
	allowlist, err = normalizeNetworkList(allowlist)
	if err != nil {
		return err
	}
	_, err = runCommandOutput(loginGuardTimeout, "bash", loginGuardScriptPath(), "sync", strconv.Itoa(port), allowlist)
	return formatExternalCommandError("login protection sync failed", err)
}

func (s *LoginGuardService) GetLoginProtectionStatus() LoginProtectionStatus {
	status := LoginProtectionStatus{Supported: loginProtectionSupported(), Jail: loginGuardJail, BannedIPs: []string{}}
	if !status.Supported {
		return status
	}
	if _, err := exec.LookPath("fail2ban-client"); err != nil {
		status.Error = "fail2ban-client is not installed"
		return status
	}
	status.Installed = true
	output, err := runCommandOutput(5*time.Second, "fail2ban-client", "status", loginGuardJail)
	if err != nil {
		status.Error = strings.TrimSpace(string(output))
		if status.Error == "" {
			status.Error = err.Error()
		}
		return status
	}
	status.Active = true
	status.BannedIPs = parseFail2banBannedIPs(string(output))
	return status
}

func parseFail2banBannedIPs(output string) []string {
	banned := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "`|- "))
		if !strings.HasPrefix(line, "Banned IP list:") {
			continue
		}
		for _, value := range strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "Banned IP list:"))) {
			if ip := net.ParseIP(value); ip != nil {
				banned = append(banned, ip.String())
			}
		}
	}
	return banned
}

func (s *LoginGuardService) UnbanLoginIP(rawIP string) error {
	if !loginProtectionSupported() {
		return errors.New("login protection requires Linux with systemd")
	}
	ip := net.ParseIP(strings.TrimSpace(rawIP))
	if ip == nil {
		return fmt.Errorf("invalid IP: %s", rawIP)
	}
	_, err := runCommandOutput(5*time.Second, "fail2ban-client", "set", loginGuardJail, "unbanip", ip.String())
	return formatExternalCommandError("login protection unban failed", err)
}

func (s *SettingService) GetLoginTrustedProxies() (string, error) {
	value, err := s.getString("loginTrustedProxies")
	if err != nil {
		return "", err
	}
	return normalizeNetworkList(value)
}
