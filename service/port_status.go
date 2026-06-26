package service

import (
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PortListenEntry struct {
	Protocol string `json:"protocol"`
	Local    string `json:"local"`
	Port     int    `json:"port"`
	Process  string `json:"process,omitempty"`
	PID      int    `json:"pid,omitempty"`
	Raw      string `json:"raw"`
}

type PortNatEntry struct {
	Family   string `json:"family"`
	Table    string `json:"table"`
	Chain    string `json:"chain"`
	Protocol string `json:"protocol,omitempty"`
	DPort    string `json:"dport,omitempty"`
	Target   string `json:"target,omitempty"`
	ToPorts  string `json:"to_ports,omitempty"`
	Raw      string `json:"raw"`
}

func (s *ServerService) GetPortStatus() map[string]interface{} {
	listeners, listenErrors := collectListenEntries()
	natIPv4, natIPv6, natErrors := collectNatEntries()

	errors := append(listenErrors, natErrors...)
	result := map[string]interface{}{
		"backend":     detectFirewallBackend(),
		"captured_at": time.Now().Format(time.RFC3339),
		"listeners":   listeners,
		"nat_ipv4":    natIPv4,
		"nat_ipv6":    natIPv6,
		"errors":      errors,
	}
	return result
}

var ssUsersRe = regexp.MustCompile(`users:\(\("([^"]+)",pid=([0-9]+),fd=[0-9]+`)

func collectListenEntries() ([]PortListenEntry, []string) {
	entries := make([]PortListenEntry, 0)
	errors := make([]string, 0)

	for _, spec := range []struct {
		protocol string
		args     []string
	}{
		{protocol: "tcp", args: []string{"-H", "-lntp"}},
		{protocol: "udp", args: []string{"-H", "-lnup"}},
	} {
		if !commandExists("ss") {
			errors = append(errors, "ss command not found")
			break
		}
		output, err := exec.Command("ss", spec.args...).CombinedOutput()
		if err != nil {
			trimmed := strings.TrimSpace(string(output))
			if trimmed != "" {
				errors = append(errors, fmt.Sprintf("ss %s failed: %s", strings.Join(spec.args, " "), trimmed))
			} else {
				errors = append(errors, fmt.Sprintf("ss %s failed: %v", strings.Join(spec.args, " "), err))
			}
			continue
		}
		entries = append(entries, parseSSListenOutput(spec.protocol, string(output))...)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Protocol != entries[j].Protocol {
			return entries[i].Protocol < entries[j].Protocol
		}
		if entries[i].Port != entries[j].Port {
			return entries[i].Port < entries[j].Port
		}
		if entries[i].Local != entries[j].Local {
			return entries[i].Local < entries[j].Local
		}
		return entries[i].Raw < entries[j].Raw
	})

	return entries, errors
}

func parseSSListenOutput(protocol, raw string) []PortListenEntry {
	lines := strings.Split(raw, "\n")
	entries := make([]PortListenEntry, 0)
	seen := map[string]struct{}{}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		local := fields[3]
		port, err := parsePortFromListenAddress(local)
		if err != nil {
			continue
		}
		process, pid := parseSSProcess(line)
		key := protocol + "|" + local + "|" + process + "|" + strconv.Itoa(pid) + "|" + line
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		entries = append(entries, PortListenEntry{
			Protocol: protocol,
			Local:    local,
			Port:     port,
			Process:  process,
			PID:      pid,
			Raw:      line,
		})
	}

	return entries
}

func parseSSProcess(raw string) (string, int) {
	match := ssUsersRe.FindStringSubmatch(raw)
	if len(match) < 3 {
		return "", 0
	}
	pid, err := strconv.Atoi(match[2])
	if err != nil {
		pid = 0
	}
	return match[1], pid
}

func parsePortFromListenAddress(addr string) (int, error) {
	idx := strings.LastIndex(addr, ":")
	if idx < 0 || idx == len(addr)-1 {
		return 0, fmt.Errorf("invalid listen addr: %s", addr)
	}
	port, err := strconv.Atoi(strings.TrimSpace(addr[idx+1:]))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid listen port: %s", addr)
	}
	return port, nil
}

