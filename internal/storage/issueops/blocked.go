package issueops

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

type blockingDepRecord struct {
	issueID, dependsOnID, depType string
	metadata                      sql.NullString
}

func optionalBlockedTable(table string) bool {
	return table == "wisps" || table == "wisp_dependencies"
}

// ComputeBlockedIDsInTx returns the set of issue IDs that are blocked by active issues.
// This is the core computation without caching — callers manage their own cache.
//
//nolint:gosec // G201: tables are hardcoded
func ComputeBlockedIDsInTx(ctx context.Context, tx *sql.Tx, includeWisps bool) ([]string, map[string]bool, error) {
	issueTables := []string{"issues"}
	depTables := []string{"dependencies"}
	if includeWisps {
		issueTables = append(issueTables, "wisps")
		depTables = append(depTables, "wisp_dependencies")
	}

	// Step 1: Get all active issue IDs
	activeIDs := make(map[string]bool)
	for _, table := range issueTables {
		activeRows, err := tx.QueryContext(ctx, fmt.Sprintf(`
			SELECT id FROM %s
			WHERE status NOT IN ('closed', 'pinned')
		`, table))
		if err != nil {
			if optionalBlockedTable(table) && isTableNotExistError(err) {
				continue
			}
			return nil, nil, fmt.Errorf("compute blocked IDs: active issues from %s: %w", table, err)
		}
		for activeRows.Next() {
			var id string
			if err := activeRows.Scan(&id); err != nil {
				_ = activeRows.Close()
				return nil, nil, fmt.Errorf("compute blocked IDs: scan active issue: %w", err)
			}
			activeIDs[id] = true
		}
		_ = activeRows.Close()
		if err := activeRows.Err(); err != nil {
			return nil, nil, fmt.Errorf("compute blocked IDs: active rows from %s: %w", table, err)
		}
	}

	// Step 2: Get blocking deps, waits-for gates, and conditional-blocks
	type depRecord struct {
		issueID, dependsOnID, depType string
		metadata                      sql.NullString
	}
	var allDeps []depRecord
	for _, depTable := range depTables {
		depRows, err := tx.QueryContext(ctx, fmt.Sprintf(`
			SELECT issue_id, depends_on_id, type, metadata FROM %s
			WHERE type IN ('blocks', 'waits-for', 'conditional-blocks')
		`, depTable))
		if err != nil {
			if optionalBlockedTable(depTable) && isTableNotExistError(err) {
				continue
			}
			return nil, nil, fmt.Errorf("compute blocked IDs: deps from %s: %w", depTable, err)
		}
		for depRows.Next() {
			var rec depRecord
			if err := depRows.Scan(&rec.issueID, &rec.dependsOnID, &rec.depType, &rec.metadata); err != nil {
				_ = depRows.Close()
				return nil, nil, fmt.Errorf("compute blocked IDs: scan dep: %w", err)
			}
			allDeps = append(allDeps, rec)
		}
		_ = depRows.Close()
		if err := depRows.Err(); err != nil {
			return nil, nil, fmt.Errorf("compute blocked IDs: dep rows from %s: %w", depTable, err)
		}
	}

	// Step 3: Filter direct blockers; collect waits-for edges
	type waitsForDep struct {
		issueID   string
		spawnerID string
		gate      string
	}
	var waitsForDeps []waitsForDep
	needsClosedChildren := false

	blockedSet := make(map[string]bool)
	for _, rec := range allDeps {
		switch rec.depType {
		case string(types.DepBlocks), string(types.DepConditionalBlocks):
			if activeIDs[rec.issueID] && activeIDs[rec.dependsOnID] {
				blockedSet[rec.issueID] = true
			}
		case string(types.DepWaitsFor):
			if !activeIDs[rec.issueID] {
				continue
			}
			gate := types.ParseWaitsForGateMetadata(rec.metadata.String)
			if gate == types.WaitsForAnyChildren {
				needsClosedChildren = true
			}
			waitsForDeps = append(waitsForDeps, waitsForDep{
				issueID:   rec.issueID,
				spawnerID: rec.dependsOnID,
				gate:      gate,
			})
		}
	}

	if len(waitsForDeps) > 0 {
		// Step 4: Load direct children for each waits-for spawner.
		spawnerIDs := make(map[string]struct{})
		for _, dep := range waitsForDeps {
			spawnerIDs[dep.spawnerID] = struct{}{}
		}

		allSpawnerIDs := make([]string, 0, len(spawnerIDs))
		for spawnerID := range spawnerIDs {
			allSpawnerIDs = append(allSpawnerIDs, spawnerID)
		}

		spawnerChildren := make(map[string][]string)
		childIDs := make(map[string]struct{})
		for _, depTbl := range depTables {
			for start := 0; start < len(allSpawnerIDs); start += queryBatchSize {
				end := start + queryBatchSize
				if end > len(allSpawnerIDs) {
					end = len(allSpawnerIDs)
				}
				placeholders, args := buildSQLInClause(allSpawnerIDs[start:end])

				childRows, err := tx.QueryContext(ctx, fmt.Sprintf(`
					SELECT issue_id, depends_on_id FROM %s
					WHERE type = 'parent-child' AND depends_on_id IN (%s)
				`, depTbl, placeholders), args...)
				if err != nil {
					if optionalBlockedTable(depTbl) && isTableNotExistError(err) {
						continue
					}
					return nil, nil, fmt.Errorf("compute blocked IDs: children from %s: %w", depTbl, err)
				}

				for childRows.Next() {
					var childID, parentID string
					if err := childRows.Scan(&childID, &parentID); err != nil {
						_ = childRows.Close()
						return nil, nil, fmt.Errorf("compute blocked IDs: scan child: %w", err)
					}
					spawnerChildren[parentID] = append(spawnerChildren[parentID], childID)
					childIDs[childID] = struct{}{}
				}
				_ = childRows.Close()
				if err := childRows.Err(); err != nil {
					return nil, nil, fmt.Errorf("compute blocked IDs: child rows from %s: %w", depTbl, err)
				}
			}
		}

		closedChildren := make(map[string]bool)
		if needsClosedChildren && len(childIDs) > 0 {
			allChildIDs := make([]string, 0, len(childIDs))
			for childID := range childIDs {
				allChildIDs = append(allChildIDs, childID)
			}

			for _, issueTbl := range issueTables {
				for start := 0; start < len(allChildIDs); start += queryBatchSize {
					end := start + queryBatchSize
					if end > len(allChildIDs) {
						end = len(allChildIDs)
					}
					placeholders, args := buildSQLInClause(allChildIDs[start:end])

					closedRows, err := tx.QueryContext(ctx, fmt.Sprintf(`
						SELECT id FROM %s
						WHERE status = 'closed' AND id IN (%s)
					`, issueTbl, placeholders), args...)
					if err != nil {
						if optionalBlockedTable(issueTbl) && isTableNotExistError(err) {
							continue
						}
						return nil, nil, fmt.Errorf("compute blocked IDs: closed children from %s: %w", issueTbl, err)
					}
					for closedRows.Next() {
						var childID string
						if err := closedRows.Scan(&childID); err != nil {
							_ = closedRows.Close()
							return nil, nil, fmt.Errorf("compute blocked IDs: scan closed child: %w", err)
						}
						closedChildren[childID] = true
					}
					_ = closedRows.Close()
					if err := closedRows.Err(); err != nil {
						return nil, nil, fmt.Errorf("compute blocked IDs: closed child rows from %s: %w", issueTbl, err)
					}
				}
			}
		}

		// Step 5: Evaluate waits-for gates against current child states.
		for _, dep := range waitsForDeps {
			children := spawnerChildren[dep.spawnerID]
			switch dep.gate {
			case types.WaitsForAnyChildren:
				if len(children) == 0 {
					continue
				}
				hasClosedChild := false
				hasActiveChild := false
				for _, childID := range children {
					if closedChildren[childID] {
						hasClosedChild = true
						break
					}
					if activeIDs[childID] {
						hasActiveChild = true
					}
				}
				if !hasClosedChild && hasActiveChild {
					blockedSet[dep.issueID] = true
				}
			default:
				for _, childID := range children {
					if activeIDs[childID] {
						blockedSet[dep.issueID] = true
						break
					}
				}
			}
		}
	}

	result := make([]string, 0, len(blockedSet))
	for id := range blockedSet {
		result = append(result, id)
	}

	return result, activeIDs, nil
}

