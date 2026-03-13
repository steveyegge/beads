package issueops

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// GetIssueInTx retrieves a single issue by ID within an existing transaction,
// including its labels. Returns storage.ErrNotFound (wrapped) if the issue
// does not exist.
//
//nolint:gosec // G201: issueTable/labelTable are caller-controlled constants
func GetIssueInTx(ctx context.Context, tx *sql.Tx, issueTable, labelTable, id string) (*types.Issue, error) {
	if issueTable == "" {
		issueTable = "issues"
	}
	if labelTable == "" {
		labelTable = "labels"
	}

	row := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT %s FROM %s WHERE id = ?`, IssueSelectColumns, issueTable), id)
	issue, err := ScanIssueFrom(row)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("%w: issue %s", storage.ErrNotFound, id)
	}
	if err != nil {
		return nil, fmt.Errorf("get issue: %w", err)
	}

	// Fetch labels in the same transaction to avoid MaxOpenConns=1 deadlock.
	labels, err := GetLabelsInTx(ctx, tx, labelTable, id)
	if err != nil {
		return nil, fmt.Errorf("get issue labels: %w", err)
	}
	issue.Labels = labels

	return issue, nil
}
