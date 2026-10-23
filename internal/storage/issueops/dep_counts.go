package issueops

import (
	"context"
	"slices"
	"strings"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

// CountDependencyEdgesInTx counts sourceID's dependency edges in dir across
// both dependency tables. It counts the far ends listDepEdgeEndsInTx lists, so
// a far end stored in both tables counts once and a missing wisp_dependencies
// counts as empty (GH#3445): the count equals the listing `bd show` subtracts
// it from, and only far ends with no row make the two differ. typeFilter
// applies after the de-dup, to the type the listing reports. With
// DepDirectionBoth the two directions are summed.
func CountDependencyEdgesInTx(ctx context.Context, tx DBTX, sourceID string, dir domain.DepDirection, typeFilter []types.DependencyType) (int64, error) {
	var total int64
	for _, inbound := range []bool{false, true} {
		if (inbound && dir == domain.DepDirectionOut) || (!inbound && dir == domain.DepDirectionIn) {
			continue
		}
		ends, err := listDepEdgeEndsInTx(ctx, tx, sourceID, inbound)
		if err != nil {
			return 0, err
		}
		for _, e := range ends {
			if len(typeFilter) == 0 || slices.Contains(typeFilter, types.DependencyType(e.depType)) {
				total++
			}
		}
	}
	return total, nil
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
