package service

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/core"
)

const (
	defaultOutboundHealthWindow    = 64
	defaultOutboundIdentityTTL     = 15 * time.Minute
	defaultOutboundIdentityBackoff = time.Minute
	maximumOutboundIdentityBackoff = 10 * time.Minute
	outboundIdentityRefreshTimeout = 10 * time.Second
)

type OutboundHealthSnapshot struct {
	Tag                  string  `json:"tag"`
	Status               string  `json:"status"`
	LatestDelay          uint16  `json:"latestDelay"`
	AverageDelay         float64 `json:"averageDelay"`
	ObservedAvailability float64 `json:"observedAvailability"`
	Samples              int     `json:"samples"`
	Successes            int     `json:"successes"`
	Failures             int     `json:"failures"`
	LastChecked          string  `json:"lastChecked,omitempty"`
	LastSuccess          string  `json:"lastSuccess,omitempty"`
	LastError            string  `json:"lastError,omitempty"`
	PublicIP             string  `json:"publicIp,omitempty"`
	CountryCode          string  `json:"countryCode,omitempty"`
	Colo                 string  `json:"colo,omitempty"`
	IdentityUpdatedAt    string  `json:"identityUpdatedAt,omitempty"`
}

type outboundObservation struct {
	ok        bool
	delay     uint16
	checkedAt time.Time
}

type outboundHealthEntry struct {
	observations      []outboundObservation
	lastSuccess       time.Time
	lastError         string
	identity          core.OutboundIdentity
	identityUpdatedAt time.Time
	identityExpiresAt time.Time
	identityInFlight  bool
	identityFailures  int
	nextIdentityTry   time.Time
}

type outboundIdentityFetcher func(context.Context, string) (core.OutboundIdentity, error)

type outboundHealthTracker struct {
	mu             sync.RWMutex
	entries        map[string]*outboundHealthEntry
	windowSize     int
	identityTTL    time.Duration
	backoffBase    time.Duration
	backoffMaximum time.Duration
	now            func() time.Time
	fetchIdentity  outboundIdentityFetcher
}

func newOutboundHealthTracker(windowSize int, fetcher outboundIdentityFetcher) *outboundHealthTracker {
	if windowSize < 1 {
		windowSize = defaultOutboundHealthWindow
	}
	return &outboundHealthTracker{
		entries:        make(map[string]*outboundHealthEntry),
		windowSize:     windowSize,
		identityTTL:    defaultOutboundIdentityTTL,
		backoffBase:    defaultOutboundIdentityBackoff,
		backoffMaximum: maximumOutboundIdentityBackoff,
		now:            time.Now,
		fetchIdentity:  fetcher,
	}
}

func fetchTrackedOutboundIdentity(ctx context.Context, tag string) (core.OutboundIdentity, error) {
	startCoreMu.Lock()
	defer startCoreMu.Unlock()
	if corePtr == nil || !corePtr.IsRunning() {
		return core.OutboundIdentity{}, errors.New("core not running")
	}
	return core.FetchOutboundIdentity(ctx, tag)
}

var sharedOutboundHealth = newOutboundHealthTracker(defaultOutboundHealthWindow, fetchTrackedOutboundIdentity)

func (t *outboundHealthTracker) record(tag string, result core.CheckOutboundResult, checkedAt time.Time) {
	if tag == "" {
		return
	}
	t.mu.Lock()
	entry := t.entryLocked(tag)
	entry.observations = append(entry.observations, outboundObservation{
		ok:        result.OK,
		delay:     result.Delay,
		checkedAt: checkedAt,
	})
	if overflow := len(entry.observations) - t.windowSize; overflow > 0 {
		copy(entry.observations, entry.observations[overflow:])
		entry.observations = entry.observations[:t.windowSize]
	}
	if result.OK {
		entry.lastSuccess = checkedAt
	} else {
		entry.lastError = result.Error
	}
	refreshIdentity := result.OK && t.markIdentityRefreshLocked(entry, checkedAt)
	t.mu.Unlock()

	if refreshIdentity {
		go t.refreshIdentity(tag)
	}
}

func (t *outboundHealthTracker) entryLocked(tag string) *outboundHealthEntry {
	entry := t.entries[tag]
	if entry == nil {
		entry = &outboundHealthEntry{}
		t.entries[tag] = entry
	}
	return entry
}

