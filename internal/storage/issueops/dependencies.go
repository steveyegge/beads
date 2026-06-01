package issueops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

type DepTargetKind int

const (
	DepTargetIssue DepTargetKind = iota
	DepTargetWisp
	DepTargetExternal
)

func ClassifyDepTarget(ctx context.Context, tx *sql.Tx, dep *types.Dependency, isCrossPrefix bool) DepTargetKind {
	if isCrossPrefix || strings.HasPrefix(dep.DependsOnID, "external:") {
		return DepTargetExternal
	}
	if IsActiveWispInTx(ctx, tx, dep.DependsOnID) {
		return DepTargetWisp
	}
	return DepTargetIssue
}

// AddDependencyOpts configures AddDependencyInTx behavior.
// When fields are left empty, AddDependencyInTx performs wisp routing
// automatically via IsActiveWispInTx. Callers that have already determined
// routing (e.g., DoltStore with its pre-tx wisp cache) can set fields
// explicitly to skip the redundant DB check.
type AddDependencyOpts struct {
	// SourceTable is the table to validate the source issue exists in
	// ("issues" or "wisps"). Auto-detected via wisp routing if empty. The
	// write-side dep table is derived from this plus the resolved target kind
	// via DepTableFor.
	SourceTable string
	// TargetTable is the table to validate the target issue exists in
	// ("issues" or "wisps"). Auto-detected via wisp routing if empty. Ignored
	// when target validation is skipped (external or cross-prefix).
	TargetTable string
	// DepTables are the tables to scan for cycle detection. Defaults to all
	// six split dep tables; cycles can hop across endpoint classes via
	// parent-child or blocks edges through intermediate nodes.
	DepTables []string
	// IsCrossPrefix is true when source and target have different prefixes,
	// meaning the target lives in another rig's database.
	IsCrossPrefix bool
	// SkipCycleCheck skips the recursive pre-insert cycle check for callers
	// that intentionally trade validation cost for bulk graph wiring speed.
	SkipCycleCheck bool
	TargetKind     *DepTargetKind
}

