package service

import (
	"sync"
	"time"
)

type statusCacheEntry struct {
	expiresAt time.Time
	value     interface{}
}

var serverStatusCache = struct {
	mu      sync.RWMutex
	entries map[string]statusCacheEntry
}{
	entries: make(map[string]statusCacheEntry),
}

var statusCacheTTL = map[string]time.Duration{
	"cpu":      2 * time.Second,
	"mem":      2 * time.Second,
	"net":      2 * time.Second,
	"dio":      2 * time.Second,
	"dsk":      10 * time.Second,
	"swp":      10 * time.Second,
	"sbd":      2 * time.Second,
	"sys":      15 * time.Second,
	"db":       15 * time.Second,
	"ports":    5 * time.Second,
	"publicip": 5 * time.Minute,
}

func getCachedStatusValue(key string, loader func() interface{}) interface{} {
	ttl := statusCacheTTL[key]
	if ttl <= 0 {
		return loader()
	}

	now := time.Now()
	serverStatusCache.mu.RLock()
	entry, ok := serverStatusCache.entries[key]
	serverStatusCache.mu.RUnlock()
	if ok && now.Before(entry.expiresAt) {
		return entry.value
	}

	value := loader()

	serverStatusCache.mu.Lock()
	serverStatusCache.entries[key] = statusCacheEntry{
		expiresAt: now.Add(ttl),
		value:     value,
	}
	serverStatusCache.mu.Unlock()

	return value
}