// ComputeBlockedCandidateIDsInTx returns the subset of candidate IDs that are
// blocked. It is the limited-query counterpart to ComputeBlockedIDsInTx: callers
// that only need a small page can avoid scanning the full dependency graph.
func ComputeBlockedCandidateIDsInTx(ctx context.Context, tx *sql.Tx, candidateIDs []string, includeWisps bool) ([]string, error) {
	if len(candidateIDs) == 0 {
		return nil, nil
	}

	issueTables := []string{"issues"}
	depTables := []string{"dependencies"}
	if includeWisps {
		issueTables = append(issueTables, "wisps")
		depTables = append(depTables, "wisp_dependencies")
	}

	candidateSet, err := loadActiveIDSetInTx(ctx, tx, issueTables, candidateIDs)
	if err != nil {
		return nil, fmt.Errorf("compute blocked candidates: active candidates: %w", err)
	}
	if len(candidateSet) == 0 {
		return nil, nil
	}

	activeCandidateIDs := make([]string, 0, len(candidateSet))
	for id := range candidateSet {
		activeCandidateIDs = append(activeCandidateIDs, id)
	}

	deps, err := loadBlockingDepsForIssueIDsInTx(ctx, tx, depTables, activeCandidateIDs)
	if err != nil {
		return nil, err
	}
	blockerIDs := make([]string, 0, len(deps))
	for _, dep := range deps {
		blockerIDs = append(blockerIDs, dep.dependsOnID)
	}
	activeBlockers, err := loadActiveIDSetInTx(ctx, tx, issueTables, blockerIDs)
	if err != nil {
		return nil, fmt.Errorf("compute blocked candidates: active blockers: %w", err)
	}

	blockedSet := make(map[string]bool)
	for id := range candidateSet {
		activeBlockers[id] = true
	}
	if err := markBlockedFromDepsInTx(ctx, tx, issueTables, depTables, deps, activeBlockers, blockedSet); err != nil {
		return nil, err
	}

	childParents, err := loadParentIDsForChildrenInTx(ctx, tx, depTables, activeCandidateIDs)
	if err != nil {
		return nil, err
	}
	if len(childParents) > 0 {
		parentIDs := make([]string, 0, len(childParents))
		for _, parentID := range childParents {
			parentIDs = append(parentIDs, parentID)
		}
		parentSet, err := loadActiveIDSetInTx(ctx, tx, issueTables, parentIDs)
		if err != nil {
			return nil, fmt.Errorf("compute blocked candidates: active parents: %w", err)
		}
		if len(parentSet) == 0 {
			return resultFromBlockedSet(blockedSet), nil
		}
		activeParentIDs := make([]string, 0, len(parentSet))
		for parentID := range parentSet {
			activeParentIDs = append(activeParentIDs, parentID)
		}
		parentDeps, err := loadBlockingDepsForIssueIDsInTx(ctx, tx, depTables, activeParentIDs)
		if err != nil {
			return nil, err
		}
		parentBlockerIDs := make([]string, 0, len(parentDeps))
		for _, dep := range parentDeps {
			parentBlockerIDs = append(parentBlockerIDs, dep.dependsOnID)
		}
		activeParentBlockers, err := loadActiveIDSetInTx(ctx, tx, issueTables, parentBlockerIDs)
		if err != nil {
			return nil, fmt.Errorf("compute blocked candidates: active parent blockers: %w", err)
		}
		for parentID := range parentSet {
			activeParentBlockers[parentID] = true
		}
		parentBlockedSet := make(map[string]bool)
		if err := markBlockedFromDepsInTx(ctx, tx, issueTables, depTables, parentDeps, activeParentBlockers, parentBlockedSet); err != nil {
			return nil, err
		}
		for childID, parentID := range childParents {
			if parentBlockedSet[parentID] && parentSet[parentID] {
				blockedSet[childID] = true
			}
		}
	}

	return resultFromBlockedSet(blockedSet), nil
}

