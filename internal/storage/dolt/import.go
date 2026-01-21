// Import support methods for DoltStore
// These methods enable bd import to work with the Dolt backend.

package dolt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// OrphanHandling constants (mirrored from sqlite for compatibility)
const (
	OrphanStrict    = "strict"
	OrphanResurrect = "resurrect"
	OrphanSkip      = "skip"
	OrphanAllow     = "allow"
)

// CheckpointWAL is a no-op for Dolt (WAL is SQLite-specific)
func (s *DoltStore) CheckpointWAL(ctx context.Context) error {
	// Dolt doesn't use WAL - this is a no-op
	return nil
}

// GetOrphanHandling returns the configured orphan handling mode
func (s *DoltStore) GetOrphanHandling(ctx context.Context) string {
	handling, err := s.GetConfig(ctx, "orphan_handling")
	if err != nil || handling == "" {
		return OrphanAllow // Default to allow for backwards compatibility
	}
	return handling
}

// BatchCreateOptions controls batch issue creation behavior
type BatchCreateOptions struct {
	SkipValidation     bool // Skip type/status validation
	PreserveDates      bool // Preserve created_at/updated_at from issue
	SkipDirtyTracking  bool // Skip marking issues as dirty
	SkipPrefixCheck    bool // Skip prefix validation
}

// CreateIssuesWithFullOptions creates multiple issues with full options control
func (s *DoltStore) CreateIssuesWithFullOptions(ctx context.Context, issues []*types.Issue, actor string, opts BatchCreateOptions) error {
	if len(issues) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, issue := range issues {
		// Set timestamps if not preserving dates
		now := time.Now().UTC()
		if !opts.PreserveDates || issue.CreatedAt.IsZero() {
			issue.CreatedAt = now
		}
		if !opts.PreserveDates || issue.UpdatedAt.IsZero() {
			issue.UpdatedAt = now
		}

		// Compute content hash if missing
		if issue.ContentHash == "" {
			issue.ContentHash = issue.ComputeContentHash()
		}

		// Insert issue
		if err := insertIssue(ctx, tx, issue); err != nil {
			// Handle duplicate key (issue already exists)
			if strings.Contains(err.Error(), "Duplicate entry") ||
				strings.Contains(err.Error(), "UNIQUE constraint") {
				continue // Skip duplicates
			}
			return fmt.Errorf("failed to insert issue %s: %w", issue.ID, err)
		}

		// Insert labels
		for _, label := range issue.Labels {
			_, err := tx.ExecContext(ctx, `
				INSERT INTO labels (issue_id, label)
				VALUES (?, ?)
			`, issue.ID, label)
			if err != nil && !strings.Contains(err.Error(), "Duplicate entry") {
				return fmt.Errorf("failed to insert label for %s: %w", issue.ID, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// ImportIssueComment adds a comment during import, preserving the original timestamp.
func (s *DoltStore) ImportIssueComment(ctx context.Context, issueID, author, text string, createdAt string) (*types.Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Parse the timestamp
	var parsedTime time.Time
	var err error
	if createdAt != "" {
		parsedTime, err = time.Parse(time.RFC3339, createdAt)
		if err != nil {
			// Try other formats
			parsedTime, err = time.Parse("2006-01-02T15:04:05Z", createdAt)
			if err != nil {
				parsedTime = time.Now().UTC()
			}
		}
	} else {
		parsedTime = time.Now().UTC()
	}

	// Insert the comment with the preserved timestamp
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO comments (issue_id, author, text, created_at)
		VALUES (?, ?, ?, ?)
	`, issueID, author, text, parsedTime.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, fmt.Errorf("failed to insert comment: %w", err)
	}

	return &types.Comment{
		Author:    author,
		Text:      text,
		CreatedAt: parsedTime,
	}, nil
}
