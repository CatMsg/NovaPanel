package service

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/CatMsg/NovaPanel/config"
	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
	"github.com/CatMsg/NovaPanel/logger"
	"github.com/CatMsg/NovaPanel/util/common"
	"gorm.io/gorm"
)

const (
	mieruStartupTimeout  = 6 * time.Second
	mieruStopTimeout     = 4 * time.Second
	mieruQueryTimeout    = 4 * time.Second
	mieruLogLimit        = 80
	mieruDebugDuration   = 10 * time.Minute
	mieruWatchInterval   = 20 * time.Second
	mieruWatchThreshold  = 3
	mieruWatchQueryTime  = 3 * time.Second
	mieruBridgeProbeTime = 2 * time.Second
	mieruBridgeHost      = "127.0.0.1"
	mieruBridgeProxyName = "novapanel"
)

var mieruRestartDelays = []time.Duration{
	time.Second,
	2 * time.Second,
	5 * time.Second,
	10 * time.Second,
	30 * time.Second,
}

var mieruBridgeState struct {
	sync.Mutex
	port int
}

var mitaTableColumns = regexp.MustCompile(`[[:space:]]{2,}`)

var mieruPtr *MieruService

type mieruRuntime struct {
	cmd             *exec.Cmd
	done            chan error
	startedAt       time.Time
	running         atomic.Bool
	intentionalStop atomic.Bool
	mu              sync.Mutex
	lastError       string
	logs            []string
}

type MieruService struct {
	syncMu           sync.Mutex
	mu               sync.Mutex
	active           *mieruRuntime
	total            int
	inboundTag       string
	users            map[string]struct{}
	trafficBaseline  map[string]mieruTrafficCounters
	recentUsers      map[string]time.Time
	appliedHash      string
	appliedConfig    []byte
	restartScheduled bool
	lastError        string
	debugUntil       time.Time
	watchCancel      context.CancelFunc
	watchDone        chan struct{}
	watchFailures    int
	watchRestarts    int
	watchLastCheck   time.Time
	watchLastError   string
	watchLastRestart time.Time
}

func NewMieruService() *MieruService {
	return &MieruService{
		users:           make(map[string]struct{}),
		trafficBaseline: make(map[string]mieruTrafficCounters),
		recentUsers:     make(map[string]time.Time),
	}
}

func SetMieruService(service *MieruService) {
	mieruPtr = service
}

func GetMieruService() *MieruService {
	return mieruPtr
}

