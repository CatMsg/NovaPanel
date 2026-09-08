package api

import (
	"fmt"
	"testing"
)

func TestLoadDataCacheIsBounded(t *testing.T) {
	loadDataCache.mu.Lock()
	loadDataCache.entries = make(map[string]apiCacheEntry)
	loadDataCache.mu.Unlock()
	t.Cleanup(func() {
		loadDataCache.mu.Lock()
		loadDataCache.entries = make(map[string]apiCacheEntry)
		loadDataCache.mu.Unlock()
	})

	for index := 0; index < loadDataCacheMaxEntries*2; index++ {
		if err := storeCachedLoadData(fmt.Sprintf("host-%d", index), map[string]interface{}{"index": index}); err != nil {
			t.Fatalf("store cache entry: %v", err)
		}
	}

	loadDataCache.mu.RLock()
	count := len(loadDataCache.entries)
	loadDataCache.mu.RUnlock()
	if count > loadDataCacheMaxEntries {
		t.Fatalf("cache contains %d entries, want at most %d", count, loadDataCacheMaxEntries)
	}
}
