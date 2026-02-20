package dolt

import (
	"context"
	"strings"

	"github.com/steveyegge/beads/internal/storage/ephemeral"
	"github.com/steveyegge/beads/internal/types"
)

// SetEphemeralStore attaches an ephemeral store for transparent routing.
// When set, operations on ephemeral IDs (containing "-wisp-") or issues
// with Ephemeral=true are routed to the SQLite store instead of Dolt.
func (s *DoltStore) SetEphemeralStore(es *ephemeral.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ephemeralStore = es
}

// EphemeralStore returns the attached ephemeral store, or nil.
func (s *DoltStore) EphemeralStore() *ephemeral.Store {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ephemeralStore
}

// HasEphemeralStore returns true if an ephemeral store is attached.
func (s *DoltStore) HasEphemeralStore() bool {
	return s.EphemeralStore() != nil
}

// IsEphemeralID returns true if the ID belongs to an ephemeral issue.
func IsEphemeralID(id string) bool {
	return ephemeral.IsEphemeralID(id)
}

// allEphemeral returns true if all IDs in the slice are ephemeral.
func allEphemeral(ids []string) bool {
	for _, id := range ids {
		if !IsEphemeralID(id) {
			return false
		}
	}
	return len(ids) > 0
}

// partitionIDs separates IDs into ephemeral and dolt groups.
func partitionIDs(ids []string) (ephIDs, doltIDs []string) {
	for _, id := range ids {
		if IsEphemeralID(id) {
			ephIDs = append(ephIDs, id)
		} else {
			doltIDs = append(doltIDs, id)
		}
	}
	return
}

// SearchIssuesDoltOnly queries Dolt directly, bypassing ephemeral routing.
// Used by migration to find ephemeral issues still in Dolt.
func (s *DoltStore) SearchIssuesDoltOnly(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	// Clear Ephemeral filter to prevent routing, since we want Dolt results
	filter.Ephemeral = nil
	// Remove the ephemeral filter and query Dolt for all matching issues,
	// then filter client-side
	results, err := s.SearchIssues(ctx, query, filter)
	if err != nil {
		return nil, err
	}
	// Filter to only ephemeral issues
	var ephIssues []*types.Issue
	for _, issue := range results {
		if issue.Ephemeral {
			ephIssues = append(ephIssues, issue)
		}
	}
	return ephIssues, nil
}

// PromoteFromEphemeral copies an issue from ephemeral to Dolt storage,
// clearing the Ephemeral flag. Used by mol squash to crystallize wisps.
func (s *DoltStore) PromoteFromEphemeral(ctx context.Context, id string, actor string) error {
	es := s.EphemeralStore()
	if es == nil {
		return nil // No ephemeral store, nothing to promote
	}

	issue, err := es.GetIssue(ctx, id)
	if err != nil {
		return err
	}
	if issue == nil {
		return nil // Not found in ephemeral, nothing to promote
	}

	// Clear ephemeral flag for persistent storage
	issue.Ephemeral = false

	// Create in Dolt
	if err := s.CreateIssue(ctx, issue, actor); err != nil {
		return err
	}

	// Copy labels
	labels, err := es.GetLabels(ctx, id)
	if err != nil {
		return err
	}
	for _, label := range labels {
		if err := s.AddLabel(ctx, id, label, actor); err != nil {
			return err
		}
	}

	// Copy dependencies
	deps, err := es.GetDependencyRecords(ctx, id)
	if err != nil {
		return err
	}
	for _, dep := range deps {
		if err := s.AddDependency(ctx, dep, actor); err != nil {
			// Skip if target doesn't exist in Dolt (external ref to other wisp)
			if strings.Contains(err.Error(), "not found") {
				continue
			}
			return err
		}
	}

	// Delete from ephemeral
	return es.DeleteIssue(ctx, id)
}