// AddDependencyInTx validates and inserts a dependency within an existing
// transaction. It handles:
//   - Wisp routing (auto-detected or caller-provided)
//   - Source/target existence validation
//   - Cross-type blocking validation (GH#1495)
//   - Cycle detection via recursive CTE across both dependency tables
//   - Idempotent same-type updates (metadata only)
//   - Type conflict detection
//
// The caller is responsible for transaction lifecycle, dolt commits, and
// any cache invalidation.
func AddDependencyInTx(ctx context.Context, tx *sql.Tx, dep *types.Dependency, actor string, opts AddDependencyOpts) error {
	// Auto-detect source routing if not provided.
	sourceTable := opts.SourceTable
	if sourceTable == "" {
		if IsActiveWispInTx(ctx, tx, dep.IssueID) {
			sourceTable = "wisps"
		} else {
			sourceTable = "issues"
		}
	}
	sourceIsWisp := sourceTable == "wisps"

	// Auto-detect target routing if not provided (skip for external/cross-prefix).
	targetTable := opts.TargetTable
	if targetTable == "" && !strings.HasPrefix(dep.DependsOnID, "external:") && !opts.IsCrossPrefix {
		if IsActiveWispInTx(ctx, tx, dep.DependsOnID) {
			targetTable = "wisps"
		} else {
			targetTable = "issues"
		}
	}
	if targetTable == "" {
		targetTable = "issues"
	}

	depTables := opts.DepTables
	if len(depTables) == 0 {
		depTables = AllDepTables()
	}

	metadata := dep.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	// Validate source issue exists and get its type.
	var sourceType string
	//nolint:gosec // G201: sourceTable is "issues" or "wisps"
	if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT issue_type FROM %s WHERE id = ?`, sourceTable), dep.IssueID).Scan(&sourceType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("issue %s not found", dep.IssueID)
		}
		return fmt.Errorf("failed to check issue existence: %w", err)
	}

	// Validate target issue exists (skip for external and cross-prefix refs).
	var targetType string
	if !strings.HasPrefix(dep.DependsOnID, "external:") && !opts.IsCrossPrefix {
		//nolint:gosec // G201: targetTable is "issues" or "wisps"
		if err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT issue_type FROM %s WHERE id = ?`, targetTable), dep.DependsOnID).Scan(&targetType); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("issue %s not found", dep.DependsOnID)
			}
			return fmt.Errorf("failed to check target issue existence: %w", err)
		}
	}

	// Cross-type blocking validation (GH#1495): tasks can only block tasks,
	// epics can only block epics.
	if dep.Type == types.DepBlocks && targetType != "" {
		sourceIsEpic := sourceType == string(types.TypeEpic)
		targetIsEpic := targetType == string(types.TypeEpic)
		if sourceIsEpic != targetIsEpic {
			if sourceIsEpic {
				return fmt.Errorf("epics can only block other epics, not tasks")
			}
			return fmt.Errorf("tasks can only block other tasks, not epics")
		}
	}

	if !opts.SkipCycleCheck {
		if err := CheckDependencyCycleInTx(ctx, tx, dep, depTables); err != nil {
			return err
		}
	}

	var kind DepTargetKind
	if opts.TargetKind != nil {
		kind = *opts.TargetKind
	} else {
		kind = ClassifyDepTarget(ctx, tx, dep, opts.IsCrossPrefix)
	}
	writeTable := DepTableFor(sourceIsWisp, kind)
	targetCol := DepTargetColumn(kind)

	// Check for existing dependency between the same pair. Each split dep
	// table has exactly one typed target column, so a (source_id, target_id)
	// lookup uniquely identifies the edge — no COALESCE needed.
	var existingType string
	//nolint:gosec // G201: writeTable from DepTableFor; targetCol from DepTargetColumn
	err := tx.QueryRowContext(ctx, fmt.Sprintf(`SELECT type FROM %s WHERE source_id = ? AND %s = ?`, writeTable, targetCol),
		dep.IssueID, dep.DependsOnID).Scan(&existingType)
	if err == nil {
		if existingType == string(dep.Type) {
			// Same type — idempotent; update metadata.
			//nolint:gosec // G201: writeTable from DepTableFor; targetCol from DepTargetColumn
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET metadata = ? WHERE source_id = ? AND %s = ?`, writeTable, targetCol),
				metadata, dep.IssueID, dep.DependsOnID); err != nil {
				return fmt.Errorf("failed to update dependency metadata: %w", err)
			}
			return nil
		}
		return fmt.Errorf("dependency %s -> %s already exists with type %q (requested %q); remove it first with 'bd dep remove' then re-add",
			dep.IssueID, dep.DependsOnID, existingType, dep.Type)
	} else if !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("failed to check existing dependency: %w", err)
	}

	//nolint:gosec // G201: writeTable from DepTableFor; targetCol from DepTargetColumn
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (source_id, %s, type, created_at, created_by, metadata, thread_id)
		VALUES (?, ?, ?, NOW(), ?, ?, ?)
	`, writeTable, targetCol), dep.IssueID, dep.DependsOnID, dep.Type, actor, metadata, dep.ThreadID); err != nil {
		return fmt.Errorf("failed to add dependency: %w", err)
	}

	srcIsWisp := sourceIsWisp
	var affectedIssues, affectedWisps []string
	var aerr error
	if srcIsWisp {
		affectedIssues, affectedWisps, aerr = AffectedByDepChangeForWispInTx(ctx, tx, dep.IssueID, dep.DependsOnID, dep.Type)
	} else {
		affectedIssues, affectedWisps, aerr = AffectedByDepChangeInTx(ctx, tx, dep.IssueID, dep.DependsOnID, dep.Type)
	}
	if aerr != nil {
		return fmt.Errorf("affected by add dependency %s -> %s: %w", dep.IssueID, dep.DependsOnID, aerr)
	}
	if dep.Type == types.DepBlocks || dep.Type == types.DepConditionalBlocks {
		if err := markDirectBlockingDependencySourceInTx(ctx, tx, dep.IssueID, srcIsWisp, dep.DependsOnID, kind); err != nil {
			return fmt.Errorf("mark direct is_blocked after add dependency %s -> %s: %w", dep.IssueID, dep.DependsOnID, err)
		}
		affectedIssues, affectedWisps = removeSourceFromAffected(dep.IssueID, srcIsWisp, affectedIssues, affectedWisps)
	}
	if dep.Type == types.DepParentChild {
		// Parent-child adds are not monotonic: adding an already-closed child can
		// satisfy an any-children waits-for gate and unblock the waiter.
		if err := RecomputeIsBlockedInTx(ctx, tx, affectedIssues, affectedWisps); err != nil {
			return fmt.Errorf("recompute is_blocked after add dependency %s -> %s: %w", dep.IssueID, dep.DependsOnID, err)
		}
		return nil
	}
	if err := MarkIsBlockedInTx(ctx, tx, affectedIssues, affectedWisps); err != nil {
		return fmt.Errorf("mark is_blocked after add dependency %s -> %s: %w", dep.IssueID, dep.DependsOnID, err)
	}
	return nil
}

