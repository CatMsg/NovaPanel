package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/CatMsg/NovaPanel/logger"
)

const (
	managedWebPortForwardTag = "panel-web-port"
	managedSubPortForwardTag = "panel-sub-port"
)

var portForwardingMu sync.Mutex
var managedPanelApplyProtocols = []string{"tcp"}
var managedPanelCleanupProtocols = []string{"tcp", "udp"}
var managedForwardProtocols = []string{"tcp", "udp"}

func ValidateManagedPanelPorts(webPort, subPort int) error {
	if webPort < 1 || webPort > 65535 {
		return fmt.Errorf("invalid panel port: %d", webPort)
	}
	if subPort < 1 || subPort > 65535 {
		return fmt.Errorf("invalid sub port: %d", subPort)
	}
	if webPort == subPort {
		return fmt.Errorf("panel port and sub port cannot be the same: %d", webPort)
	}
	return validateInboundPortsAgainstSSH(nil, []int{webPort, subPort})
}

func syncManagedPortForwarding(tag string, oldPort, newPort int) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	if newPort < 1 || newPort > 65535 {
		return fmt.Errorf("invalid port: %d", newPort)
	}
	if err := validateInboundPortsAgainstSSH(nil, []int{newPort}); err != nil {
		return err
	}
	if oldPort == newPort {
		return nil
	}

	if err := runPortForwardScript("apply", tag, newPort, []int{newPort}, managedPanelApplyProtocols); err != nil {
		if oldPort > 0 {
			rollbackErr := runPortForwardScript("apply", tag, oldPort, []int{oldPort}, managedPanelApplyProtocols)
			if rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("rollback managed port forwarding for %s failed: %w", tag, rollbackErr))
			}
		} else {
			rollbackErr := runPortForwardScript("remove", tag, newPort, []int{newPort}, managedPanelCleanupProtocols)
			if rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("cleanup managed port forwarding for %s failed: %w", tag, rollbackErr))
			}
		}
		return err
	}

	return nil
}

func runPortForwardScript(action string, tag string, listenPort int, ports []int, protocols []string) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	if action != "purge" && tag == "" {
		return nil
	}

	portForwardingMu.Lock()
	defer portForwardingMu.Unlock()

	args := []string{action}
	if action != "purge" {
		args = append(args, tag, strconv.Itoa(listenPort), joinPorts(ports))
		args = append(args, strings.Join(protocols, ","))
	}
	cmd := exec.Command("bash", append([]string{hy2ForwardScript}, args...)...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.TrimSpace(string(output))
		if trimmed != "" {
			err = fmt.Errorf("%w: %s", err, trimmed)
		}
		logger.Warning("port forwarding sync failed: ", err)
		return err
	}

	return nil
}

func (s *SettingService) SyncManagedPortForwarding(tag string, oldPort, newPort int) error {
	return syncManagedPortForwarding(tag, oldPort, newPort)
}

func syncManagedPanelPortForwarding(oldWebPort, newWebPort, oldSubPort, newSubPort int) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	if oldWebPort == newWebPort && oldSubPort == newSubPort {
		return nil
	}
	if err := ValidateManagedPanelPorts(newWebPort, newSubPort); err != nil {
		return err
	}
	if _, err := ensureFirewallBackend(); err != nil {
		return err
	}

	changedWeb := oldWebPort != newWebPort
	changedSub := oldSubPort != newSubPort

	if changedWeb {
		if err := syncManagedPortForwarding(managedWebPortForwardTag, oldWebPort, newWebPort); err != nil {
			return err
		}
	}

	if changedSub {
		if err := syncManagedPortForwarding(managedSubPortForwardTag, oldSubPort, newSubPort); err != nil {
			if changedWeb {
				var rollbackErr error
				if oldWebPort > 0 {
					rollbackErr = runPortForwardScript("apply", managedWebPortForwardTag, oldWebPort, []int{oldWebPort}, managedPanelApplyProtocols)
				} else {
					rollbackErr = runPortForwardScript("remove", managedWebPortForwardTag, newWebPort, []int{newWebPort}, managedPanelCleanupProtocols)
				}
				if rollbackErr != nil {
					return errors.Join(err, fmt.Errorf("rollback managed web port forwarding failed: %w", rollbackErr))
				}
			}
			return err
		}
	}

	return nil
}

func (s *SettingService) SyncManagedPanelPortForwarding(oldWebPort, newWebPort, oldSubPort, newSubPort int) error {
	return syncManagedPanelPortForwarding(oldWebPort, newWebPort, oldSubPort, newSubPort)
}

func (s *SettingService) RebuildManagedPortForwarding() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	backend, err := ensureFirewallBackend()
	if err != nil {
		return err
	}
	logger.Info("rebuilding managed port forwarding with backend: ", backend)

	return s.rebuildManagedPanelPortForwardingFromCurrentState()
}

