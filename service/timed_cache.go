package service

import (
	"sync"
	"time"
)

type timedCacheEntry struct {
	expiresAt time.Time
	version   int64
	value     interface{}
}

type timedCache struct {
	mu      sync.RWMutex
	entries map[string]timedCacheEntry
}

func newTimedCache() *timedCache {
	return &timedCache{
		entries: make(map[string]timedCacheEntry),
	}
}

func (c *timedCache) get(key string, version int64) (interface{}, bool) {
	now := time.Now()
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok || entry.version != version || now.After(entry.expiresAt) {
		return nil, false
	}
	return entry.value, true
}

func (c *timedCache) set(key string, version int64, ttl time.Duration, value interface{}) {
	c.mu.Lock()
	c.entries[key] = timedCacheEntry{
		expiresAt: time.Now().Add(ttl),
		version:   version,
		value:     value,
	}
	c.mu.Unlock()
}

func (c *timedCache) delete(key string) {
	c.mu.Lock()
	delete(c.entries, key)
	c.mu.Unlock()
}

func (c *timedCache) clear() {
	c.mu.Lock()
	c.entries = make(map[string]timedCacheEntry)
	c.mu.Unlock()
}
