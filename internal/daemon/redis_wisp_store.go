package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/steveyegge/beads/internal/types"
)

const (
	defaultNamespace = "bd"
	defaultWispTTL   = 24 * time.Hour
)

// RedisWispOption is a functional option for configuring the Redis wisp store.
type RedisWispOption func(*redisWispStore)

// WithNamespace sets the key namespace prefix for Redis keys.
func WithNamespace(ns string) RedisWispOption {
	return func(s *redisWispStore) {
		if ns != "" {
			s.namespace = ns
		}
	}
}

// WithTTL sets the TTL for wisp keys in Redis.
func WithTTL(ttl time.Duration) RedisWispOption {
	return func(s *redisWispStore) {
		if ttl > 0 {
			s.ttl = ttl
		}
	}
}

// redisWispStore implements WispStore using Redis as the backing store.
// All wisp data is stored as JSON-serialized types.Issue values with automatic
// TTL-based expiry via Redis EXPIRE.
type redisWispStore struct {
	client    *redis.Client
	namespace string
	ttl       time.Duration
	closed    atomic.Bool
}

// NewRedisWispStore creates a new Redis-backed wisp store.
// redisURL should be a valid Redis URL (e.g., "redis://localhost:6379/0").
// Returns an error if the connection cannot be established.
func NewRedisWispStore(redisURL string, opts ...RedisWispOption) (WispStore, error) {
	redisOpts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("invalid redis URL: %w", err)
	}

	client := redis.NewClient(redisOpts)

	s := &redisWispStore{
		client:    client,
		namespace: defaultNamespace,
		ttl:       defaultWispTTL,
	}

	for _, opt := range opts {
		opt(s)
	}

	// Verify connectivity
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return s, nil
}

// wispKey returns the Redis key for a wisp by ID.
func (s *redisWispStore) wispKey(id string) string {
	return s.namespace + ":wisp:" + id
}

// indexKey returns the Redis key for the wisp index set.
func (s *redisWispStore) indexKey() string {
	return s.namespace + ":wisp:index"
}

// Create adds a new wisp to the store.
func (s *redisWispStore) Create(ctx context.Context, issue *types.Issue) error {
	if s.closed.Load() {
		return fmt.Errorf("wisp store is closed")
	}

	if issue == nil {
		return fmt.Errorf("issue cannot be nil")
	}

	if issue.ID == "" {
		return fmt.Errorf("issue ID cannot be empty")
	}

	// Check for duplicate
	exists, err := s.client.Exists(ctx, s.wispKey(issue.ID)).Result()
	if err != nil {
		return fmt.Errorf("checking existence: %w", err)
	}
	if exists > 0 {
		return fmt.Errorf("wisp %s already exists", issue.ID)
	}

	// Clone to prevent mutation of caller's data
	clone := CloneIssue(issue)
	clone.Ephemeral = true

	now := time.Now()
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = now
	}
	if clone.UpdatedAt.IsZero() {
		clone.UpdatedAt = now
	}

	data, err := json.Marshal(clone)
	if err != nil {
		return fmt.Errorf("marshaling wisp: %w", err)
	}

	pipe := s.client.Pipeline()
	pipe.Set(ctx, s.wispKey(issue.ID), data, s.ttl)
	pipe.SAdd(ctx, s.indexKey(), issue.ID)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("creating wisp: %w", err)
	}

	return nil
}

// Get retrieves a wisp by ID. Returns nil, nil if not found.
func (s *redisWispStore) Get(ctx context.Context, id string) (*types.Issue, error) {
	if s.closed.Load() {
		return nil, fmt.Errorf("wisp store is closed")
	}

	data, err := s.client.Get(ctx, s.wispKey(id)).Bytes()
	if err == redis.Nil {
		// Not found - also clean up the index if it has a stale entry
		s.client.SRem(ctx, s.indexKey(), id)
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting wisp: %w", err)
	}

	var issue types.Issue
	if err := json.Unmarshal(data, &issue); err != nil {
		return nil, fmt.Errorf("unmarshaling wisp: %w", err)
	}

	return &issue, nil
}