func (s *SettingService) rebuildManagedPanelPortForwardingFromCurrentState() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	webPort, err := s.GetPort()
	if err != nil {
		return err
	}
	subPort, err := s.GetSubPort()
	if err != nil {
		return err
	}

	if err := ValidateManagedPanelPorts(webPort, subPort); err != nil {
		return err
	}

	var errs []error
	if err := s.SyncManagedPanelPortForwarding(0, webPort, 0, subPort); err != nil {
		errs = append(errs, fmt.Errorf("rebuild managed panel port forwarding: %w", err))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func collectEndpointForwardPorts(endpoint *model.Endpoint) (int, []int, []string, bool, error) {
	if endpoint == nil {
		return 0, nil, nil, false, nil
	}

	full, err := endpoint.MarshalJSON()
	if err != nil {
		return 0, nil, nil, false, err
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(full, &payload); err != nil {
		return 0, nil, nil, false, err
	}

	portKey := "listen_port"
	protocols := managedForwardProtocols
	if strings.EqualFold(endpoint.Type, "tailscale") {
		portKey = "relay_server_port"
	} else if strings.EqualFold(endpoint.Type, "masque") {
		portKey = "port"
		protocols = []string{"udp"}
	}

	rawPort, ok := payload[portKey]
	if !ok || rawPort == nil {
		return 0, nil, nil, false, nil
	}

	listenPort, err := normalizeManagedPort(rawPort)
	if err != nil {
		return 0, nil, nil, false, fmt.Errorf("invalid %s for endpoint %s: %w", portKey, endpoint.Tag, err)
	}

	return listenPort, []int{listenPort}, protocols, true, nil
}

func normalizeManagedPort(raw interface{}) (int, error) {
	switch v := raw.(type) {
	case float64:
		if v < 1 || v > 65535 {
			return 0, fmt.Errorf("invalid port: %v", v)
		}
		return int(v), nil
	case json.Number:
		port, err := v.Int64()
		if err != nil {
			return 0, err
		}
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("invalid port: %d", port)
		}
		return int(port), nil
	default:
		port, err := strconv.Atoi(fmt.Sprint(raw))
		if err != nil {
			return 0, err
		}
		if port < 1 || port > 65535 {
			return 0, fmt.Errorf("invalid port: %d", port)
		}
		return port, nil
	}
}

func syncManagedEndpointPortForwarding(oldEndpoint, newEndpoint *model.Endpoint) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	var oldTag string
	var oldListenPort int
	var oldPorts []int
	var oldProtocols []string
	var oldActive bool
	var err error
	if oldEndpoint != nil {
		oldTag = oldEndpoint.Tag
		oldListenPort, oldPorts, oldProtocols, oldActive, err = collectEndpointForwardPorts(oldEndpoint)
		if err != nil {
			return err
		}
	}

	var newTag string
	var newListenPort int
	var newPorts []int
	var newProtocols []string
	var newActive bool
	if newEndpoint != nil {
		newTag = newEndpoint.Tag
		newListenPort, newPorts, newProtocols, newActive, err = collectEndpointForwardPorts(newEndpoint)
		if err != nil {
			return err
		}
		if newActive {
			if err := validateInboundPortsAgainstSSH(nil, newPorts); err != nil {
				return err
			}
		}
	}

	if oldActive && newActive && oldTag == newTag && oldListenPort == newListenPort {
		return nil
	}

	if oldActive {
		if err := runPortForwardScript("remove", oldTag, oldListenPort, oldPorts, oldProtocols); err != nil {
			return err
		}
	}

	if !newActive {
		return nil
	}

	if err := runPortForwardScript("apply", newTag, newListenPort, newPorts, newProtocols); err != nil {
		if oldActive {
			rollbackErr := runPortForwardScript("apply", oldTag, oldListenPort, oldPorts, oldProtocols)
			if rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("rollback managed endpoint port forwarding for %s failed: %w", newTag, rollbackErr))
			}
		} else {
			rollbackErr := runPortForwardScript("remove", newTag, newListenPort, newPorts, newProtocols)
			if rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("cleanup managed endpoint port forwarding for %s failed: %w", newTag, rollbackErr))
			}
		}
		return err
	}

	return nil
}

func (s *EndpointService) SyncManagedEndpointPortForwarding(oldEndpoint, newEndpoint *model.Endpoint) error {
	return syncManagedEndpointPortForwarding(oldEndpoint, newEndpoint)
}

func (s *EndpointService) RebuildEndpointPortForwarding() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	backend, err := ensureFirewallBackend()
	if err != nil {
		return err
	}
	logger.Info("rebuilding endpoint port forwarding with backend: ", backend)

	return s.rebuildEndpointPortForwardingFromCurrentState()
}

func (s *EndpointService) rebuildEndpointPortForwardingFromCurrentState() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	var endpoints []*model.Endpoint
	if err := database.GetDB().Model(model.Endpoint{}).Find(&endpoints).Error; err != nil {
		return err
	}

	var errs []error
	for _, endpoint := range endpoints {
		if err := syncManagedEndpointPortForwarding(nil, endpoint); err != nil {
			wrapped := fmt.Errorf("rebuild %s: %w", endpoint.Tag, err)
			errs = append(errs, wrapped)
			logger.Warning("endpoint port forwarding rebuild failed: ", wrapped)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (s *SettingService) RebuildAllManagedPortForwarding(inboundService *InboundService, endpointService *EndpointService) error {
	if runtime.GOOS != "linux" {
		return nil
	}

	backend, err := ensureFirewallBackend()
	if err != nil {
		return err
	}
	logger.Info("rebuilding all managed port forwarding with backend: ", backend)

	if err := runPortForwardScript("purge", "", 0, nil, nil); err != nil {
		return err
	}

	var errs []error
	if err := s.rebuildManagedPanelPortForwardingFromCurrentState(); err != nil {
		errs = append(errs, err)
	}
	if inboundService != nil {
		if err := inboundService.rebuildInboundPortForwardingFromCurrentState(); err != nil {
			errs = append(errs, err)
		}
	}
	if endpointService != nil {
		if err := endpointService.rebuildEndpointPortForwardingFromCurrentState(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
