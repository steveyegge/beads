package issueops

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// deleteBatchSize controls the maximum number of IDs per IN-clause query
// for delete operations. Kept small to avoid large IN-clause queries.
const deleteBatchSize = 50

// maxRecursiveResults is the safety limit for the total number of issues
// discovered during recursive dependent traversal.
const maxRecursiveResults = 10000

//nolint:gosec // G201: table names come from WispTableRouting (hardcoded constants)
func DeleteIssueInTx(ctx context.Context, regularTx, ignoredTx *sql.Tx, id string) error {
	isWisp := IsActiveWispInTx(ctx, ignoredTx, id)
	issueTable, _, _, _ := WispTableRouting(isWisp)

	tx := regularTx
	if isWisp {
		tx = ignoredTx
	}

	result, err := tx.ExecContext(ctx, fmt.Sprintf("DELETE FROM %s WHERE id = ?", issueTable), id)
	if err != nil {
		return fmt.Errorf("delete issue from %s: %w", issueTable, err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("issue not found: %s", id)
	}

	return nil
}

//nolint:gosec // G201: inClause contains only ? placeholders
func DeleteIssuesInTx(ctx context.Context, regularTx, ignoredTx *sql.Tx, ids []string, cascade bool, force bool, dryRun bool) (*types.DeleteIssuesResult, error) {
	if len(ids) == 0 {
		return &types.DeleteIssuesResult{}, nil
	}

	wispIDs, regularIDs, err := PartitionWispIDsInTx(ctx, ignoredTx, ids)
	if err != nil {
		return nil, err
	}

	wispDeleteCount := 0
	if len(wispIDs) > 0 && !dryRun {
		for _, id := range wispIDs {
			if err := DeleteIssueInTx(ctx, regularTx, ignoredTx, id); err != nil {
				return nil, fmt.Errorf("delete wisp %s: %w", id, err)
			}
			wispDeleteCount++
		}
	} else {
		wispDeleteCount = len(wispIDs)
	}

	ids = regularIDs
	if len(ids) == 0 {
		return &types.DeleteIssuesResult{DeletedCount: wispDeleteCount}, nil
	}

	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	result := &types.DeleteIssuesResult{}

	expandedIDs := ids
	if cascade {
		allToDelete, err := findAllDependentsRecursiveInTx(ctx, regularTx, ids)
		if err != nil {
			return nil, fmt.Errorf("find dependents: %w", err)
		}
		expandedIDs = make([]string, 0, len(allToDelete))
		for id := range allToDelete {
			expandedIDs = append(expandedIDs, id)
		}
	} else if !force {
		for i := 0; i < len(ids); i += deleteBatchSize {
			end := i + deleteBatchSize
			if end > len(ids) {
				end = len(ids)
			}
			batch := ids[i:end]
			inClause, args := buildSQLInClause(batch)

			rows, err := regularTx.QueryContext(ctx,
				fmt.Sprintf(`SELECT depends_on_id, issue_id FROM dependencies WHERE depends_on_id IN (%s)`, inClause),
				args...)
			if err != nil {
				return nil, fmt.Errorf("check dependents: %w", err)
			}

			externalBySource := make(map[string][]string)
			for rows.Next() {
				var depOnID, issueID string
				if err := rows.Scan(&depOnID, &issueID); err != nil {
					_ = rows.Close()
					return nil, fmt.Errorf("scan dependent: %w", err)
				}
				if !idSet[issueID] {
					externalBySource[depOnID] = append(externalBySource[depOnID], issueID)
				}
			}
			_ = rows.Close()
			if err := rows.Err(); err != nil {
				return nil, fmt.Errorf("iterate dependents: %w", err)
			}

			for _, id := range batch {
				if deps, ok := externalBySource[id]; ok {
					result.OrphanedIssues = deps
					return result, fmt.Errorf("issue %s has dependents not in deletion set; use --cascade to delete them or --force to orphan them", id)
				}
			}
		}
	} else {
		orphans, err := findExternalDependentsBatchedInTx(ctx, regularTx, ids, idSet)
		if err != nil {
			return nil, fmt.Errorf("get dependents: %w", err)
		}
		result.OrphanedIssues = orphans
	}

	expandedIDSet := make(map[string]bool, len(expandedIDs))
	for _, id := range expandedIDs {
		expandedIDSet[id] = true
	}

	var depsCount, labelsCount, eventsCount int
	for i := 0; i < len(expandedIDs); i += deleteBatchSize {
		end := i + deleteBatchSize
		if end > len(expandedIDs) {
			end = len(expandedIDs)
		}
		batch := expandedIDs[i:end]
		batchInClause, batchArgs := buildSQLInClause(batch)

		var batchDeps int
		if err := regularTx.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT COUNT(*) FROM dependencies WHERE issue_id IN (%s)`, batchInClause),
			batchArgs...).Scan(&batchDeps); err != nil {
			return nil, fmt.Errorf("count dependencies: %w", err)
		}
		depsCount += batchDeps

		var batchLabels int
		if err := regularTx.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT COUNT(*) FROM labels WHERE issue_id IN (%s)`, batchInClause),
			batchArgs...).Scan(&batchLabels); err != nil {
			return nil, fmt.Errorf("count labels: %w", err)
		}
		labelsCount += batchLabels

		var batchEvents int
		if err := regularTx.QueryRowContext(ctx,
			fmt.Sprintf(`SELECT COUNT(*) FROM events WHERE issue_id IN (%s)`, batchInClause),
			batchArgs...).Scan(&batchEvents); err != nil {
			return nil, fmt.Errorf("count events: %w", err)
		}
		eventsCount += batchEvents
	}

	// Pass 2: inbound deps from outside the deletion set.
	for i := 0; i < len(expandedIDs); i += deleteBatchSize {
		end := i + deleteBatchSize
		if end > len(expandedIDs) {
			end = len(expandedIDs)
		}
		batch := expandedIDs[i:end]
		batchInClause, batchArgs := buildSQLInClause(batch)

		rows, err := regularTx.QueryContext(ctx,
			fmt.Sprintf(`SELECT issue_id FROM dependencies WHERE depends_on_id IN (%s)`, batchInClause),
			batchArgs...)
		if err != nil {
			return nil, fmt.Errorf("count inbound dependencies: %w", err)
		}
		for rows.Next() {
			var issID string
			if err := rows.Scan(&issID); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan inbound dependency: %w", err)
			}
			if !expandedIDSet[issID] {
				depsCount++
			}
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate inbound dependencies: %w", err)
		}
	}

	result.DependenciesCount = depsCount
	result.LabelsCount = labelsCount
	result.EventsCount = eventsCount
	result.DeletedCount = len(expandedIDs) + wispDeleteCount

	if dryRun {
		return result, nil
	}

	totalDeleted := 0
	for i := 0; i < len(expandedIDs); i += deleteBatchSize {
		end := i + deleteBatchSize
		if end > len(expandedIDs) {
			end = len(expandedIDs)
		}
		batch := expandedIDs[i:end]
		batchInClause, batchArgs := buildSQLInClause(batch)

		deleteResult, err := regularTx.ExecContext(ctx,
			fmt.Sprintf(`DELETE FROM issues WHERE id IN (%s)`, batchInClause),
			batchArgs...)
		if err != nil {
			return nil, fmt.Errorf("delete issues: %w", err)
		}
		rowsAffected, _ := deleteResult.RowsAffected()
		totalDeleted += int(rowsAffected)
	}
	result.DeletedCount = totalDeleted + wispDeleteCount

	return result, nil
}

