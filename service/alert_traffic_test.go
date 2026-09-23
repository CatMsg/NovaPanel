package service

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
)

func TestTrafficAlertLifecycleIsIndependentFromGenericHealthWarnings(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "traffic-alerts.db")); err != nil {
		t.Fatal(err)
	}
	if _, err := (&SettingService{}).GetAllSetting(); err != nil {
		t.Fatal(err)
	}
	settings := &SettingService{}
	if err := settings.saveSetting("alertCooldownMinutes", "60"); err != nil {
		t.Fatal(err)
	}

	oldSender := alertMessageSender
	messages := make([]string, 0)
	alertMessageSender = func(_ *AlertService, _ map[string]string, message string) error {
		messages = append(messages, message)
		return nil
	}
	t.Cleanup(func() { alertMessageSender = oldSender })

	service := &AlertService{}
	now := time.Date(2026, 9, 23, 1, 0, 0, 0, time.UTC)
	reportFor := func(level string, percent float64) *HealthReport {
		status := TrafficBudgetStatus{
			Enabled: true, Supported: true, Level: level, UsedPercent: percent,
			UsedBytes: uint64(percent * 10), ClientPoolBytes: 1000,
			PoolRemainingBytes: uint64(1000 - percent*10),
		}
		return &HealthReport{
			Diagnostics: map[string]interface{}{"trafficBudget": status},
			Checks: []HealthCheck{
				{ID: "traffic-budget", Status: map[string]string{"warning": "warning", "critical": "error", "blocked": "error", "normal": "ok"}[level]},
				{ID: "disk", Status: "warning", Title: "系统磁盘", Summary: "空间不足"},
			},
		}
	}
	run := func(report *HealthReport, at time.Time) {
		values, err := service.alertSettingValues()
		if err != nil {
			t.Fatal(err)
		}
		if err := service.evaluateTrafficAlert(values, report, at); err != nil {
			t.Fatal(err)
		}
	}

	run(reportFor("warning", 80), now)
	run(reportFor("warning", 81), now.Add(10*time.Minute))
	run(reportFor("critical", 91), now.Add(11*time.Minute))
	run(reportFor("blocked", 100), now.Add(12*time.Minute))
	run(reportFor("normal", 20), now.Add(13*time.Minute))

	if len(messages) != 4 {
		t.Fatalf("messages = %d, want 4: %#v", len(messages), messages)
	}
	for index, marker := range []string{"流量预警", "流量严重", "流量硬保护", "流量恢复"} {
		if !strings.Contains(messages[index], marker) {
			t.Fatalf("message %d = %q, want marker %q", index, messages[index], marker)
		}
	}
	level, err := settings.getString("alertTrafficLastLevel")
	if err != nil {
		t.Fatal(err)
	}
	if level != "normal" {
		t.Fatalf("stored traffic alert level = %q, want normal", level)
	}
}
