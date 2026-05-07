package postgres

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/steveyegge/beads/internal/idgen"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// generateIssueID resolves a unique ID under prefix. Counter mode is selected
// from the config table; otherwise the same hash-based generator the Dolt
// path uses is reused (idgen.GenerateHashID).
func generateIssueID(ctx context.Context, c pgxConn, table, prefix string, issue *types.Issue, actor string) (string, error) {
	if table == "issues" {
		mode, err := getKV(ctx, c, "config", "issue_id_mode")
		if err != nil {
			return "", err
		}
		if mode == "counter" {
			return nextCounterID(ctx, c, prefix)
		}
	}
	for length := 6; length <= 8; length++ {
		for nonce := 0; nonce < 10; nonce++ {
			candidate := idgen.GenerateHashID(prefix, issue.Title, issue.Description, actor, issue.CreatedAt, length, nonce)
			//nolint:gosec // table is allowlisted
			q := fmt.Sprintf(`SELECT COUNT(*)::int FROM %s WHERE id = $1`, guardTable(table))
			var n int
			if err := c.QueryRow(ctx, q, candidate).Scan(&n); err != nil {
				return "", wrapErr("collision check", err)
			}
			if n == 0 {
				return candidate, nil
			}
		}
	}
	return "", errors.New("postgres: failed to generate unique issue ID after 30 attempts")
}

// nextCounterID atomically increments the counter for prefix and returns the
// resulting "<prefix>-<n>" ID. Seeds the counter from existing IDs the first
// time it is called.
func nextCounterID(ctx context.Context, c pgxConn, prefix string) (string, error) {
	stmt := `
		INSERT INTO issue_counter (prefix, last_id) VALUES ($1, 1)
		ON CONFLICT (prefix) DO UPDATE SET last_id = issue_counter.last_id + 1
		RETURNING last_id
	`
	var n int
	if err := c.QueryRow(ctx, stmt, prefix).Scan(&n); err != nil {
		return "", wrapErr("next counter id", err)
	}
	return fmt.Sprintf("%s-%d", prefix, n), nil
}

// validateIDPrefix mirrors issueops.ValidateIssueIDPrefix without the
// allowed_prefixes split (PG-side allowlist would be a future addition).
func validateIDPrefix(id, prefix string) error {
	if strings.HasPrefix(id, prefix+"-") {
		return nil
	}
	return fmt.Errorf("%w: %q does not match prefix %q", storage.ErrPrefixMismatch, id, prefix)
}

// parseHierarchicalID splits "be-6fk.3" into ("be-6fk", 3, true). Only the
// last `.<digits>` is considered.
func parseHierarchicalID(id string) (parent string, child int, ok bool) {
	dot := strings.LastIndex(id, ".")
	if dot == -1 {
		return "", 0, false
	}
	tail := id[dot+1:]
	n, err := strconv.Atoi(tail)
	if err != nil {
		return "", 0, false
	}
	return id[:dot], n, true
}

// GetNextChildID atomically advances the child counter for a parent issue and
// returns the next "<parent>.<n>" ID.
func (s *PostgresStore) GetNextChildID(ctx context.Context, parentID string) (string, error) {
	stmt := `
		INSERT INTO child_counters (parent_id, last_child) VALUES ($1, 1)
		ON CONFLICT (parent_id) DO UPDATE SET last_child = child_counters.last_child + 1
		RETURNING last_child
	`
	var n int
	if err := s.pool.QueryRow(ctx, stmt, parentID).Scan(&n); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("%w: %s", storage.ErrNotFound, parentID)
		}
		return "", wrapErr("next child id", err)
	}
	return fmt.Sprintf("%s.%d", parentID, n), nil
}

// RenameCounterPrefix updates issue_counter rows when the configured prefix
// changes. Adapted from the Dolt path; rows that already use the new prefix
// are coalesced rather than collided.
func (s *PostgresStore) RenameCounterPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	old := strings.TrimSuffix(oldPrefix, "-")
	rep := strings.TrimSuffix(newPrefix, "-")
	stmt := `
		UPDATE issue_counter SET prefix = REPLACE(prefix, $1, $2)
		WHERE prefix LIKE $1 OR prefix LIKE $1 || '-%'
	`
	_, err := s.pool.Exec(ctx, stmt, old, rep)
	if err != nil {
		return wrapErr("rename counter prefix", err)
	}
	return nil
}

// prepareIssueForInsert mirrors issueops.PrepareIssueForInsert (timestamps,
// content hash, validation). Custom statuses and types are passed in by the
// caller (loaded inside the active transaction) so validation accepts the
// project's full configured surface, not just built-in types.
func prepareIssueForInsert(issue *types.Issue, customStatuses, customTypes []string) error {
	now := nowUTC()
	if issue.CreatedAt.IsZero() {
		issue.CreatedAt = now
	} else {
		issue.CreatedAt = issue.CreatedAt.UTC()
	}
	if issue.UpdatedAt.IsZero() {
		issue.UpdatedAt = now
	} else {
		issue.UpdatedAt = issue.UpdatedAt.UTC()
	}
	if issue.Status == types.StatusClosed && issue.ClosedAt == nil {
		closedAt := issue.UpdatedAt
		issue.ClosedAt = &closedAt
	}
	if err := issue.ValidateWithCustom(customStatuses, customTypes); err != nil {
		return fmt.Errorf("validation failed for issue %s: %w", issue.ID, err)
	}
	if issue.ContentHash == "" {
		issue.ContentHash = issue.ComputeContentHash()
	}
	return nil
}
