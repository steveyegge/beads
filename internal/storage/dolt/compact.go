//go:build cgo

package dolt

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

const (
	defaultTier1Days = 30
	defaultTier2Days = 90
)

// CheckEligibility checks if an issue is eligible for compaction at the given tier.
func (s *DoltStore) CheckEligibility(ctx context.Context, issueID string, tier int) (bool, string, error) {
	var status string
	var closedAt sql.NullTime
	var compactionLevel int
	var descLen, designLen, notesLen, acLen int

	err := s.queryRowContext(ctx, func(row *sql.Row) error {
		return row.Scan(&status, &closedAt, &compactionLevel, &descLen, &designLen, &notesLen, &acLen)
	}, `SELECT status, closed_at, COALESCE(compaction_level, 0),
		LENGTH(COALESCE(description, '')), LENGTH(COALESCE(design, '')),
		LENGTH(COALESCE(notes, '')), LENGTH(COALESCE(acceptance_criteria, ''))
	FROM issues WHERE id = ?`, issueID)

	if err == sql.ErrNoRows {
		return false, "issue not found", nil
	}
	if err != nil {
		return false, "", fmt.Errorf("failed to query issue: %w", err)
	}

	if status != "closed" {
		return false, "issue is not closed", nil
	}
	if !closedAt.Valid {
		return false, "issue has no closed_at timestamp", nil
	}

	// Check compaction level
	requiredLevel := tier - 1
	if compactionLevel != requiredLevel {
		return false, fmt.Sprintf("compaction_level is %d, need %d for tier %d", compactionLevel, requiredLevel, tier), nil
	}

	// Check content size
	contentSize := descLen + designLen + notesLen + acLen
	if contentSize == 0 {
		return false, "no content to compact", nil
	}

	// Check age threshold
	thresholdDays, err := s.getCompactionThresholdDays(ctx, tier)
	if err != nil {
		return false, "", err
	}
	daysSinceClosed := int(time.Since(closedAt.Time).Hours() / 24)
	if daysSinceClosed < thresholdDays {
		return false, fmt.Sprintf("closed %d days ago, need %d for tier %d", daysSinceClosed, thresholdDays, tier), nil
	}

	return true, "", nil
}

// ApplyCompaction records a compaction result in the database.
func (s *DoltStore) ApplyCompaction(ctx context.Context, issueID string, tier int, originalSize int, _ int, commitHash string) error {
	_, err := s.execContext(ctx, `
		UPDATE issues SET compaction_level = ?, compacted_at = NOW(),
			compacted_at_commit = ?, original_size = ?
		WHERE id = ?`,
		tier, commitHash, originalSize, issueID)
	if err != nil {
		return fmt.Errorf("failed to apply compaction: %w", err)
	}
	return nil
}

// GetTier1Candidates returns issues eligible for tier 1 compaction.
func (s *DoltStore) GetTier1Candidates(ctx context.Context) ([]*types.CompactionCandidate, error) {
	return s.getCandidates(ctx, 1)
}

// GetTier2Candidates returns issues eligible for tier 2 compaction.
func (s *DoltStore) GetTier2Candidates(ctx context.Context) ([]*types.CompactionCandidate, error) {
	return s.getCandidates(ctx, 2)
}

// getCandidates returns compaction candidates for the given tier.
func (s *DoltStore) getCandidates(ctx context.Context, tier int) ([]*types.CompactionCandidate, error) {
	requiredLevel := tier - 1
	thresholdDays, err := s.getCompactionThresholdDays(ctx, tier)
	if err != nil {
		return nil, err
	}

	rows, err := s.queryContext(ctx, `
		SELECT id, closed_at,
			LENGTH(COALESCE(description, '')) + LENGTH(COALESCE(design, '')) +
			LENGTH(COALESCE(notes, '')) + LENGTH(COALESCE(acceptance_criteria, '')) as original_size
		FROM issues
		WHERE status = 'closed'
			AND closed_at IS NOT NULL
			AND COALESCE(compaction_level, 0) = ?
			AND TIMESTAMPDIFF(DAY, closed_at, NOW()) >= ?
			AND (LENGTH(COALESCE(description, '')) + LENGTH(COALESCE(design, '')) +
				LENGTH(COALESCE(notes, '')) + LENGTH(COALESCE(acceptance_criteria, ''))) > 0
		ORDER BY closed_at ASC`,
		requiredLevel, thresholdDays)
	if err != nil {
		return nil, fmt.Errorf("failed to query tier %d candidates: %w", tier, err)
	}
	defer rows.Close()

	var candidates []*types.CompactionCandidate
	for rows.Next() {
		var c types.CompactionCandidate
		if err := rows.Scan(&c.IssueID, &c.ClosedAt, &c.OriginalSize); err != nil {
			return nil, fmt.Errorf("failed to scan candidate: %w", err)
		}
		candidates = append(candidates, &c)
	}
	return candidates, rows.Err()
}

// getCompactionThresholdDays reads the compaction threshold from config, with defaults.
func (s *DoltStore) getCompactionThresholdDays(ctx context.Context, tier int) (int, error) {
	key := fmt.Sprintf("compact_tier%d_days", tier)
	value, err := s.GetConfig(ctx, key)
	if err != nil {
		return 0, fmt.Errorf("failed to read config %s: %w", key, err)
	}
	if value != "" {
		days, err := strconv.Atoi(value)
		if err == nil && days > 0 {
			return days, nil
		}
	}
	switch tier {
	case 1:
		return defaultTier1Days, nil
	case 2:
		return defaultTier2Days, nil
	default:
		return defaultTier1Days, nil
	}
}
