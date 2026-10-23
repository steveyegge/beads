// Package dolt — iter_dependents.go
//
// Streaming iterator over the issues × dependencies join. Same dedicated-
// conn pattern as iter_issues.go — labels (when needed) hydrate on a
// second pool connection so the cursor never deadlocks.
//
// Both dependency tables are included so results match the slice path
// GetDependentsWithMetadata, which the human-readable `bd show` uses.
// IterDependentsWithMetadata joins each dependency table to each issue table,
// as four queries in one read transaction. WispTableRouting routes every edge
// to exactly one table by its source's wisp status, so in step only
// `dependencies` × `issues` (permanent-source edges) and `wisp_dependencies`
// × `wisps` (wisp-source edges) return rows. Where the planes are out of step
// (GH#3445), the other two joins make the stream return what the slice path
// returns, which reads the edges with issueops.listDepEdgeEndsInTx and the
// rows with issueops.GetIssuesByIDsInTx: a missing wisp table reads as empty,
// and each dependent streams once, with the type of its `dependencies` row
// when it has one, and with its `wisps` row when its id is resident in both
// planes.
// IterDependenciesWithMetadata delegates to the slice path: a dependency's
// *target* may be permanent or wisp independent of which table holds the
// edge, which a single streaming join cannot resolve, and the method has no
// streaming caller today.
package dolt

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dberrors"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/storage/sqlbuild"
	"github.com/steveyegge/beads/internal/types"
)

// doltDependentsIter streams the issues × dependency join of
// IterDependentsWithMetadata, returning each row as an
// IssueWithDependencyMetadata. It runs its queries in order inside one read
// transaction, opening the next when the previous one is drained, and skips
// a dependent whose id an earlier row already returned.
type doltDependentsIter struct {
	s       *DoltStore
	conn    *sql.Conn
	tx      *sql.Tx
	issueID string
	queries []string // not yet run, in order
	rows    *sql.Rows
	seen    map[string]bool
	cur     *types.IssueWithDependencyMetadata
	err     error
	closed  bool
}

// IterDependentsWithMetadata streams dependents (issues that depend on
// issueID) with the relationship type attached. Includes both permanent
// (`dependencies`) and wisp (`wisp_dependencies`) dependents so the stream
// matches GetDependentsWithMetadata. A dependent streams with the first row
// the joins return for its id, so their order decides its type and its row:
// `dependencies` before `wisp_dependencies`, as listDepEdgeEndsInTx keeps the
// durable type, and for each, `wisps` before `issues`, as GetIssuesByIDsInTx
// reads an id resident in both planes from `wisps`. Each join streams in
// created_at order, so the dependents with a `dependencies` row stream first,
// then those with only a `wisp_dependencies` row. See the package doc for
// which joins return rows.
func (s *DoltStore) IterDependentsWithMetadata(ctx context.Context, issueID string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	var queries []string
	for _, depTable := range []string{"dependencies", "wisp_dependencies"} {
		for _, issueTable := range []string{"wisps", "issues"} {
			queries = append(queries, fmt.Sprintf(`
				SELECT %s, d.type
				FROM %s i
				JOIN %s d ON d.issue_id = i.id
				%s
				WHERE %s = ?
				ORDER BY i.created_at ASC
			`, prefixedIssueColumns("i"), issueTable, depTable, sqlbuild.LeaseJoin("i"), depTargetExprWithAlias("d")))
		}
	}
	return s.iterIssuesWithDepType(ctx, issueID, queries...)
}

func depTargetExprWithAlias(alias string) string {
	if alias == "" {
		return depTargetExpr
	}
	return fmt.Sprintf("COALESCE(%s.depends_on_issue_id, %s.depends_on_wisp_id, %s.depends_on_external)", alias, alias, alias)
}

// IterDependenciesWithMetadata streams dependencies (issues issueID depends
// on) with the relationship type attached. It delegates to the slice path
// GetDependenciesWithMetadata (which resolves targets across both `issues`
// and `wisps`) rather than a streaming join, because a dependency's target
// table cannot be determined from the edge table alone. There is no streaming
// caller for this direction today; revisit if one appears.
func (s *DoltStore) IterDependenciesWithMetadata(ctx context.Context, issueID string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	deps, err := s.GetDependenciesWithMetadata(ctx, issueID)
	if err != nil {
		return nil, err
	}
	return storage.NewSliceIter(deps), nil
}

// iterIssuesWithDepType streams the rows of queries, each bound to issueID,
// in order. The queries share one read transaction, so together they see one
// snapshot, as a single statement would: a concurrent promotion cannot move
// an edge out of the table not yet read into the one already read. The first
// query runs before it returns, so its failure is this call's error.
func (s *DoltStore) iterIssuesWithDepType(ctx context.Context, issueID string, queries ...string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	if s.closed.Load() {
		return nil, ErrStoreClosed
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("iter dependents: acquire conn: %w", err)
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("iter dependents: begin read tx: %w", err)
	}
	it := &doltDependentsIter{s: s, conn: conn, tx: tx, issueID: issueID, queries: queries, seen: make(map[string]bool)}
	if err := it.runNextQuery(ctx); err != nil {
		_ = it.Close()
		return nil, err
	}
	return it, nil
}

