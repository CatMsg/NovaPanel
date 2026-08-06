package api

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/service"
)

const (
	loadDataCacheTTL        = 3 * time.Second
	loadDataCacheMaxEntries = 64
)

type apiCacheEntry struct {
	expiresAt time.Time
	version   int64
	payload   []byte
}

var loadDataCache = struct {
	mu      sync.RWMutex
	entries map[string]apiCacheEntry
}{
	entries: make(map[string]apiCacheEntry),
}

func getCachedLoadData(key string) (map[string]interface{}, bool) {
	now := time.Now()
	version := service.CurrentDataVersion()

	loadDataCache.mu.RLock()
	entry, ok := loadDataCache.entries[key]
	loadDataCache.mu.RUnlock()
	if !ok || entry.version != version || now.After(entry.expiresAt) {
		if ok {
			loadDataCache.mu.Lock()
			if current, exists := loadDataCache.entries[key]; exists &&
				(current.version != version || now.After(current.expiresAt)) {
				delete(loadDataCache.entries, key)
			}
			loadDataCache.mu.Unlock()
		}
		return nil, false
	}

	var result map[string]interface{}
	if err := json.Unmarshal(entry.payload, &result); err != nil {
		return nil, false
	}
	return result, true
}

func storeCachedLoadData(key string, value map[string]interface{}) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}

	loadDataCache.mu.Lock()
	pruneLoadDataCache(time.Now())
	loadDataCache.entries[key] = apiCacheEntry{
		expiresAt: time.Now().Add(loadDataCacheTTL),
		version:   service.CurrentDataVersion(),
		payload:   payload,
	}
	loadDataCache.mu.Unlock()
	return nil
}

// pruneLoadDataCache removes expired entries and bounds the cache even when a
// reverse proxy forwards arbitrary Host headers. The caller must hold the lock.
func pruneLoadDataCache(now time.Time) {
	for key, entry := range loadDataCache.entries {
		if now.After(entry.expiresAt) {
			delete(loadDataCache.entries, key)
		}
	}
	for len(loadDataCache.entries) >= loadDataCacheMaxEntries {
		var oldestKey string
		var oldestExpiry time.Time
		for key, entry := range loadDataCache.entries {
			if oldestKey == "" || entry.expiresAt.Before(oldestExpiry) {
				oldestKey = key
				oldestExpiry = entry.expiresAt
			}
		}
		delete(loadDataCache.entries, oldestKey)
	}
}