func resultFromBlockedSet(blockedSet map[string]bool) []string {
	result := make([]string, 0, len(blockedSet))
	for id := range blockedSet {
		result = append(result, id)
	}
	return result
}

type candidateWaitsForDep struct {
	issueID   string
	spawnerID string
	gate      string
}

func markBlockedFromDepsInTx(
	ctx context.Context,
	tx *sql.Tx,
	issueTables []string,
	depTables []string,
	deps []blockingDepRecord,
	activeIDs map[string]bool,
	blockedSet map[string]bool,
) error {
	var waitsForDeps []candidateWaitsForDep
	needsClosedChildren := false

	for _, rec := range deps {
		switch rec.depType {
		case string(types.DepBlocks), string(types.DepConditionalBlocks):
			if activeIDs[rec.issueID] && activeIDs[rec.dependsOnID] {
				blockedSet[rec.issueID] = true
			}
		case string(types.DepWaitsFor):
			if !activeIDs[rec.issueID] {
				continue
			}
			gate := types.ParseWaitsForGateMetadata(rec.metadata.String)
			if gate == types.WaitsForAnyChildren {
				needsClosedChildren = true
			}
			waitsForDeps = append(waitsForDeps, candidateWaitsForDep{
				issueID:   rec.issueID,
				spawnerID: rec.dependsOnID,
				gate:      gate,
			})
		}
	}

	if len(waitsForDeps) == 0 {
		return nil
	}

	spawnerIDs := make(map[string]struct{})
	for _, dep := range waitsForDeps {
		spawnerIDs[dep.spawnerID] = struct{}{}
	}

	allSpawnerIDs := make([]string, 0, len(spawnerIDs))
	for spawnerID := range spawnerIDs {
		allSpawnerIDs = append(allSpawnerIDs, spawnerID)
	}

	spawnerChildren := make(map[string][]string)
	childIDs := make(map[string]struct{})
	for _, depTbl := range depTables {
		for start := 0; start < len(allSpawnerIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(allSpawnerIDs) {
				end = len(allSpawnerIDs)
			}
			placeholders, args := buildSQLInClause(allSpawnerIDs[start:end])

			childRows, err := tx.QueryContext(ctx, fmt.Sprintf(`
				SELECT issue_id, depends_on_id FROM %s
				WHERE type = 'parent-child' AND depends_on_id IN (%s)
			`, depTbl, placeholders), args...)
			if err != nil {
				if optionalBlockedTable(depTbl) && isTableNotExistError(err) {
					continue
				}
				return fmt.Errorf("compute blocked IDs: children from %s: %w", depTbl, err)
			}

			for childRows.Next() {
				var childID, parentID string
				if err := childRows.Scan(&childID, &parentID); err != nil {
					_ = childRows.Close()
					return fmt.Errorf("compute blocked IDs: scan child: %w", err)
				}
				spawnerChildren[parentID] = append(spawnerChildren[parentID], childID)
				childIDs[childID] = struct{}{}
			}
			_ = childRows.Close()
			if err := childRows.Err(); err != nil {
				return fmt.Errorf("compute blocked IDs: child rows from %s: %w", depTbl, err)
			}
		}
	}

	closedChildren := make(map[string]bool)
	if len(childIDs) > 0 {
		allChildIDs := make([]string, 0, len(childIDs))
		for childID := range childIDs {
			allChildIDs = append(allChildIDs, childID)
		}

		activeChildren, err := loadActiveIDSetInTx(ctx, tx, issueTables, allChildIDs)
		if err != nil {
			return fmt.Errorf("compute blocked IDs: active children: %w", err)
		}
		for childID := range activeChildren {
			activeIDs[childID] = true
		}

		if needsClosedChildren {
			for _, issueTbl := range issueTables {
				for start := 0; start < len(allChildIDs); start += queryBatchSize {
					end := start + queryBatchSize
					if end > len(allChildIDs) {
						end = len(allChildIDs)
					}
					placeholders, args := buildSQLInClause(allChildIDs[start:end])

					closedRows, err := tx.QueryContext(ctx, fmt.Sprintf(`
						SELECT id FROM %s
						WHERE status = 'closed' AND id IN (%s)
					`, issueTbl, placeholders), args...)
					if err != nil {
						if optionalBlockedTable(issueTbl) && isTableNotExistError(err) {
							continue
						}
						return fmt.Errorf("compute blocked IDs: closed children from %s: %w", issueTbl, err)
					}
					for closedRows.Next() {
						var childID string
						if err := closedRows.Scan(&childID); err != nil {
							_ = closedRows.Close()
							return fmt.Errorf("compute blocked IDs: scan closed child: %w", err)
						}
						closedChildren[childID] = true
					}
					_ = closedRows.Close()
					if err := closedRows.Err(); err != nil {
						return fmt.Errorf("compute blocked IDs: closed child rows from %s: %w", issueTbl, err)
					}
				}
			}
		}
	}

	for _, dep := range waitsForDeps {
		children := spawnerChildren[dep.spawnerID]
		switch dep.gate {
		case types.WaitsForAnyChildren:
			if len(children) == 0 {
				continue
			}
			hasClosedChild := false
			hasActiveChild := false
			for _, childID := range children {
				if closedChildren[childID] {
					hasClosedChild = true
					break
				}
				if activeIDs[childID] {
					hasActiveChild = true
				}
			}
			if !hasClosedChild && hasActiveChild {
				blockedSet[dep.issueID] = true
			}
		default:
			for _, childID := range children {
				if activeIDs[childID] {
					blockedSet[dep.issueID] = true
					break
				}
			}
		}
	}

	return nil
}

