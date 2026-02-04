// Package daemon provides daemon process management and in-memory stores.
package daemon

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// WispStore is an in-memory store for ephemeral wisps.
// Wisps are transient beads that are not persisted to disk - they exist only
// in the daemon's memory and are lost on restart (by design).
//
// Thread-safe: all operations are protected by a read-write mutex.
type WispStore interface {
	// Create adds a new wisp to the store.
	// Returns an error if a wisp with the same ID already exists.
	Create(ctx context.Context, issue *types.Issue) error

	// Get retrieves a wisp by ID.
	// Returns nil, nil if not found.
	Get(ctx context.Context, id string) (*types.Issue, error)

	// List returns wisps matching the filter.
	// Supports filtering by status, type, labels, etc.
	List(ctx context.Context, filter types.IssueFilter) ([]*types.Issue, error)

	// Update modifies an existing wisp.
	// Returns an error if the wisp doesn't exist.
	Update(ctx context.Context, issue *types.Issue) error

	// Delete removes a wisp by ID.
	// Returns an error if the wisp doesn't exist.
	Delete(ctx context.Context, id string) error

	// Count returns the number of wisps in the store.
	Count() int

	// Clear removes all wisps from the store.
	Clear()

	// Close releases any resources held by the store.
	Close() error
}

// memoryWispStore is the default in-memory implementation of WispStore.
type memoryWispStore struct {
	mu     sync.RWMutex
	wisps  map[string]*types.Issue
	closed atomic.Bool
}

// NewWispStore creates a new in-memory wisp store.
func NewWispStore() WispStore {
	return &memoryWispStore{
		wisps: make(map[string]*types.Issue),
	}
}

