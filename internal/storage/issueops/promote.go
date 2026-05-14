package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/steveyegge/beads/internal/storage"
)

//nolint:gosec // G201: table names are hardcoded constants
func PromoteFromEphemeralInTx(ctx context.Context, regularTx, ignoredTx *sql.Tx, id string, actor string) error {
	if !IsActiveWispInTx(ctx, ignoredTx, id) {
		return fmt.Errorf("wisp %s not found", id)
	}

	issue, err := GetIssueInTx(ctx, ignoredTx, id)
	if err != nil {
		return fmt.Errorf("get wisp for promote: %w", err)
	}
	if issue == nil {
		return fmt.Errorf("wisp %s not found", id)
	}

	issue.Ephemeral = false

	bc, err := NewBatchContext(ctx, regularTx, storage.BatchCreateOptions{
		SkipPrefixValidation: true,
	})
	if err != nil {
		return fmt.Errorf("new batch context: %w", err)
	}
	if err := CreateIssueInTx(ctx, regularTx, ignoredTx, bc, issue, actor); err != nil {
		return fmt.Errorf("promote wisp to issues: %w", err)
	}

	if _, err := regularTx.ExecContext(ctx, `
		INSERT IGNORE INTO labels (issue_id, label)
		SELECT issue_id, label FROM wisp_labels WHERE issue_id = ?
	`, id); err != nil {
		log.Printf("promote %s: failed to copy labels: %v", id, err)
	}

	if _, err := regularTx.ExecContext(ctx, `
		INSERT IGNORE INTO dependencies (issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external, type, created_at, created_by, metadata, thread_id)
		SELECT issue_id, depends_on_issue_id, depends_on_wisp_id, depends_on_external, type, created_at, created_by, metadata, thread_id
		FROM wisp_dependencies WHERE issue_id = ?
	`, id); err != nil {
		log.Printf("promote %s: failed to copy dependencies: %v", id, err)
	}

	if _, err := regularTx.ExecContext(ctx, `
		INSERT IGNORE INTO events (issue_id, event_type, actor, old_value, new_value, comment, created_at)
		SELECT issue_id, event_type, actor, old_value, new_value, comment, created_at
		FROM wisp_events WHERE issue_id = ?
	`, id); err != nil {
		log.Printf("promote %s: failed to copy events: %v", id, err)
	}

	if _, err := regularTx.ExecContext(ctx, `
		INSERT IGNORE INTO comments (issue_id, author, text, created_at)
		SELECT issue_id, author, text, created_at
		FROM wisp_comments WHERE issue_id = ?
	`, id); err != nil {
		log.Printf("promote %s: failed to copy comments: %v", id, err)
	}

	return DeleteIssueInTx(ctx, regularTx, ignoredTx, id)
}