func loadBlockingDepsForIssueIDsInTx(ctx context.Context, tx *sql.Tx, depTables []string, issueIDs []string) ([]blockingDepRecord, error) {
	var deps []blockingDepRecord
	for _, depTable := range depTables {
		for start := 0; start < len(issueIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(issueIDs) {
				end = len(issueIDs)
			}
			placeholders, args := buildSQLInClause(issueIDs[start:end])
			rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
				SELECT issue_id, depends_on_id, type, metadata FROM %s
				WHERE issue_id IN (%s)
				  AND type IN ('blocks', 'waits-for', 'conditional-blocks')
			`, depTable, placeholders), args...)
			if err != nil {
				if optionalBlockedTable(depTable) && isTableNotExistError(err) {
					break
				}
				return nil, fmt.Errorf("compute blocked IDs: deps from %s: %w", depTable, err)
			}
			for rows.Next() {
				var rec blockingDepRecord
				if err := rows.Scan(&rec.issueID, &rec.dependsOnID, &rec.depType, &rec.metadata); err != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("compute blocked IDs: scan dep: %w", err)
				}
				deps = append(deps, rec)
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("compute blocked IDs: dep rows from %s: %w", depTable, err)
			}
		}
	}
	return deps, nil
}

func loadActiveIDSetInTx(ctx context.Context, tx *sql.Tx, issueTables []string, ids []string) (map[string]bool, error) {
	activeIDs := make(map[string]bool)
	if len(ids) == 0 {
		return activeIDs, nil
	}
	for _, issueTable := range issueTables {
		for start := 0; start < len(ids); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(ids) {
				end = len(ids)
			}
			placeholders, args := buildSQLInClause(ids[start:end])
			rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
				SELECT id FROM %s
				WHERE id IN (%s)
				  AND status NOT IN ('closed', 'pinned')
			`, issueTable, placeholders), args...)
			if err != nil {
				if optionalBlockedTable(issueTable) && isTableNotExistError(err) {
					break
				}
				return nil, fmt.Errorf("active IDs from %s: %w", issueTable, err)
			}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("scan active issue: %w", err)
				}
				activeIDs[id] = true
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("active rows from %s: %w", issueTable, err)
			}
		}
	}
	return activeIDs, nil
}

