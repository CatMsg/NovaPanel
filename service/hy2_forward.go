package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
)

const hy2ForwardScript = "scripts/hy2-forward.sh"

func (s *InboundService) RebuildInboundPortForwarding() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	backend, err := ensureFirewallBackend()
	if err != nil {
		return err
	}
	logger.Info("rebuilding inbound port forwarding with backend: ", backend)

	if err := runInboundForwardScript("purge", "", 0, nil); err != nil {
		return err
	}

	var inbounds []*model.Inbound
	if err := database.GetDB().Model(model.Inbound{}).Find(&inbounds).Error; err != nil {
		return err
	}

	var errs []error
	for _, inbound := range inbounds {
		if err := s.syncInboundPortForwarding(nil, inbound); err != nil {
			var conflictErr *sshPortConflictError
			if errors.As(err, &conflictErr) {
				logger.Warning("inbound port forwarding conflict detected, removing inbound: ", inbound.Tag, " ports: ", conflictErr.ports)
				if removeErr := s.removeInboundByTag(inbound.Tag); removeErr != nil {
					wrapped := fmt.Errorf("remove conflicted inbound %s: %w", inbound.Tag, removeErr)
					errs = append(errs, wrapped)
					logger.Warning("remove conflicted inbound failed: ", wrapped)
				}
				continue
			}
			wrapped := fmt.Errorf("rebuild %s: %w", inbound.Tag, err)
			errs = append(errs, wrapped)
			logger.Warning("inbound port forwarding rebuild failed: ", wrapped)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (s *InboundService) RebuildHy2PortForwarding() error {
	return s.RebuildInboundPortForwarding()
}

func (s *InboundService) syncInboundPortForwarding(oldInbound *model.Inbound, inbound *model.Inbound) error {
	var listenPort int
	var ports []int
	if inbound != nil {
		var err error
		listenPort, ports, err = collectInboundForwardPorts(inbound)
		if err != nil {
			return err
		}

		if err := validateInboundPortsAgainstSSH(inbound, ports); err != nil {
			return err
		}
	}

	if oldInbound != nil {
		oldListenPort, oldPorts, err := collectInboundForwardPorts(oldInbound)
		if err != nil {
			return err
		}
		if err := runInboundForwardScript("remove", oldInbound.Tag, oldListenPort, oldPorts); err != nil {
			return err
		}
	}

	if inbound == nil {
		return nil
	}

	if err := runInboundForwardScript("apply", inbound.Tag, listenPort, ports); err != nil {
		return err
	}

	return nil
}

func (s *InboundService) syncHy2PortForwarding(oldInbound *model.Inbound, inbound *model.Inbound) error {
	return s.syncInboundPortForwarding(oldInbound, inbound)
}

func collectInboundForwardPorts(inbound *model.Inbound) (int, []int, error) {
	listenPort, err := getInboundListenPort(inbound)
	if err != nil {
		return 0, nil, err
	}

	ports := []int{listenPort}
	if inbound.Type == "hysteria2" {
		extraPorts, err := getHy2ServerPorts(inbound.OutJson)
		if err != nil {
			return 0, nil, err
		}
		ports = mergeInboundForwardPorts(listenPort, extraPorts)
	}

	return listenPort, ports, nil
}

func runInboundForwardScript(action string, tag string, listenPort int, ports []int) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	if action != "purge" && tag == "" {
		return nil
	}

	args := []string{action}
	if action != "purge" {
		args = append(args, tag, strconv.Itoa(listenPort), joinPorts(ports))
	}
	cmd := exec.Command("bash", append([]string{hy2ForwardScript}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			err = fmt.Errorf("%w: %s", err, trimmed)
		}
		logger.Warning("inbound port forwarding sync failed: ", err)
		return err
	}

	return nil
}

func getInboundListenPort(inbound *model.Inbound) (int, error) {
	full, err := inbound.MarshalFull()
	if err != nil {
		return 0, err
	}

	rawPort, ok := (*full)["listen_port"]
	if !ok || rawPort == nil {
		return 0, fmt.Errorf("missing listen_port for inbound %s", inbound.Tag)
	}

	switch v := rawPort.(type) {
	case float64:
		if v < 1 || v > 65535 {
			return 0, fmt.Errorf("invalid listen_port for inbound %s", inbound.Tag)
		}
		return int(v), nil
	case json.Number:
		port, err := v.Int64()
		if err != nil {
			return 0, err
		}
		return int(port), nil
	default:
		port, err := strconv.Atoi(fmt.Sprint(rawPort))
		if err != nil {
			return 0, err
		}
		return port, nil
	}
}

func getHy2ServerPorts(outJson json.RawMessage) ([]int, error) {
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
			start, end, err := parseHy2PortRange(raw)
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

func parseHy2PortRange(raw string) (int, int, error) {
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

func mergeInboundForwardPorts(listenPort int, ports []int) []int {
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

func mergeHy2ForwardPorts(listenPort int, ports []int) []int {
	return mergeInboundForwardPorts(listenPort, ports)
}

func joinPorts(ports []int) string {
	if len(ports) == 0 {
		return ""
	}

	values := make([]string, 0, len(ports))
	for _, port := range ports {
		values = append(values, strconv.Itoa(port))
	}
	return strings.Join(values, ",")
}