// findAllDependentsRecursiveInTx finds all issues that depend on the given
// issues, recursively. Uses batched IN-clause queries. Traversal is capped
// at maxRecursiveResults total discovered IDs.
//
//nolint:gosec // G201: inClause contains only ? placeholders
func findAllDependentsRecursiveInTx(ctx context.Context, tx *sql.Tx, ids []string) (map[string]bool, error) {
	result := make(map[string]bool)
	for _, id := range ids {
		result[id] = true
	}

	toProcess := make([]string, len(ids))
	copy(toProcess, ids)

	for len(toProcess) > 0 {
		if len(result) > maxRecursiveResults {
			return nil, fmt.Errorf("cascade traversal discovered over %d issues; aborting to prevent runaway deletion", maxRecursiveResults)
		}
		batchEnd := deleteBatchSize
		if batchEnd > len(toProcess) {
			batchEnd = len(toProcess)
		}
		batch := toProcess[:batchEnd]
		toProcess = toProcess[batchEnd:]

		inClause, args := buildSQLInClause(batch)
		rows, err := tx.QueryContext(ctx,
			fmt.Sprintf(`SELECT issue_id FROM dependencies WHERE depends_on_id IN (%s)`, inClause),
			args...)
		if err != nil {
			return nil, fmt.Errorf("query dependents for batch: %w", err)
		}

		for rows.Next() {
			var depID string
			if err := rows.Scan(&depID); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan dependent: %w", err)
			}
			if !result[depID] {
				result[depID] = true
				toProcess = append(toProcess, depID)
			}
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate dependents for batch: %w", err)
		}
	}

	return result, nil
}

// findExternalDependentsBatchedInTx finds all dependents of the given IDs
// that are NOT in the idSet.
//
//nolint:gosec // G201: inClause contains only ? placeholders
func findExternalDependentsBatchedInTx(ctx context.Context, tx *sql.Tx, ids []string, idSet map[string]bool) ([]string, error) {
	orphanSet := make(map[string]bool)
	for i := 0; i < len(ids); i += deleteBatchSize {
		end := i + deleteBatchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		inClause, args := buildSQLInClause(batch)

		rows, err := tx.QueryContext(ctx,
			fmt.Sprintf(`SELECT issue_id FROM dependencies WHERE depends_on_id IN (%s)`, inClause),
			args...)
		if err != nil {
			return nil, fmt.Errorf("query dependents: %w", err)
		}
		for rows.Next() {
			var depID string
			if err := rows.Scan(&depID); err != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("scan dependent: %w", err)
			}
			if !idSet[depID] {
				orphanSet[depID] = true
			}
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("iterate dependents: %w", err)
		}
	}

	result := make([]string, 0, len(orphanSet))
	for id := range orphanSet {
		result = append(result, id)
	}
	return result, nil
}