func loadParentIDsForChildrenInTx(ctx context.Context, tx *sql.Tx, depTables []string, childIDs []string) (map[string]string, error) {
	childParents := make(map[string]string)
	for _, depTable := range depTables {
		for start := 0; start < len(childIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(childIDs) {
				end = len(childIDs)
			}
			placeholders, args := buildSQLInClause(childIDs[start:end])
			rows, err := tx.QueryContext(ctx, fmt.Sprintf(`
				SELECT issue_id, depends_on_id FROM %s
				WHERE issue_id IN (%s)
				  AND type = 'parent-child'
			`, depTable, placeholders), args...)
			if err != nil {
				if optionalBlockedTable(depTable) && isTableNotExistError(err) {
					break
				}
				return nil, fmt.Errorf("candidate parents from %s: %w", depTable, err)
			}
			for rows.Next() {
				var childID, parentID string
				if err := rows.Scan(&childID, &parentID); err != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("scan candidate parent: %w", err)
				}
				childParents[childID] = parentID
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("candidate parent rows from %s: %w", depTable, err)
			}
		}
	}
	return childParents, nil
}

// GetChildrenWithParentsInTx returns a map of childID -> parentID for direct children
// (parent-child deps) of the given parent IDs.
//
//nolint:gosec // G201: tables are hardcoded
func GetChildrenWithParentsInTx(ctx context.Context, tx *sql.Tx, parentIDs []string) (map[string]string, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	result := make(map[string]string)
	for _, depTable := range []string{"dependencies", "wisp_dependencies"} {
		for start := 0; start < len(parentIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(parentIDs) {
				end = len(parentIDs)
			}
			placeholders, args := buildSQLInClause(parentIDs[start:end])

			query := fmt.Sprintf(`
				SELECT issue_id, depends_on_id FROM %s
				WHERE type = 'parent-child' AND depends_on_id IN (%s)
			`, depTable, placeholders)
			rows, err := tx.QueryContext(ctx, query, args...)
			if err != nil {
				if optionalBlockedTable(depTable) && isTableNotExistError(err) {
					break
				}
				return nil, fmt.Errorf("get children with parents from %s: %w", depTable, err)
			}
			for rows.Next() {
				var childID, parentID string
				if err := rows.Scan(&childID, &parentID); err != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("scan children with parents: %w", err)
				}
				result[childID] = parentID
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("children with parents rows from %s: %w", depTable, err)
			}
		}
	}
	return result, nil
}

