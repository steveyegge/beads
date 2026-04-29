//go:build cgo

package doltlite

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

func (s *DoltliteStore) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	if issue == nil {
		return fmt.Errorf("issue must not be nil")
	}
	// Route infra types to wisps, matching DoltStore.CreateIssue behavior.
	if s.IsInfraTypeCtx(ctx, issue.IssueType) {
		issue.Ephemeral = true
	}

	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		bc, err := issueops.NewBatchContext(ctx, tx, storage.BatchCreateOptions{
			SkipPrefixValidation: true,
		})
		if err != nil {
			return err
		}
		return createIssueSQLite(ctx, tx, bc, issue, actor)
	})
}

func (s *DoltliteStore) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	return s.CreateIssuesWithFullOptions(ctx, issues, actor, storage.BatchCreateOptions{
		OrphanHandling:       storage.OrphanAllow,
		SkipPrefixValidation: false,
	})
}

func (s *DoltliteStore) CreateIssuesWithFullOptions(ctx context.Context, issues []*types.Issue, actor string, opts storage.BatchCreateOptions) error {
	if len(issues) == 0 {
		return nil
	}

	for _, issue := range issues {
		if issueops.IsWisp(issue) && !issue.NoHistory {
			issue.Ephemeral = true
		}
		if err := s.withConn(ctx, true, func(tx *sql.Tx) error {
			bc, err := issueops.NewBatchContext(ctx, tx, opts)
			if err != nil {
				return err
			}
			return createIssueSQLite(ctx, tx, bc, issue, actor)
		}); err != nil {
			return err
		}
	}
	return nil
}

func createIssueSQLite(ctx context.Context, tx *sql.Tx, bc *issueops.BatchContext, issue *types.Issue, actor string) error {
	if err := issueops.PrepareIssueForInsert(issue, bc.CustomStatuses, bc.CustomTypes); err != nil {
		return err
	}
	issueTable, eventTable := issueops.TableRouting(issue)
	if issue.ID == "" {
		prefix := bc.ConfigPrefix
		if issue.PrefixOverride != "" {
			prefix = issue.PrefixOverride
		} else if issue.IDPrefix != "" {
			prefix = bc.ConfigPrefix + "-" + issue.IDPrefix
		} else if issueops.IsWisp(issue) {
			prefix = bc.ConfigPrefix + "-wisp"
		}
		var err error
		issue.ID, err = issueops.GenerateIssueIDInTable(ctx, tx, issueTable, prefix, issue, actor)
		if err != nil {
			return fmt.Errorf("failed to generate issue ID: %w", err)
		}
	}
	if skip, err := issueops.CheckOrphan(ctx, tx, issue, issueTable, bc.Opts.OrphanHandling); err != nil {
		return err
	} else if skip {
		return nil
	}

	var existingCount int
	if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE id = ?", issueTable), issue.ID).Scan(&existingCount); err != nil {
		return fmt.Errorf("failed to check issue existence for %s: %w", issue.ID, err)
	}
	if err := insertIssueSQLite(ctx, tx, issueTable, issue); err != nil {
		return err
	}
	if existingCount == 0 {
		if err := issueops.RecordEventInTableWithDialect(ctx, tx, eventTable, issue.ID, types.EventCreated, actor, "", issueops.SQLDialectSQLite); err != nil {
			return fmt.Errorf("failed to record event for %s: %w", issue.ID, err)
		}
	}
	if err := issueops.PersistLabelsWithDialect(ctx, tx, issue, issueops.SQLDialectSQLite); err != nil {
		return err
	}
	return issueops.PersistComments(ctx, tx, issue)
}

func insertIssueSQLite(ctx context.Context, tx *sql.Tx, table string, issue *types.Issue) error {
	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT OR REPLACE INTO %s (
			id, content_hash, title, description, design, acceptance_criteria, notes,
			status, priority, issue_type, assignee, estimated_minutes,
			created_at, created_by, owner, updated_at, started_at, closed_at, external_ref, spec_id,
			compaction_level, compacted_at, compacted_at_commit, original_size,
			sender, ephemeral, no_history, wisp_type, pinned, is_template,
			mol_type, work_type, source_system, source_repo, close_reason,
			event_kind, actor, target, payload,
			await_type, await_id, timeout_ns, waiters,
			due_at, defer_until, metadata
		) VALUES (
			?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?, ?,
			?, ?, ?
		)
	`, table),
		issue.ID, issue.ContentHash, issue.Title, issue.Description, issue.Design, issue.AcceptanceCriteria, issue.Notes,
		issue.Status, issue.Priority, issue.IssueType, issueops.NullString(issue.Assignee), issueops.NullInt(issue.EstimatedMinutes),
		issue.CreatedAt, issue.CreatedBy, issue.Owner, issue.UpdatedAt, issue.StartedAt, issue.ClosedAt, issueops.NullStringPtr(issue.ExternalRef), issue.SpecID,
		issue.CompactionLevel, issue.CompactedAt, issueops.NullStringPtr(issue.CompactedAtCommit), issueops.NullIntVal(issue.OriginalSize),
		issue.Sender, issue.Ephemeral, issue.NoHistory, issue.WispType, issue.Pinned, issue.IsTemplate,
		issue.MolType, issue.WorkType, issue.SourceSystem, issue.SourceRepo, issue.CloseReason,
		issue.EventKind, issue.Actor, issue.Target, issue.Payload,
		issue.AwaitType, issue.AwaitID, issue.Timeout.Nanoseconds(), issueops.FormatJSONStringArray(issue.Waiters),
		issue.DueAt, issue.DeferUntil, issueops.JSONMetadata(issue.Metadata),
	)
	if err != nil {
		return fmt.Errorf("insert issue into %s: %w", table, err)
	}
	return nil
}
