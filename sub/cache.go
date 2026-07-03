package sub

import (
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/service"
)

const subCacheTTL = 8 * time.Second

type cachedResult struct {
	expiresAt time.Time
	version   int64
	body      string
	headers   []string
}

var subResultCache = struct {
	mu      sync.RWMutex
	entries map[string]cachedResult
}{
	entries: make(map[string]cachedResult),
}

func getCachedSubResult(key string) (*string, []string, bool) {
	now := time.Now()
	version := service.LastUpdate

	subResultCache.mu.RLock()
	entry, ok := subResultCache.entries[key]
	subResultCache.mu.RUnlock()
	if !ok || entry.version != version || now.After(entry.expiresAt) {
		return nil, nil, false
	}

	headers := append([]string(nil), entry.headers...)
	body := entry.body
	return &body, headers, true
}

func storeCachedSubResult(key string, body string, headers []string) {
	subResultCache.mu.Lock()
	subResultCache.entries[key] = cachedResult{
		expiresAt: time.Now().Add(subCacheTTL),
		version:   service.LastUpdate,
		body:      body,
		headers:   append([]string(nil), headers...),
	}
	subResultCache.mu.Unlock()
}