// Create adds a new wisp to the store.
func (s *memoryWispStore) Create(ctx context.Context, issue *types.Issue) error {
	if s.closed.Load() {
		return fmt.Errorf("wisp store is closed")
	}

	if issue == nil {
		return fmt.Errorf("issue cannot be nil")
	}

	if issue.ID == "" {
		return fmt.Errorf("issue ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.wisps[issue.ID]; exists {
		return fmt.Errorf("wisp %s already exists", issue.ID)
	}

	// Ensure the wisp is marked as ephemeral
	issue.Ephemeral = true

	// Set timestamps if not already set
	now := time.Now()
	if issue.CreatedAt.IsZero() {
		issue.CreatedAt = now
	}
	if issue.UpdatedAt.IsZero() {
		issue.UpdatedAt = now
	}

	// Store a copy to prevent external mutation
	s.wisps[issue.ID] = cloneIssue(issue)

	return nil
}

// Get retrieves a wisp by ID.
func (s *memoryWispStore) Get(ctx context.Context, id string) (*types.Issue, error) {
	if s.closed.Load() {
		return nil, fmt.Errorf("wisp store is closed")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	issue, exists := s.wisps[id]
	if !exists {
		return nil, nil // Not found is not an error
	}

	return cloneIssue(issue), nil
}

// List returns wisps matching the filter.
func (s *memoryWispStore) List(ctx context.Context, filter types.IssueFilter) ([]*types.Issue, error) {
	if s.closed.Load() {
		return nil, fmt.Errorf("wisp store is closed")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*types.Issue

	for _, issue := range s.wisps {
		if matchesFilter(issue, filter) {
			results = append(results, cloneIssue(issue))
		}
	}

	// Apply limit if specified
	if filter.Limit > 0 && len(results) > filter.Limit {
		results = results[:filter.Limit]
	}

	return results, nil
}

// Update modifies an existing wisp.
func (s *memoryWispStore) Update(ctx context.Context, issue *types.Issue) error {
	if s.closed.Load() {
		return fmt.Errorf("wisp store is closed")
	}

	if issue == nil {
		return fmt.Errorf("issue cannot be nil")
	}

	if issue.ID == "" {
		return fmt.Errorf("issue ID cannot be empty")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.wisps[issue.ID]; !exists {
		return fmt.Errorf("wisp %s not found", issue.ID)
	}

	// Ensure the wisp remains ephemeral
	issue.Ephemeral = true

	// Update timestamp
	issue.UpdatedAt = time.Now()

	// Store a copy
	s.wisps[issue.ID] = cloneIssue(issue)

	return nil
}

// Delete removes a wisp by ID.
func (s *memoryWispStore) Delete(ctx context.Context, id string) error {
	if s.closed.Load() {
		return fmt.Errorf("wisp store is closed")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.wisps[id]; !exists {
		return fmt.Errorf("wisp %s not found", id)
	}

	delete(s.wisps, id)
	return nil
}

// Count returns the number of wisps in the store.
func (s *memoryWispStore) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.wisps)
}

// Clear removes all wisps from the store.
func (s *memoryWispStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wisps = make(map[string]*types.Issue)
}

// Close releases any resources held by the store.
func (s *memoryWispStore) Close() error {
	s.closed.Store(true)
	s.Clear()
	return nil
}

// matchesFilter checks if an issue matches the given filter.
func matchesFilter(issue *types.Issue, filter types.IssueFilter) bool {
	// ParentID filter: wisps are ephemeral and don't have parent-child relationships
	// stored in the database, so they can never be children of a specified parent.
	// Exclude all wisps when filtering by parent.
	if filter.ParentID != nil {
		return false
	}

	// Status filter
	if filter.Status != nil && issue.Status != *filter.Status {
		return false
	}

	// Priority filter
	if filter.Priority != nil && issue.Priority != *filter.Priority {
		return false
	}

	// Issue type filter
	if filter.IssueType != nil && issue.IssueType != *filter.IssueType {
		return false
	}

	// Assignee filter
	if filter.Assignee != nil && issue.Assignee != *filter.Assignee {
		return false
	}

	// Labels filter (AND semantics)
	if len(filter.Labels) > 0 {
		issueLabels := make(map[string]bool)
		for _, l := range issue.Labels {
			issueLabels[l] = true
		}
		for _, required := range filter.Labels {
			if !issueLabels[required] {
				return false
			}
		}
	}

	// LabelsAny filter (OR semantics)
	if len(filter.LabelsAny) > 0 {
		found := false
		issueLabels := make(map[string]bool)
		for _, l := range issue.Labels {
			issueLabels[l] = true
		}
		for _, any := range filter.LabelsAny {
			if issueLabels[any] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// ID filter
	if len(filter.IDs) > 0 {
		found := false
		for _, id := range filter.IDs {
			if issue.ID == id {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// ID prefix filter
	if filter.IDPrefix != "" && !strings.HasPrefix(issue.ID, filter.IDPrefix) {
		return false
	}

	// Title search (case-insensitive)
	if filter.TitleSearch != "" {
		if !strings.Contains(strings.ToLower(issue.Title), strings.ToLower(filter.TitleSearch)) {
			return false
		}
	}

	// Title contains
	if filter.TitleContains != "" {
		if !strings.Contains(issue.Title, filter.TitleContains) {
			return false
		}
	}

	// Description contains
	if filter.DescriptionContains != "" {
		if !strings.Contains(issue.Description, filter.DescriptionContains) {
			return false
		}
	}

	// Notes contains
	if filter.NotesContains != "" {
		if !strings.Contains(issue.Notes, filter.NotesContains) {
			return false
		}
	}

	// Date range filters
	if filter.CreatedAfter != nil && issue.CreatedAt.Before(*filter.CreatedAfter) {
		return false
	}
	if filter.CreatedBefore != nil && issue.CreatedAt.After(*filter.CreatedBefore) {
		return false
	}
	if filter.UpdatedAfter != nil && issue.UpdatedAt.Before(*filter.UpdatedAfter) {
		return false
	}
	if filter.UpdatedBefore != nil && issue.UpdatedAt.After(*filter.UpdatedBefore) {
		return false
	}

	// Ephemeral filter (wisps are always ephemeral, but support the filter)
	if filter.Ephemeral != nil && issue.Ephemeral != *filter.Ephemeral {
		return false
	}

	// Parent filter: check if issue has a parent-child dependency pointing to the specified parent
	if filter.ParentID != nil {
		hasParent := false
		for _, dep := range issue.Dependencies {
			if dep != nil && dep.Type == types.DepParentChild && dep.DependsOnID == *filter.ParentID {
				hasParent = true
				break
			}
		}
		if !hasParent {
			return false
		}
	}

	return true
}

// cloneIssue creates a deep copy of an issue to prevent external mutation.
func cloneIssue(issue *types.Issue) *types.Issue {
	if issue == nil {
		return nil
	}

	clone := *issue // Shallow copy

	// Deep copy slices
	if issue.Labels != nil {
		clone.Labels = make([]string, len(issue.Labels))
		copy(clone.Labels, issue.Labels)
	}

	if issue.Dependencies != nil {
		clone.Dependencies = make([]*types.Dependency, len(issue.Dependencies))
		for i, dep := range issue.Dependencies {
			if dep != nil {
				depCopy := *dep
				clone.Dependencies[i] = &depCopy
			}
		}
	}

	if issue.Comments != nil {
		clone.Comments = make([]*types.Comment, len(issue.Comments))
		for i, comment := range issue.Comments {
			if comment != nil {
				commentCopy := *comment
				clone.Comments[i] = &commentCopy
			}
		}
	}

	if issue.BondedFrom != nil {
		clone.BondedFrom = make([]types.BondRef, len(issue.BondedFrom))
		copy(clone.BondedFrom, issue.BondedFrom)
	}

	// Deep copy pointers
	if issue.ClosedAt != nil {
		closedAt := *issue.ClosedAt
		clone.ClosedAt = &closedAt
	}

	if issue.EstimatedMinutes != nil {
		est := *issue.EstimatedMinutes
		clone.EstimatedMinutes = &est
	}

	if issue.DueAt != nil {
		dueAt := *issue.DueAt
		clone.DueAt = &dueAt
	}

	if issue.DeferUntil != nil {
		deferUntil := *issue.DeferUntil
		clone.DeferUntil = &deferUntil
	}

	if issue.ExternalRef != nil {
		extRef := *issue.ExternalRef
		clone.ExternalRef = &extRef
	}

	return &clone
}
