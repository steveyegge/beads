package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// pgxTransaction implements storage.Transaction backed by a pgx.Tx.
type pgxTransaction struct {
	tx     pgx.Tx
	store  *PostgresStore
	opts   storage.BatchCreateOptions
	prefix string // cached issue_prefix for ID generation
}

var _ storage.Transaction = (*pgxTransaction)(nil)

// RunInTransaction begins a pgx transaction with the default READ COMMITTED
// isolation level and dispatches to fn with a Transaction-flavored handle.
// commitMsg is ignored — PG has no Dolt-equivalent commit graph.
func (s *PostgresStore) RunInTransaction(ctx context.Context, _ string, fn func(tx storage.Transaction) error) error {
	if s.IsClosed() {
		return errors.New("postgres: store is closed")
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return wrapErr("begin transaction", err)
	}
	pt := &pgxTransaction{tx: tx, store: s, opts: storage.BatchCreateOptions{
		OrphanHandling:       storage.OrphanAllow,
		SkipPrefixValidation: false,
	}}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(context.Background())
			panic(r)
		}
	}()
	if err := fn(pt); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return wrapErr("commit transaction", err)
	}
	return nil
}

// CreateIssue handles single-issue creation in a transaction. Resolves the
// configured ID prefix on first use, generates an ID if missing, validates,
// inserts, then writes a created event.
func (t *pgxTransaction) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	if issue == nil {
		return errors.New("postgres: CreateIssue: nil issue")
	}
	customStatuses, customTypes, err := loadCustomConfigInTx(ctx, t.tx)
	if err != nil {
		return wrapErr("load custom config for validation", err)
	}
	if err := prepareIssueForInsert(issue, customStatuses, customTypes); err != nil {
		return err
	}

	issueTable, eventTable := issueTableFor(issue)

	if issue.ID == "" {
		prefix, err := t.resolvePrefix(ctx)
		if err != nil {
			return err
		}
		if issue.PrefixOverride != "" {
			prefix = issue.PrefixOverride
		} else if issue.IDPrefix != "" {
			prefix = prefix + "-" + issue.IDPrefix
		} else if issue.Ephemeral || issue.NoHistory {
			prefix = prefix + "-wisp"
		}
		id, err := generateIssueID(ctx, t.tx, issueTable, prefix, issue, actor)
		if err != nil {
			return err
		}
		issue.ID = id
	} else if !t.opts.SkipPrefixValidation {
		prefix, err := t.resolvePrefix(ctx)
		if err == nil {
			if err := validateIDPrefix(issue.ID, prefix); err != nil {
				return err
			}
		}
	}

	if err := insertIssueRow(ctx, t.tx, issueTable, issue); err != nil {
		return err
	}
	if err := recordEvent(ctx, t.tx, eventTable, issue.ID, types.EventCreated, actor, "", ""); err != nil {
		return err
	}

	labelTable := "labels"
	if issue.Ephemeral || issue.NoHistory {
		labelTable = "wisp_labels"
	}
	for _, label := range issue.Labels {
		if err := addLabelRow(ctx, t.tx, labelTable, issue.ID, label); err != nil {
			return err
		}
	}
	for _, comment := range issue.Comments {
		if comment == nil {
			continue
		}
		createdAt := comment.CreatedAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		_, err := t.tx.Exec(ctx,
			`INSERT INTO comments (issue_id, author, text, created_at) VALUES ($1, $2, $3, $4)`,
			issue.ID, comment.Author, comment.Text, createdAt,
		)
		if err != nil {
			return wrapErr("insert imported comment", err)
		}
	}
	for _, dep := range issue.Dependencies {
		if dep == nil {
			continue
		}
		if dep.IssueID == "" {
			dep.IssueID = issue.ID
		}
		if err := addDependencyRow(ctx, t.tx, dep, t.opts.SkipPrefixValidation); err != nil {
			return err
		}
	}

	// Counter bookkeeping for hierarchical IDs (e.g. be-6fk.3) only when the
	// issue claims a child ID already.
	if parent, _, ok := parseHierarchicalID(issue.ID); ok {
		if _, err := t.tx.Exec(ctx, `
			INSERT INTO child_counters (parent_id, last_child) VALUES ($1, 0)
			ON CONFLICT (parent_id) DO NOTHING
		`, parent); err != nil {
			return wrapErr("ensure child_counter", err)
		}
	}
	return nil
}

