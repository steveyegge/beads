//go:build cgo

package doltlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

func doltliteAsOfInTx(ctx context.Context, tx *sql.Tx, issueID string, ref string) (*types.Issue, error) {
	if err := issueops.ValidateRef(ref); err != nil {
		return nil, fmt.Errorf("invalid ref: %w", err)
	}

	query := fmt.Sprintf(`
		SELECT %s
		FROM dolt_at_issues(?)
		WHERE id = ?
	`, issueops.IssueSelectColumns)
	issue, err := issueops.ScanIssueFrom(tx.QueryRowContext(ctx, query, ref, issueID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: issue %s as of %s", storage.ErrNotFound, issueID, ref)
	}
	if err != nil {
		return nil, fmt.Errorf("get issue as of %s: %w", ref, err)
	}
	return issue, nil
}

func doltliteDiffInTx(ctx context.Context, tx *sql.Tx, fromRef, toRef string) ([]*storage.DiffEntry, error) {
	if err := issueops.ValidateRef(fromRef); err != nil {
		return nil, fmt.Errorf("invalid fromRef: %w", err)
	}
	if err := issueops.ValidateRef(toRef); err != nil {
		return nil, fmt.Errorf("invalid toRef: %w", err)
	}

	rows, err := tx.QueryContext(ctx, `
		SELECT
			COALESCE(from_id, '') as from_id,
			COALESCE(to_id, '') as to_id,
			diff_type,
			from_title, to_title,
			from_description, to_description,
			from_status, to_status,
			from_priority, to_priority
		FROM dolt_diff_issues(?, ?)
	`, fromRef, toRef)
	if err != nil {
		return nil, fmt.Errorf("failed to get diff: %w", err)
	}
	defer rows.Close()

	var entries []*storage.DiffEntry
	for rows.Next() {
		var fromID, toID, diffType string
		var fromTitle, toTitle, fromDesc, toDesc, fromStatus, toStatus *string
		var fromPriority, toPriority *int

		if err := rows.Scan(&fromID, &toID, &diffType,
			&fromTitle, &toTitle,
			&fromDesc, &toDesc,
			&fromStatus, &toStatus,
			&fromPriority, &toPriority); err != nil {
			return nil, fmt.Errorf("failed to scan diff: %w", err)
		}

		entry := &storage.DiffEntry{DiffType: diffType}
		if toID != "" {
			entry.IssueID = toID
		} else {
			entry.IssueID = fromID
		}
		if diffType != "added" && fromID != "" {
			entry.OldValue = &types.Issue{ID: fromID}
			if fromTitle != nil {
				entry.OldValue.Title = *fromTitle
			}
			if fromDesc != nil {
				entry.OldValue.Description = *fromDesc
			}
			if fromStatus != nil {
				entry.OldValue.Status = types.Status(*fromStatus)
			}
			if fromPriority != nil {
				entry.OldValue.Priority = *fromPriority
			}
		}
		if diffType != "removed" && toID != "" {
			entry.NewValue = &types.Issue{ID: toID}
			if toTitle != nil {
				entry.NewValue.Title = *toTitle
			}
			if toDesc != nil {
				entry.NewValue.Description = *toDesc
			}
			if toStatus != nil {
				entry.NewValue.Status = types.Status(*toStatus)
			}
			if toPriority != nil {
				entry.NewValue.Priority = *toPriority
			}
		}

		entries = append(entries, entry)
	}

	return entries, rows.Err()
}

func doltliteHistoryInTx(ctx context.Context, tx *sql.Tx, issueID string) ([]*storage.HistoryEntry, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT commit_hash, committer, commit_date
		FROM dolt_history_issues
		WHERE id = ?
		ORDER BY commit_date DESC
	`, issueID)
	if err != nil {
		return nil, fmt.Errorf("failed to get issue history: %w", err)
	}

	type historyMeta struct {
		hash      string
		committer string
		date      any
	}
	var metas []historyMeta
	for rows.Next() {
		var meta historyMeta
		if err := rows.Scan(&meta.hash, &meta.committer, &meta.date); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("failed to scan history: %w", err)
		}
		metas = append(metas, meta)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	entries := make([]*storage.HistoryEntry, 0, len(metas))
	for _, meta := range metas {
		issue, err := doltliteAsOfInTx(ctx, tx, issueID, meta.hash)
		if err != nil {
			return nil, err
		}
		entries = append(entries, &storage.HistoryEntry{
			CommitHash: meta.hash,
			Committer:  meta.committer,
			CommitDate: parseDoltliteTimeValue(meta.date),
			Issue:      issue,
		})
	}
	return entries, nil
}
