package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
)

const (
	mieruStartupTimeout = 800 * time.Millisecond
	mieruStopTimeout    = 4 * time.Second
	mieruQueryTimeout   = 4 * time.Second
	mieruLogLimit       = 80
)

var mieruPtr *MieruService

type mieruRuntime struct {
	cmd       *exec.Cmd
	done      chan error
	startedAt time.Time
	running   atomic.Bool
	mu        sync.Mutex
	lastError string
	logs      []string
}

type MieruService struct {
	syncMu sync.Mutex
	mu     sync.Mutex
	active *mieruRuntime
	total  int
}

func NewMieruService() *MieruService {
	return &MieruService{}
}

func SetMieruService(service *MieruService) {
	mieruPtr = service
}

func GetMieruService() *MieruService {
	return mieruPtr
}

func (s *MieruService) SyncFromDB() error {
	if s == nil {
		return nil
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	configs, err := loadMieruEndpointConfigs()
	if err != nil {
		return err
	}
	s.mu.Lock()
	oldRuntime := s.active
	s.active = nil
	s.total = len(configs)
	s.mu.Unlock()
	if oldRuntime != nil {
		stopMieruRuntime(oldRuntime)
	}
	if len(configs) == 0 {
		removeMitaRuntimeFiles()
		return nil
	}
	if runtime.GOOS != "linux" {
		return nil
	}

	serverConfig, err := buildMitaServerConfig(configs)
	if err != nil {
		return err
	}
	configPath, socketPath, err := writeMitaServerConfig(serverConfig)
	if err != nil {
		return err
	}
	binary, err := mieruBinaryPath()
	if err != nil {
		return err
	}
	runtimeState, err := startMieruRuntime(binary, configPath, socketPath)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.active = runtimeState
	s.mu.Unlock()
	logger.Info("mieru service started with ", len(configs), " endpoint(s)")
	return nil
}

func (s *MieruService) Stop() error {
	if s == nil {
		return nil
	}
	s.syncMu.Lock()
	defer s.syncMu.Unlock()
	s.mu.Lock()
	runtimeState := s.active
	s.active = nil
	s.total = 0
	s.mu.Unlock()
	if runtimeState != nil {
		stopMieruRuntime(runtimeState)
	}
	return nil
}

func (s *MieruService) GetStatus(tag string) (map[string]interface{}, error) {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil, common.NewError("missing endpoint tag")
	}
	endpoint := &model.Endpoint{}
	if err := database.GetDB().Model(&model.Endpoint{}).Where("tag = ? AND type = ?", tag, "mieru").First(endpoint).Error; err != nil {
		return nil, err
	}
	config, err := parseMieruEndpoint(endpoint)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	runtimeState := s.active
	total := s.total
	s.mu.Unlock()
	running := runtimeState != nil && runtimeState.running.Load()
	result := map[string]interface{}{
		"tag":            config.Tag,
		"server":         config.Server,
		"port":           config.Port,
		"port_range":     config.PortRange,
		"transport":      config.Transport,
		"username":       config.Username,
		"multiplexing":   config.Multiplexing,
		"handshake_mode": config.HandshakeMode,
		"mtu":            config.MTU,
		"running":        running,
		"endpoint_count": total,
		"binary":         "",
		"version":        "",
		"status":         "",
		"connections":    "",
		"users":          "",
	}
	if runtimeState != nil {
		runtimeState.mu.Lock()
		result["last_error"] = runtimeState.lastError
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
			result["users"] = strings.TrimSpace(string(output))
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
	details := make([]string, 0, 1)
	if summary["total"] > 0 && summary["running"] != summary["total"] {
		details = append(details, "mita 服务未运行")
	}
	s.mu.Lock()
	runtimeState := s.active
	s.mu.Unlock()
	if runtimeState != nil {
		runtimeState.mu.Lock()
		if runtimeState.lastError != "" {
			details = append(details, runtimeState.lastError)
		}
		runtimeState.mu.Unlock()
	}
	return summary, details
}

func loadMieruEndpointConfigs() ([]*mieruEndpointConfig, error) {
	var endpoints []*model.Endpoint
	if err := database.GetDB().Model(&model.Endpoint{}).Where("type = ?", "mieru").Order("id ASC").Find(&endpoints).Error; err != nil {
		return nil, err
	}
	configs := make([]*mieruEndpointConfig, 0, len(endpoints))
	for _, endpoint := range endpoints {
		config, err := parseMieruEndpoint(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse mieru endpoint %s: %w", endpoint.Tag, err)
		}
		configs = append(configs, config)
	}
	if _, err := buildMitaServerConfig(configs); err != nil {
		return nil, err
	}
	return configs, nil
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

func writeMitaServerConfig(serverConfig *mitaServerConfig) (string, string, error) {
	configPath, socketPath := mitaRuntimePaths()
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
		return "", "", err
	}
	payload, err := json.MarshalIndent(serverConfig, "", "  ")
	if err != nil {
		return "", "", err
	}
	tempPath := configPath + ".tmp"
	if err := os.WriteFile(tempPath, payload, 0600); err != nil {
		return "", "", err
	}
	if err := os.Rename(tempPath, configPath); err != nil {
		return "", "", err
	}
	_ = os.Remove(socketPath)
	return configPath, socketPath, nil
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

func startMieruRuntime(binary, configPath, socketPath string) (*mieruRuntime, error) {
	command := exec.Command(binary, "run")
	command.Env = append(os.Environ(),
		"MITA_CONFIG_JSON_FILE="+configPath,
		"MITA_UDS_PATH="+socketPath,
		"MITA_INSECURE_UDS=1",
		"MITA_LOG_NO_TIMESTAMP=1",
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
	}()

	timer := time.NewTimer(mieruStartupTimeout)
	defer timer.Stop()
	select {
	case waitErr := <-runtimeState.done:
		if waitErr == nil {
			waitErr = common.NewError("mita exited during startup")
		}
		return nil, waitErr
	case <-timer.C:
		return runtimeState, nil
	}
}

func stopMieruRuntime(runtimeState *mieruRuntime) {
	if runtimeState == nil || runtimeState.cmd == nil || runtimeState.cmd.Process == nil {
		return
	}
	if !runtimeState.running.Load() {
		return
	}
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

func runMitaQuery(binary, socketPath string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), mieruQueryTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, binary, args...)
	command.Env = append(os.Environ(),
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
