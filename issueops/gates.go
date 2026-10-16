package issueops

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

// GateListExpression is the query `bd gate list` asks: every row of the gate
// type, on both planes, whatever its other fields. It names no status, so the
// Querier's default closed exclusion applies unless GateListRequest.All lifts
// it (QueryRequest.IncludeClosed).
const GateListExpression = "type=gate"

// GateListRequest describes `bd gate list` over the whole workspace.
type GateListRequest struct {
	// All admits closed gates, which the listing otherwise hides.
	All bool
	// Limit bounds the rows returned; 0 means unlimited. A negative limit is
	// the Querier's ErrValidation.
	Limit int
}

// ListGates answers `bd gate list` (no issue argument) through the Querier
// role, which every backend serves (a remote one as queryIssues, which every
// deployed server publishes). It is the one library entry for the listing:
// the CLI's direct, proxied and remote routes each call it with their route's
// Querier, so no route builds a storage filter of its own.
//
// The question is exactly the one the raw filter used to ask: the gate type,
// both planes, closed rows excluded unless All is set, no other default
// suppression (templates, pinned rows and ephemeral gates are all listed),
// in the store's default order, cut at Limit.
//
// A failed read is an error, never an empty list. Rows come back in the
// Querier's order; a row carrying no issue is dropped.
func ListGates(ctx context.Context, querier Querier, req GateListRequest) ([]*types.Issue, error) {
	if querier == nil {
		return nil, &ErrUnsupported{Op: "ListGates", Backend: "nil"}
	}
	limit := req.Limit
	page, err := querier.Query(ctx, QueryRequest{
		Expression:    GateListExpression,
		IncludeClosed: req.All,
		Limit:         &limit,
	})
	if err != nil {
		return nil, fmt.Errorf("listing gates: %w", err)
	}
	gates := make([]*types.Issue, 0, len(page.Items))
	for _, row := range page.Items {
		if row != nil && row.Issue != nil {
			gates = append(gates, row.Issue)
		}
	}
	return gates, nil
}