func getMieruBridgePort() (int, error) {
	mieruBridgeState.Lock()
	defer mieruBridgeState.Unlock()
	if mieruBridgeState.port != 0 {
		return mieruBridgeState.port, nil
	}
	listener, err := net.Listen("tcp4", net.JoinHostPort(mieruBridgeHost, "0"))
	if err != nil {
		return 0, fmt.Errorf("allocate Mieru routing bridge port: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		return 0, fmt.Errorf("release Mieru routing bridge port: %w", err)
	}
	mieruBridgeState.port = port
	return port, nil
}

func buildMieruBridgeInbound(tag string, credentials []mieruClientCredential) ([]byte, error) {
	port, err := getMieruBridgePort()
	if err != nil {
		return nil, err
	}
	users := make([]map[string]string, 0, len(credentials))
	for _, credential := range credentials {
		name := strings.TrimSpace(credential.Name)
		password := strings.TrimSpace(credential.Password)
		if name == "" || password == "" {
			continue
		}
		users = append(users, map[string]string{
			"username": name,
			"password": hashMieruPassword(name, password),
		})
	}
	config := map[string]interface{}{
		"type":        "socks",
		"tag":         strings.TrimSpace(tag),
		"listen":      mieruBridgeHost,
		"listen_port": port,
	}
	if len(users) > 0 {
		config["users"] = users
	}
	return json.Marshal(config)
}

func buildMieruBridgeInboundFromDB(db *gorm.DB, inbound *model.Inbound) ([]byte, error) {
	if inbound == nil {
		return nil, common.NewError("Mieru inbound is required")
	}
	credentials, err := loadMieruClientCredentials(db, inbound.Id)
	if err != nil {
		return nil, err
	}
	return buildMieruBridgeInbound(inbound.Tag, credentials)
}

func (s *MieruService) SyncFromDB() error {
	if s == nil {
		return nil
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	if err := migrateMieruHandshakeModeOnce(); err != nil {
		return fmt.Errorf("migrate Mieru handshake mode: %w", err)
	}
	return s.syncFromDBLocked()
}

func (s *MieruService) syncFromDBLocked() error {
	inboundConfig, credentials, err := loadMieruInboundConfig()
	if err != nil {
		return err
	}
	if inboundConfig == nil {
		s.mu.Lock()
		oldRuntime := s.active
		s.active = nil
		s.total = 0
		s.inboundTag = ""
		s.users = make(map[string]struct{})
		s.trafficBaseline = make(map[string]mieruTrafficCounters)
		s.recentUsers = make(map[string]time.Time)
		s.appliedHash = ""
		s.appliedConfig = nil
		s.restartScheduled = false
		s.lastError = ""
		s.mu.Unlock()
		if oldRuntime != nil {
			stopMieruRuntime(oldRuntime)
		}
		removeMitaRuntimeFiles()
		return nil
	}
	if len(credentials) == 0 {
		s.mu.Lock()
		oldRuntime := s.active
		s.active = nil
		s.total = 1
		s.inboundTag = inboundConfig.Tag
		s.users = make(map[string]struct{})
		s.trafficBaseline = make(map[string]mieruTrafficCounters)
		s.recentUsers = make(map[string]time.Time)
		s.appliedHash = ""
		s.appliedConfig = nil
		s.restartScheduled = false
		s.lastError = ""
		s.mu.Unlock()
		if oldRuntime != nil {
			stopMieruRuntime(oldRuntime)
		}
		removeMitaRuntimeFiles()
		logger.Info("mieru inbound ", inboundConfig.Tag, " has no enabled users; mita service stopped")
		return nil
	}

	serverConfig, err := buildMitaServerConfig(inboundConfig, credentials)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if time.Now().Before(s.debugUntil) {
		serverConfig.LoggingLevel = "DEBUG"
	}
	s.mu.Unlock()
	payload, err := marshalMitaServerConfig(serverConfig)
	if err != nil {
		return err
	}
	configHash := fmt.Sprintf("%x", sha256.Sum256(payload))

	s.mu.Lock()
	active := s.active
	appliedHash := s.appliedHash
	oldPayload := append([]byte(nil), s.appliedConfig...)
	s.total = 1
	s.inboundTag = inboundConfig.Tag
	s.users = make(map[string]struct{}, len(credentials))
	for _, credential := range credentials {
		s.users[credential.Name] = struct{}{}
	}
	for username := range s.trafficBaseline {
		if _, exists := s.users[username]; !exists {
			delete(s.trafficBaseline, username)
			delete(s.recentUsers, username)
		}
	}
	s.mu.Unlock()
	if runtime.GOOS != "linux" {
		s.mu.Lock()
		s.appliedHash = configHash
		s.appliedConfig = append([]byte(nil), payload...)
		s.lastError = ""
		s.mu.Unlock()
		return nil
	}
	if corePtr == nil || !corePtr.IsRunning() {
		return common.NewError("sing-box is not running; Mieru routing bridge unavailable")
	}

	binary, err := mieruBinaryPath()
	if err != nil {
		return err
	}
	configPath, socketPath := mitaRuntimePaths()
	if active != nil && active.running.Load() {
		if appliedHash == configHash {
			s.mu.Lock()
			s.restartScheduled = false
			s.lastError = ""
			s.mu.Unlock()
			if !active.running.Load() {
				s.handleRuntimeExit(active, common.NewError("mita exited after config check"))
			}
			return nil
		}
		if !requiresMitaRestart(oldPayload, payload) {
			if err := writeMitaServerConfigPayload(configPath, payload); err != nil {
				return err
			}
			if _, err := runMitaQuery(binary, socketPath, "reload"); err != nil {
				reloadErr := fmt.Errorf("reload mita config: %w", err)
				if len(oldPayload) > 0 {
					if rollbackErr := writeMitaServerConfigPayload(configPath, oldPayload); rollbackErr != nil {
						return errors.Join(reloadErr, fmt.Errorf("restore previous mita config: %w", rollbackErr))
					}
					if _, rollbackErr := runMitaQuery(binary, socketPath, "reload"); rollbackErr != nil {
						return errors.Join(reloadErr, fmt.Errorf("reload previous mita config: %w", rollbackErr))
					}
				}
				return reloadErr
			}
			s.mu.Lock()
			s.appliedHash = configHash
			s.appliedConfig = append([]byte(nil), payload...)
			s.restartScheduled = false
			s.lastError = ""
			s.mu.Unlock()
			if !active.running.Load() {
				s.handleRuntimeExit(active, common.NewError("mita exited after reload"))
			}
			logger.Info("mieru service reloaded for inbound ", inboundConfig.Tag, " with ", len(credentials), " user(s)")
			return nil
		}
		logger.Info("non-reloadable mieru config changed; restarting shared mita service")
	}

	s.mu.Lock()
	if s.active == active {
		s.active = nil
	}
	s.mu.Unlock()
	if active != nil {
		stopMieruRuntime(active)
	}
	if err := writeMitaServerConfigPayload(configPath, payload); err != nil {
		return err
	}
	_ = os.Remove(socketPath)
	runtimeState, err := startMieruRuntime(binary, configPath, socketPath, s.handleRuntimeExit)
	if err != nil {
		s.mu.Lock()
		s.lastError = err.Error()
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.active = runtimeState
	s.appliedHash = configHash
	s.appliedConfig = append([]byte(nil), payload...)
	s.restartScheduled = false
	s.lastError = ""
	s.mu.Unlock()
	if !runtimeState.running.Load() {
		s.handleRuntimeExit(runtimeState, common.NewError("mita exited after startup"))
	}
	logger.Info("mieru service started for inbound ", inboundConfig.Tag, " with ", len(credentials), " user(s)")
	return nil
}

func (s *MieruService) EnableTemporaryDebug() (time.Time, error) {
	if s == nil {
		return time.Time{}, common.NewError("mieru service not initialized")
	}
	deadline := time.Now().Add(mieruDebugDuration)
	s.mu.Lock()
	previous := s.debugUntil
	s.debugUntil = deadline
	s.mu.Unlock()
	if err := s.SyncFromDB(); err != nil {
		s.mu.Lock()
		if s.debugUntil.Equal(deadline) {
			s.debugUntil = previous
		}
		s.mu.Unlock()
		return time.Time{}, err
	}
	go func(expected time.Time) {
		timer := time.NewTimer(time.Until(expected))
		defer timer.Stop()
		<-timer.C
		s.mu.Lock()
		if !s.debugUntil.Equal(expected) {
			s.mu.Unlock()
			return
		}
		s.debugUntil = time.Time{}
		s.mu.Unlock()
		if err := s.SyncFromDB(); err != nil {
			logger.Warning("restore Mieru INFO logging failed: ", err)
		}
	}(deadline)
	return deadline, nil
}

func (s *MieruService) StartWatchdog() {
	if s == nil || runtime.GOOS != "linux" {
		return
	}
	s.mu.Lock()
	if s.watchCancel != nil {
		s.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	s.watchCancel = cancel
	s.watchDone = done
	s.mu.Unlock()

	go s.runWatchdog(ctx, done)
}

func (s *MieruService) stopWatchdog() {
	s.mu.Lock()
	cancel := s.watchCancel
	done := s.watchDone
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
	s.mu.Lock()
	if s.watchDone == done {
		s.watchCancel = nil
		s.watchDone = nil
	}
	s.mu.Unlock()
}

func (s *MieruService) runWatchdog(ctx context.Context, done chan struct{}) {
	defer close(done)
	ticker := time.NewTicker(mieruWatchInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runWatchdogCheck(ctx)
		}
	}
}

func (s *MieruService) runWatchdogCheck(ctx context.Context) {
	s.mu.Lock()
	runtimeState := s.active
	total := s.total
	appliedConfig := append([]byte(nil), s.appliedConfig...)
	s.mu.Unlock()
	if total == 0 {
		s.recordWatchdogResult(runtimeState, nil)
		return
	}

	probeErr := s.probeRuntime(runtimeState, appliedConfig)
	if ctx.Err() != nil {
		return
	}
	if !s.recordWatchdogResult(runtimeState, probeErr) {
		return
	}

	diagnostics := s.collectWatchdogDiagnostics(runtimeState, probeErr)
	logger.Warning("mieru watchdog detected persistent failure: ", diagnostics)
	if err := s.restartUnhealthyRuntime(runtimeState, probeErr.Error()); err != nil {
		logger.Warning("mieru watchdog recovery failed: ", err)
		return
	}
	logger.Info("mieru watchdog recovered the data plane")
}

func (s *MieruService) probeRuntime(runtimeState *mieruRuntime, appliedConfig []byte) error {
	if runtimeState == nil {
		return common.NewError("mita runtime is missing")
	}
	if !runtimeState.running.Load() {
		return common.NewError("mita process is not running")
	}
	binary, err := mieruBinaryPath()
	if err != nil {
		return err
	}
	_, socketPath := mitaRuntimePaths()
	status, err := runMitaQueryWithTimeout(mieruWatchQueryTime, binary, socketPath, "status")
	if err != nil {
		return fmt.Errorf("query mita status: %w", err)
	}
	if !strings.Contains(strings.ToUpper(string(status)), "RUNNING") {
		return fmt.Errorf("mita status is not RUNNING: %s", strings.TrimSpace(string(status)))
	}
	if _, err := runMitaQueryWithTimeout(mieruWatchQueryTime, binary, socketPath, "get", "connections"); err != nil {
		return fmt.Errorf("query mita connections: %w", err)
	}
	if err := probeMieruBridge(appliedConfig); err != nil {
		return err
	}
	return nil
}

func probeMieruBridge(payload []byte) error {
	var serverConfig mitaServerConfig
	if len(payload) == 0 || json.Unmarshal(payload, &serverConfig) != nil {
		return common.NewError("parse active mita config for bridge probe")
	}
	var bridge *mitaEgressProxy
	for index := range serverConfig.Egress.Proxies {
		candidate := &serverConfig.Egress.Proxies[index]
		if candidate.Name == mieruBridgeProxyName && candidate.Protocol == "SOCKS5_PROXY_PROTOCOL" {
			bridge = candidate
			break
		}
	}
	if bridge == nil || strings.TrimSpace(bridge.Host) == "" || bridge.Port < 1 {
		return common.NewError("Mieru routing bridge is not configured")
	}
	address := net.JoinHostPort(bridge.Host, fmt.Sprint(bridge.Port))
	connection, err := net.DialTimeout("tcp", address, mieruBridgeProbeTime)
	if err != nil {
		return fmt.Errorf("connect Mieru routing bridge %s: %w", address, err)
	}
	defer connection.Close()
	if err := connection.SetDeadline(time.Now().Add(mieruBridgeProbeTime)); err != nil {
		return err
	}
	if _, err := connection.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return fmt.Errorf("write Mieru routing bridge handshake: %w", err)
	}
	reply := make([]byte, 2)
	if _, err := io.ReadFull(connection, reply); err != nil {
		return fmt.Errorf("read Mieru routing bridge handshake: %w", err)
	}
	if reply[0] != 0x05 || reply[1] != 0x00 {
		return fmt.Errorf("Mieru routing bridge rejected handshake: %x", reply)
	}
	return nil
}

func (s *MieruService) recordWatchdogResult(runtimeState *mieruRuntime, probeErr error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != runtimeState {
		return false
	}
	s.watchLastCheck = time.Now()
	if probeErr == nil {
		s.watchFailures = 0
		return false
	}
	s.watchFailures++
	s.watchLastError = probeErr.Error()
	if s.watchFailures == 1 {
		logger.Warning("mieru watchdog probe failed: ", probeErr)
	}
	return s.watchFailures >= mieruWatchThreshold
}

func (s *MieruService) collectWatchdogDiagnostics(runtimeState *mieruRuntime, probeErr error) string {
	parts := []string{"probe=" + probeErr.Error()}
	if binary, err := mieruBinaryPath(); err == nil {
		_, socketPath := mitaRuntimePaths()
		for _, query := range []struct {
			name string
			args []string
		}{
			{name: "status", args: []string{"status"}},
			{name: "connections", args: []string{"get", "connections"}},
			{name: "metrics", args: []string{"get", "metrics"}},
		} {
			output, queryErr := runMitaQueryWithTimeout(time.Second, binary, socketPath, query.args...)
			if queryErr != nil {
				parts = append(parts, query.name+"_error="+queryErr.Error())
			} else {
				parts = append(parts, query.name+"="+limitMieruDiagnostic(string(output), 1600))
			}
		}
	}
	if runtimeState != nil {
		runtimeState.mu.Lock()
		logs := append([]string(nil), runtimeState.logs...)
		runtimeError := runtimeState.lastError
		runtimeState.mu.Unlock()
		if len(logs) > 12 {
			logs = logs[len(logs)-12:]
		}
		if runtimeError != "" {
			parts = append(parts, "runtime_error="+runtimeError)
		}
		if len(logs) > 0 {
			parts = append(parts, "recent_logs="+limitMieruDiagnostic(strings.Join(logs, " | "), 2400))
		}
	}
	return strings.Join(parts, "; ")
}

func limitMieruDiagnostic(value string, limit int) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " | "))
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