// runNextQuery runs the next query in it.queries. A missing wisp table (one
// sqlbuild.OptionalWispTable names: dolt-ignored, so a server whose working
// set was replaced can lack it until the next writable open recreates it)
// leaves the query with no rows. Any other error fails the read, a missing
// `issues`, `dependencies` or `leases` included.
func (it *doltDependentsIter) runNextQuery(ctx context.Context) error {
	q := it.queries[0]
	it.queries = it.queries[1:]
	rows, err := it.tx.QueryContext(ctx, q, it.issueID)
	if err != nil {
		if name, ok := dberrors.MissingTableName(err); ok && sqlbuild.OptionalWispTable(name) {
			return nil
		}
		return fmt.Errorf("iter dependents: query: %w", err)
	}
	it.rows = rows
	return nil
}

func (it *doltDependentsIter) Next(ctx context.Context) bool {
	if it.err != nil || it.closed {
		return false
	}
	for {
		if err := ctx.Err(); err != nil {
			it.err = err
			return false
		}
		if it.rows == nil {
			if len(it.queries) == 0 {
				return false
			}
			if err := it.runNextQuery(ctx); err != nil {
				it.err = err
				return false
			}
			continue
		}
		if !it.rows.Next() {
			err := it.rows.Err()
			// The transaction's connection runs one statement at a time, so
			// the drained cursor must close before the next query can run.
			_ = it.rows.Close()
			it.rows = nil
			if err != nil {
				it.err = err
				return false
			}
			continue
		}
		iss, depType, err := scanIssueWithDepTypeFrom(it.rows)
		if err != nil {
			it.err = fmt.Errorf("iter dependents: scan: %w", err)
			return false
		}
		if it.seen[iss.ID] {
			continue
		}
		it.seen[iss.ID] = true
		// NOTE: labels are NOT hydrated here. The corresponding slice path
		// (GetDependentsWithMetadata in dolt/dependencies.go) does not hydrate
		// either, so this matches for parity. Callers that need labels on
		// dependents must call GetLabels(id) explicitly.
		it.cur = &types.IssueWithDependencyMetadata{
			Issue:          *iss,
			DependencyType: types.DependencyType(depType),
		}
		return true
	}
}

func (it *doltDependentsIter) Value() *types.IssueWithDependencyMetadata { return it.cur }
func (it *doltDependentsIter) Err() error                                { return it.err }

func (it *doltDependentsIter) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true
	if it.rows != nil {
		_ = it.rows.Close()
	}
	if it.tx != nil {
		_ = it.tx.Rollback()
	}
	if it.conn != nil {
		_ = it.conn.Close()
	}
	return nil
}

// prefixedIssueColumns returns IssueSelectColumns with each issues-row column
// prefixed by the given table alias (e.g. "i") so it can be used in JOIN
// queries. The lease overlay columns keep their own leases. qualifier — the
// caller's FROM must include sqlbuild.LeaseJoin(alias).
func prefixedIssueColumns(alias string) string {
	cols := splitCommaList(sqlbuild.IssueBaseColumns)
	for i, c := range cols {
		cols[i] = alias + "." + c
	}
	return joinCommaList(cols) + ", " + sqlbuild.LeaseSelectColumns
}

// splitCommaList splits a multi-line comma-separated column list into
// trimmed, non-empty entries. Used by prefixedIssueColumns.
func splitCommaList(s string) []string {
	out := make([]string, 0, 32)
	cur := make([]byte, 0, 32)
	flush := func() {
		t := trimSpaces(cur)
		if len(t) > 0 {
			out = append(out, string(t))
		}
		cur = cur[:0]
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ',' {
			flush()
			continue
		}
		cur = append(cur, c)
	}
	flush()
	return out
}

func joinCommaList(items []string) string {
	if len(items) == 0 {
		return ""
	}
	n := 0
	for _, it := range items {
		n += len(it) + 2
	}
	out := make([]byte, 0, n)
	for i, it := range items {
		if i > 0 {
			out = append(out, ',', ' ')
		}
		out = append(out, it...)
	}
	return string(out)
}

func trimSpaces(b []byte) []byte {
	start, end := 0, len(b)
	for start < end {
		c := b[start]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			start++
			continue
		}
		break
	}
	for end > start {
		c := b[end-1]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			end--
			continue
		}
		break
	}
	return b[start:end]
}

// scanIssueWithDepTypeFrom scans a row that contains all IssueSelectColumns
// followed by a single dep_type column. Mirrors ScanIssueFrom from issueops
// but with the trailing dep_type field.
func scanIssueWithDepTypeFrom(rows *sql.Rows) (*types.Issue, string, error) {
	// Reuse ScanIssueFrom by wrapping rows in a Scan adapter that consumes
	// the trailing dep_type. ScanIssueFrom expects the exact column count
	// of IssueSelectColumns; the JOIN query selects one extra column, so we
	// bind the trailing field explicitly.
	var depType string
	rowAdapter := &issueWithExtraScan{rows: rows, extra: []any{&depType}}
	iss, err := issueops.ScanIssueFrom(rowAdapter)
	if err != nil {
		return nil, "", err
	}
	return iss, depType, nil
}

// issueWithExtraScan adapts a *sql.Rows so ScanIssueFrom consumes the
// IssueSelectColumns AND the trailing dep_type column in one Scan call.
type issueWithExtraScan struct {
	rows  *sql.Rows
	extra []any
}

func (a *issueWithExtraScan) Scan(dest ...any) error {
	all := make([]any, 0, len(dest)+len(a.extra))
	all = append(all, dest...)
	all = append(all, a.extra...)
	return a.rows.Scan(all...)
}
