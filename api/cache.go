package api

import (
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/service"
)

const loadDataCacheTTL = 3 * time.Second

type apiCacheEntry struct {
	expiresAt time.Time
	version   int64
	value     map[string]interface{}
}

var loadDataCache = struct {
	mu      sync.RWMutex
	entries map[string]apiCacheEntry
}{
	entries: make(map[string]apiCacheEntry),
}

func getCachedLoadData(key string) (map[string]interface{}, bool) {
	now := time.Now()
	version := service.LastUpdate

	loadDataCache.mu.RLock()
	entry, ok := loadDataCache.entries[key]
	loadDataCache.mu.RUnlock()
	if !ok || entry.version != version || now.After(entry.expiresAt) {
		return nil, false
	}

	result := make(map[string]interface{}, len(entry.value))
	for k, v := range entry.value {
		result[k] = v
	}
	return result, true
}

func storeCachedLoadData(key string, value map[string]interface{}) {
	cloned := make(map[string]interface{}, len(value))
	for k, v := range value {
		cloned[k] = v
	}

	loadDataCache.mu.Lock()
	loadDataCache.entries[key] = apiCacheEntry{
		expiresAt: time.Now().Add(loadDataCacheTTL),
		version:   service.LastUpdate,
		value:     cloned,
	}
	loadDataCache.mu.Unlock()
}
