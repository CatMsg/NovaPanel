package service

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
)

var (
	sshListenPortsMu sync.RWMutex
	sshListenPorts   []int
)

type sshPortConflictError struct {
	inboundTag string
	ports      []int
}

func (e *sshPortConflictError) Error() string {
	if e == nil {
		return ""
	}
	if e.inboundTag != "" {
		return fmt.Sprintf("保存失败：入站 %s 与 SSH 监听端口 %v 冲突，请先修改端口", e.inboundTag, e.ports)
	}
	return fmt.Sprintf("保存失败：检测到 SSH 监听端口 %v，不能与入站端口重叠", e.ports)
}

// InitSSHListenPorts snapshots the current ssh listener ports for later save-time checks.
func InitSSHListenPorts() error {
	ports, err := detectSSHListenPorts()
	if err != nil || len(ports) == 0 {
		ports = []int{22}
		if err != nil {
			return storeSSHListenPorts(ports, fmt.Errorf("detect ssh listen port failed, fallback to 22: %w", err))
		}
		return storeSSHListenPorts(ports, fmt.Errorf("detect ssh listen port failed, fallback to 22"))
	}
	return storeSSHListenPorts(ports, nil)
}

func storeSSHListenPorts(ports []int, err error) error {
	sshListenPortsMu.Lock()
	defer sshListenPortsMu.Unlock()
	sshListenPorts = append([]int(nil), ports...)
	if err != nil {
		return err
	}
	return nil
}

func getSSHListenPorts() []int {
	sshListenPortsMu.RLock()
	defer sshListenPortsMu.RUnlock()
	return append([]int(nil), sshListenPorts...)
}

func detectSSHListenPorts() ([]int, error) {
	if ports, err := detectSSHPortsFromSSHDT(); err == nil && len(ports) > 0 {
		return ports, nil
	}
	if ports, err := detectSSHPortsFromSS(); err == nil && len(ports) > 0 {
		return ports, nil
	}
	if ports, err := detectSSHPortsFromConfig(); err == nil && len(ports) > 0 {
		return ports, nil
	}
	return nil, fmt.Errorf("no ssh port detected")
}

func detectSSHPortsFromSSHDT() ([]int, error) {
	bin, err := exec.LookPath("sshd")
	if err != nil {
		return nil, err
	}

	output, err := runCommandOutput(6*time.Second, bin, "-T")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseSSHDPortsOutput(string(output)), nil
}

func detectSSHPortsFromSS() ([]int, error) {
	bin, err := exec.LookPath("ss")
	if err != nil {
		return nil, err
	}

	output, err := runCommandOutput(6*time.Second, bin, "-H", "-ltnp")
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseSSListenPorts(string(output)), nil
}

func detectSSHPortsFromConfig() ([]int, error) {
	candidates := []string{
		"/etc/ssh/sshd_config",
		"/usr/local/etc/ssh/sshd_config",
	}
	ports := make([]int, 0)
	seen := map[int]struct{}{}
	for _, file := range candidates {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, port := range parseSSHDPortsOutput(string(data)) {
			if _, exists := seen[port]; exists {
				continue
			}
			seen[port] = struct{}{}
			ports = append(ports, port)
		}
	}
	if len(ports) == 0 {
		return nil, fmt.Errorf("no ssh port found in config")
	}
	return ports, nil
}

var sshPortLineRe = regexp.MustCompile(`(?mi)^\s*port\s+(\d+)\s*$`)

func parseSSHDPortsOutput(raw string) []int {
	matches := sshPortLineRe.FindAllStringSubmatch(raw, -1)
	return normalizePortList(matches, 1)
}

func parseSSListenPorts(raw string) []int {
	lines := strings.Split(raw, "\n")
	ports := make([]int, 0)
	seen := map[int]struct{}{}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, "sshd") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		local := fields[3]
		idx := strings.LastIndex(local, ":")
		if idx < 0 || idx == len(local)-1 {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSpace(local[idx+1:]))
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		if _, exists := seen[port]; exists {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func normalizePortList(matches [][]string, group int) []int {
	ports := make([]int, 0)
	seen := map[int]struct{}{}
	for _, match := range matches {
		if len(match) <= group {
			continue
		}
		port, err := strconv.Atoi(match[group])
		if err != nil || port < 1 || port > 65535 {
			continue
		}
		if _, exists := seen[port]; exists {
			continue
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	sort.Ints(ports)
	return ports
}

func validateInboundPortsAgainstSSH(inbound *model.Inbound, ports []int) error {
	return validateInboundPortRangesAgainstSSH(inbound, managedPortRangesFromPorts(ports))
}

func validateInboundPortRangesAgainstSSH(inbound *model.Inbound, ranges []managedPortRange) error {
	sshPorts := getSSHListenPorts()
	if len(sshPorts) == 0 {
		return nil
	}
	ranges = normalizeManagedPortRanges(ranges)

	conflicts := make([]int, 0)
	seen := map[int]struct{}{}
	for _, port := range sshPorts {
		matched := false
		for _, item := range ranges {
			if managedPortRangeContains(item, port) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if _, exists := seen[port]; exists {
			continue
		}
		seen[port] = struct{}{}
		conflicts = append(conflicts, port)
	}

	if len(conflicts) == 0 {
		return nil
	}

	sort.Ints(conflicts)
	tag := ""
	if inbound != nil {
		tag = inbound.Tag
	}
	return &sshPortConflictError{inboundTag: tag, ports: conflicts}
}
