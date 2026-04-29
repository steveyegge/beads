//go:build cgo

package doltlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// ClaimIssue atomically claims an issue using compare-and-swap semantics.
// Delegates SQL work to issueops; EmbeddedDolt auto-commits the transaction.
func (s *DoltliteStore) ClaimIssue(ctx context.Context, id string, actor string) error {
	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		_, err := issueops.ClaimIssueInTx(ctx, tx, id, actor)
		return err
	})
}

// UpdateIssue updates fields on an issue.
// Delegates SQL work to issueops; EmbeddedDolt auto-commits the transaction.
func (s *DoltliteStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	// Validate metadata against schema before routing.
	if rawMeta, ok := updates["metadata"]; ok {
		metadataStr, err := storage.NormalizeMetadataValue(rawMeta)
		if err != nil {
			return fmt.Errorf("invalid metadata: %w", err)
		}
		if err := issueops.ValidateMetadataIfConfigured(json.RawMessage(metadataStr)); err != nil {
			return err
		}
	}

	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		_, err := issueops.UpdateIssueInTxWithDialect(ctx, tx, id, updates, actor, issueops.SQLDialectSQLite)
		return err
	})
}

// ReopenIssue reopens a closed issue, setting status to open and clearing
// closed_at and defer_until. If reason is non-empty, it is recorded as a comment.
// Wraps UpdateIssue; EmbeddedDolt auto-commits the transaction.
func (s *DoltliteStore) ReopenIssue(ctx context.Context, id string, reason string, actor string) error {
	updates := map[string]interface{}{
		"status":      string(types.StatusOpen),
		"defer_until": nil,
	}
	if err := s.UpdateIssue(ctx, id, updates, actor); err != nil {
		return err
	}
	if reason != "" {
		if err := s.AddComment(ctx, id, actor, reason); err != nil {
			return fmt.Errorf("reopen comment: %w", err)
		}
	}
	return nil
}

// UpdateIssueType changes the issue_type field of an issue.
// Wraps UpdateIssue; EmbeddedDolt auto-commits the transaction.
func (s *DoltliteStore) UpdateIssueType(ctx context.Context, id string, issueType string, actor string) error {
	return s.UpdateIssue(ctx, id, map[string]interface{}{"issue_type": issueType}, actor)
}

// CloseIssue closes an issue with a reason.
// Delegates SQL work to issueops; EmbeddedDolt auto-commits the transaction.
func (s *DoltliteStore) CloseIssue(ctx context.Context, id string, reason string, actor string, session string) error {
	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		_, err := issueops.CloseIssueInTxWithDialect(ctx, tx, id, reason, actor, session, issueops.SQLDialectSQLite)
		return err
	})
}

// IsBlocked checks if an issue is blocked by active dependencies.
func (s *DoltliteStore) IsBlocked(ctx context.Context, issueID string) (bool, []string, error) {
	var blocked bool
	var blockers []string
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		blocked, blockers, err = issueops.IsBlockedInTx(ctx, tx, issueID)
		return err
	})
	return blocked, blockers, err
}

// GetNewlyUnblockedByClose finds issues that become unblocked when closedIssueID is closed.
func (s *DoltliteStore) GetNewlyUnblockedByClose(ctx context.Context, closedIssueID string) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetNewlyUnblockedByCloseInTx(ctx, tx, closedIssueID)
		return err
	})
	return result, err
}