func (s *MieruService) restartUnhealthyRuntime(expected *mieruRuntime, reason string) error {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.mu.Lock()
	if s.active != expected || s.total == 0 {
		s.mu.Unlock()
		return nil
	}
	s.active = nil
	s.appliedHash = ""
	s.restartScheduled = false
	s.watchFailures = 0
	s.watchLastError = reason
	s.mu.Unlock()
	if expected != nil {
		stopMieruRuntime(expected)
	}
	if err := s.syncFromDBLocked(); err != nil {
		s.mu.Lock()
		s.lastError = "mieru watchdog recovery failed: " + err.Error()
		s.mu.Unlock()
		return err
	}
	s.mu.Lock()
	s.watchRestarts++
	s.watchLastRestart = time.Now()
	s.mu.Unlock()
	return nil
}

func (s *MieruService) Stop() error {
	if s == nil {
		return nil
	}
	s.stopWatchdog()
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.mu.Lock()
	runtimeState := s.active
	s.active = nil
	s.total = 0
	s.inboundTag = ""
	s.users = make(map[string]struct{})
	s.trafficBaseline = make(map[string]mieruTrafficCounters)
	s.recentUsers = make(map[string]time.Time)
	s.appliedHash = ""
	s.appliedConfig = nil
	s.restartScheduled = false
	s.lastError = ""
	s.mu.Unlock()
	if runtimeState != nil {
		stopMieruRuntime(runtimeState)
	}
	return nil
}

