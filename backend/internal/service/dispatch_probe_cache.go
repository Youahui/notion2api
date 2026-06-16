package service

import (
	"strings"
	"sync"
	"time"
)

type dispatchProbeCacheEntry struct {
	lastChecked time.Time
	lastOK      bool
}

type dispatchProbeCache struct {
	mu      sync.Mutex
	entries map[string]dispatchProbeCacheEntry
}

func newDispatchProbeCache() *dispatchProbeCache {
	return &dispatchProbeCache{entries: map[string]dispatchProbeCacheEntry{}}
}

func (c *dispatchProbeCache) shouldProbe(accountKey string, ttl time.Duration, now time.Time) bool {
	if c == nil {
		return true
	}
	accountKey = strings.TrimSpace(strings.ToLower(accountKey))
	if accountKey == "" || ttl <= 0 {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[accountKey]
	if !ok || !entry.lastOK {
		return true
	}
	return now.Sub(entry.lastChecked) >= ttl
}

func (c *dispatchProbeCache) markSuccess(accountKey string, now time.Time) {
	if c == nil {
		return
	}
	accountKey = strings.TrimSpace(strings.ToLower(accountKey))
	if accountKey == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]dispatchProbeCacheEntry{}
	}
	c.entries[accountKey] = dispatchProbeCacheEntry{lastChecked: now, lastOK: true}
}

func (c *dispatchProbeCache) markFailure(accountKey string) {
	if c == nil {
		return
	}
	accountKey = strings.TrimSpace(strings.ToLower(accountKey))
	if accountKey == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, accountKey)
}
