package database

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"gorm.io/gorm"
)

func cleanupRestoredInboundConflicts() error {
	sshPorts, err := detectSSHListenPortsForRestore()
	if err != nil || len(sshPorts) == 0 {
		sshPorts = []int{22}
		if err != nil {
			logger.Warning("detect ssh listen ports for restore failed, fallback to 22:", err)
		}
	}
	return pruneInboundConflictsBySSHPorts(sshPorts)
}

func cleanupRestoredEndpointConflicts() error {
	sshPorts, err := detectSSHListenPortsForRestore()
	if err != nil || len(sshPorts) == 0 {
		sshPorts = []int{22}
		if err != nil {
			logger.Warning("detect ssh listen ports for restore failed, fallback to 22:", err)
		}
	}
	return pruneEndpointConflictsBySSHPorts(sshPorts)
}

func pruneInboundConflictsBySSHPorts(sshPorts []int) error {
	if len(sshPorts) == 0 || db == nil {
		return nil
	}

	sshSet := make(map[int]struct{}, len(sshPorts))
	for _, port := range sshPorts {
		if port < 1 || port > 65535 {
			continue
		}
		sshSet[port] = struct{}{}
	}
	if len(sshSet) == 0 {
		return nil
	}

	var inbounds []model.Inbound
	removedTags := make([]string, 0)
	err := WithRetryTx(5, 150*time.Millisecond, func(tx *gorm.DB) error {
		if err := tx.Model(model.Inbound{}).Find(&inbounds).Error; err != nil {
			return err
		}
		for _, inbound := range inbounds {
			ranges, ok, err := collectInboundPortRangesForRestore(&inbound)
			if err != nil {
				logger.Warning("skip inbound restore conflict check failed: ", err)
				continue
			}
			if !ok || !hasPortRangeConflict(ranges, sshSet) {
				continue
			}
			if err := removeInboundFromRestoreTx(tx, &inbound); err != nil {
				return err
			}
			removedTags = append(removedTags, inbound.Tag)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(removedTags) > 0 {
		logger.Info("removed conflicted inbounds during restore: ", strings.Join(removedTags, ","))
	}
	return nil
}

func pruneEndpointConflictsBySSHPorts(sshPorts []int) error {
	if len(sshPorts) == 0 || db == nil {
		return nil
	}

	sshSet := make(map[int]struct{}, len(sshPorts))
	for _, port := range sshPorts {
		if port < 1 || port > 65535 {
			continue
		}
		sshSet[port] = struct{}{}
	}
	if len(sshSet) == 0 {
		return nil
	}

	var endpoints []model.Endpoint
	removedTags := make([]string, 0)
	err := WithRetryTx(5, 150*time.Millisecond, func(tx *gorm.DB) error {
		if err := tx.Model(model.Endpoint{}).Find(&endpoints).Error; err != nil {
			return err
		}
		for _, endpoint := range endpoints {
			ranges, ok, err := collectEndpointPortRangesForRestore(&endpoint)
			if err != nil {
				logger.Warning("skip endpoint restore conflict check failed: ", err)
				continue
			}
			if !ok || !hasPortRangeConflict(ranges, sshSet) {
				continue
			}
			if err := removeEndpointFromRestoreTx(tx, &endpoint); err != nil {
				return err
			}
			removedTags = append(removedTags, endpoint.Tag)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(removedTags) > 0 {
		logger.Info("removed conflicted endpoints during restore: ", strings.Join(removedTags, ","))
	}
	return nil
}

type restorePortRange struct {
	start int
	end   int
}

func collectInboundPortRangesForRestore(inbound *model.Inbound) ([]restorePortRange, bool, error) {
	full, err := inbound.MarshalFull()
	if err != nil {
		return nil, false, err
	}

	rawPort, ok := (*full)["listen_port"]
	if !ok || rawPort == nil {
		return nil, false, nil
	}

	listenPort, err := normalizeInboundPort(rawPort)
	if err != nil {
		return nil, false, err
	}

	ranges := []restorePortRange{{start: listenPort, end: listenPort}}
	if inbound.Type == "hysteria2" {
		extraRanges, err := parseRestoreHy2ServerPortRanges(inbound.OutJson)
		if err != nil {
			return nil, false, err
		}
		ranges = normalizeRestorePortRanges(append(ranges, extraRanges...))
	}
	if inbound.Type == "mieru" {
		if listenPort < 1025 {
			return nil, false, fmt.Errorf("invalid Mieru listen port %d: expected 1025-65535", listenPort)
		}
		rawPortRange := strings.TrimSpace(fmt.Sprint((*full)["port_range"]))
		if rawPortRange != "" && rawPortRange != "<nil>" {
			start, end, err := parseRestorePortRange(rawPortRange)
			if err != nil {
				return nil, false, err
			}
			if start < 1025 || listenPort != start {
				return nil, false, fmt.Errorf("invalid Mieru port range %q", rawPortRange)
			}
			if end-start+1 > 512 {
				return nil, false, fmt.Errorf("mieru port range is too large: maximum 512 ports")
			}
			ranges = []restorePortRange{{start: start, end: end}}
		}
	}

	return normalizeRestorePortRanges(ranges), true, nil
}

func collectEndpointPortRangesForRestore(endpoint *model.Endpoint) ([]restorePortRange, bool, error) {
	full, err := endpoint.MarshalJSON()
	if err != nil {
		return nil, false, err
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(full, &payload); err != nil {
		return nil, false, err
	}
	portKey := model.EndpointPortKey(endpoint.Type)
	rawPort, ok := payload[portKey]
	if !ok || rawPort == nil {
		return nil, false, nil
	}

	listenPort, err := normalizeRestorePort(rawPort)
	if err != nil {
		return nil, false, err
	}
	return []restorePortRange{{start: listenPort, end: listenPort}}, true, nil
}

func normalizeInboundPort(raw interface{}) (int, error) {
	switch v := raw.(type) {
	case float64:
		if v < 1 || v > 65535 {
			return 0, fmt.Errorf("invalid listen_port: %v", v)
		}
		return int(v), nil
	case json.Number:
		port, err := v.Int64()
		if err != nil {
			return 0, err
		}
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("invalid listen_port: %d", port)
		}
		return int(port), nil
	default:
		port, err := strconv.Atoi(fmt.Sprint(raw))
		if err != nil {
			return 0, err
		}
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("invalid listen_port: %d", port)
		}
		return port, nil
	}
}

func normalizeRestorePort(raw interface{}) (int, error) {
	switch v := raw.(type) {
	case float64:
		if v < 1 || v > 65535 {
			return 0, fmt.Errorf("invalid listen_port: %v", v)
		}
		return int(v), nil
	case json.Number:
		port, err := v.Int64()
		if err != nil {
			return 0, err
		}
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("invalid listen_port: %d", port)
		}
		return int(port), nil
	default:
		port, err := strconv.Atoi(fmt.Sprint(raw))
		if err != nil {
			return 0, err
		}
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("invalid listen_port: %d", port)
		}
		return port, nil
	}
}

func parseRestoreHy2ServerPortRanges(outJson json.RawMessage) ([]restorePortRange, error) {
	if len(outJson) == 0 {
		return nil, nil
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(outJson, &payload); err != nil {
		return nil, err
	}

	rawPorts, ok := payload["server_ports"]
	if !ok || rawPorts == nil {
		return nil, nil
	}

	ranges := make([]restorePortRange, 0)
	appendToken := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if strings.Count(raw, "-") == 1 {
			start, end, err := parseRestorePortRange(raw)
			if err != nil {
				return
			}
			ranges = append(ranges, restorePortRange{start: start, end: end})
			return
		}
		port, err := strconv.Atoi(raw)
		if err != nil {
			return
		}
		if port >= 1 && port <= 65535 {
			ranges = append(ranges, restorePortRange{start: port, end: port})
		}
	}

	switch typed := rawPorts.(type) {
	case []interface{}:
		for _, item := range typed {
			if item == nil {
				continue
			}
			appendToken(fmt.Sprint(item))
		}
	case []string:
		for _, item := range typed {
			appendToken(item)
		}
	case string:
		for _, item := range strings.Split(typed, ",") {
			appendToken(item)
		}
	default:
		return nil, fmt.Errorf("unsupported server_ports format")
	}

	return normalizeRestorePortRanges(ranges), nil
}

func parseRestorePortRange(raw string) (int, int, error) {
	parts := strings.SplitN(raw, "-", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid server_ports token: %s", raw)
	}
	start, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid server_ports token: %s", raw)
	}
	end, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return 0, 0, fmt.Errorf("invalid server_ports token: %s", raw)
	}
	if start < 1 || start > 65535 || end < 1 || end > 65535 {
		return 0, 0, fmt.Errorf("invalid server_ports token: %s", raw)
	}
	if start > end {
		return 0, 0, fmt.Errorf("invalid server_ports range: %s", raw)
	}
	return start, end, nil
}

func normalizeRestorePortRanges(ranges []restorePortRange) []restorePortRange {
	normalized := make([]restorePortRange, 0, len(ranges))
	for _, item := range ranges {
		if item.start < 1 || item.end > 65535 || item.start > item.end {
			continue
		}
		normalized = append(normalized, item)
	}
	sort.Slice(normalized, func(i, j int) bool {
		if normalized[i].start != normalized[j].start {
			return normalized[i].start < normalized[j].start
		}
		return normalized[i].end < normalized[j].end
	})
	if len(normalized) == 0 {
		return nil
	}
	merged := normalized[:1]
	for _, item := range normalized[1:] {
		last := &merged[len(merged)-1]
		if item.start <= last.end+1 {
			if item.end > last.end {
				last.end = item.end
			}
			continue
		}
		merged = append(merged, item)
	}
	return merged
}

func hasPortRangeConflict(ranges []restorePortRange, sshSet map[int]struct{}) bool {
	for port := range sshSet {
		for _, item := range ranges {
			if port >= item.start && port <= item.end {
				return true
			}
		}
	}
	return false
}

func removeInboundFromRestoreTx(tx *gorm.DB, inbound *model.Inbound) error {
	var clientIds []uint
	if err := tx.Raw("SELECT clients.id FROM clients, json_each(clients.inbounds) AS je WHERE je.value = ?", inbound.Id).Scan(&clientIds).Error; err != nil {
		return err
	}

	if len(clientIds) > 0 {
		var clients []model.Client
		if err := tx.Model(model.Client{}).Where("id IN ?", clientIds).Find(&clients).Error; err != nil {
			return err
		}
		for _, client := range clients {
			if err := pruneInboundFromClient(&client, inbound.Id, inbound.Tag); err != nil {
				return err
			}
			if err := tx.Save(&client).Error; err != nil {
				return err
			}
		}
	}

	if err := tx.Where("tag = ?", inbound.Tag).Delete(model.Inbound{}).Error; err != nil {
		return err
	}
	return tx.Where("scope = ? AND owner_id = ?", "inbound", inbound.Id).Delete(&model.ManagedPortEntry{}).Error
}

func pruneInboundFromClient(client *model.Client, inboundID uint, inboundTag string) error {
	var clientInbounds []uint
	if len(client.Inbounds) > 0 {
		if err := json.Unmarshal(client.Inbounds, &clientInbounds); err != nil {
			return err
		}
	}
	newClientInbounds := make([]uint, 0, len(clientInbounds))
	for _, id := range clientInbounds {
		if id != inboundID {
			newClientInbounds = append(newClientInbounds, id)
		}
	}
	client.Inbounds, _ = json.MarshalIndent(newClientInbounds, "", "  ")

	var clientLinks []map[string]string
	if len(client.Links) > 0 {
		if err := json.Unmarshal(client.Links, &clientLinks); err != nil {
			return err
		}
	}
	newClientLinks := make([]map[string]string, 0, len(clientLinks))
	for _, link := range clientLinks {
		if link["remark"] != inboundTag {
			newClientLinks = append(newClientLinks, link)
		}
	}
	client.Links, _ = json.MarshalIndent(newClientLinks, "", "  ")
	return nil
}

func detectSSHListenPortsForRestore() ([]int, error) {
	if ports, err := detectSSHPortsFromSSHD(); err == nil && len(ports) > 0 {
		return ports, nil
	}
	if ports, err := detectSSHPortsFromSS(); err == nil && len(ports) > 0 {
		return ports, nil
	}
	if ports, err := detectSSHPortsFromConfigFile(); err == nil && len(ports) > 0 {
		return ports, nil
	}
	return nil, fmt.Errorf("no ssh port detected")
}

func removeEndpointFromRestoreTx(tx *gorm.DB, endpoint *model.Endpoint) error {
	if err := tx.Where("tag = ?", endpoint.Tag).Delete(model.Endpoint{}).Error; err != nil {
		return err
	}
	return tx.Where("scope = ? AND owner_id = ?", "endpoint", endpoint.Id).Delete(&model.ManagedPortEntry{}).Error
}

func detectSSHPortsFromSSHD() ([]int, error) {
	bin, err := exec.LookPath("sshd")
	if err != nil {
		return nil, err
	}
	output, err := runRestoreCommand(6*time.Second, bin, "-T")
	if err != nil {
		return nil, err
	}
	return parseSSHPortLines(string(output)), nil
}

func detectSSHPortsFromSS() ([]int, error) {
	bin, err := exec.LookPath("ss")
	if err != nil {
		return nil, err
	}
	output, err := runRestoreCommand(6*time.Second, bin, "-H", "-ltnp")
	if err != nil {
		return nil, err
	}
	return parseSSHListenPorts(string(output)), nil
}

func runRestoreCommand(timeout time.Duration, name string, args ...string) ([]byte, error) {
	if timeout <= 0 {
		timeout = 6 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return output, nil
	}

	trimmed := strings.TrimSpace(string(output))
	command := strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))
	reason := "failed"
	if ctx.Err() == context.DeadlineExceeded {
		reason = "timed out"
	}
	return output, fmt.Errorf("%s: %s: %w: %s", command, reason, err, trimmed)
}

func detectSSHPortsFromConfigFile() ([]int, error) {
	candidates := []string{"/etc/ssh/sshd_config", "/usr/local/etc/ssh/sshd_config"}
	ports := make([]int, 0)
	seen := map[int]struct{}{}
	for _, file := range candidates {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, port := range parseSSHPortLines(string(data)) {
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

func parseSSHPortLines(raw string) []int {
	matches := sshPortLineRe.FindAllStringSubmatch(raw, -1)
	ports := make([]int, 0)
	seen := map[int]struct{}{}
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		port, err := strconv.Atoi(match[1])
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

func parseSSHListenPorts(raw string) []int {
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