func (s *MieruService) GetStatus(tag string) (map[string]interface{}, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil, common.NewError("missing inbound tag")
	}
	inbound := &model.Inbound{}
	if err := database.GetDB().Model(&model.Inbound{}).Where("tag = ? AND type = ?", tag, "mieru").First(inbound).Error; err != nil {
		return nil, err
	}
	config, err := parseMieruInbound(inbound)
	if err != nil {
		return nil, err
	}
	credentials, err := loadMieruClientCredentials(database.GetDB(), inbound.Id)
	if err != nil {
		return nil, err
	}
	usernames := make([]string, 0, len(credentials))
	for _, credential := range credentials {
		usernames = append(usernames, credential.Name)
	}

	s.mu.Lock()
	runtimeState := s.active
	total := s.total
	serviceError := s.lastError
	appliedConfig := append([]byte(nil), s.appliedConfig...)
	watchFailures := s.watchFailures
	watchRestarts := s.watchRestarts
	watchLastCheck := s.watchLastCheck
	watchLastError := s.watchLastError
	watchLastRestart := s.watchLastRestart
	s.mu.Unlock()
	running := runtimeState != nil && runtimeState.running.Load()
	result := map[string]interface{}{
		"tag":                    config.Tag,
		"port":                   config.ListenPort,
		"port_range":             config.PortRange,
		"transport":              config.Transport,
		"user_count":             len(usernames),
		"usernames":              usernames,
		"multiplexing":           config.Multiplexing,
		"handshake_mode":         config.HandshakeMode,
		"traffic_pattern":        config.TrafficPattern,
		"server_traffic_pattern": "DEFAULT",
		"mtu":                    config.MTU,
		"running":                running,
		"inbound_count":          total,
		"binary":                 "",
		"version":                "",
		"status":                 "",
		"connections":            "",
		"users":                  "",
		"user_stats":             map[string]map[string]string{},
		"metrics":                map[string]int64{},
		"watchdog_failures":      watchFailures,
		"watchdog_restarts":      watchRestarts,
	}
	if !watchLastCheck.IsZero() {
		result["watchdog_last_check"] = watchLastCheck.Format(time.RFC3339)
	}
	if watchLastError != "" {
		result["watchdog_last_error"] = watchLastError
	}
	if !watchLastRestart.IsZero() {
		result["watchdog_last_restart"] = watchLastRestart.Format(time.RFC3339)
	}
	var runtimeConfig mitaServerConfig
	if json.Unmarshal(appliedConfig, &runtimeConfig) == nil {
		result["server_traffic_pattern"] = mieruTrafficPatternName(runtimeConfig.TrafficPattern)
	}
	s.mu.Lock()
	debugUntil := s.debugUntil
	s.mu.Unlock()
	if time.Now().Before(debugUntil) {
		result["debug_active"] = true
		result["debug_until"] = debugUntil.Format(time.RFC3339)
		result["debug_remaining_seconds"] = int64(time.Until(debugUntil).Seconds())
	} else {
		result["debug_active"] = false
	}
	if serviceError != "" {
		result["last_error"] = serviceError
	}
	if runtimeState != nil {
		runtimeState.mu.Lock()
		if runtimeState.lastError != "" && serviceError == "" {
			result["last_error"] = runtimeState.lastError
		}
		result["logs"] = append([]string(nil), runtimeState.logs...)
		runtimeState.mu.Unlock()
		if !runtimeState.startedAt.IsZero() {
			result["started_at"] = runtimeState.startedAt.Format(time.RFC3339)
			result["uptime_seconds"] = int64(time.Since(runtimeState.startedAt).Seconds())
		}
	}
	if runtime.GOOS != "linux" {
		result["platform_note"] = "Mieru 服务端仅在 Linux 上运行，本地仅预览配置"
		return result, nil
	}

	binary, binaryErr := mieruBinaryPath()
	if binaryErr != nil {
		result["last_error"] = binaryErr.Error()
		return result, nil
	}
	result["binary"] = binary
	_, socketPath := mitaRuntimePaths()
	if output, queryErr := runMitaQuery(binary, socketPath, "version"); queryErr == nil {
		result["version"] = strings.TrimSpace(string(output))
	}
	if running {
		if output, queryErr := runMitaQuery(binary, socketPath, "status"); queryErr == nil {
			result["status"] = strings.TrimSpace(string(output))
		}
		if output, queryErr := runMitaQuery(binary, socketPath, "get", "connections"); queryErr == nil {
			result["connections"] = strings.TrimSpace(string(output))
		}
		if output, queryErr := runMitaQuery(binary, socketPath, "get", "users"); queryErr == nil {
			usersOutput := strings.TrimSpace(string(output))
			result["users"] = usersOutput
			result["user_stats"] = parseMitaAllUserStats(usersOutput)
		}
		if output, queryErr := runMitaQuery(binary, socketPath, "get", "metrics"); queryErr == nil {
			result["metrics"] = parseMitaMetrics(output)
		}
	}
	return result, nil
}