// CreateIssues batches single-issue inserts under a single transaction.
func (t *pgxTransaction) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	for _, issue := range issues {
		if err := t.CreateIssue(ctx, issue, actor); err != nil {
			return err
		}
	}
	return nil
}

// UpdateIssue applies an update map to an issue and writes an audit event.
func (t *pgxTransaction) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	if len(updates) == 0 {
		return nil
	}
	current, err := getIssueFrom(ctx, t.tx, "issues", id)
	if err != nil {
		// Try wisps as a fallback before returning ErrNotFound.
		alt, werr := getIssueFrom(ctx, t.tx, "wisps", id)
		if werr != nil {
			return err
		}
		current = alt
	}
	target := "issues"
	if current.Ephemeral || current.NoHistory {
		target = "wisps"
	}

	// updated_at is always touched.
	if _, ok := updates["updated_at"]; !ok {
		updates["updated_at"] = time.Now().UTC()
	}

	if err := updateIssueRow(ctx, t.tx, target, id, updates); err != nil {
		return err
	}
	eventTable := "events"
	if target == "wisps" {
		eventTable = "wisp_events"
	}
	for k, v := range updates {
		if k == "updated_at" {
			continue
		}
		oldVal := readField(current, k)
		newVal := formatEventValue(v)
		if oldVal == newVal {
			continue
		}
		evtType := types.EventUpdated
		if k == "status" {
			evtType = types.EventStatusChanged
			if newVal == string(types.StatusClosed) {
				evtType = types.EventClosed
			} else if oldVal == string(types.StatusClosed) {
				evtType = types.EventReopened
			}
		}
		if err := recordEvent(ctx, t.tx, eventTable, id, evtType, actor, oldVal, newVal); err != nil {
			return err
		}
	}
	return nil
}

// CloseIssue marks an issue as closed.
func (t *pgxTransaction) CloseIssue(ctx context.Context, id, reason, actor, session string) error {
	now := time.Now().UTC()
	updates := map[string]interface{}{
		"status":            string(types.StatusClosed),
		"closed_at":         now,
		"closed_by_session": session,
		"close_reason":      reason,
		"updated_at":        now,
	}
	return t.UpdateIssue(ctx, id, updates, actor)
}

// DeleteIssue removes an issue + cascades dependent rows.
func (t *pgxTransaction) DeleteIssue(ctx context.Context, id string) error {
	for _, table := range []string{"issues", "wisps"} {
		guarded := guardTable(table)
		//nolint:gosec // table allowlisted
		stmt := fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, guarded)
		tag, err := t.tx.Exec(ctx, stmt, id)
		if err != nil {
			return wrapErr(fmt.Sprintf("delete from %s", table), err)
		}
		if tag.RowsAffected() > 0 {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", storage.ErrNotFound, id)
}

// GetIssue is a transaction-scoped read so callers can read-their-writes.
func (t *pgxTransaction) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	issue, err := getIssueFrom(ctx, t.tx, "issues", id)
	if err == nil {
		return issue, nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return nil, err
	}
	return getIssueFrom(ctx, t.tx, "wisps", id)
}

// SearchIssues runs a transaction-scoped IssueFilter query — same engine as
// the store-level method, just bound to the tx.
func (t *pgxTransaction) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	// Reuse the store-level method by reading the read-only path against the
	// pool. v1 transactions don't make heavy use of read-your-writes searches.
	return t.store.SearchIssues(ctx, query, filter)
}

// AddDependency inserts a dep edge with optional cycle check.
func (t *pgxTransaction) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	if err := addDependencyRow(ctx, t.tx, dep, t.opts.SkipPrefixValidation); err != nil {
		return err
	}
	_, eventTable := dependencyTablesForID(ctx, t.tx, dep.IssueID)
	return recordEvent(ctx, t.tx, eventTable, dep.IssueID, types.EventDependencyAdded, actor, "", dep.DependsOnID)
}

// AddDependencyWithOptions allows per-call cycle-check skipping.
func (t *pgxTransaction) AddDependencyWithOptions(ctx context.Context, dep *types.Dependency, actor string, opts storage.DependencyAddOptions) error {
	if err := addDependencyRow(ctx, t.tx, dep, opts.SkipCycleCheck); err != nil {
		return err
	}
	_, eventTable := dependencyTablesForID(ctx, t.tx, dep.IssueID)
	return recordEvent(ctx, t.tx, eventTable, dep.IssueID, types.EventDependencyAdded, actor, "", dep.DependsOnID)
}

