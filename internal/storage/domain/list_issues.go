package domain

import (
	"context"

	"github.com/steveyegge/beads/internal/types"
)

type ListProjection struct {
	Labels           bool
	Dependencies     bool
	DependencyCounts bool
	Parent           bool
	CommentCounts    bool
	Comments         bool
}

type ListResult struct {
	Issues    []*types.IssueWithCounts
	Labels    map[string][]string
	BlockedBy map[string][]string
	Blocks    map[string][]string
	Parent    map[string]string
}

type ListIssuesUseCase interface {
	List(ctx context.Context, filter types.IssueFilter, proj ListProjection) (ListResult, error)
}

func NewListIssuesUseCase(
	issueRepo IssueSQLRepository,
	depRepo DependencySQLRepository,
	labelRepo LabelSQLRepository,
	commentRepo CommentSQLRepository,
) ListIssuesUseCase {
	return nil
}

// IssueQueryRepository is the optional fast-path collapsed-query interface.
// Implementations may run search + hydration as a single multi-statement
// round-trip when the underlying driver supports it. ListIssuesUseCase
// should prefer this when available and fall back to the per-field
// repositories otherwise.
type IssueQueryRepository interface {
	SearchHydrated(ctx context.Context, filter types.IssueFilter, proj ListProjection) (ListResult, error)
}