func removeSourceFromAffected(source string, srcIsWisp bool, issueIDs, wispIDs []string) ([]string, []string) {
	if srcIsWisp {
		return issueIDs, removeID(wispIDs, source)
	}
	return removeID(issueIDs, source), wispIDs
}

func removeID(ids []string, remove string) []string {
	if len(ids) == 0 {
		return ids
	}
	out := ids[:0]
	for _, id := range ids {
		if id != remove {
			out = append(out, id)
		}
	}
	return out
}

//nolint:gosec // G201: table names are selected from fixed issue/wisp tables.
func markDirectBlockingDependencySourceInTx(ctx context.Context, tx *sql.Tx, source string, srcIsWisp bool, target string, targetKind DepTargetKind) error {
	sourceTable := "issues"
	if srcIsWisp {
		sourceTable = "wisps"
	}
	targetTable := ""
	switch targetKind {
	case DepTargetIssue:
		targetTable = "issues"
	case DepTargetWisp:
		targetTable = "wisps"
	default:
		return nil
	}

	_, err := tx.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s s SET s.is_blocked = 1
		WHERE s.id = ?
		  AND s.is_blocked = 0
		  AND s.status <> 'closed' AND s.status <> 'pinned'
		  AND EXISTS (
		    SELECT 1 FROM %s t
		    WHERE t.id = ?
		      AND t.status <> 'closed' AND t.status <> 'pinned'
		  )
	`, sourceTable, targetTable), source, target)
	return err
}

// CheckDependencyCycleInTx rejects self-dependencies and blocking dependency
// cycles before a dependency insert. The caller may pass a restricted depTables
// list for a known storage bucket; nil uses all dependency tables.
func CheckDependencyCycleInTx(ctx context.Context, tx *sql.Tx, dep *types.Dependency, depTables []string) error {
	if dep.IssueID == dep.DependsOnID {
		return fmt.Errorf("cannot add self-dependency: %s cannot depend on itself", dep.IssueID)
	}
	if dep.Type != types.DepBlocks && dep.Type != types.DepConditionalBlocks {
		return nil
	}
	if len(depTables) == 0 {
		depTables = cycleDetectionTables()
	}
	var reachable int
	query := cycleReachabilityQuery(depTables)
	if err := tx.QueryRowContext(ctx, query, dep.DependsOnID, dep.IssueID).Scan(&reachable); err != nil {
		return fmt.Errorf("failed to check for dependency cycle: %w", err)
	}
	if reachable > 0 {
		return fmt.Errorf("adding dependency would create a cycle")
	}
	return nil
}

// cycleReachabilityQuery uses UNION distinct recursion so cyclic and diamond
// graphs terminate by unique reachable node instead of enumerating paths. The
// recursive step UNIONs across the supplied split dep tables, each projecting
// its typed target column as `depends_on_id` so the CTE can treat edges
// uniformly regardless of endpoint class.
func cycleReachabilityQuery(depTables []string) string {
	if len(depTables) == 1 {
		col := DepTargetColumnForTable(depTables[0])
		return fmt.Sprintf(`
			WITH RECURSIVE reachable(node) AS (
				SELECT ?
				UNION
				SELECT d.%s
				FROM reachable r
				JOIN %s d ON d.source_id = r.node AND d.type IN ('blocks', 'conditional-blocks')
			)
			SELECT COUNT(*) FROM reachable WHERE node = ?
		`, col, depTables[0])
	}

	var unions []string
	for _, t := range depTables {
		col := DepTargetColumnForTable(t)
		if col == "" {
			continue
		}
		unions = append(unions, fmt.Sprintf("SELECT source_id, %s AS depends_on_id FROM %s WHERE type IN ('blocks', 'conditional-blocks')", col, t))
	}
	unionQuery := strings.Join(unions, " UNION ")
	return fmt.Sprintf(`
		WITH RECURSIVE reachable(node) AS (
			SELECT ?
			UNION
			SELECT d.depends_on_id
			FROM reachable r
			JOIN (%s) d ON d.source_id = r.node
		)
		SELECT COUNT(*) FROM reachable WHERE node = ?
	`, unionQuery)
}

func cycleDetectionTables() []string {
	return AllDepTables()
}

func DeleteWispFromDependenciesInTx(ctx context.Context, tx *sql.Tx, wispID string) error {
	for _, table := range TargetDepTables(DepTargetWisp) {
		//nolint:gosec // G201: table from TargetDepTables (fixed constants)
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE depends_on_wisp_id = ?", table), wispID); err != nil {
			if isTableNotExistError(err) {
				continue
			}
			return fmt.Errorf("delete wisp %s from %s: %w", wispID, table, err)
		}
	}
	return nil
}

//nolint:gosec // G201: inClause contains only ? placeholders
func DeleteWispsFromDependenciesInTx(ctx context.Context, tx *sql.Tx, wispIDs []string) error {
	if len(wispIDs) == 0 {
		return nil
	}
	inClause, args := buildSQLInClause(wispIDs)
	for _, table := range TargetDepTables(DepTargetWisp) {
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE depends_on_wisp_id IN (%s)", table, inClause),
			args...); err != nil {
			if isTableNotExistError(err) {
				continue
			}
			return fmt.Errorf("delete wisps from %s: %w", table, err)
		}
	}
	return nil
}

// Dependency target rewrites reinsert matching rows because Dolt can leave the
// stored generated depends_on_id column stale after a split target column is
// updated by FK cascade.
func UpdateWispIDInDependenciesInTx(ctx context.Context, tx *sql.Tx, oldID, newID string) error {
	for _, table := range TargetDepTables(DepTargetWisp) {
		if err := replaceDependencyTargetInTx(ctx, tx, table, "depends_on_wisp_id", oldID, newID); err != nil {
			return fmt.Errorf("update wisp %s -> %s in %s: %w", oldID, newID, table, err)
		}
	}
	return nil
}

func UpdateIssueIDInDependenciesInTx(ctx context.Context, tx *sql.Tx, oldID, newID string) error {
	for _, table := range TargetDepTables(DepTargetIssue) {
		if err := replaceDependencyTargetInTx(ctx, tx, table, "depends_on_issue_id", oldID, newID); err != nil {
			return fmt.Errorf("update issue target %s -> %s in %s: %w", oldID, newID, table, err)
		}
	}
	for _, table := range SourceDepTables(false) {
		//nolint:gosec // G201: table from SourceDepTables (fixed constants)
		if _, err := tx.ExecContext(ctx,
			fmt.Sprintf("UPDATE %s SET source_id = ? WHERE source_id = ?", table),
			newID, oldID); err != nil {
			if isTableNotExistError(err) {
				continue
			}
			return fmt.Errorf("update issue source %s -> %s in %s: %w", oldID, newID, table, err)
		}
	}
	return nil
}

func replaceDependencyTargetInTx(ctx context.Context, tx *sql.Tx, table, column, oldID, newID string) error {
	// Dolt does not reliably recompute the stored generated depends_on_id when
	// only the split target column changes. Reinsert rows so the generated key
	// is calculated from the new target value.
	if err := checkRenameTargetCollision(ctx, tx, table, column, newID); err != nil {
		return err
	}

	type depRow struct {
		sourceID  string
		target    string
		depType   string
		createdAt sql.NullTime
		createdBy sql.NullString
		metadata  sql.NullString
		threadID  sql.NullString
	}

	rows := make([]depRow, 0)
	//nolint:gosec // table and column are hardcoded by callers.
	queryRows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT source_id, %s, type, created_at, created_by, metadata, thread_id
		FROM %s
		WHERE %s = ?
	`, column, table, column), oldID)
	if err != nil {
		if isTableNotExistError(err) {
			return nil
		}
		return fmt.Errorf("query dependency targets: %w", err)
	}
	for queryRows.Next() {
		var row depRow
		if err := queryRows.Scan(&row.sourceID, &row.target, &row.depType, &row.createdAt, &row.createdBy, &row.metadata, &row.threadID); err != nil {
			_ = queryRows.Close()
			return fmt.Errorf("scan dependency target: %w", err)
		}
		row.target = newID
		rows = append(rows, row)
	}
	_ = queryRows.Close()
	if err := queryRows.Err(); err != nil {
		return fmt.Errorf("iterate dependency targets: %w", err)
	}

	//nolint:gosec // table and column are hardcoded by callers.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, table, column), oldID); err != nil {
		return fmt.Errorf("delete old dependency target: %w", err)
	}
	for _, row := range rows {
		//nolint:gosec // table and column are hardcoded by callers.
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s (source_id, %s, type, created_at, created_by, metadata, thread_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, table, column), row.sourceID, row.target, row.depType, nullTimeValue(row.createdAt), nullStringValue(row.createdBy), nullStringValue(row.metadata), nullStringValue(row.threadID)); err != nil {
			return fmt.Errorf("insert replacement dependency target: %w", err)
		}
	}
	return nil
}