func collectNatEntries() ([]PortNatEntry, []PortNatEntry, []string) {
	errors := make([]string, 0)

	if commandExists("iptables-save") || commandExists("ip6tables-save") {
		ipv4, err4 := collectNatEntriesFromIptables("iptables-save", "ipv4")
		ipv6, err6 := collectNatEntriesFromIptables("ip6tables-save", "ipv6")
		if err4 != nil {
			errors = append(errors, err4.Error())
		}
		if err6 != nil {
			errors = append(errors, err6.Error())
		}
		if len(ipv4) > 0 || len(ipv6) > 0 || len(errors) > 0 {
			sortNatEntries(ipv4)
			sortNatEntries(ipv6)
			return ipv4, ipv6, errors
		}
	}

	if commandExists("nft") {
		ipv4, ipv6, err := collectNatEntriesFromNFT()
		if err != nil {
			errors = append(errors, err.Error())
		}
		sortNatEntries(ipv4)
		sortNatEntries(ipv6)
		return ipv4, ipv6, errors
	}

	if !commandExists("iptables-save") && !commandExists("ip6tables-save") && !commandExists("nft") {
		errors = append(errors, "no nat inspection command found")
	}

	return nil, nil, errors
}

func sortNatEntries(entries []PortNatEntry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Chain != entries[j].Chain {
			return entries[i].Chain < entries[j].Chain
		}
		if entries[i].Protocol != entries[j].Protocol {
			return entries[i].Protocol < entries[j].Protocol
		}
		if entries[i].DPort != entries[j].DPort {
			return entries[i].DPort < entries[j].DPort
		}
		return entries[i].Raw < entries[j].Raw
	})
}

func collectNatEntriesFromIptables(bin, family string) ([]PortNatEntry, error) {
	if !commandExists(bin) {
		return nil, nil
	}

	output, err := exec.Command(bin, "-t", "nat").CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return nil, fmt.Errorf("%s -t nat failed: %s", bin, trimmed)
		}
		return nil, fmt.Errorf("%s -t nat failed: %w", bin, err)
	}

	lines := strings.Split(string(output), "\n")
	entries := make([]PortNatEntry, 0)
	for _, raw := range lines {
		if entry := parseIptablesNatLine(strings.TrimSpace(raw), family); entry != nil {
			entries = append(entries, *entry)
		}
	}
	return entries, nil
}

func parseIptablesNatLine(line, family string) *PortNatEntry {
	if !strings.HasPrefix(line, "-A ") {
		return nil
	}

	fields := strings.Fields(line)
	if len(fields) < 2 {
		return nil
	}

	entry := &PortNatEntry{
		Family: family,
		Table:  "nat",
		Chain:  fields[1],
		Raw:    line,
	}

	for i := 2; i < len(fields); i++ {
		switch fields[i] {
		case "-p":
			if i+1 < len(fields) {
				entry.Protocol = fields[i+1]
				i++
			}
		case "--dport":
			if i+1 < len(fields) {
				entry.DPort = fields[i+1]
				i++
			}
		case "-j":
			if i+1 < len(fields) {
				entry.Target = fields[i+1]
				i++
			}
		case "--to-ports":
			if i+1 < len(fields) {
				entry.ToPorts = fields[i+1]
				i++
			}
		}
	}

	return entry
}

func collectNatEntriesFromNFT() ([]PortNatEntry, []PortNatEntry, error) {
	if !commandExists("nft") {
		return nil, nil, nil
	}

	output, err := exec.Command("nft", "list", "ruleset").CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			return nil, nil, fmt.Errorf("nft list ruleset failed: %s", trimmed)
		}
		return nil, nil, fmt.Errorf("nft list ruleset failed: %w", err)
	}

	lines := strings.Split(string(output), "\n")
	ipv4 := make([]PortNatEntry, 0)
	ipv6 := make([]PortNatEntry, 0)
	family := ""
	table := ""
	chain := ""
	inNatTable := false

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "table ") {
			fields := strings.Fields(line)
			if len(fields) >= 3 {
				family = fields[1]
				table = strings.TrimSuffix(fields[2], "{")
				inNatTable = table == "nat"
				chain = ""
			}
			continue
		}
		if !inNatTable {
			continue
		}
		if strings.HasPrefix(line, "chain ") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				chain = strings.TrimSuffix(fields[1], "{")
			}
			continue
		}
		if line == "}" {
			continue
		}
		if strings.Contains(line, "dport") || strings.Contains(line, "redirect") || strings.Contains(line, "masquerade") {
			entry := PortNatEntry{
				Family: family,
				Table:  table,
				Chain:  chain,
				Raw:    line,
			}
			if family == "ip6" {
				ipv6 = append(ipv6, entry)
			} else {
				ipv4 = append(ipv4, entry)
			}
		}
	}

	return ipv4, ipv6, nil
}