func (t *outboundHealthTracker) markIdentityRefreshLocked(entry *outboundHealthEntry, now time.Time) bool {
	if t.fetchIdentity == nil || entry.identityInFlight || now.Before(entry.nextIdentityTry) {
		return false
	}
	if entry.identity.PublicIP != "" && now.Before(entry.identityExpiresAt) {
		return false
	}
	entry.identityInFlight = true
	return true
}

func (t *outboundHealthTracker) refreshIdentity(tag string) {
	ctx, cancel := context.WithTimeout(context.Background(), outboundIdentityRefreshTimeout)
	identity, err := t.fetchIdentity(ctx, tag)
	cancel()
	now := t.now()

	t.mu.Lock()
	defer t.mu.Unlock()
	entry := t.entryLocked(tag)
	entry.identityInFlight = false
	if err == nil && identity.PublicIP != "" {
		entry.identity = identity
		entry.identityUpdatedAt = now
		entry.identityExpiresAt = now.Add(t.identityTTL)
		entry.identityFailures = 0
		entry.nextIdentityTry = time.Time{}
		return
	}
	entry.identityFailures++
	entry.nextIdentityTry = now.Add(t.identityBackoff(entry.identityFailures))
}

func (t *outboundHealthTracker) identityBackoff(failures int) time.Duration {
	backoff := t.backoffBase
	for attempt := 1; attempt < failures && backoff < t.backoffMaximum; attempt++ {
		backoff *= 2
		if backoff > t.backoffMaximum {
			return t.backoffMaximum
		}
	}
	return backoff
}

func (t *outboundHealthTracker) snapshots(tags []string) []OutboundHealthSnapshot {
	t.mu.RLock()
	allTags := make(map[string]struct{}, len(t.entries)+len(tags))
	if tags == nil {
		for tag := range t.entries {
			allTags[tag] = struct{}{}
		}
	}
	for _, tag := range tags {
		if tag != "" {
			allTags[tag] = struct{}{}
		}
	}
	result := make([]OutboundHealthSnapshot, 0, len(allTags))
	for tag := range allTags {
		result = append(result, snapshotFromEntry(tag, t.entries[tag]))
	}
	t.mu.RUnlock()
	sort.Slice(result, func(i, j int) bool { return result[i].Tag < result[j].Tag })
	return result
}

func snapshotFromEntry(tag string, entry *outboundHealthEntry) OutboundHealthSnapshot {
	snapshot := OutboundHealthSnapshot{Tag: tag, Status: "untested"}
	if entry == nil || len(entry.observations) == 0 {
		return snapshot
	}
	snapshot.Samples = len(entry.observations)
	var successfulDelay uint64
	for _, observation := range entry.observations {
		if observation.ok {
			snapshot.Successes++
			successfulDelay += uint64(observation.delay)
		} else {
			snapshot.Failures++
		}
	}
	latest := entry.observations[len(entry.observations)-1]
	if latest.ok {
		snapshot.Status = "healthy"
		snapshot.LatestDelay = latest.delay
	} else {
		snapshot.Status = "unhealthy"
	}
	if snapshot.Successes > 0 {
		snapshot.AverageDelay = float64(successfulDelay) / float64(snapshot.Successes)
	}
	snapshot.ObservedAvailability = float64(snapshot.Successes) * 100 / float64(snapshot.Samples)
	snapshot.LastChecked = latest.checkedAt.UTC().Format(time.RFC3339Nano)
	if !entry.lastSuccess.IsZero() {
		snapshot.LastSuccess = entry.lastSuccess.UTC().Format(time.RFC3339Nano)
	}
	snapshot.LastError = entry.lastError
	snapshot.PublicIP = entry.identity.PublicIP
	snapshot.CountryCode = entry.identity.CountryCode
	snapshot.Colo = entry.identity.Colo
	if !entry.identityUpdatedAt.IsZero() {
		snapshot.IdentityUpdatedAt = entry.identityUpdatedAt.UTC().Format(time.RFC3339Nano)
	}
	return snapshot
}

func OutboundHealthSnapshots(tags []string) []OutboundHealthSnapshot {
	return sharedOutboundHealth.snapshots(tags)
}
