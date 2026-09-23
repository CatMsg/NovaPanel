package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestRecordTrafficBudgetSampleUpsertsFiveMinuteBucket(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "traffic-history.db")); err != nil {
		t.Fatal(err)
	}
	service := &TrafficBudgetService{}
	period := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	first := TrafficBudgetStatus{
		Enabled: true, Supported: true, MeteredRxBytes: 100, MeteredTxBytes: 200,
		UsedBytes: 200, ClientPoolBytes: 1000, Level: "normal",
	}
	if err := service.recordTrafficBudgetSample(first, period, period.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	second := first
	second.MeteredRxBytes = 150
	second.MeteredTxBytes = 350
	second.UsedBytes = 350
	second.Level = "warning"
	if err := service.recordTrafficBudgetSample(second, period, period.Add(4*time.Minute)); err != nil {
		t.Fatal(err)
	}

	var rows []model.TrafficBudgetSample
	if err := database.GetDB().Order("date_time ASC").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("sample rows = %d, want 1", len(rows))
	}
	if rows[0].UsedBytes != 350 || rows[0].MeteredTxBytes != 350 || rows[0].Level != "warning" {
		t.Fatalf("bucket was not updated with latest snapshot: %#v", rows[0])
	}
}

func TestDownsampleTrafficBudgetSamplesKeepsNewestPointPerBucket(t *testing.T) {
	samples := make([]model.TrafficBudgetSample, 10)
	for i := range samples {
		samples[i] = model.TrafficBudgetSample{DateTime: int64(i + 1), UsedBytes: uint64((i + 1) * 100)}
	}
	got := downsampleTrafficBudgetSamples(samples, 3)
	if len(got) != 3 {
		t.Fatalf("downsample length = %d, want 3", len(got))
	}
	if got[0].DateTime != 3 || got[1].DateTime != 6 || got[2].DateTime != 10 {
		t.Fatalf("unexpected downsample points: %#v", got)
	}
}
