package service

import (
	"strings"
	"testing"
)

func TestLocalizeHealthReportUsesRequestedLanguage(t *testing.T) {
	status := TrafficBudgetStatus{
		Enabled: true, Supported: true, Level: "warning",
		UsedPercent: 82.5, UsedBytes: 825, ClientPoolBytes: 1000, PoolRemainingBytes: 175,
	}
	report := &HealthReport{
		Checks: []HealthCheck{
			{ID: "traffic-budget", Title: "VPS 总流量", Status: "warning", Summary: "已达到预警阈值"},
			{ID: "disk", Title: "系统磁盘", Status: "warning", Summary: "已使用 91.2%"},
		},
		Diagnostics: map[string]interface{}{"trafficBudget": status},
	}
	localized := LocalizeHealthReport(report, "en")
	if localized == report {
		t.Fatal("localized report should be a copy")
	}
	if localized.Checks[0].Title != "VPS traffic" || !strings.Contains(localized.Checks[0].Summary, "Warning threshold reached") {
		t.Fatalf("traffic check was not localized: %#v", localized.Checks[0])
	}
	if localized.Checks[1].Title != "System disk" || localized.Checks[1].Summary != "Used 91.2%" {
		t.Fatalf("disk check was not localized: %#v", localized.Checks[1])
	}
	if report.Checks[0].Title != "VPS 总流量" {
		t.Fatal("source report was mutated")
	}
}

func TestLocalizeHealthReportKeepsChineseDefaultAndTranslatesTraditional(t *testing.T) {
	report := &HealthReport{Checks: []HealthCheck{{ID: "database", Title: "数据库", Status: "ok", Summary: "SQLite quick_check 通过"}}}
	if got := LocalizeHealthReport(report, "zhHans"); got != report {
		t.Fatal("Simplified Chinese should use the native health report")
	}
	traditional := LocalizeHealthReport(report, "zhHant")
	if traditional.Checks[0].Title != "資料庫" || traditional.Checks[0].Summary != "正常" {
		t.Fatalf("unexpected Traditional Chinese health check: %#v", traditional.Checks[0])
	}
}
