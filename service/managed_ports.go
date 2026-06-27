package service

import (
	"errors"
	"fmt"
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

func runPortForwardScript(action string, tag string, listenPort int, ports []int) error {
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

	if err := runPortForwardScript("apply", tag, newPort, []int{newPort}); err != nil {
		if oldPort > 0 {
			rollbackErr := runPortForwardScript("apply", tag, oldPort, []int{oldPort})
			if rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("rollback managed port forwarding for %s failed: %w", tag, rollbackErr))
			}
		} else {
			rollbackErr := runPortForwardScript("remove", tag, newPort, []int{newPort})
			if rollbackErr != nil {
				return errors.Join(err, fmt.Errorf("cleanup managed port forwarding for %s failed: %w", tag, rollbackErr))
			}
		}
		return err
	}

	return nil
}

func (s *SettingService) SyncManagedPanelPortForwarding(oldWebPort, newWebPort, oldSubPort, newSubPort int) error {
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
		if err := s.SyncManagedPortForwarding(managedWebPortForwardTag, oldWebPort, newWebPort); err != nil {
			return err
		}
	}

	if changedSub {
		if err := s.SyncManagedPortForwarding(managedSubPortForwardTag, oldSubPort, newSubPort); err != nil {
			if changedWeb {
				var rollbackErr error
				if oldWebPort > 0 {
					rollbackErr = runPortForwardScript("apply", managedWebPortForwardTag, oldWebPort, []int{oldWebPort})
				} else {
					rollbackErr = runPortForwardScript("remove", managedWebPortForwardTag, newWebPort, []int{newWebPort})
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

func (s *SettingService) RebuildManagedPortForwarding() error {
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

	backend, err := ensureFirewallBackend()
	if err != nil {
		return err
	}
	logger.Info("rebuilding managed port forwarding with backend: ", backend)

	var errs []error
	if err := s.SyncManagedPanelPortForwarding(0, webPort, 0, subPort); err != nil {
		errs = append(errs, fmt.Errorf("rebuild managed panel port forwarding: %w", err))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}
