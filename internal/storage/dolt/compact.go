//go:build cgo

package dolt

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// CheckEligibility checks if an issue is eligible for compaction at the given tier.
// Returns (eligible, reason, error).
func (s *DoltStore) CheckEligibility(_ context.Context, _ string, _ int) (bool, string, error) {
	return false, "compaction not yet implemented for Dolt backend", nil
}

// ApplyCompaction records that a compaction was applied to an issue.
func (s *DoltStore) ApplyCompaction(_ context.Context, _ string, _ int, _ int, _ int, _ string) error {
	return fmt.Errorf("compaction not yet implemented for Dolt backend")
}

// GetTier1Candidates returns issues eligible for tier-1 compaction.
func (s *DoltStore) GetTier1Candidates(_ context.Context) ([]*types.CompactionCandidate, error) {
	return nil, nil // No candidates until compaction is implemented
}

// GetTier2Candidates returns issues eligible for tier-2 compaction.
func (s *DoltStore) GetTier2Candidates(_ context.Context) ([]*types.CompactionCandidate, error) {
	return nil, nil // No candidates until compaction is implemented
}
