package issueops

import (
	"context"
	"strings"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

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

// CountDependencyEdgesInTx counts the dependency edges at one issue's near
// end, both planes merged, as distinct far ends — the same rule the hydrated
// readers apply — so a count can never disagree with the listing it
// summarizes (GH#3445: the raw two-plane SUM doubled a mirrored edge and
// printed a false "far end has no row" notice next to a correct CHILDREN
// listing). The type filter applies after the de-dup, against the surviving
// (durable-first) edge's type, matching filterDepsByType in
// internal/storage/domain/db/dependency.go.
func CountDependencyEdgesInTx(ctx context.Context, tx DBTX, sourceID string, dir domain.DepDirection, typeFilter []types.DependencyType) (int64, error) {
	var allowed map[types.DependencyType]struct{}
	if len(typeFilter) > 0 {
		allowed = make(map[types.DependencyType]struct{}, len(typeFilter))
		for _, t := range typeFilter {
			allowed[t] = struct{}{}
		}
	}

	count := func(inbound bool) (int64, error) {
		ends, err := listDepEdgeEndsInTx(ctx, tx, sourceID, inbound)
		if err != nil {
			return 0, err
		}
		var n int64
		for _, e := range ends {
			if allowed != nil {
				if _, ok := allowed[types.DependencyType(e.depType)]; !ok {
					continue
				}
			}
			n++
		}
		return n, nil
	}

	var total int64
	if dir == domain.DepDirectionOut || dir == domain.DepDirectionBoth {
		n, err := count(false)
		if err != nil {
			return 0, err
		}
		total += n
	}
	if dir == domain.DepDirectionIn || dir == domain.DepDirectionBoth {
		n, err := count(true)
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}