// GetChildrenOfIssuesInTx returns IDs of direct children (parent-child deps) of the given issue IDs.
//
//nolint:gosec // G201: tables are hardcoded
func GetChildrenOfIssuesInTx(ctx context.Context, tx *sql.Tx, parentIDs []string) ([]string, error) {
	if len(parentIDs) == 0 {
		return nil, nil
	}
	var children []string
	for _, depTable := range []string{"dependencies", "wisp_dependencies"} {
		for start := 0; start < len(parentIDs); start += queryBatchSize {
			end := start + queryBatchSize
			if end > len(parentIDs) {
				end = len(parentIDs)
			}
			placeholders, args := buildSQLInClause(parentIDs[start:end])

			query := fmt.Sprintf(`
				SELECT issue_id FROM %s
				WHERE type = 'parent-child' AND depends_on_id IN (%s)
			`, depTable, placeholders)
			rows, err := tx.QueryContext(ctx, query, args...)
			if err != nil {
				if optionalBlockedTable(depTable) && isTableNotExistError(err) {
					break
				}
				return nil, fmt.Errorf("get children of issues from %s: %w", depTable, err)
			}
			for rows.Next() {
				var childID string
				if err := rows.Scan(&childID); err != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("scan child: %w", err)
				}
				children = append(children, childID)
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("children rows from %s: %w", depTable, err)
			}
		}
	}
	return children, nil
}

