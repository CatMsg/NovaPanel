package service

import (
	"context"
	"errors"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/CatMsg/NovaPanel/core"
)

func TestOutboundHealthRollingWindow(t *testing.T) {
	tracker := newOutboundHealthTracker(3, nil)
	start := time.Date(2026, time.September, 11, 10, 0, 0, 0, time.FixedZone("CST", 8*60*60))
	tracker.record("tokyo", core.CheckOutboundResult{OK: true, Delay: 100}, start)
	tracker.record("tokyo", core.CheckOutboundResult{Error: "timeout"}, start.Add(time.Minute))
	tracker.record("tokyo", core.CheckOutboundResult{OK: true, Delay: 200}, start.Add(2*time.Minute))
	tracker.record("tokyo", core.CheckOutboundResult{OK: true, Delay: 300}, start.Add(3*time.Minute))

	snapshot := tracker.snapshots([]string{"tokyo"})[0]
	if snapshot.Status != "healthy" || snapshot.LatestDelay != 300 {
		t.Fatalf("unexpected latest state: %#v", snapshot)
	}
	if snapshot.Samples != 3 || snapshot.Successes != 2 || snapshot.Failures != 1 {
		t.Fatalf("unexpected rolling counts: %#v", snapshot)
	}
	if snapshot.AverageDelay != 250 {
		t.Fatalf("unexpected successful average: %v", snapshot.AverageDelay)
	}
	if math.Abs(snapshot.ObservedAvailability-66.6666666667) > 0.0001 {
		t.Fatalf("unexpected observed availability: %v", snapshot.ObservedAvailability)
	}
	if snapshot.LastSuccess != start.Add(3*time.Minute).UTC().Format(time.RFC3339Nano) {
		t.Fatalf("unexpected last success: %q", snapshot.LastSuccess)
	}
	if snapshot.LastError != "timeout" {
		t.Fatalf("last error should retain the most recent failure: %q", snapshot.LastError)
	}
}

func TestOutboundHealthFailureAndUntestedState(t *testing.T) {
	tracker := newOutboundHealthTracker(4, nil)
	now := time.Date(2026, time.September, 11, 2, 0, 0, 0, time.UTC)
	tracker.record("failed", core.CheckOutboundResult{Error: "connection refused"}, now)

	snapshots := tracker.snapshots([]string{"failed", "untested"})
	byTag := make(map[string]OutboundHealthSnapshot, len(snapshots))
	for _, snapshot := range snapshots {
		byTag[snapshot.Tag] = snapshot
	}
	if byTag["failed"].Status != "unhealthy" || byTag["failed"].LastError != "connection refused" {
		t.Fatalf("unexpected failure snapshot: %#v", byTag["failed"])
	}
	if byTag["untested"].Status != "untested" || byTag["untested"].Samples != 0 {
		t.Fatalf("unexpected untested snapshot: %#v", byTag["untested"])
	}
}

func TestOutboundIdentityRefreshDeduplicatesAndCaches(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	tracker := newOutboundHealthTracker(8, func(context.Context, string) (core.OutboundIdentity, error) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		return core.OutboundIdentity{PublicIP: "203.0.113.9", CountryCode: "US", Colo: "SJC"}, nil
	})
	now := time.Date(2026, time.September, 11, 2, 0, 0, 0, time.UTC)
	tracker.now = func() time.Time { return now }

	tracker.record("primary", core.CheckOutboundResult{OK: true, Delay: 20}, now)
	<-started
	tracker.record("primary", core.CheckOutboundResult{OK: true, Delay: 21}, now)
	if calls.Load() != 1 {
		t.Fatalf("concurrent refresh was not deduplicated: %d calls", calls.Load())
	}
	close(release)
	waitForIdentityState(t, tracker, "primary", func(entry *outboundHealthEntry) bool {
		return !entry.identityInFlight && entry.identity.PublicIP != ""
	})

	tracker.record("primary", core.CheckOutboundResult{OK: true, Delay: 22}, now.Add(14*time.Minute))
	time.Sleep(10 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("cached identity refreshed too early: %d calls", calls.Load())
	}
	identity := tracker.snapshots([]string{"primary"})[0]
	if identity.PublicIP != "203.0.113.9" || identity.CountryCode != "US" || identity.Colo != "SJC" {
		t.Fatalf("unexpected cached identity: %#v", identity)
	}
}

func TestOutboundIdentityFailureBackoff(t *testing.T) {
	var calls atomic.Int32
	now := time.Date(2026, time.September, 11, 2, 0, 0, 0, time.UTC)
	tracker := newOutboundHealthTracker(8, func(context.Context, string) (core.OutboundIdentity, error) {
		calls.Add(1)
		return core.OutboundIdentity{}, errors.New("metadata unavailable")
	})
	tracker.now = func() time.Time { return now }
	tracker.backoffBase = time.Minute
	tracker.backoffMaximum = 4 * time.Minute

	tracker.record("backup", core.CheckOutboundResult{OK: true, Delay: 30}, now)
	waitForIdentityState(t, tracker, "backup", func(entry *outboundHealthEntry) bool {
		return !entry.identityInFlight && entry.identityFailures == 1
	})
	tracker.record("backup", core.CheckOutboundResult{OK: true, Delay: 31}, now.Add(30*time.Second))
	time.Sleep(10 * time.Millisecond)
	if calls.Load() != 1 {
		t.Fatalf("identity retry ignored backoff: %d calls", calls.Load())
	}

	now = now.Add(time.Minute)
	tracker.record("backup", core.CheckOutboundResult{OK: true, Delay: 32}, now)
	waitForIdentityState(t, tracker, "backup", func(entry *outboundHealthEntry) bool {
		return !entry.identityInFlight && entry.identityFailures == 2
	})
	tracker.mu.RLock()
	nextTry := tracker.entries["backup"].nextIdentityTry
	tracker.mu.RUnlock()
	if calls.Load() != 2 || !nextTry.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("unexpected exponential backoff: calls=%d next=%v", calls.Load(), nextTry)
	}
}

func waitForIdentityState(t *testing.T, tracker *outboundHealthTracker, tag string, ready func(*outboundHealthEntry) bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		tracker.mu.RLock()
		entry := tracker.entries[tag]
		matched := entry != nil && ready(entry)
		tracker.mu.RUnlock()
		if matched {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("timed out waiting for identity refresh")
}