func nullStringValue(value sql.NullString) any {
	if !value.Valid {
		return nil
	}
	return value.String
}

func nullTimeValue(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time
}

func RetargetInboundDependenciesToWispInTx(ctx context.Context, tx *sql.Tx, id string) error {
	srcTables := TargetDepTables(DepTargetIssue)
	dstTables := TargetDepTables(DepTargetWisp)
	for i, srcTable := range srcTables {
		dstTable := dstTables[i]
		if err := retargetInboundCrossTableInTx(ctx, tx, srcTable, dstTable, "depends_on_issue_id", "depends_on_wisp_id", id); err != nil {
			return fmt.Errorf("retarget inbound dependencies to wisp in %s->%s for %s: %w", srcTable, dstTable, id, err)
		}
	}
	return nil
}

func RetargetInboundDependenciesToIssueInTx(ctx context.Context, tx *sql.Tx, id string) error {
	srcTables := TargetDepTables(DepTargetWisp)
	dstTables := TargetDepTables(DepTargetIssue)
	for i, srcTable := range srcTables {
		dstTable := dstTables[i]
		if err := retargetInboundCrossTableInTx(ctx, tx, srcTable, dstTable, "depends_on_wisp_id", "depends_on_issue_id", id); err != nil {
			return fmt.Errorf("retarget inbound dependencies to issue in %s->%s for %s: %w", srcTable, dstTable, id, err)
		}
	}
	return nil
}

