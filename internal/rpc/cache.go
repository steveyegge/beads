package rpc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// QueryCache provides in-memory caching for read query results.
// It uses a simple hash of operation + args as the cache key and
// invalidates all entries on any write operation for safety.
type QueryCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
	maxSize int
	enabled bool

	// Metrics
	hits   int64
	misses int64
}

type cacheEntry struct {
	response  Response
	timestamp time.Time
}

// CacheableOperations defines which operations can be cached.
// Only read operations that don't have side effects should be cached.
var CacheableOperations = map[string]bool{
	OpList:          true,
	OpShow:          true,
	OpCount:         true,
	OpReady:         true,
	OpBlocked:       true,
	OpStale:         true,
	OpStats:         true,
	OpCommentList:   true,
	OpEpicStatus:    true,
	OpDecisionGet:   true,
	OpDecisionList:  true,
	OpGateList:      true,
	OpGateShow:      true,
	OpResolveID:     true,
}

// NewQueryCache creates a new query cache with the given TTL.
// If ttl is 0, uses the default of 10 seconds.
// The cache can be disabled by setting BEADS_CACHE_DISABLE=1.
func NewQueryCache(ttl time.Duration, maxSize int) *QueryCache {
	if ttl == 0 {
		ttl = 10 * time.Second // Default TTL
	}
	if maxSize == 0 {
		maxSize = 1000 // Default max entries
	}

	enabled := true
	if os.Getenv("BEADS_CACHE_DISABLE") == "1" {
		enabled = false
	}

	return &QueryCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
		enabled: enabled,
	}
}

// IsCacheable returns true if the operation can be cached.
func (c *QueryCache) IsCacheable(op string) bool {
	if !c.enabled {
		return false
	}
	return CacheableOperations[op]
}

// MakeKey creates a cache key from the operation and args.
// The key is a SHA256 hash of the operation + JSON-serialized args.
func (c *QueryCache) MakeKey(op string, args json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(op))
	h.Write([]byte(":"))
	h.Write(args)
	return hex.EncodeToString(h.Sum(nil))[:16] // Use first 16 chars for shorter keys
}

// Get retrieves a cached response if it exists and is not expired.
// Returns the response and true if found, or an empty response and false if not.
func (c *QueryCache) Get(key string) (Response, bool) {
	if !c.enabled {
		return Response{}, false
	}

	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return Response{}, false
	}

	// Check if expired
	if time.Since(entry.timestamp) > c.ttl {
		c.mu.Lock()
		delete(c.entries, key)
		c.misses++
		c.mu.Unlock()
		return Response{}, false
	}

	c.mu.Lock()
	c.hits++
	c.mu.Unlock()

	return entry.response, true
}

// Set stores a response in the cache.
// If the cache is full, it evicts expired entries or the oldest entry.
func (c *QueryCache) Set(key string, resp Response) {
	if !c.enabled {
		return
	}

	// Don't cache error responses
	if !resp.Success {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// If at max size, try to evict expired entries first
	if len(c.entries) >= c.maxSize {
		c.evictExpiredLocked()
	}

	// If still at max, evict oldest entry
	if len(c.entries) >= c.maxSize {
		c.evictOldestLocked()
	}

	c.entries[key] = &cacheEntry{
		response:  resp,
		timestamp: time.Now(),
	}
}

// Invalidate clears all cache entries.
// Called when any write operation occurs.
func (c *QueryCache) Invalidate() {
	if !c.enabled {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Clear all entries - simple and safe invalidation strategy
	c.entries = make(map[string]*cacheEntry)
}

// Stats returns cache statistics.
func (c *QueryCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return CacheStats{
		Entries:  len(c.entries),
		MaxSize:  c.maxSize,
		TTL:      c.ttl.String(),
		Hits:     c.hits,
		Misses:   c.misses,
		HitRatio: c.hitRatio(),
		Enabled:  c.enabled,
	}
}

// CacheStats contains cache performance metrics.
type CacheStats struct {
	Entries  int     `json:"entries"`
	MaxSize  int     `json:"max_size"`
	TTL      string  `json:"ttl"`
	Hits     int64   `json:"hits"`
	Misses   int64   `json:"misses"`
	HitRatio float64 `json:"hit_ratio"`
	Enabled  bool    `json:"enabled"`
}

func (c *QueryCache) hitRatio() float64 {
	total := c.hits + c.misses
	if total == 0 {
		return 0
	}
	return float64(c.hits) / float64(total)
}

// evictExpiredLocked removes expired entries. Must be called with lock held.
func (c *QueryCache) evictExpiredLocked() {
	now := time.Now()
	for key, entry := range c.entries {
		if now.Sub(entry.timestamp) > c.ttl {
			delete(c.entries, key)
		}
	}
}

// evictOldestLocked removes the oldest entry. Must be called with lock held.
func (c *QueryCache) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	first := true

	for key, entry := range c.entries {
		if first || entry.timestamp.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.timestamp
			first = false
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

// String returns a human-readable description of the cache.
func (c *QueryCache) String() string {
	stats := c.Stats()
	return fmt.Sprintf("QueryCache{entries=%d, hits=%d, misses=%d, ratio=%.2f, enabled=%v}",
		stats.Entries, stats.Hits, stats.Misses, stats.HitRatio, stats.Enabled)
}
