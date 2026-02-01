package rpc

import (
	"encoding/json"
	"testing"
	"time"
)

func TestQueryCache_BasicOperations(t *testing.T) {
	cache := NewQueryCache(100*time.Millisecond, 10)

	// Test cache miss
	resp, ok := cache.Get("nonexistent")
	if ok {
		t.Error("expected cache miss for nonexistent key")
	}

	// Test cache set and hit
	testResp := Response{Success: true, Data: json.RawMessage(`{"test": true}`)}
	cache.Set("key1", testResp)

	resp, ok = cache.Get("key1")
	if !ok {
		t.Error("expected cache hit for key1")
	}
	if !resp.Success {
		t.Error("expected success response")
	}

	// Verify stats
	stats := cache.Stats()
	if stats.Hits != 1 {
		t.Errorf("expected 1 hit, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", stats.Misses)
	}
}

func TestQueryCache_TTLExpiry(t *testing.T) {
	cache := NewQueryCache(50*time.Millisecond, 10)

	testResp := Response{Success: true, Data: json.RawMessage(`{"test": true}`)}
	cache.Set("key1", testResp)

	// Should hit immediately
	_, ok := cache.Get("key1")
	if !ok {
		t.Error("expected cache hit before expiry")
	}

	// Wait for expiry
	time.Sleep(60 * time.Millisecond)

	// Should miss after expiry
	_, ok = cache.Get("key1")
	if ok {
		t.Error("expected cache miss after expiry")
	}
}

func TestQueryCache_Invalidation(t *testing.T) {
	cache := NewQueryCache(10*time.Second, 10)

	// Set multiple entries
	testResp := Response{Success: true, Data: json.RawMessage(`{"test": true}`)}
	cache.Set("key1", testResp)
	cache.Set("key2", testResp)
	cache.Set("key3", testResp)

	// Verify entries exist
	if _, ok := cache.Get("key1"); !ok {
		t.Error("expected key1 to exist")
	}
	if _, ok := cache.Get("key2"); !ok {
		t.Error("expected key2 to exist")
	}

	// Invalidate all
	cache.Invalidate()

	// Verify all entries are gone
	if _, ok := cache.Get("key1"); ok {
		t.Error("expected key1 to be invalidated")
	}
	if _, ok := cache.Get("key2"); ok {
		t.Error("expected key2 to be invalidated")
	}
	if _, ok := cache.Get("key3"); ok {
		t.Error("expected key3 to be invalidated")
	}
}

func TestQueryCache_MaxSize(t *testing.T) {
	cache := NewQueryCache(10*time.Second, 3)

	testResp := Response{Success: true, Data: json.RawMessage(`{"test": true}`)}

	// Fill cache to max
	cache.Set("key1", testResp)
	cache.Set("key2", testResp)
	cache.Set("key3", testResp)

	stats := cache.Stats()
	if stats.Entries != 3 {
		t.Errorf("expected 3 entries, got %d", stats.Entries)
	}

	// Add one more - should evict oldest
	time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	cache.Set("key4", testResp)

	stats = cache.Stats()
	if stats.Entries > 3 {
		t.Errorf("expected max 3 entries, got %d", stats.Entries)
	}

	// key4 should exist
	if _, ok := cache.Get("key4"); !ok {
		t.Error("expected key4 to exist")
	}
}

func TestQueryCache_CacheableOperations(t *testing.T) {
	cache := NewQueryCache(10*time.Second, 10)

	// Read operations should be cacheable
	cacheableOps := []string{OpList, OpShow, OpCount, OpReady, OpBlocked}
	for _, op := range cacheableOps {
		if !cache.IsCacheable(op) {
			t.Errorf("expected %s to be cacheable", op)
		}
	}

	// Write operations should not be cacheable
	nonCacheableOps := []string{OpCreate, OpUpdate, OpDelete, OpClose}
	for _, op := range nonCacheableOps {
		if cache.IsCacheable(op) {
			t.Errorf("expected %s to NOT be cacheable", op)
		}
	}
}

func TestQueryCache_MakeKey(t *testing.T) {
	cache := NewQueryCache(10*time.Second, 10)

	// Same operation and args should produce same key
	args := json.RawMessage(`{"status": "open"}`)
	key1 := cache.MakeKey(OpList, args)
	key2 := cache.MakeKey(OpList, args)

	if key1 != key2 {
		t.Errorf("same inputs should produce same key: %s != %s", key1, key2)
	}

	// Different args should produce different key
	args2 := json.RawMessage(`{"status": "closed"}`)
	key3 := cache.MakeKey(OpList, args2)

	if key1 == key3 {
		t.Errorf("different args should produce different keys: %s == %s", key1, key3)
	}

	// Different operation should produce different key
	key4 := cache.MakeKey(OpCount, args)

	if key1 == key4 {
		t.Errorf("different operations should produce different keys: %s == %s", key1, key4)
	}
}

func TestQueryCache_DoesNotCacheErrors(t *testing.T) {
	cache := NewQueryCache(10*time.Second, 10)

	// Error responses should not be cached
	errorResp := Response{Success: false, Error: "test error"}
	cache.Set("error_key", errorResp)

	_, ok := cache.Get("error_key")
	if ok {
		t.Error("error responses should not be cached")
	}
}

func TestQueryCache_DisabledByEnv(t *testing.T) {
	// This tests the disabled behavior
	cache := &QueryCache{enabled: false}

	if cache.IsCacheable(OpList) {
		t.Error("disabled cache should not report operations as cacheable")
	}

	testResp := Response{Success: true, Data: json.RawMessage(`{"test": true}`)}
	cache.Set("key1", testResp)

	_, ok := cache.Get("key1")
	if ok {
		t.Error("disabled cache should not return cached values")
	}
}