//nolint:gosec // G201: table and column names are fixed constants from TargetDepTables.
func retargetInboundCrossTableInTx(ctx context.Context, tx *sql.Tx, srcTable, dstTable, srcCol, dstCol, id string) error {
	if err := checkRenameTargetCollision(ctx, tx, dstTable, dstCol, id); err != nil {
		return err
	}
	type depRow struct {
		sourceID  string
		depType   string
		createdAt sql.NullTime
		createdBy sql.NullString
		metadata  sql.NullString
		threadID  sql.NullString
	}
	rows := make([]depRow, 0)
	queryRows, err := tx.QueryContext(ctx, fmt.Sprintf(`
		SELECT source_id, type, created_at, created_by, metadata, thread_id
		FROM %s
		WHERE %s = ?
	`, srcTable, srcCol), id)
	if err != nil {
		if isTableNotExistError(err) {
			return nil
		}
		return fmt.Errorf("query retarget rows: %w", err)
	}
	for queryRows.Next() {
		var row depRow
		if err := queryRows.Scan(&row.sourceID, &row.depType, &row.createdAt, &row.createdBy, &row.metadata, &row.threadID); err != nil {
			_ = queryRows.Close()
			return fmt.Errorf("scan retarget row: %w", err)
		}
		rows = append(rows, row)
	}
	_ = queryRows.Close()
	if err := queryRows.Err(); err != nil {
		return fmt.Errorf("iterate retarget rows: %w", err)
	}
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM %s WHERE %s = ?`, srcTable, srcCol), id); err != nil {
		return fmt.Errorf("delete retarget rows from %s: %w", srcTable, err)
	}
	for _, row := range rows {
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			INSERT INTO %s (source_id, %s, type, created_at, created_by, metadata, thread_id)
			VALUES (?, ?, ?, ?, ?, ?, ?)
		`, dstTable, dstCol), row.sourceID, id, row.depType, nullTimeValue(row.createdAt), nullStringValue(row.createdBy), nullStringValue(row.metadata), nullStringValue(row.threadID)); err != nil {
			return fmt.Errorf("insert retarget row into %s: %w", dstTable, err)
		}
	}
	return nil
}