// GetDescendantIDsInTx returns IDs of all transitive parent-child descendants
// of rootID, traversing parent-child edges in both the dependencies and
// wisp_dependencies tables. rootID itself is NOT included. Cycles are broken
// inside the recursive CTE. maxDepth caps traversal depth only when positive;
// reaching that cap returns an explicit error rather than silently truncating.
func GetDescendantIDsInTx(ctx context.Context, tx *sql.Tx, rootID string, maxDepth int) ([]string, error) {
	if rootID == "" {
		return nil, nil
	}

	queryDescendants := func(includeWisps bool) ([]string, bool, error) {
		edgeQuery := `
			SELECT issue_id, depends_on_id FROM dependencies WHERE type = 'parent-child'
		`
		if includeWisps {
			edgeQuery += `
			UNION ALL
			SELECT issue_id, depends_on_id FROM wisp_dependencies WHERE type = 'parent-child'
		`
		}

		query := fmt.Sprintf(`
			WITH RECURSIVE
			parent_edges(issue_id, depends_on_id) AS (
				%s
			),
			descendants(id, depth, path) AS (
				SELECT issue_id, 1, CONCAT(',', ?, ',', issue_id, ',')
				FROM parent_edges
				WHERE depends_on_id = ?
				UNION ALL
				SELECT e.issue_id, d.depth + 1, CONCAT(d.path, e.issue_id, ',')
				FROM parent_edges e
				JOIN descendants d ON e.depends_on_id = d.id
				WHERE (? <= 0 OR d.depth < ?)
				  AND LOCATE(CONCAT(',', e.issue_id, ','), d.path) = 0
			)
			SELECT id, depth FROM descendants WHERE id <> ?
		`, edgeQuery)

		rows, err := tx.QueryContext(ctx, query, rootID, rootID, maxDepth, maxDepth, rootID)
		if err != nil {
			return nil, false, err
		}
		defer func() { _ = rows.Close() }()

		var result []string
		reachedMaxDepth := false
		for rows.Next() {
			var id string
			var depth int
			if err := rows.Scan(&id, &depth); err != nil {
				return nil, false, fmt.Errorf("scan descendant: %w", err)
			}
			result = append(result, id)
			if maxDepth > 0 && depth >= maxDepth {
				reachedMaxDepth = true
			}
		}
		if err := rows.Err(); err != nil {
			return nil, false, fmt.Errorf("descendant rows: %w", err)
		}
		return result, reachedMaxDepth, nil
	}

	result, reachedMaxDepth, err := queryDescendants(true)
	if err != nil {
		if !isTableNotExistError(err) {
			return nil, err
		}
		result, reachedMaxDepth, err = queryDescendants(false)
		if err != nil {
			return nil, err
		}
	}
	if reachedMaxDepth {
		return nil, fmt.Errorf("parent descendant traversal for %s reached max depth %d", rootID, maxDepth)
	}
	return result, nil
}