// RemoveDependency drops the dep edge and writes an event.
func (t *pgxTransaction) RemoveDependency(ctx context.Context, issueID, dependsOnID, actor string) error {
	if err := removeDependencyRow(ctx, t.tx, issueID, dependsOnID); err != nil {
		return err
	}
	_, eventTable := dependencyTablesForID(ctx, t.tx, issueID)
	return recordEvent(ctx, t.tx, eventTable, issueID, types.EventDependencyRemoved, actor, dependsOnID, "")
}

// GetDependencyRecords returns dep records for one issue from inside the tx.
func (t *pgxTransaction) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	return getDependencyRecords(ctx, t.tx, []string{issueID})
}

// AddLabel attaches a label.
func (t *pgxTransaction) AddLabel(ctx context.Context, issueID, label, actor string) error {
	labelTable, eventTable := labelTablesForID(ctx, t.tx, issueID)
	if err := addLabelRow(ctx, t.tx, labelTable, issueID, label); err != nil {
		return err
	}
	return recordEvent(ctx, t.tx, eventTable, issueID, types.EventLabelAdded, actor, "", label)
}

// RemoveLabel detaches a label.
func (t *pgxTransaction) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	labelTable, eventTable := labelTablesForID(ctx, t.tx, issueID)
	if err := removeLabelRow(ctx, t.tx, labelTable, issueID, label); err != nil {
		return err
	}
	return recordEvent(ctx, t.tx, eventTable, issueID, types.EventLabelRemoved, actor, label, "")
}

// GetLabels reads labels in-tx, picking the correct table based on whether
// issueID is an active wisp.
func (t *pgxTransaction) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	labelTable, _ := labelTablesForID(ctx, t.tx, issueID)
	return getLabelsFromTable(ctx, t.tx, labelTable, issueID)
}

// SetConfig / GetConfig pass through.
func (t *pgxTransaction) SetConfig(ctx context.Context, key, value string) error {
	return setKV(ctx, t.tx, "config", key, value)
}
func (t *pgxTransaction) GetConfig(ctx context.Context, key string) (string, error) {
	return getKV(ctx, t.tx, "config", key)
}

// SetMetadata / GetMetadata pass through.
func (t *pgxTransaction) SetMetadata(ctx context.Context, key, value string) error {
	return setKV(ctx, t.tx, "metadata", key, value)
}
func (t *pgxTransaction) GetMetadata(ctx context.Context, key string) (string, error) {
	return getKV(ctx, t.tx, "metadata", key)
}

// SetLocalMetadata / GetLocalMetadata pass through.
func (t *pgxTransaction) SetLocalMetadata(ctx context.Context, key, value string) error {
	return setKV(ctx, t.tx, "local_metadata", key, value)
}
func (t *pgxTransaction) GetLocalMetadata(ctx context.Context, key string) (string, error) {
	return getKV(ctx, t.tx, "local_metadata", key)
}

// AddComment / ImportIssueComment / GetIssueComments — tx-scoped.
func (t *pgxTransaction) AddComment(ctx context.Context, issueID, actor, comment string) error {
	commentTable, eventTable := commentTablesForID(ctx, t.tx, issueID)
	//nolint:gosec // commentTable is allowlisted via guardTable inside commentTablesForID
	stmt := fmt.Sprintf(
		`INSERT INTO %s (issue_id, author, text, created_at) VALUES ($1, $2, $3, NOW())`,
		commentTable,
	)
	if _, err := t.tx.Exec(ctx, stmt, issueID, actor, comment); err != nil {
		return wrapErr("add comment in tx", err)
	}
	return recordEvent(ctx, t.tx, eventTable, issueID, types.EventCommented, actor, "", comment)
}

