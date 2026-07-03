package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/CatMsg/NovaPanel/logger"
	"gorm.io/gorm"
)

const (
	managedWebPortForwardTag = "panel-web-port"
	managedSubPortForwardTag = "panel-sub-port"
)

var portForwardingMu sync.Mutex
var managedPanelApplyProtocols = []string{"tcp"}
var managedPanelCleanupProtocols = []string{"tcp", "udp"}
var managedForwardProtocols = []string{"tcp", "udp"}

type managedForwardSpec struct {
	tag             string
	listenPort      int
	ports           []int
	protocols       []string
	removeProtocols []string
	active          bool
}

func (s managedForwardSpec) normalized() managedForwardSpec {
	s.ports = normalizeManagedPorts(s.ports)
	s.protocols = normalizeManagedProtocols(s.protocols)
	s.removeProtocols = normalizeManagedProtocols(s.removeProtocols)
	if len(s.removeProtocols) == 0 {
		s.removeProtocols = append([]string(nil), s.protocols...)
	}
	if s.listenPort < 1 || s.listenPort > 65535 || len(s.ports) == 0 || len(s.protocols) == 0 {
		s.active = false
	}
	return s
}

func managedForwardSpecsEqual(a, b managedForwardSpec) bool {
	a = a.normalized()
	b = b.normalized()
	if a.active != b.active || a.tag != b.tag || a.listenPort != b.listenPort {
		return false
	}
	if len(a.ports) != len(b.ports) || len(a.protocols) != len(b.protocols) {
		return false
	}
	for i := range a.ports {
		if a.ports[i] != b.ports[i] {
			return false
		}
	}
	for i := range a.protocols {
		if a.protocols[i] != b.protocols[i] {
			return false
		}
	}
	return true
}

func normalizeManagedProtocols(protocols []string) []string {
	if len(protocols) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(protocols))
	normalized := make([]string, 0, len(protocols))
	for _, protocol := range protocols {
		protocol = strings.ToLower(strings.TrimSpace(protocol))
		if protocol == "" {
			continue
		}
		switch protocol {
		case "tcp", "udp":
		default:
			continue
		}
		if _, ok := seen[protocol]; ok {
			continue
		}
		seen[protocol] = struct{}{}
		normalized = append(normalized, protocol)
	}
	return normalized
}

func validateManagedForwardSpecPorts(spec managedForwardSpec) error {
	spec = spec.normalized()
	if !spec.active {
		return nil
	}
	return validateInboundPortsAgainstSSH(nil, spec.ports)
}

func applyManagedForwardSpec(spec managedForwardSpec) error {
	spec = spec.normalized()
	if !spec.active {
		return nil
	}
	return runPortForwardScript("apply", spec.tag, spec.listenPort, spec.ports, spec.protocols)
}

func removeManagedForwardSpec(spec managedForwardSpec) error {
	spec = spec.normalized()
	if !spec.active {
		return nil
	}
	return runPortForwardScript("remove", spec.tag, spec.listenPort, spec.ports, spec.removeProtocols)
}

func syncManagedForwardSpecs(oldSpec, newSpec managedForwardSpec) error {
	oldSpec = oldSpec.normalized()
	newSpec = newSpec.normalized()

	if err := validateManagedForwardSpecPorts(newSpec); err != nil {
		return err
	}
	if managedForwardSpecsEqual(oldSpec, newSpec) {
		return nil
	}

	if oldSpec.active {
		if err := removeManagedForwardSpec(oldSpec); err != nil {
			return err
		}
	}

	if !newSpec.active {
		return nil
	}

	if err := applyManagedForwardSpec(newSpec); err != nil {
		if oldSpec.active {
			rollbackErr := applyManagedForwardSpec(oldSpec)
			if rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("rollback managed port forwarding for %s failed: %w", newSpec.tag, rollbackErr))
			}
		} else {
			rollbackErr := removeManagedForwardSpec(newSpec)
			if rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("cleanup managed port forwarding for %s failed: %w", newSpec.tag, rollbackErr))
			}
		}
		return err
	}

	return nil
}

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

func ValidateManagedPanelPortsWithConflicts(tx *gorm.DB, webPort, subPort int) error {
	if err := ValidateManagedPanelPorts(webPort, subPort); err != nil {
		return err
	}
	if tx == nil {
		return nil
	}
	return validateManagedPanelPortConflicts(tx, webPort, subPort)
}

func syncManagedPortForwarding(tag string, oldPort, newPort int) error {
	if runtime.GOOS != "linux" {
		return nil
	}
	if newPort < 1 || newPort > 65535 {
		return fmt.Errorf("invalid port: %d", newPort)
	}
	oldSpec := managedForwardSpec{
		tag:             tag,
		listenPort:      oldPort,
		ports:           []int{oldPort},
		protocols:       managedPanelApplyProtocols,
		removeProtocols: managedPanelCleanupProtocols,
		active:          oldPort > 0,
	}
	newSpec := managedForwardSpec{
		tag:             tag,
		listenPort:      newPort,
		ports:           []int{newPort},
		protocols:       managedPanelApplyProtocols,
		removeProtocols: managedPanelCleanupProtocols,
		active:          newPort > 0,
	}
	return syncManagedForwardSpecs(oldSpec, newSpec)
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
	_, err := runCommandOutput(externalCommandTimeout, "bash", append([]string{hy2ForwardScript}, args...)...)
	if err != nil {
		wrapped := formatExternalCommandError("port forwarding sync failed", err)
		logger.Warning(wrapped)
		return wrapped
	}
	serverStatusCache.mu.Lock()
	delete(serverStatusCache.entries, "ports")
	delete(serverStatusCache.entries, "sys")
	serverStatusCache.mu.Unlock()

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

	portKey := model.EndpointPortKey(endpoint.Type)
	protocols := managedForwardProtocols
	if strings.EqualFold(endpoint.Type, "masque") {
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

func collectEndpointForwardSpec(endpoint *model.Endpoint) (managedForwardSpec, error) {
	if endpoint == nil {
		return managedForwardSpec{}, nil
	}

	listenPort, ports, protocols, active, err := collectEndpointForwardPorts(endpoint)
	if err != nil {
		return managedForwardSpec{}, err
	}
	return managedForwardSpec{
		tag:             endpoint.Tag,
		listenPort:      listenPort,
		ports:           ports,
		protocols:       protocols,
		removeProtocols: protocols,
		active:          active,
	}.normalized(), nil
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

	var oldSpec managedForwardSpec
	var err error
	if oldEndpoint != nil {
		oldSpec, err = collectEndpointForwardSpec(oldEndpoint)
		if err != nil {
			return err
		}
	}

	var newSpec managedForwardSpec
	if newEndpoint != nil {
		newSpec, err = collectEndpointForwardSpec(newEndpoint)
		if err != nil {
			return err
		}
	}

	return syncManagedForwardSpecs(oldSpec, newSpec)
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
