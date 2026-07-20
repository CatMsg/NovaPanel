package service

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	updateStateDir = "/var/lib/s-ui"
	updateStatus   = updateStateDir + "/update.status"
	updateLog      = updateStateDir + "/update.log"
	updatePID      = updateStateDir + "/update.pid"
)

var (
	backgroundUpdateMu sync.Mutex
	updateCandidates   = []string{"/usr/bin/s-ui", "/usr/local/s-ui/s-ui.sh"}
	lastUpdateLaunch   time.Time
)

// StartBackgroundUpdate delegates the upgrade to the installed compatibility
// script. The new process is detached so an SSH disconnect cannot interrupt it.
func StartBackgroundUpdate() (bool, error) {
	backgroundUpdateMu.Lock()
	defer backgroundUpdateMu.Unlock()
	if time.Since(lastUpdateLaunch) < 10*time.Second {
		return false, nil
	}

	status, err := GetUpdateStatus()
	if err != nil {
		return false, err
	}
	if running, _ := status["running"].(bool); running {
		return false, nil
	}

	for _, path := range updateCandidates {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		command := exec.Command(path, "update", "--background")
		devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
		if err != nil {
			return false, err
		}
		command.Stdin = devNull
		command.Stdout = devNull
		command.Stderr = devNull
		if err := command.Start(); err != nil {
			_ = devNull.Close()
			continue
		}
		_ = devNull.Close()
		lastUpdateLaunch = time.Now()
		return true, nil
	}
	return false, errors.New("未找到 s-ui 更新脚本")
}

// GetUpdateStatus returns a small, safe status object for the local panel and
// the fleet page. The log is intentionally capped to prevent a large response.
func GetUpdateStatus() (map[string]interface{}, error) {
	result := map[string]interface{}{
		"state":   "never",
		"message": "尚未执行后台更新",
		"logs":    []string{},
	}
	if raw, err := os.ReadFile(updateStatus); err == nil {
		for scanner := bufio.NewScanner(strings.NewReader(string(raw))); scanner.Scan(); {
			parts := strings.SplitN(scanner.Text(), "=", 2)
			if len(parts) != 2 {
				continue
			}
			switch parts[0] {
			case "state":
				result["state"] = parts[1]
			case "message":
				result["message"] = parts[1]
			case "updated_at":
				result["updatedAt"] = parts[1]
			}
		}
	}
	if raw, err := os.ReadFile(updatePID); err == nil {
		pidValue := strings.TrimSpace(string(raw))
		if strings.HasPrefix(pidValue, "systemd:") {
			unit := strings.TrimPrefix(pidValue, "systemd:")
			result["unit"] = unit
			if unit != "" {
				command := exec.Command("systemctl", "is-active", "--quiet", unit)
				result["running"] = command.Run() == nil
			}
		} else if pid, parseErr := strconv.Atoi(pidValue); parseErr == nil && pid > 0 {
			result["pid"] = pid
			if process, processErr := os.FindProcess(pid); processErr == nil && process.Signal(syscall.Signal(0)) == nil {
				result["running"] = true
			}
		}
	}
	if raw, err := os.Open(updateLog); err == nil {
		defer raw.Close()
		lines, readErr := tailLines(raw, 100)
		if readErr != nil {
			return nil, readErr
		}
		result["logs"] = lines
	}
	return result, nil
}

func tailLines(reader io.Reader, limit int) ([]string, error) {
	if limit < 1 {
		return []string{}, nil
	}
	lines := make([]string, 0, limit)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		if len(lines) == limit {
			copy(lines, lines[1:])
			lines[len(lines)-1] = scanner.Text()
			continue
		}
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("读取更新日志失败: %w", err)
	}
	return lines, nil
}