func (t *pgxTransaction) ImportIssueComment(ctx context.Context, issueID, author, text string, createdAt time.Time) (*types.Comment, error) {
	c := &types.Comment{
		IssueID:   issueID,
		Author:    author,
		Text:      text,
		CreatedAt: createdAt,
	}
	commentTable, _ := commentTablesForID(ctx, t.tx, issueID)
	//nolint:gosec // commentTable is allowlisted via guardTable inside commentTablesForID
	stmt := fmt.Sprintf(
		`INSERT INTO %s (issue_id, author, text, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		commentTable,
	)
	if err := t.tx.QueryRow(ctx, stmt, issueID, author, text, createdAt).Scan(&c.ID); err != nil {
		return nil, wrapErr("import comment in tx", err)
	}
	return c, nil
}

func (t *pgxTransaction) GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	commentTable, _ := commentTablesForID(ctx, t.tx, issueID)
	//nolint:gosec // commentTable is allowlisted via guardTable inside commentTablesForID
	q := fmt.Sprintf(`SELECT id::text, issue_id, author, text, created_at FROM %s WHERE issue_id = $1 ORDER BY created_at ASC`, commentTable)
	rows, err := t.tx.Query(ctx, q, issueID)
	if err != nil {
		return nil, wrapErr("get comments in tx", err)
	}
	defer rows.Close()
	var out []*types.Comment
	for rows.Next() {
		c := &types.Comment{}
		if err := rows.Scan(&c.ID, &c.IssueID, &c.Author, &c.Text, &c.CreatedAt); err != nil {
			return nil, wrapErr("scan comments in tx", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// readField returns a string-form of a column value pulled from an Issue. Used
// to populate event old_value when computing diffs from updates.
func readField(issue *types.Issue, column string) string {
	switch column {
	case "status":
		return string(issue.Status)
	case "priority":
		return fmt.Sprintf("%d", issue.Priority)
	case "issue_type":
		return string(issue.IssueType)
	case "assignee":
		return issue.Assignee
	case "owner":
		return issue.Owner
	case "title":
		return issue.Title
	case "description":
		return issue.Description
	case "design":
		return issue.Design
	case "acceptance_criteria":
		return issue.AcceptanceCriteria
	case "notes":
		return issue.Notes
	case "spec_id":
		return issue.SpecID
	case "external_ref":
		if issue.ExternalRef == nil {
			return ""
		}
		return *issue.ExternalRef
	case "close_reason":
		return issue.CloseReason
	case "closed_at":
		if issue.ClosedAt == nil {
			return ""
		}
		return issue.ClosedAt.UTC().Format(time.RFC3339)
	case "started_at":
		if issue.StartedAt == nil {
			return ""
		}
		return issue.StartedAt.UTC().Format(time.RFC3339)
	case "due_at":
		if issue.DueAt == nil {
			return ""
		}
		return issue.DueAt.UTC().Format(time.RFC3339)
	case "defer_until":
		if issue.DeferUntil == nil {
			return ""
		}
		return issue.DeferUntil.UTC().Format(time.RFC3339)
	}
	return ""
}

// resolvePrefix reads the issue_prefix config row once per tx.
func (t *pgxTransaction) resolvePrefix(ctx context.Context) (string, error) {
	if t.prefix != "" {
		return t.prefix, nil
	}
	v, err := getKV(ctx, t.tx, "config", "issue_prefix")
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", fmt.Errorf("%w: issue_prefix config is missing (run 'bd init --prefix <prefix>' first)", storage.ErrNotInitialized)
	}
	t.prefix = trimSuffix(v, "-")
	return t.prefix, nil
}

func trimSuffix(s, suffix string) string {
	if len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix {
		return s[:len(s)-len(suffix)]
	}
	return s
}

// loadCustomConfigInTx reads custom_statuses and custom_types within the
// supplied transaction. Used by CreateIssue to populate ValidateWithCustom so
// project-defined types (e.g. "session", "molecule") aren't rejected as
// "invalid issue type". Querying inside the same tx ensures we see config
// writes made earlier in the same transaction (e.g. by bd init flows that
// set types and immediately create infra issues).
func loadCustomConfigInTx(ctx context.Context, tx pgx.Tx) ([]string, []string, error) {
	statuses, err := scanNameRows(ctx, tx, `SELECT name FROM custom_statuses ORDER BY name`)
	if err != nil {
		return nil, nil, fmt.Errorf("custom_statuses: %w", err)
	}
	customTypes, err := scanNameRows(ctx, tx, `SELECT name FROM custom_types ORDER BY name`)
	if err != nil {
		return nil, nil, fmt.Errorf("custom_types: %w", err)
	}
	return statuses, customTypes, nil
}

func scanNameRows(ctx context.Context, tx pgx.Tx, query string) ([]string, error) {
	rows, err := tx.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
