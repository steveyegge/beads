package main

import (
	"github.com/steveyegge/beads/internal/beads"
)

// MergeResult contains the outcome of a 3-way merge
type MergeResult struct {
	Merged    []*beads.Issue    // Final merged state
	Conflicts int               // Number of true conflicts resolved
	Strategy  map[string]string // Per-issue: "local", "remote", "merged"
}

// MergeIssues performs 3-way merge: base x local x remote -> merged
//
// Phase 1 (Tracer Bullet): This is a stub implementation that just returns
// remote issues. This validates the pull-first architecture without implementing
// the full merge algorithm.
//
// Future phases will implement:
// - Phase 2: Content-level 3-way merge algorithm
// - Phase 3: Field-level resolution (LWW for scalars, union for sets)
func MergeIssues(base, local, remote []*beads.Issue) (*MergeResult, error) {
	// Phase 1 stub: just return remote issues (pull-first validated)
	// This proves the architecture works before adding merge complexity
	strategy := make(map[string]string)
	for _, issue := range remote {
		strategy[issue.ID] = "remote"
	}

	return &MergeResult{
		Merged:    remote,
		Conflicts: 0,
		Strategy:  strategy,
	}, nil
}

// MergeIssue merges a single issue using 3-way algorithm
//
// Phase 1 (Tracer Bullet): Returns remote issue (stub).
// Phase 2+ will implement the actual merge logic:
//
//	if base == local { return remote }  // Only remote changed
//	if base == remote { return local }  // Only local changed
//	if local == remote { return local } // Same change
//	// True conflict: LWW by updated_at
//	if local.UpdatedAt.After(remote.UpdatedAt) { return local }
//	return remote
func MergeIssue(base, local, remote *beads.Issue) (*beads.Issue, string) {
	// Phase 1 stub: return remote
	return remote, "remote"
}
