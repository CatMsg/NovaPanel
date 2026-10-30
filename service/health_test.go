package service

import (
	"errors"
	"strings"
	"testing"
)

func TestHealthReportSummary(t *testing.T) {
	report := &HealthReport{Summary: map[string]int{"ok": 0, "warning": 0, "error": 0, "info": 0}}
	report.add(HealthCheck{ID: "one", Status: "ok"})
	report.add(HealthCheck{ID: "two", Status: "error"})
	report.add(HealthCheck{ID: "three", Status: "unexpected"})

	if report.Summary["ok"] != 1 || report.Summary["error"] != 1 || report.Summary["info"] != 1 {
		t.Fatalf("unexpected health summary: %+v", report.Summary)
	}
	if report.Checks[2].Status != "info" {
		t.Fatalf("unexpected normalized status: %+v", report.Checks[2])
	}
}

func TestHealthWeightOrdersFailuresFirst(t *testing.T) {
	if healthWeight("error") <= healthWeight("warning") || healthWeight("warning") <= healthWeight("ok") {
		t.Fatal("health status weights do not prioritize failures")
	}
}

func TestPanelHostHealthCheck(t *testing.T) {
	tests := []struct {
		name       string
		domain     string
		listen     string
		domainErr  error
		listenErr  error
		status     string
		summaryHas string
	}{
		{name: "configured domain enables allowlist", domain: "panel.example", listen: "", status: "ok", summaryHas: "Host 白名单"},
		{name: "unset domain on wildcard listener warns", listen: "", status: "warning", summaryHas: "Host 白名单未启用"},
		{name: "unset domain on loopback is informational", listen: "127.0.0.1", status: "info", summaryHas: "仅限本机"},
		{name: "read failure warns", listenErr: errors.New("read failed"), status: "warning", summaryHas: "无法读取"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			check := panelHostHealthCheck(test.domain, test.listen, test.domainErr, test.listenErr)
			if check.Status != test.status || !strings.Contains(check.Summary, test.summaryHas) {
				t.Fatalf("check = %+v; want status %q and summary containing %q", check, test.status, test.summaryHas)
			}
			if test.name == "unset domain on wildcard listener warns" {
				localized := LocalizeHealthReport(&HealthReport{Checks: []HealthCheck{check}}, "en")
				if !strings.Contains(localized.Checks[0].Summary, "Host allowlist is disabled") {
					t.Fatalf("Host warning was not localized: %q", localized.Checks[0].Summary)
				}
			}
		})
	}
}

func TestTLSDisabledHealthCheckAccountsForListenerExposure(t *testing.T) {
	for _, test := range []struct {
		listen string
		status string
	}{
		{listen: "", status: "warning"},
		{listen: "0.0.0.0", status: "warning"},
		{listen: "::", status: "warning"},
		{listen: "192.0.2.10", status: "warning"},
		{listen: "localhost", status: "info"},
		{listen: "127.0.0.1", status: "info"},
		{listen: "::1", status: "info"},
	} {
		check := tlsDisabledHealthCheck("panel-tls", "面板 TLS", test.listen, nil)
		if check.Status != test.status {
			t.Errorf("listener %q status = %q, want %q", test.listen, check.Status, test.status)
		}
		if test.status != "warning" {
			continue
		}
		localized := LocalizeHealthReport(&HealthReport{Checks: []HealthCheck{check}}, "en")
		if !strings.Contains(localized.Checks[0].Summary, "HTTPS reverse-proxy") {
			t.Errorf("listener %q warning was not localized with remediation guidance: %q", test.listen, localized.Checks[0].Summary)
		}
	}

	check := tlsDisabledHealthCheck("subscription-tls", "订阅 TLS", "", errors.New("synthetic listen error"))
	if check.Status != "warning" {
		t.Fatalf("unreadable listener status = %q, want warning", check.Status)
	}
	localized := LocalizeHealthReport(&HealthReport{Checks: []HealthCheck{check}}, "en")
	if !strings.Contains(localized.Checks[0].Summary, "Cannot read the listener address") {
		t.Fatalf("unreadable listener warning was not localized: %q", localized.Checks[0].Summary)
	}
}