func (s *MieruService) GetSummary() map[string]int {
	result := map[string]int{"total": 0, "running": 0}
	if s == nil {
		return result
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result["total"] = s.total
	if s.total > 0 && s.active != nil && s.active.running.Load() {
		result["running"] = s.total
	}
	return result
}

func (s *MieruService) healthSummary() (map[string]int, []string) {
	summary := s.GetSummary()
	summary["watchdog_failures"] = 0
	summary["watchdog_restarts"] = 0
	details := make([]string, 0, 1)
	if summary["total"] > 0 && summary["running"] != summary["total"] {
		s.mu.Lock()
		userCount := len(s.users)
		s.mu.Unlock()
		if userCount == 0 {
			details = append(details, "未绑定启用用户")
		} else {
			details = append(details, "mita 服务未运行")
		}
	}
	s.mu.Lock()
	runtimeState := s.active
	serviceError := s.lastError
	watchFailures := s.watchFailures
	watchRestarts := s.watchRestarts
	watchLastError := s.watchLastError
	s.mu.Unlock()
	summary["watchdog_failures"] = watchFailures
	summary["watchdog_restarts"] = watchRestarts
	if watchFailures > 0 && watchLastError != "" {
		details = append(details, fmt.Sprintf("数据面探活连续失败 %d 次: %s", watchFailures, watchLastError))
	}
	if serviceError != "" {
		details = append(details, serviceError)
	}
	if runtimeState != nil {
		runtimeState.mu.Lock()
		if runtimeState.lastError != "" && runtimeState.lastError != serviceError {
			details = append(details, runtimeState.lastError)
		}
		runtimeState.mu.Unlock()
	}
	return summary, details
}

func loadMieruInboundConfig() (*mieruInboundConfig, []mieruClientCredential, error) {
	var inbounds []*model.Inbound
	db := database.GetDB()
	if err := db.Model(&model.Inbound{}).Where("type = ?", "mieru").Order("id ASC").Find(&inbounds).Error; err != nil {
		return nil, nil, err
	}
	if len(inbounds) == 0 {
		return nil, nil, nil
	}
	if len(inbounds) > 1 {
		return nil, nil, common.NewError("only one Mieru inbound is allowed per server")
	}
	config, err := parseMieruInbound(inbounds[0])
	if err != nil {
		return nil, nil, fmt.Errorf("parse mieru inbound %s: %w", inbounds[0].Tag, err)
	}
	credentials, err := loadMieruClientCredentials(db, inbounds[0].Id)
	if err != nil {
		return nil, nil, err
	}
	if _, err := buildMitaServerConfig(config, credentials); err != nil {
		return nil, nil, err
	}
	return config, credentials, nil
}

func loadMieruClientCredentials(db *gorm.DB, inboundID uint) ([]mieruClientCredential, error) {
	var clients []*model.Client
	if err := db.Raw(`
		SELECT clients.*
		FROM clients, json_each(clients.inbounds) AS je
		WHERE clients.enable = true AND CAST(je.value AS INTEGER) = ?
		ORDER BY clients.id ASC
	`, inboundID).Scan(&clients).Error; err != nil {
		return nil, err
	}
	credentials := make([]mieruClientCredential, 0, len(clients))
	for _, client := range clients {
		credential, err := parseMieruClientCredential(client)
		if err != nil {
			return nil, err
		}
		credentials = append(credentials, credential)
	}
	return credentials, nil
}

func mitaRuntimePaths() (string, string) {
	base := filepath.Join(config.GetDBFolderPath(), "mieru")
	return filepath.Join(base, "server.json"), filepath.Join(base, "mita.sock")
}

func removeMitaRuntimeFiles() {
	configPath, socketPath := mitaRuntimePaths()
	_ = os.Remove(configPath)
	_ = os.Remove(socketPath)
}

func marshalMitaServerConfig(serverConfig *mitaServerConfig) ([]byte, error) {
	return json.MarshalIndent(serverConfig, "", "  ")
}

func requiresMitaRestart(oldPayload, newPayload []byte) bool {
	if len(oldPayload) == 0 {
		return true
	}
	var oldConfig, newConfig mitaServerConfig
	if json.Unmarshal(oldPayload, &oldConfig) != nil || json.Unmarshal(newPayload, &newConfig) != nil {
		return true
	}
	return !reflect.DeepEqual(oldConfig.TrafficPattern, newConfig.TrafficPattern) ||
		!reflect.DeepEqual(oldConfig.DNS, newConfig.DNS) ||
		!reflect.DeepEqual(oldConfig.PortBindings, newConfig.PortBindings) ||
		oldConfig.MTU != newConfig.MTU ||
		!reflect.DeepEqual(oldConfig.Egress, newConfig.Egress) ||
		mitaUsersRequireRestart(oldConfig.Users, newConfig.Users)
}

func mitaUsersRequireRestart(oldUsers, newUsers []mitaUser) bool {
	newHashes := make(map[string]string, len(newUsers))
	for _, user := range newUsers {
		newHashes[user.Name] = user.HashedPassword
	}
	for _, user := range oldUsers {
		if newHashes[user.Name] != user.HashedPassword {
			return true
		}
	}
	return false
}

func mieruTrafficPatternName(pattern *mitaTrafficPattern) string {
	if pattern == nil {
		return "DEFAULT"
	}
	switch pattern.Seed {
	case 1031:
		return "BALANCED"
	case 2053:
		return "ENHANCED"
	default:
		return "CUSTOM"
	}
}

func writeMitaServerConfigPayload(configPath string, payload []byte) error {
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return err
	}
	tempPath := configPath + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0600); err != nil {
		return err
	}
	if err := os.Rename(tempPath, configPath); err != nil {
		_ = os.Remove(tempPath)
		return err
	}
	return nil
}