// UpdateIssueIDInDependencyTargetsInTx is called after the issues PK is updated
// from oldID to newID. FK ON UPDATE CASCADE has already propagated
// depends_on_issue_id from oldID to newID across the *_issue_dependencies
// tables, so no rewrite is needed.
func UpdateIssueIDInDependencyTargetsInTx(ctx context.Context, tx *sql.Tx, _, newID string) error {
	for _, table := range TargetDepTables(DepTargetIssue) {
		if err := checkRenameTargetCollision(ctx, tx, table, "depends_on_issue_id", newID); err != nil {
			return err
		}
	}
	return nil
}

func checkRenameTargetCollision(_ context.Context, _ *sql.Tx, _, _, _ string) error {
	// Under the split-dependency schema each table has exactly one typed
	// target column, so the legacy multi-column collision check is no longer
	// meaningful; row uniqueness is enforced by the composite primary key.
	return nil
}

// RemoveDependencyInTx removes a dependency between two issues within an
// existing transaction. Automatically routes by source class (issue vs wisp)
// and probes all three target-typed tables for the edge — the target class
// at insert time may differ from current class (e.g., the target was
// promoted from issue to wisp after the edge was created), so we cannot
// assume which table holds the row from `dependsOnID` alone.
func RemoveDependencyInTx(ctx context.Context, tx *sql.Tx, issueID, dependsOnID string) error {
	isWisp := IsActiveWispInTx(ctx, tx, issueID)

	// Find which of the three source-matching tables actually holds the edge,
	// and capture its type so we can dispatch the right affected-set helper.
	tables := SourceDepTables(isWisp)
	probe := []struct {
		table     string
		targetCol string
	}{
		{tables[0], "depends_on_issue_id"},
		{tables[1], "depends_on_wisp_id"},
		{tables[2], "depends_on_external_id"},
	}

	var hitTable, depType string
	for _, p := range probe {
		//nolint:gosec // G201: table and target column are fixed constants from SourceDepTables
		err := tx.QueryRowContext(ctx, fmt.Sprintf(
			`SELECT type FROM %s WHERE source_id = ? AND %s = ?`, p.table, p.targetCol),
			issueID, dependsOnID).Scan(&depType)
		if err == nil {
			hitTable = p.table
			break
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("lookup dependency type for %s -> %s: %w", issueID, dependsOnID, err)
		}
	}
	if hitTable == "" {
		return nil
	}

	var targetCol string
	switch hitTable {
	case probe[0].table:
		targetCol = probe[0].targetCol
	case probe[1].table:
		targetCol = probe[1].targetCol
	default:
		targetCol = probe[2].targetCol
	}

	//nolint:gosec // G201: hitTable and targetCol are fixed constants from SourceDepTables
	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE source_id = ? AND %s = ?`, hitTable, targetCol),
		issueID, dependsOnID); err != nil {
		return fmt.Errorf("remove dependency: %w", err)
	}

	var affectedIssues, affectedWisps []string
	var aerr error
	if isWisp {
		affectedIssues, affectedWisps, aerr = AffectedByDepChangeForWispInTx(ctx, tx, issueID, dependsOnID, types.DependencyType(depType))
	} else {
		affectedIssues, affectedWisps, aerr = AffectedByDepChangeInTx(ctx, tx, issueID, dependsOnID, types.DependencyType(depType))
	}
	if aerr != nil {
		return fmt.Errorf("affected by remove dependency %s -> %s: %w", issueID, dependsOnID, aerr)
	}
	if err := RecomputeIsBlockedInTx(ctx, tx, affectedIssues, affectedWisps); err != nil {
		return fmt.Errorf("recompute is_blocked after remove dependency %s -> %s: %w", issueID, dependsOnID, err)
	}
	return nil
}

// GetIssuesByIDsInTx retrieves multiple issues by ID within an existing
// transaction, including labels. Automatically routes each ID to the correct
// table (issues/wisps). Uses batched IN clauses.
//
// wispSet is an optional pre-built set of active wisp IDs scoped to
// cover ids (see WispIDSetInTx). Pass nil to have the helper build
// a scoped set internally; callers hydrating multiple batches inside
// one tx can build the set once over the union of their IDs and
// reuse it across calls.
//
//nolint:gosec // G201: table names come from WispTableRouting (hardcoded constants)
func GetIssuesByIDsInTx(ctx context.Context, tx *sql.Tx, ids []string, wispSet map[string]struct{}) ([]*types.Issue, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	if wispSet == nil {
		var err error
		wispSet, err = WispIDSetInTx(ctx, tx, ids)
		if err != nil {
			return nil, fmt.Errorf("get issues by IDs: build wisp set: %w", err)
		}
	}

	// Partition IDs by wisp status.
	wispIDs, permIDs := partitionByWispSet(ids, wispSet)

	var allIssues []*types.Issue
	for _, pair := range []struct {
		table    string
		labelTbl string
		ids      []string
	}{
		{"issues", "labels", permIDs},
		{"wisps", "wisp_labels", wispIDs},
	} {
		if len(pair.ids) == 0 {
			continue
		}
		for start := 0; start < len(pair.ids); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(pair.ids) {
				end = len(pair.ids)
			}
			batch := pair.ids[start:end]

			placeholders := make([]string, len(batch))
			args := make([]any, len(batch))
			for i, id := range batch {
				placeholders[i] = "?"
				args[i] = id
			}
			inClause := strings.Join(placeholders, ",")

			rows, err := tx.QueryContext(ctx, fmt.Sprintf(
				`SELECT %s FROM %s WHERE id IN (%s)`,
				IssueSelectColumns, pair.table, inClause), args...)
			if err != nil {
				return nil, fmt.Errorf("get issues by IDs from %s: %w", pair.table, err)
			}
			issueMap := make(map[string]*types.Issue)
			for rows.Next() {
				issue, scanErr := ScanIssueFrom(rows)
				if scanErr != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("get issues by IDs: scan: %w", scanErr)
				}
				allIssues = append(allIssues, issue)
				issueMap[issue.ID] = issue
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("get issues by IDs: rows: %w", err)
			}

			// Hydrate labels.
			if len(issueMap) > 0 {
				labelRows, err := tx.QueryContext(ctx, fmt.Sprintf(
					`SELECT issue_id, label FROM %s WHERE issue_id IN (%s) ORDER BY issue_id, label`,
					pair.labelTbl, inClause), args...)
				if err != nil {
					return nil, fmt.Errorf("get issues by IDs: labels from %s: %w", pair.labelTbl, err)
				}
				for labelRows.Next() {
					var issueID, label string
					if scanErr := labelRows.Scan(&issueID, &label); scanErr != nil {
						_ = labelRows.Close()
						return nil, fmt.Errorf("get issues by IDs: scan label: %w", scanErr)
					}
					if issue, ok := issueMap[issueID]; ok {
						issue.Labels = append(issue.Labels, label)
					}
				}
				_ = labelRows.Close()
				if err := labelRows.Err(); err != nil {
					return nil, fmt.Errorf("get issues by IDs: label rows: %w", err)
				}
			}
		}
	}

	return allIssues, nil
}

// GetDependenciesWithMetadataInTx returns issues that the given issueID depends on,
// along with the dependency type. Works within an existing transaction.
// Queries both dependency tables to handle cross-table dependencies.
//
//nolint:gosec // G201: table names come from hardcoded constants
func GetDependenciesWithMetadataInTx(ctx context.Context, tx *sql.Tx, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	type depMeta struct {
		depID, depType string
	}

	// Query all split dep tables to find all dependencies.
	var deps []depMeta
	for _, depTable := range AllDepTables() {
		col := DepTargetColumnForTable(depTable)
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(
			`SELECT %s AS depends_on_id, type FROM %s WHERE source_id = ?`, col, depTable), issueID)
		if err != nil {
			if optionalBlockedTable(depTable) && isTableNotExistError(err) {
				continue
			}
			return nil, fmt.Errorf("get dependencies from %s: %w", depTable, err)
		}
		for rows.Next() {
			var d depMeta
			if scanErr := rows.Scan(&d.depID, &d.depType); scanErr != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("get dependencies: scan: %w", scanErr)
			}
			deps = append(deps, d)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get dependencies: rows from %s: %w", depTable, err)
		}
	}

	if len(deps) == 0 {
		return nil, nil
	}

	// Fetch all dependency target issues.
	ids := make([]string, len(deps))
	for i, d := range deps {
		ids[i] = d.depID
	}
	issues, err := GetIssuesByIDsInTx(ctx, tx, ids, nil)
	if err != nil {
		return nil, fmt.Errorf("get dependencies: fetch issues: %w", err)
	}
	issueMap := make(map[string]*types.Issue, len(issues))
	for _, iss := range issues {
		issueMap[iss.ID] = iss
	}

	var results []*types.IssueWithDependencyMetadata
	for _, d := range deps {
		issue, ok := issueMap[d.depID]
		if !ok {
			continue
		}
		results = append(results, &types.IssueWithDependencyMetadata{
			Issue:          *issue,
			DependencyType: types.DependencyType(d.depType),
		})
	}
	return results, nil
}

// GetDependentsWithMetadataInTx returns issues that depend on the given issueID
// along with the dependency type. Works within an existing transaction.
//
//nolint:gosec // G201: table names come from WispTableRouting (hardcoded constants)
func GetDependentsWithMetadataInTx(ctx context.Context, tx *sql.Tx, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	type depMeta struct {
		depID, depType string
	}

	// Query all split dep tables to find all dependents.
	var deps []depMeta
	for _, depTable := range AllDepTables() {
		col := DepTargetColumnForTable(depTable)
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(
			`SELECT source_id, type FROM %s WHERE %s = ?`, depTable, col), issueID)
		if err != nil {
			if optionalBlockedTable(depTable) && isTableNotExistError(err) {
				continue
			}
			return nil, fmt.Errorf("get dependents from %s: %w", depTable, err)
		}
		for rows.Next() {
			var d depMeta
			if scanErr := rows.Scan(&d.depID, &d.depType); scanErr != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("get dependents: scan: %w", scanErr)
			}
			deps = append(deps, d)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get dependents: rows from %s: %w", depTable, err)
		}
	}

	if len(deps) == 0 {
		return nil, nil
	}

	// Fetch all dependent issues.
	ids := make([]string, len(deps))
	for i, d := range deps {
		ids[i] = d.depID
	}
	issues, err := GetIssuesByIDsInTx(ctx, tx, ids, nil)
	if err != nil {
		return nil, fmt.Errorf("get dependents: fetch issues: %w", err)
	}
	issueMap := make(map[string]*types.Issue, len(issues))
	for _, iss := range issues {
		issueMap[iss.ID] = iss
	}

	var results []*types.IssueWithDependencyMetadata
	for _, d := range deps {
		issue, ok := issueMap[d.depID]
		if !ok {
			continue
		}
		results = append(results, &types.IssueWithDependencyMetadata{
			Issue:          *issue,
			DependencyType: types.DependencyType(d.depType),
		})
	}
	return results, nil
}

// GetDependenciesInTx returns issues that the given issueID depends on.
// Queries both dependencies and wisp_dependencies tables.
//
//nolint:gosec // G201: table names come from hardcoded constants
func GetDependenciesInTx(ctx context.Context, tx *sql.Tx, issueID string) ([]*types.Issue, error) {
	var ids []string
	for _, depTable := range AllDepTables() {
		col := DepTargetColumnForTable(depTable)
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(
			`SELECT %s AS depends_on_id FROM %s WHERE source_id = ?`, col, depTable), issueID)
		if err != nil {
			if optionalBlockedTable(depTable) && isTableNotExistError(err) {
				continue
			}
			return nil, fmt.Errorf("get dependencies from %s: %w", depTable, err)
		}
		for rows.Next() {
			var id string
			if scanErr := rows.Scan(&id); scanErr != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("get dependencies: scan: %w", scanErr)
			}
			ids = append(ids, id)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get dependencies: rows from %s: %w", depTable, err)
		}
	}

	if len(ids) == 0 {
		return nil, nil
	}

	return GetIssuesByIDsInTx(ctx, tx, ids, nil)
}

// GetDependentsInTx returns issues that depend on the given issueID.
// Queries both dependencies and wisp_dependencies tables.
//
//nolint:gosec // G201: table names come from hardcoded constants
func GetDependentsInTx(ctx context.Context, tx *sql.Tx, issueID string) ([]*types.Issue, error) {
	var ids []string
	for _, depTable := range AllDepTables() {
		col := DepTargetColumnForTable(depTable)
		rows, err := tx.QueryContext(ctx, fmt.Sprintf(
			`SELECT source_id FROM %s WHERE %s = ?`, depTable, col), issueID)
		if err != nil {
			if optionalBlockedTable(depTable) && isTableNotExistError(err) {
				continue
			}
			return nil, fmt.Errorf("get dependents from %s: %w", depTable, err)
		}
		for rows.Next() {
			var id string
			if scanErr := rows.Scan(&id); scanErr != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("get dependents: scan: %w", scanErr)
			}
			ids = append(ids, id)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("get dependents: rows from %s: %w", depTable, err)
		}
	}

	if len(ids) == 0 {
		return nil, nil
	}

	return GetIssuesByIDsInTx(ctx, tx, ids, nil)
}
