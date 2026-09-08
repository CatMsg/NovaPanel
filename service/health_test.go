package service

import "testing"

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
