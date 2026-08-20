package core

import (
	"context"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/database/model"
)

func TestUserRateLimitTrackerSharesLimitAndSeparatesDirections(t *testing.T) {
	tracker := NewUserRateLimitTracker()
	tracker.SetLimits([]model.Client{{
		Name: "alice", Enable: true, UploadLimit: 10_000, DownloadLimit: 10_000,
	}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	if err := tracker.WaitUpload(ctx, "alice", 1500); err != nil {
		t.Fatalf("consume upload burst: %v", err)
	}
	start := time.Now()
	if err := tracker.WaitUpload(ctx, "alice", 1000); err != nil {
		t.Fatalf("wait for shared upload limit: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 70*time.Millisecond {
		t.Fatalf("upload bucket was not shared, wait lasted only %v", elapsed)
	}

	start = time.Now()
	if err := tracker.WaitDownload(ctx, "alice", 1000); err != nil {
		t.Fatalf("wait for download limit: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 40*time.Millisecond {
		t.Fatalf("download unexpectedly reused upload bucket: %v", elapsed)
	}
}

func TestUserRateLimitTrackerHotReloadRemovesLimit(t *testing.T) {
	tracker := NewUserRateLimitTracker()
	tracker.SetLimits([]model.Client{{Name: "alice", UploadLimit: 1000}})
	if tracker.limiter("alice", true) == nil {
		t.Fatal("expected upload limiter")
	}

	tracker.SetLimits([]model.Client{{Name: "alice"}})
	if tracker.limiter("alice", true) != nil {
		t.Fatal("zero limit should remove the limiter")
	}
}