// List returns wisps matching the filter.
func (s *redisWispStore) List(ctx context.Context, filter types.IssueFilter) ([]*types.Issue, error) {
	if s.closed.Load() {
		return nil, fmt.Errorf("wisp store is closed")
	}

	ids, err := s.client.SMembers(ctx, s.indexKey()).Result()
	if err != nil {
		return nil, fmt.Errorf("listing wisp index: %w", err)
	}

	if len(ids) == 0 {
		return nil, nil
	}

	// Build keys for MGET
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = s.wispKey(id)
	}

	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("fetching wisps: %w", err)
	}

	var results []*types.Issue
	var expiredIDs []interface{}

	for i, val := range values {
		if val == nil {
			// Key expired but still in index - collect for cleanup
			expiredIDs = append(expiredIDs, ids[i])
			continue
		}

		str, ok := val.(string)
		if !ok {
			continue
		}

		var issue types.Issue
		if err := json.Unmarshal([]byte(str), &issue); err != nil {
			continue
		}

		if MatchesFilter(&issue, filter) {
			results = append(results, &issue)
		}
	}

	// Clean up expired entries from index
	if len(expiredIDs) > 0 {
		s.client.SRem(ctx, s.indexKey(), expiredIDs...)
	}

	// Apply limit if specified
	if filter.Limit > 0 && len(results) > filter.Limit {
		results = results[:filter.Limit]
	}

	return results, nil
}

// Update modifies an existing wisp.
func (s *redisWispStore) Update(ctx context.Context, issue *types.Issue) error {
	if s.closed.Load() {
		return fmt.Errorf("wisp store is closed")
	}

	if issue == nil {
		return fmt.Errorf("issue cannot be nil")
	}

	if issue.ID == "" {
		return fmt.Errorf("issue ID cannot be empty")
	}

	exists, err := s.client.Exists(ctx, s.wispKey(issue.ID)).Result()
	if err != nil {
		return fmt.Errorf("checking existence: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("wisp %s not found", issue.ID)
	}

	clone := CloneIssue(issue)
	clone.Ephemeral = true
	clone.UpdatedAt = time.Now()

	data, err := json.Marshal(clone)
	if err != nil {
		return fmt.Errorf("marshaling wisp: %w", err)
	}

	if err := s.client.Set(ctx, s.wispKey(issue.ID), data, s.ttl).Err(); err != nil {
		return fmt.Errorf("updating wisp: %w", err)
	}

	return nil
}

// Delete removes a wisp by ID.
func (s *redisWispStore) Delete(ctx context.Context, id string) error {
	if s.closed.Load() {
		return fmt.Errorf("wisp store is closed")
	}

	exists, err := s.client.Exists(ctx, s.wispKey(id)).Result()
	if err != nil {
		return fmt.Errorf("checking existence: %w", err)
	}
	if exists == 0 {
		return fmt.Errorf("wisp %s not found", id)
	}

	pipe := s.client.Pipeline()
	pipe.Del(ctx, s.wispKey(id))
	pipe.SRem(ctx, s.indexKey(), id)
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("deleting wisp: %w", err)
	}

	return nil
}

// Count returns the number of wisps in the store.
func (s *redisWispStore) Count() int {
	if s.closed.Load() {
		return 0
	}

	ctx := context.Background()
	count, err := s.client.SCard(ctx, s.indexKey()).Result()
	if err != nil {
		return 0
	}

	return int(count)
}

// Clear removes all wisps from the store.
func (s *redisWispStore) Clear() {
	if s.closed.Load() {
		return
	}

	ctx := context.Background()

	ids, err := s.client.SMembers(ctx, s.indexKey()).Result()
	if err != nil {
		return
	}

	if len(ids) > 0 {
		keys := make([]string, len(ids)+1)
		for i, id := range ids {
			keys[i] = s.wispKey(id)
		}
		keys[len(ids)] = s.indexKey()
		s.client.Del(ctx, keys...)
	}
}

// Close releases Redis resources.
func (s *redisWispStore) Close() error {
	s.closed.Store(true)
	return s.client.Close()
}
