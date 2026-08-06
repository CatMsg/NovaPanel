package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database"
	"github.com/CatMsg/NovaPanel/database/model"
)

func TestGetStatsDownsamplesInDatabase(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "stats.db")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	stats := make([]model.Stats, 0, 240)
	for index := 0; index < 120; index++ {
		for _, direction := range []bool{false, true} {
			stats = append(stats, model.Stats{
				DateTime:  now - int64(120-index),
				Resource:  "inbound",
				Tag:       "test-inbound",
				Direction: direction,
				Traffic:   int64(index + 1),
			})
		}
	}
	if err := database.GetDB().Create(&stats).Error; err != nil {
		t.Fatal(err)
	}

	result, err := (&StatsService{}).GetStats("inbound", "test-inbound", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) == 0 || len(result) > 60 {
		t.Fatalf("downsampled rows = %d, want 1..60", len(result))
	}
	for index, stat := range result {
		if stat.Traffic <= 0 {
			t.Fatalf("row %d traffic = %d, want positive average", index, stat.Traffic)
		}
		if index > 0 && stat.DateTime < result[index-1].DateTime {
			t.Fatalf("rows are not ordered at index %d", index)
		}
	}
}

func TestGetStatsReturnsSmallResultWithoutAggregation(t *testing.T) {
	if err := database.InitDB(filepath.Join(t.TempDir(), "small-stats.db")); err != nil {
		t.Fatal(err)
	}
	now := time.Now().Unix()
	stats := []model.Stats{
		{DateTime: now - 1, Resource: "user", Tag: "alice", Traffic: 7},
		{DateTime: now - 2, Resource: "user", Tag: "alice", Traffic: 3},
	}
	if err := database.GetDB().Create(&stats).Error; err != nil {
		t.Fatal(err)
	}

	result, err := (&StatsService{}).GetStats("user", "alice", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || result[0].Traffic != 3 || result[1].Traffic != 7 {
		t.Fatalf("unexpected small result: %#v", result)
	}
}