func mieruBinaryPath() (string, error) {
	if override := strings.TrimSpace(os.Getenv("SUI_MITA_BIN")); override != "" {
		if info, err := os.Stat(override); err == nil && !info.IsDir() {
			return override, nil
		}
		return "", fmt.Errorf("configured mita binary is unavailable: %s", override)
	}
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	path := filepath.Join(filepath.Dir(executable), "bin", "mita")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path, nil
	}
	if path, err := exec.LookPath("mita"); err == nil {
		return path, nil
	}
	return "", common.NewError("mita binary is not installed; update NovaPanel or place mita in bin/mita")
}

func startMieruRuntime(binary, configPath, socketPath string, onExit func(*mieruRuntime, error)) (*mieruRuntime, error) {
	command := exec.Command(binary, "run")
	command.Env = append(os.Environ(),
		"MITA_CONFIG_JSON_FILE="+configPath,
		"MITA_UDS_PATH="+socketPath,
		"MITA_INSECURE_UDS=1",
		"MITA_LOG_NO_TIMESTAMP=1",
		"MITA_NOVAPANEL_BRIDGE_AUTH=1",
	)
	runtimeState := &mieruRuntime{
		cmd:       command,
		done:      make(chan error, 1),
		startedAt: time.Now(),
		logs:      make([]string, 0, mieruLogLimit),
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	runtimeState.running.Store(true)
	go runtimeState.captureOutput(stdout)
	go runtimeState.captureOutput(stderr)
	go func() {
		waitErr := command.Wait()
		runtimeState.running.Store(false)
		if waitErr != nil {
			runtimeState.setError(waitErr.Error())
		}
		runtimeState.done <- waitErr
		if onExit != nil {
			onExit(runtimeState, waitErr)
		}
	}()

	deadline := time.Now().Add(mieruStartupTimeout)
	var lastQueryErr error
	for time.Now().Before(deadline) {
		if !runtimeState.running.Load() {
			if lastError := runtimeState.getError(); lastError != "" {
				return nil, common.NewError(lastError)
			}
			return nil, common.NewError("mita exited during startup")
		}
		output, queryErr := runMitaQueryWithTimeout(750*time.Millisecond, binary, socketPath, "status")
		if queryErr == nil && strings.Contains(strings.ToUpper(string(output)), "RUNNING") {
			return runtimeState, nil
		}
		lastQueryErr = queryErr
		time.Sleep(120 * time.Millisecond)
	}
	stopMieruRuntime(runtimeState)
	if lastQueryErr != nil {
		return nil, fmt.Errorf("mita did not become ready: %w", lastQueryErr)
	}
	return nil, common.NewError("mita did not become ready before timeout")
}

func stopMieruRuntime(runtimeState *mieruRuntime) {
	if runtimeState == nil || runtimeState.cmd == nil || runtimeState.cmd.Process == nil {
		return
	}
	if !runtimeState.running.Load() {
		return
	}
	runtimeState.intentionalStop.Store(true)
	_ = runtimeState.cmd.Process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(mieruStopTimeout)
	defer timer.Stop()
	select {
	case <-runtimeState.done:
	case <-timer.C:
		_ = runtimeState.cmd.Process.Kill()
		select {
		case <-runtimeState.done:
		case <-time.After(time.Second):
		}
	}
	runtimeState.running.Store(false)
}

func (r *mieruRuntime) captureOutput(reader interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		r.mu.Lock()
		r.logs = append(r.logs, line)
		if len(r.logs) > mieruLogLimit {
			r.logs = append([]string(nil), r.logs[len(r.logs)-mieruLogLimit:]...)
		}
		r.mu.Unlock()
		logger.Info("mita: ", line)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
		r.setError(err.Error())
	}
}

