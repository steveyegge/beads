package issueops

import (
	"context"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

// CountDependencyEdgesInTx counts the DISTINCT edges of sourceID in dir: the
// matching `dependencies` rows plus the matching `wisp_dependencies` rows
// whose far end is not already an edge of the same anchor in `dependencies`.
// It is the count behind `bd show`'s dependent_count/dependency_count on the
// proxied leg and — via the DoltStore and EmbeddedDoltStore delegations — on
// the server and embedded legs, so it must agree with the anchorEdgesInTx
// listings, which de-duplicate the same way (#3445): a both-table duplicate
// edge counts once, and a missing wisp_dependencies table leaves the durable
// count as the whole answer instead of failing the read.
func CountDependencyEdgesInTx(ctx context.Context, tx DBTX, sourceID string, dir domain.DepDirection, typeFilter []types.DependencyType) (int64, error) {
	typeClause, typeArgs := buildDepTypeClause(typeFilter)

	var total int64
	if dir == domain.DepDirectionOut || dir == domain.DepDirectionBoth {
		n, err := countDependencyEdgesInDir(ctx, tx, domain.DepDirectionOut, sourceID, typeClause, typeArgs)
		if err != nil {
			return 0, err
		}
		total += n
	}
	if dir == domain.DepDirectionIn || dir == domain.DepDirectionBoth {
		n, err := countDependencyEdgesInDir(ctx, tx, domain.DepDirectionIn, sourceID, typeClause, typeArgs)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}

// countDependencyEdgesInDir counts the distinct edges of anchor in one
// direction: the durable rows plus the wisp-only rows. The type filter applies
// to the counted rows on both sides but never to the de-duplication subquery —
// see countWispEdgesNotInDurableInTx.
func countDependencyEdgesInDir(ctx context.Context, tx DBTX, dir domain.DepDirection, anchor, typeClause string, typeArgs []any) (int64, error) {
	anchorExpr := countAnchorExpr(dir)
	durable, err := countDepWhere(ctx, tx, "dependencies", anchorExpr+" = ?", typeClause, anchor, typeArgs)
	if err != nil {
		return 0, err
	}
	wispExtra, err := countWispEdgesNotInDurableInTx(ctx, tx, dir, anchor, typeClause, typeArgs)
	if err != nil {
		// wisp_dependencies absent: the durable count is the whole answer.
		if missingOptionalDepTable(err, "wisp_dependencies") {
			return durable, nil
		}
		return 0, err
	}
	return durable + wispExtra, nil
}

// countAnchorExpr is the column a direction's edges hang off the anchor by:
// outbound edges are the anchor's own rows (issue_id), inbound edges point at
// the anchor (the target expression). The listing's predicate is kept, not
// the sargable per-column OR, so count and listing see the same rows and the
// split-column COALESCE rationale in dolt/counts.go still holds.
func countAnchorExpr(dir domain.DepDirection) string {
	if dir == domain.DepDirectionOut {
		return "issue_id"
	}
	return DepTargetExpr
}

// countWispEdgesNotInDurableInTx counts the wisp-only edges of anchor in dir:
// matching wisp_dependencies rows whose far end is not already an edge of the
// same anchor in `dependencies`. Colliding edges count on the durable side,
// so this is the exact complement that makes the total distinct.
//
// The subquery carries NO type filter and the far-end column (never the row
// `id`): the listings de-duplicate before any type filter, so a type-divergent
// duplicate must count on the durable side alone — filtering the subquery
// would let its wisp copy through and disagree with the filtered listing.
// Outbound adds IS NOT NULL to the subquery's far end: a NULL in a NOT IN
// subquery's result makes the predicate unknown and would drop every wisp-only
// row.
//
//nolint:gosec // G201: table names are hardcoded constants; anchor and type args are bound as parameters.
func countWispEdgesNotInDurableInTx(ctx context.Context, tx DBTX, dir domain.DepDirection, anchor, typeClause string, typeArgs []any) (int64, error) {
	anchorExpr := countAnchorExpr(dir)
	farExpr := "issue_id"
	if dir == domain.DepDirectionOut {
		farExpr = DepTargetExpr
	}

	nullGuard := ""
	if dir == domain.DepDirectionOut {
		nullGuard = " AND " + DepTargetExpr + " IS NOT NULL"
	}

	query := fmt.Sprintf("SELECT COUNT(*) FROM wisp_dependencies WHERE %s = ?", anchorExpr)
	args := make([]any, 0, 2+len(typeArgs))
	args = append(args, anchor)
	if typeClause != "" {
		query += " AND " + typeClause
		args = append(args, typeArgs...)
	}
	query += fmt.Sprintf(" AND %s NOT IN (SELECT %s FROM dependencies WHERE %s = ?%s)",
		farExpr, farExpr, anchorExpr, nullGuard)
	args = append(args, anchor)

	var n int64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count wisp-only dependency edges: %w", err)
	}
	return n, nil
}

func countDepWhere(ctx context.Context, tx DBTX, table, whereExpr, typeClause string, sourceID string, typeArgs []any) (int64, error) {
	q := fmt.Sprintf("SELECT count(*) FROM %s WHERE %s", table, whereExpr) //nolint:gosec
	if typeClause != "" {
		q += " AND " + typeClause
	}
	args := append([]any{sourceID}, typeArgs...)
	var n int64
	if err := tx.QueryRowContext(ctx, q, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count %s: %w", table, err)
	}
	return n, nil
}

func buildDepTypeClause(filter []types.DependencyType) (string, []any) {
	if len(filter) == 0 {
		return "", nil
	}
	placeholders := make([]string, len(filter))
	args := make([]any, len(filter))
	for i, t := range filter {
		placeholders[i] = "?"
		args[i] = string(t)
	}
	return "type IN (" + strings.Join(placeholders, ",") + ")", args
}