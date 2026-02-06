package rpc

import (
	"context"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/storage"
)

// LabelCache provides an in-memory cache for issue labels.
// The daemon sees all writes, so invalidation is exact.
// This eliminates expensive batch label queries to Dolt.
type LabelCache struct {
	mu        sync.RWMutex
	labels    map[string][]string // issueID → []label
	populated bool
	loading   sync.Once
	store     storage.Storage
	lastLoad  time.Time
	// Refresh interval - periodically reload to catch any out-of-band changes
	refreshInterval time.Duration
}

// NewLabelCache creates a new label cache.
func NewLabelCache(store storage.Storage) *LabelCache {
	return &LabelCache{
		labels:          make(map[string][]string),
		store:           store,
		refreshInterval: 60 * time.Second,
	}
}

// GetLabelsForIssues returns labels for the given issue IDs from cache.
// On first call, loads all labels from the database.
func (c *LabelCache) GetLabelsForIssues(ctx context.Context, issueIDs []string) (map[string][]string, error) {
	c.ensureLoaded(ctx)

	// Check if we need a background refresh
	c.mu.RLock()
	needsRefresh := time.Since(c.lastLoad) > c.refreshInterval
	c.mu.RUnlock()

	if needsRefresh {
		go c.refresh(context.Background())
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string][]string, len(issueIDs))
	for _, id := range issueIDs {
		if labels, ok := c.labels[id]; ok {
			result[id] = labels
		}
	}
	return result, nil
}

// ensureLoaded does a one-time load of all labels.
func (c *LabelCache) ensureLoaded(ctx context.Context) {
	c.loading.Do(func() {
		c.loadAll(ctx)
	})
}

// loadAll loads all labels from the database into the cache.
func (c *LabelCache) loadAll(ctx context.Context) {
	if c.store == nil {
		return
	}

	// Use a generous timeout for initial load
	loadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	allLabels, err := c.store.GetAllLabels(loadCtx)
	if err != nil {
		// Fall through - cache stays empty, queries will go to DB
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.labels = allLabels
	c.populated = true
	c.lastLoad = time.Now()
}

// refresh reloads all labels in the background.
func (c *LabelCache) refresh(ctx context.Context) {
	if c.store == nil {
		return
	}

	loadCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	allLabels, err := c.store.GetAllLabels(loadCtx)
	if err != nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.labels = allLabels
	c.lastLoad = time.Now()
}

// InvalidateIssue removes cached labels for a specific issue.
// Call this when labels are modified for an issue.
func (c *LabelCache) InvalidateIssue(issueID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.labels, issueID)
}

// SetLabels updates the cache for a specific issue.
// Call this after a successful label write to keep cache consistent.
func (c *LabelCache) SetLabels(issueID string, labels []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if labels == nil {
		delete(c.labels, issueID)
	} else {
		// Copy to prevent mutation
		cached := make([]string, len(labels))
		copy(cached, labels)
		c.labels[issueID] = cached
	}
}

// AddLabel adds a label to the cache for a specific issue.
func (c *LabelCache) AddLabel(issueID, label string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	existing := c.labels[issueID]
	// Check for duplicate
	for _, l := range existing {
		if l == label {
			return
		}
	}
	c.labels[issueID] = append(existing, label)
}

// RemoveLabel removes a label from the cache for a specific issue.
func (c *LabelCache) RemoveLabel(issueID, label string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	existing := c.labels[issueID]
	for i, l := range existing {
		if l == label {
			c.labels[issueID] = append(existing[:i], existing[i+1:]...)
			return
		}
	}
}

// IsPopulated returns whether the cache has been loaded.
func (c *LabelCache) IsPopulated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.populated
}

// Size returns the number of issues with cached labels.
func (c *LabelCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.labels)
}