func (r *mieruRuntime) setError(message string) {
	r.mu.Lock()
	r.lastError = strings.TrimSpace(message)
	r.mu.Unlock()
}

func (r *mieruRuntime) getError() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastError
}

func runMitaQuery(binary, socketPath string, args ...string) ([]byte, error) {
	return runMitaQueryWithTimeout(mieruQueryTimeout, binary, socketPath, args...)
}

func runMitaQueryWithTimeout(timeout time.Duration, binary, socketPath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	configPath, _ := mitaRuntimePaths()
	command.Env = append(os.Environ(),
		"MITA_CONFIG_JSON_FILE="+configPath,
		"MITA_UDS_PATH="+socketPath,
		"MITA_INSECURE_UDS=1",
	)
	output, err := command.CombinedOutput()
	if ctx.Err() != nil {
		return output, ctx.Err()
	}
	if err != nil {
		return output, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, nil
}

func (s *MieruService) handleRuntimeExit(runtimeState *mieruRuntime, waitErr error) {
	if s == nil || runtimeState == nil || runtimeState.intentionalStop.Load() {
		return
	}
	message := "mita exited unexpectedly"
	if waitErr != nil {
		message += ": " + waitErr.Error()
	}
	s.mu.Lock()
	if s.active != runtimeState || s.total == 0 || s.restartScheduled {
		s.mu.Unlock()
		return
	}
	s.lastError = message
	s.restartScheduled = true
	s.mu.Unlock()
	logger.Warning(message, "; scheduling automatic restart")

	go func() {
		for attempt := 0; ; attempt++ {
			delayIndex := attempt
			if delayIndex >= len(mieruRestartDelays) {
				delayIndex = len(mieruRestartDelays) - 1
			}
			delay := mieruRestartDelays[delayIndex]
			time.Sleep(delay)
			s.mu.Lock()
			if !s.restartScheduled || s.total == 0 {
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
			if err := s.SyncFromDB(); err == nil {
				logger.Info("mieru service recovered automatically")
				return
			} else {
				s.mu.Lock()
				s.lastError = "automatic mita restart failed: " + err.Error()
				s.mu.Unlock()
				logger.Warning("automatic mita restart failed: ", err)
			}
		}
	}()
}

type mieruTrafficCounters struct {
	Upload   int64
	Download int64
}

func (s *MieruService) CollectStats() ([]model.Stats, onlines, error) {
	if s == nil || runtime.GOOS != "linux" {
		return nil, onlines{}, nil
	}
	s.mu.Lock()
	runtimeState := s.active
	inboundTag := s.inboundTag
	users := make(map[string]struct{}, len(s.users))
	for username := range s.users {
		users[username] = struct{}{}
	}
	s.mu.Unlock()
	if runtimeState == nil || !runtimeState.running.Load() || inboundTag == "" || len(users) == 0 {
		return nil, onlines{}, nil
	}

	binary, err := mieruBinaryPath()
	if err != nil {
		return nil, onlines{}, err
	}
	_, socketPath := mitaRuntimePaths()
	output, err := runMitaQuery(binary, socketPath, "get", "metrics")
	if err != nil {
		return nil, onlines{}, err
	}
	current := parseMitaTrafficCounters(output)
	now := time.Now()
	s.mu.Lock()
	if s.trafficBaseline == nil {
		s.trafficBaseline = make(map[string]mieruTrafficCounters)
	}
	if s.recentUsers == nil {
		s.recentUsers = make(map[string]time.Time)
	}
	for username := range users {
		counter := current[username]
		previous := s.trafficBaseline[username]
		uploadDelta := counter.Upload - previous.Upload
		if uploadDelta < 0 {
			uploadDelta = counter.Upload
		}
		downloadDelta := counter.Download - previous.Download
		if downloadDelta < 0 {
			downloadDelta = counter.Download
		}
		s.trafficBaseline[username] = counter
		if uploadDelta > 0 || downloadDelta > 0 {
			s.recentUsers[username] = now
		}
	}
	sampled := onlines{}
	for username, lastSeen := range s.recentUsers {
		if _, exists := users[username]; !exists {
			delete(s.recentUsers, username)
			delete(s.trafficBaseline, username)
			continue
		}
		if now.Sub(lastSeen) <= 35*time.Second {
			sampled.User = append(sampled.User, username)
		}
	}
	if len(sampled.User) > 0 {
		sampled.Inbound = append(sampled.Inbound, inboundTag)
	}
	s.mu.Unlock()

	// Traffic is counted by the authenticated Sing-box bridge. Mita metrics are
	// retained only for online detection to avoid charging every byte twice.
	return nil, sampled, nil
}

func parseMitaUserStats(output, username string) map[string]string {
	return parseMitaAllUserStats(output)[strings.TrimSpace(username)]
}

func parseMitaAllUserStats(output string) map[string]map[string]string {
	result := make(map[string]map[string]string)
	for _, line := range strings.Split(output, "\n") {
		fields := mitaTableColumns.Split(strings.TrimSpace(line), -1)
		if len(fields) < 8 || fields[0] == "User" {
			continue
		}
		result[fields[0]] = map[string]string{
			"last_active":    fields[1],
			"day_download":   fields[2],
			"day_upload":     fields[3],
			"week_download":  fields[4],
			"week_upload":    fields[5],
			"month_download": fields[6],
			"month_upload":   fields[7],
		}
	}
	return result
}

func parseMitaMetrics(output []byte) map[string]int64 {
	output = extractMitaJSON(output)
	var groups map[string]json.RawMessage
	if err := json.Unmarshal(output, &groups); err != nil {
		return map[string]int64{}
	}
	result := map[string]int64{}
	copyMetric := func(resultName, groupName, metricName string) {
		if raw, ok := groups[groupName]; ok {
			var group map[string]int64
			if json.Unmarshal(raw, &group) == nil {
				value, exists := group[metricName]
				if !exists {
					return
				}
				result[resultName] = value
			}
		}
	}
	copyMetric("active_connections", "connections", "CurrEstablished")
	copyMetric("max_connections", "connections", "MaxConn")
	copyMetric("underlay_connections", "underlay", "CurrEstablished")
	copyMetric("unsolicited_udp", "underlay", "UnsolicitedUDP")
	copyMetric("failed_decrypt", "cipher - server", "FailedDirectDecrypt")
	return result
}

func parseMitaTrafficCounters(output []byte) map[string]mieruTrafficCounters {
	result := make(map[string]mieruTrafficCounters)
	var payload struct {
		Users map[string]map[string]int64 `json:"users"`
	}
	if err := json.Unmarshal(extractMitaJSON(output), &payload); err != nil {
		return result
	}
	for username, metrics := range payload.Users {
		result[username] = mieruTrafficCounters{
			Upload:   metrics["UploadBytes"],
			Download: metrics["DownloadBytes"],
		}
	}
	return result
}

func extractMitaJSON(output []byte) []byte {
	text := string(output)
	if start := strings.IndexByte(text, '{'); start >= 0 {
		if end := strings.LastIndexByte(text, '}'); end >= start {
			return []byte(text[start : end+1])
		}
	}
	return output
}
