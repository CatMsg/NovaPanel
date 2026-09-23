package service

import (
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

var alertMessageSender = func(service *AlertService, values map[string]string, message string) error {
	return service.sendAlert(values, message)
}

func (s *AlertService) evaluateTrafficAlert(values map[string]string, report *HealthReport, now time.Time) error {
	status, ok := report.Diagnostics["trafficBudget"].(TrafficBudgetStatus)
	if !ok {
		return nil
	}
	current := trafficAlertLevel(status)
	previous := strings.TrimSpace(values["alertTrafficLastLevel"])
	lastSent, _ := strconv.ParseInt(values["alertTrafficLastSentAt"], 10, 64)
	cooldown, _ := strconv.Atoi(values["alertCooldownMinutes"])

	send := false
	switch {
	case previous == "":
		send = trafficAlertProblemLevel(current)
	case current != previous:
		send = trafficAlertProblemLevel(current) || trafficAlertProblemLevel(previous)
	case trafficAlertProblemLevel(current) && cooldown > 0 && now.Sub(time.Unix(lastSent, 0)) >= time.Duration(cooldown)*time.Minute:
		send = true
	}

	if send {
		if err := alertMessageSender(s, values, trafficAlertMessage(values["alertLanguage"], previous, current, status)); err != nil {
			return err
		}
		return s.persistTrafficAlertState(current, now, true)
	}
	if previous != current {
		return s.persistTrafficAlertState(current, time.Time{}, false)
	}
	return nil
}

func trafficAlertLevel(status TrafficBudgetStatus) string {
	if !status.Enabled || !status.Supported {
		return "normal"
	}
	switch status.Level {
	case "warning", "critical", "blocked", "error":
		return status.Level
	default:
		return "normal"
	}
}

func trafficAlertProblemLevel(level string) bool {
	switch level {
	case "warning", "critical", "blocked", "error":
		return true
	default:
		return false
	}
}

func trafficAlertMessage(language, previous, current string, status TrafficBudgetStatus) string {
	summary := localizedTrafficSummary(language, status)
	switch current {
	case "warning":
		return localizedAlertText(language, "traffic_warning", summary)
	case "critical":
		return localizedAlertText(language, "traffic_critical", summary)
	case "blocked":
		return localizedAlertText(language, "traffic_blocked", summary)
	case "error":
		detail := strings.TrimSpace(status.Error)
		if detail == "" {
			detail = summary
		}
		return localizedAlertText(language, "traffic_error", detail)
	default:
		if trafficAlertProblemLevel(previous) {
			return localizedAlertText(language, "traffic_recovery", summary)
		}
		return summary
	}
}

func (s *AlertService) persistTrafficAlertState(level string, sentAt time.Time, updateSentAt bool) error {
	return retryWriteTx(func(tx *gorm.DB) error {
		if err := s.saveSettingTx(tx, "alertTrafficLastLevel", level); err != nil {
			return err
		}
		if updateSentAt {
			return s.saveSettingTx(tx, "alertTrafficLastSentAt", strconv.FormatInt(sentAt.Unix(), 10))
		}
		return nil
	})
}
