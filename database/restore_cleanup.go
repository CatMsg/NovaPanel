package database

import (
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
			ports, ok, err := collectInboundPortsForRestore(&inbound)
			if err != nil {
				logger.Warning("skip inbound restore conflict check failed: ", err)
				continue
			}
			if !ok || !hasPortConflict(ports, sshSet) {
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
			ports, ok, err := collectEndpointPortsForRestore(&endpoint)
			if err != nil {
				logger.Warning("skip endpoint restore conflict check failed: ", err)
				continue
			}
			if !ok || !hasPortConflict(ports, sshSet) {
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

func collectInboundPortsForRestore(inbound *model.Inbound) ([]int, bool, error) {
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

	ports := []int{listenPort}
	if inbound.Type == "hysteria2" {
		extraPorts, err := parseRestoreHy2ServerPorts(inbound.OutJson)
		if err != nil {
			return nil, false, err
		}
		ports = mergeRestorePorts(listenPort, extraPorts)
	}

	return ports, true, nil
}

func collectEndpointPortsForRestore(endpoint *model.Endpoint) ([]int, bool, error) {
	full, err := endpoint.MarshalJSON()
	if err != nil {
		return nil, false, err
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(full, &payload); err != nil {
		return nil, false, err
	}

	rawPort, ok := payload["listen_port"]
	if !ok || rawPort == nil {
		return nil, false, nil
	}

	listenPort, err := normalizeRestorePort(rawPort)
	if err != nil {
		return nil, false, err
	}

	return []int{listenPort}, true, nil
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

func parseRestoreHy2ServerPorts(outJson json.RawMessage) ([]int, error) {
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

	ports := make([]int, 0)
	seen := map[int]struct{}{}
	appendPort := func(port int) {
		if port < 1 || port > 65535 {
			return
		}
		if _, exists := seen[port]; exists {
			return
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	appendToken := func(raw string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil
		}
		if strings.Count(raw, "-") == 1 {
			start, end, err := parseRestorePortRange(raw)
			if err != nil {
				return err
			}
			for port := start; port <= end; port++ {
				appendPort(port)
			}
			return nil
		}
		port, err := strconv.Atoi(raw)
		if err != nil {
			return fmt.Errorf("invalid server_ports token: %s", raw)
		}
		appendPort(port)
		return nil
	}

	switch typed := rawPorts.(type) {
	case []interface{}:
		for _, item := range typed {
			if item == nil {
				continue
			}
			if err := appendToken(fmt.Sprint(item)); err != nil {
				return nil, err
			}
		}
	case []string:
		for _, item := range typed {
			if err := appendToken(item); err != nil {
				return nil, err
			}
		}
	case string:
		for _, item := range strings.Split(typed, ",") {
			if err := appendToken(item); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unsupported server_ports format")
	}

	return ports, nil
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

func mergeRestorePorts(listenPort int, ports []int) []int {
	merged := make([]int, 0, len(ports)+1)
	seen := map[int]struct{}{}
	appendPort := func(port int) {
		if port < 1 || port > 65535 {
			return
		}
		if _, exists := seen[port]; exists {
			return
		}
		seen[port] = struct{}{}
		merged = append(merged, port)
	}
	appendPort(listenPort)
	for _, port := range ports {
		appendPort(port)
	}
	return merged
}

func hasPortConflict(ports []int, sshSet map[int]struct{}) bool {
	for _, port := range ports {
		if _, ok := sshSet[port]; ok {
			return true
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

	return tx.Where("tag = ?", inbound.Tag).Delete(model.Inbound{}).Error
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
	return tx.Where("tag = ?", endpoint.Tag).Delete(model.Endpoint{}).Error
}

func detectSSHPortsFromSSHD() ([]int, error) {
	bin, err := exec.LookPath("sshd")
	if err != nil {
		return nil, err
	}
	output, err := exec.Command(bin, "-T").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseSSHPortLines(string(output)), nil
}

func detectSSHPortsFromSS() ([]int, error) {
	bin, err := exec.LookPath("ss")
	if err != nil {
		return nil, err
	}
	output, err := exec.Command(bin, "-H", "-ltnp").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return parseSSHListenPorts(string(output)), nil
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
