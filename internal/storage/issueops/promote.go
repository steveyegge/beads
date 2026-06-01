package issueops

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
)

//nolint:gosec // G201: table names are hardcoded constants
func PromoteFromEphemeralInTx(ctx context.Context, tx *sql.Tx, id string, actor string) error {
	if !IsActiveWispInTx(ctx, tx, id) {
		return fmt.Errorf("wisp %s not found", id)
	}

	issue, err := GetIssueInTx(ctx, tx, id)
	if err != nil {
		return fmt.Errorf("get wisp for promote: %w", err)
	}
	if issue == nil {
		return fmt.Errorf("wisp %s not found", id)
	}

	issue.Ephemeral = false

	bc, err := NewBatchContext(ctx, tx, storage.BatchCreateOptions{
		SkipPrefixValidation: true,
	})
	if err != nil {
		return fmt.Errorf("new batch context: %w", err)
	}
	if err := PrepareIssueForInsert(issue, bc.CustomStatuses, bc.CustomTypes); err != nil {
		return fmt.Errorf("promote wisp to issues: %w", err)
	}
	if _, err := InsertIssueIfNew(ctx, tx, "issues", issue, false); err != nil {
		return fmt.Errorf("promote wisp to issues: %w", err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO labels (issue_id, label)
		SELECT issue_id, label FROM wisp_labels WHERE issue_id = ?
	`, id); err != nil {
		return fmt.Errorf("copy labels for promoted wisp %s: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM wisp_labels WHERE issue_id = ?`, id); err != nil {
		return fmt.Errorf("delete copied wisp labels for promoted wisp %s: %w", id, err)
	}

	for _, pair := range []struct {
		src, dst, targetCol string
	}{
		{"wisp_issue_dependencies", "issue_issue_dependencies", "depends_on_issue_id"},
		{"wisp_wisp_dependencies", "issue_wisp_dependencies", "depends_on_wisp_id"},
		{"wisp_external_dependencies", "issue_external_dependencies", "depends_on_external_id"},
	} {
		//nolint:gosec // G201: table and column names are fixed split-dep constants
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT IGNORE INTO %s (source_id, %s, type, created_at, created_by, metadata, thread_id)
			SELECT source_id, %s, type, created_at, created_by, metadata, thread_id
			FROM %s WHERE source_id = ?
		`, pair.dst, pair.targetCol, pair.targetCol, pair.src), id); err != nil {
			return fmt.Errorf("copy %s for promoted wisp %s: %w", pair.src, id, err)
		}
		//nolint:gosec // G201: table name is a fixed split-dep constant
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE source_id = ?`, pair.src), id); err != nil {
			return fmt.Errorf("delete copied %s for promoted wisp %s: %w", pair.src, id, err)
		}
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO events (issue_id, event_type, actor, old_value, new_value, comment, created_at)
		SELECT issue_id, event_type, actor, old_value, new_value, comment, created_at
		FROM wisp_events WHERE issue_id = ?
	`, id); err != nil {
		return fmt.Errorf("copy events for promoted wisp %s: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM wisp_events WHERE issue_id = ?`, id); err != nil {
		return fmt.Errorf("delete copied wisp events for promoted wisp %s: %w", id, err)
	}

	if _, err := tx.ExecContext(ctx, `
		INSERT IGNORE INTO comments (issue_id, author, text, created_at)
		SELECT issue_id, author, text, created_at
		FROM wisp_comments WHERE issue_id = ?
	`, id); err != nil {
		return fmt.Errorf("copy comments for promoted wisp %s: %w", id, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM wisp_comments WHERE issue_id = ?`, id); err != nil {
		return fmt.Errorf("delete copied wisp comments for promoted wisp %s: %w", id, err)
	}

	if err := RetargetInboundDependenciesToIssueInTx(ctx, tx, id); err != nil {
		return err
	}

	result, err := tx.ExecContext(ctx, `DELETE FROM wisps WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete promoted wisp row %s: %w", id, err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get promoted wisp rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("wisp %s not found", id)
	}

	affectedIssues, affectedWisps, aerr := AffectedByStatusChangeInTx(ctx, tx, id)
	if aerr != nil {
		return fmt.Errorf("affected by promote for %s: %w", id, aerr)
	}
	if err := RecomputeIsBlockedInTx(ctx, tx, affectedIssues, affectedWisps); err != nil {
		return fmt.Errorf("recompute is_blocked after promote for %s: %w", id, err)
	}
	return nil
}
