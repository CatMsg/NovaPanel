package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

type AlertSettings struct {
	Enabled            bool   `json:"enabled"`
	TelegramToken      string `json:"telegramToken,omitempty"`
	TelegramTokenSet   bool   `json:"telegramTokenSet"`
	TelegramChatID     string `json:"telegramChatId"`
	IntervalMinutes    int    `json:"intervalMinutes"`
	CooldownMinutes    int    `json:"cooldownMinutes"`
	ClearTelegramToken bool   `json:"clearTelegramToken,omitempty"`
}

type AlertService struct {
	SettingService
	HealthService
}

var (
	alertRunMu     sync.Mutex
	lastAlertCheck time.Time
)

func (s *AlertService) GetAlertSettings() (*AlertSettings, error) {
	values, err := s.alertSettingValues()
	if err != nil {
		return nil, err
	}
	interval, _ := strconv.Atoi(values["alertIntervalMinutes"])
	cooldown, _ := strconv.Atoi(values["alertCooldownMinutes"])
	return &AlertSettings{
		Enabled:          values["alertEnabled"] == "true",
		TelegramTokenSet: strings.TrimSpace(values["alertTelegramToken"]) != "",
		TelegramChatID:   values["alertTelegramChatID"],
		IntervalMinutes:  interval,
		CooldownMinutes:  cooldown,
	}, nil
}

func (s *AlertService) SaveAlertSettings(input AlertSettings) error {
	input.TelegramToken = strings.TrimSpace(input.TelegramToken)
	input.TelegramChatID = strings.TrimSpace(input.TelegramChatID)
	if input.IntervalMinutes < 1 || input.IntervalMinutes > 1440 {
		return fmt.Errorf("检查间隔必须在 1 到 1440 分钟之间")
	}
	if input.CooldownMinutes < 1 || input.CooldownMinutes > 10080 {
		return fmt.Errorf("冷却时间必须在 1 到 10080 分钟之间")
	}
	return retryWriteTx(func(tx *gorm.DB) error {
		storedToken, err := s.getStringTx(tx, "alertTelegramToken")
		if err != nil {
			return err
		}
		effectiveToken := storedToken
		if input.ClearTelegramToken {
			effectiveToken = ""
		} else if input.TelegramToken != "" {
			effectiveToken = input.TelegramToken
		}
		if input.Enabled && (strings.TrimSpace(effectiveToken) == "" || input.TelegramChatID == "") {
			return fmt.Errorf("启用告警前必须配置 Telegram Bot Token 和 Chat ID")
		}
		values := map[string]string{
			"alertEnabled":         strconv.FormatBool(input.Enabled),
			"alertTelegramChatID":  input.TelegramChatID,
			"alertIntervalMinutes": strconv.Itoa(input.IntervalMinutes),
			"alertCooldownMinutes": strconv.Itoa(input.CooldownMinutes),
		}
		if input.ClearTelegramToken {
			values["alertTelegramToken"] = ""
		} else if input.TelegramToken != "" {
			values["alertTelegramToken"] = input.TelegramToken
		}
		for key, value := range values {
			if err := s.saveSettingTx(tx, key, value); err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *AlertService) TestAlert() error {
	values, err := s.alertSettingValues()
	if err != nil {
		return err
	}
	return s.sendAlert(values, "NovaPanel 告警测试\n通知通道配置正常。")
}

func (s *AlertService) EvaluateAndNotify() error {
	if !alertRunMu.TryLock() {
		return nil
	}
	defer alertRunMu.Unlock()

	values, err := s.alertSettingValues()
	if err != nil || values["alertEnabled"] != "true" {
		return err
	}
	interval, _ := strconv.Atoi(values["alertIntervalMinutes"])
	if interval > 0 && !lastAlertCheck.IsZero() && time.Since(lastAlertCheck) < time.Duration(interval)*time.Minute {
		return nil
	}
	lastAlertCheck = time.Now()
	lastSent, _ := strconv.ParseInt(values["alertLastSentAt"], 10, 64)

	report := s.GetHealthReport(false)
	problems := make([]string, 0)
	for _, check := range report.Checks {
		if check.Status == "error" || check.Status == "warning" {
			problems = append(problems, fmt.Sprintf("[%s] %s: %s", check.Status, check.Title, check.Summary))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	fingerprintBytes := sha256.Sum256([]byte(strings.Join(problems, "\n")))
	fingerprint := hex.EncodeToString(fingerprintBytes[:])
	cooldown, _ := strconv.Atoi(values["alertCooldownMinutes"])
	if fingerprint == values["alertLastFingerprint"] && cooldown > 0 && time.Since(time.Unix(lastSent, 0)) < time.Duration(cooldown)*time.Minute {
		return nil
	}
	message := "NovaPanel 健康告警\n" + strings.Join(problems, "\n")
	if err := s.sendAlert(values, message); err != nil {
		return err
	}
	return retryWriteTx(func(tx *gorm.DB) error {
		if err := s.saveSettingTx(tx, "alertLastFingerprint", fingerprint); err != nil {
			return err
		}
		return s.saveSettingTx(tx, "alertLastSentAt", strconv.FormatInt(time.Now().Unix(), 10))
	})
}

func (s *AlertService) alertSettingValues() (map[string]string, error) {
	keys := []string{"alertEnabled", "alertTelegramToken", "alertTelegramChatID", "alertIntervalMinutes", "alertCooldownMinutes", "alertLastFingerprint", "alertLastSentAt"}
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		value, err := s.getString(key)
		if err != nil {
			return nil, err
		}
		values[key] = value
	}
	return values, nil
}

func (s *AlertService) sendAlert(values map[string]string, message string) error {
	token := strings.TrimSpace(values["alertTelegramToken"])
	chatID := strings.TrimSpace(values["alertTelegramChatID"])
	if token == "" || chatID == "" {
		return fmt.Errorf("Telegram Bot Token 或 Chat ID 未配置")
	}
	endpoint := "https://api.telegram.org/bot" + url.PathEscape(token) + "/sendMessage"
	if err := postAlertJSON(endpoint, map[string]string{"chat_id": chatID, "text": message}); err != nil {
		return fmt.Errorf("telegram: %w", err)
	}
	return nil
}

func postAlertJSON(endpoint string, payload interface{}) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(responseBody)))
	}
	return nil
}
