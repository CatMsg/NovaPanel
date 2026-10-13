package service

import (
	"errors"
	"mime/multipart"
	"sync"
	"sync/atomic"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/util/common"
)

const maintenanceModeSetting = "maintenanceMode"

var (
	maintenanceModeEnabled   atomic.Bool
	maintenanceTransitioning atomic.Bool
	maintenanceSingleton     = NewMaintenanceService()
)

type MaintenanceStatus struct {
	Enabled       bool   `json:"enabled"`
	Transitioning bool   `json:"transitioning"`
	Error         string `json:"error,omitempty"`
}

type maintenanceHooks struct {
	stopFailover           func()
	stopProxyPlanes        func() error
	reconcileTrafficBudget func() error
	startProxyPlanes       func() error
	startAfterResume       func()
}

type MaintenanceService struct {
	SettingService
	transitionMu sync.Mutex
	statusMu     sync.RWMutex
	status       MaintenanceStatus
	hooks        maintenanceHooks
}

func NewMaintenanceService() *MaintenanceService {
	return newMaintenanceService(defaultMaintenanceHooks())
}

func newMaintenanceService(hooks maintenanceHooks) *MaintenanceService {
	return &MaintenanceService{
		status: MaintenanceStatus{Enabled: maintenanceModeEnabled.Load()},
		hooks:  hooks,
	}
}

func GetMaintenanceService() *MaintenanceService {
	return maintenanceSingleton
}

// IsMaintenanceMode is a cached process-wide gate; callers must not read the
// setting from the database on request or packet paths.
func IsMaintenanceMode() bool {
	return maintenanceModeEnabled.Load()
}

func (s *MaintenanceService) Initialize() error {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	enabled, err := s.SettingService.getBool(maintenanceModeSetting)
	if err != nil {
		// A corrupt or unreadable setting must never allow startup to resume the
		// proxy plane implicitly.
		maintenanceModeEnabled.Store(true)
		maintenanceTransitioning.Store(false)
		s.setStatus(true, false, err.Error())
		return err
	}
	maintenanceModeEnabled.Store(enabled)
	maintenanceTransitioning.Store(false)
	s.setStatus(enabled, false, "")
	return nil
}

func (s *MaintenanceService) Status() MaintenanceStatus {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return s.status
}

func (s *MaintenanceService) SetEnabled(enabled bool) error {
	// Config saves hold saveConfigMu through their post-commit runtime action.
	// Take it first so resume cannot start a config snapshot that races a save.
	saveConfigMu.Lock()
	defer saveConfigMu.Unlock()

	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	if enabled {
		// Repeating a pause request retries any stop that previously failed.
		return s.pauseLocked()
	}
	if !maintenanceModeEnabled.Load() {
		return nil
	}
	return s.resumeLocked()
}

// ImportDatabase serializes restore against configuration saves and lifecycle
// transitions, then carries the current pause into the replacement database.
func (s *MaintenanceService) ImportDatabase(file multipart.File) error {
	saveConfigMu.Lock()
	defer saveConfigMu.Unlock()

	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()

	return database.ImportDBWithMaintenance(file, IsMaintenanceMode())
}

func (s *MaintenanceService) pauseLocked() error {
	maintenanceTransitioning.Store(true)
	maintenanceModeEnabled.Store(true)
	s.setStatus(true, true, "")

	var errs []error
	if err := s.SettingService.setString(maintenanceModeSetting, "true"); err != nil {
		errs = append(errs, common.NewErrorf("persist maintenance mode: %v", err))
	}
	if s.hooks.stopFailover != nil {
		s.hooks.stopFailover()
	}
	if s.hooks.stopProxyPlanes != nil {
		if err := s.hooks.stopProxyPlanes(); err != nil {
			errs = append(errs, err)
		}
	}
	return s.finishTransition(true, errors.Join(errs...))
}

func (s *MaintenanceService) resumeLocked() error {
	maintenanceTransitioning.Store(true)
	s.setStatus(true, true, "")

	if s.hooks.reconcileTrafficBudget != nil {
		if err := s.hooks.reconcileTrafficBudget(); err != nil {
			return s.failResumeLocked(err)
		}
	}
	if IsTrafficBudgetBlocked() {
		return s.failResumeLocked(common.NewError("VPS 总流量预算仍处于硬保护状态，无法恢复代理数据面"))
	}
	if s.hooks.startProxyPlanes != nil {
		if err := s.hooks.startProxyPlanes(); err != nil {
			return s.failResumeLocked(err)
		}
	}
	if err := s.SettingService.setString(maintenanceModeSetting, "false"); err != nil {
		return s.failResumeLocked(common.NewErrorf("persist maintenance mode resume: %v", err))
	}

	maintenanceModeEnabled.Store(false)
	maintenanceTransitioning.Store(false)
	s.setStatus(false, false, "")
	if s.hooks.startAfterResume != nil {
		s.hooks.startAfterResume()
	}
	return nil
}

func (s *MaintenanceService) failResumeLocked(cause error) error {
	var errs []error
	errs = append(errs, cause)
	if s.hooks.stopFailover != nil {
		s.hooks.stopFailover()
	}
	if s.hooks.stopProxyPlanes != nil {
		if err := s.hooks.stopProxyPlanes(); err != nil {
			errs = append(errs, common.NewErrorf("stop partially resumed proxy data plane: %v", err))
		}
	}
	if err := s.SettingService.setString(maintenanceModeSetting, "true"); err != nil {
		errs = append(errs, common.NewErrorf("retain persisted maintenance mode: %v", err))
	}
	maintenanceModeEnabled.Store(true)
	return s.finishTransition(true, errors.Join(errs...))
}

func (s *MaintenanceService) finishTransition(enabled bool, err error) error {
	maintenanceTransitioning.Store(false)
	message := ""
	if err != nil {
		message = err.Error()
	}
	s.setStatus(enabled, false, message)
	return err
}

func (s *MaintenanceService) setStatus(enabled, transitioning bool, message string) {
	s.statusMu.Lock()
	s.status = MaintenanceStatus{Enabled: enabled, Transitioning: transitioning, Error: message}
	s.statusMu.Unlock()
}

func defaultMaintenanceHooks() maintenanceHooks {
	return maintenanceHooks{
		stopFailover: func() {
			if failoverPtr != nil {
				failoverPtr.Stop()
			}
		},
		stopProxyPlanes: stopProxyDataPlaneForTrafficBudget,
		reconcileTrafficBudget: func() error {
			return GetTrafficBudgetService().reconcileForMaintenanceResume()
		},
		startProxyPlanes: startProxyDataPlaneForMaintenanceResume,
		startAfterResume: func() {
			if failoverPtr != nil {
				failoverPtr.Start()
			}
			if mieru := GetMieruService(); mieru != nil {
				mieru.StartWatchdog()
			}
		},
	}
}

func startProxyDataPlaneForMaintenanceResume() error {
	if corePtr == nil {
		return common.NewError("sing-box core is unavailable")
	}
	if err := (&ConfigService{}).startCoreDuringMaintenanceResume(); err != nil {
		return err
	}
	if mieru := GetMieruService(); mieru != nil {
		if err := mieru.syncFromDBDuringMaintenanceResume(); err != nil {
			return common.NewErrorf("resume Mieru service: %v", err)
		}
	}
	return nil
}
