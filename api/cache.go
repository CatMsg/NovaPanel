package api

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/CatMsg/NovaPanel/service"
)

const loadDataCacheTTL = 3 * time.Second

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
	version := service.LastUpdate

	loadDataCache.mu.RLock()
	entry, ok := loadDataCache.entries[key]
	loadDataCache.mu.RUnlock()
	if !ok || entry.version != version || now.After(entry.expiresAt) {
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
	loadDataCache.entries[key] = apiCacheEntry{
		expiresAt: time.Now().Add(loadDataCacheTTL),
		version:   service.LastUpdate,
		payload:   payload,
	}
	loadDataCache.mu.Unlock()
	return nil
}