// GetBlockedIssuesInTx returns issues that are blocked by other issues.
// This is the full implementation including transitive child blocking and parent filtering.
//
//nolint:gosec // G201: tables are hardcoded
func GetBlockedIssuesInTx(ctx context.Context, tx *sql.Tx, filter types.WorkFilter) ([]*types.BlockedIssue, error) {
	// Step 1: Compute blocked IDs (also returns activeIDs for later use)
	blockedIDList, activeIDs, err := ComputeBlockedIDsInTx(ctx, tx, true)
	if err != nil {
		return nil, fmt.Errorf("compute blocked IDs: %w", err)
	}

	blockedSet := make(map[string]bool, len(blockedIDList))
	for _, id := range blockedIDList {
		blockedSet[id] = true
	}

	// Step 2: Include children of blocked parents (GH#1495).
	childToParent, childErr := GetChildrenWithParentsInTx(ctx, tx, blockedIDList)
	if childErr == nil {
		for childID := range childToParent {
			if activeIDs[childID] && !blockedSet[childID] {
				blockedSet[childID] = true
				blockedIDList = append(blockedIDList, childID)
			}
		}
	}

	// Step 3: Get blocking deps to build BlockedBy lists
	blockerMap := make(map[string][]string)
	for _, depTable := range []string{"dependencies", "wisp_dependencies"} {
		depRows, err := tx.QueryContext(ctx, fmt.Sprintf(`
			SELECT issue_id, depends_on_id FROM %s
			WHERE type IN ('blocks', 'waits-for', 'conditional-blocks')
		`, depTable))
		if err != nil {
			return nil, fmt.Errorf("get blocking deps from %s: %w", depTable, err)
		}

		for depRows.Next() {
			var issueID, blockerID string
			if err := depRows.Scan(&issueID, &blockerID); err != nil {
				_ = depRows.Close()
				return nil, fmt.Errorf("scan dependency: %w", err)
			}
			if blockedSet[issueID] && activeIDs[blockerID] {
				blockerMap[issueID] = append(blockerMap[issueID], blockerID)
			}
		}
		_ = depRows.Close()
		if err := depRows.Err(); err != nil {
			return nil, fmt.Errorf("dependency rows from %s: %w", depTable, err)
		}
	}

	// Step 3b: Add transitively blocked children to blockerMap (GH#1495).
	if childErr == nil {
		for childID, parentID := range childToParent {
			if activeIDs[childID] && blockedSet[childID] {
				if _, hasDirectBlocker := blockerMap[childID]; !hasDirectBlocker {
					blockerMap[childID] = []string{parentID}
				}
			}
		}
	}

	// Step 4: Batch-fetch all blocked issues
	blockedIDs := make([]string, 0, len(blockerMap))
	for id := range blockerMap {
		blockedIDs = append(blockedIDs, id)
	}
	issues, err := GetIssuesByIDsInTx(ctx, tx, blockedIDs, nil)
	if err != nil {
		return nil, fmt.Errorf("batch-fetch blocked issues: %w", err)
	}
	issueMap := make(map[string]*types.Issue, len(issues))
	for _, issue := range issues {
		issueMap[issue.ID] = issue
	}

	// Parent filtering: restrict to children of specified parent (GH#2009)
	var parentChildSet map[string]bool
	if filter.ParentID != nil {
		parentChildSet = make(map[string]bool)
		parentID := *filter.ParentID
		children, childErr := GetChildrenOfIssuesInTx(ctx, tx, []string{parentID})
		if childErr == nil {
			for _, childID := range children {
				parentChildSet[childID] = true
			}
		}
		// Also include dotted-ID children (e.g., "parent.1.2")
		for id := range blockerMap {
			if strings.HasPrefix(id, parentID+".") {
				parentChildSet[id] = true
			}
		}
	}

	var results []*types.BlockedIssue
	for id, blockerIDs := range blockerMap {
		if parentChildSet != nil && !parentChildSet[id] {
			continue
		}

		issue, ok := issueMap[id]
		if !ok || issue == nil {
			continue
		}

		results = append(results, &types.BlockedIssue{
			Issue:          *issue,
			BlockedByCount: len(blockerIDs),
			BlockedBy:      blockerIDs,
		})
	}

	// Sort by priority ASC, then created_at DESC
	sort.Slice(results, func(i, j int) bool {
		if results[i].Issue.Priority != results[j].Issue.Priority {
			return results[i].Issue.Priority < results[j].Issue.Priority
		}
		return results[i].Issue.CreatedAt.After(results[j].Issue.CreatedAt)
	})

	return results, nil
}
