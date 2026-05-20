package domain

import (
	"context"

	"github.com/steveyegge/beads/internal/types"
)

type OrphanHandling int

const (
	OrphanAllow OrphanHandling = iota
	OrphanReject
)

type InsertIssueOpts struct {
	SkipPrefixValidation bool
	OrphanHandling       OrphanHandling
	UseWispsTable        bool
}

// IssueTableOpts pivots Update/Get/GetByIDs/Search to the wisps partition.
// Issue and wisp rows live in independent tables with the same column shape,
// so the same SQL works against either — only the table name changes.
type IssueTableOpts struct {
	UseWispsTable bool
}

type IssueSQLRepository interface {
	Insert(ctx context.Context, issue *types.Issue, actor string, opts InsertIssueOpts) error
	InsertBatch(ctx context.Context, issues []*types.Issue, actor string, opts InsertIssueOpts) error
	Update(ctx context.Context, id string, updates map[string]any, actor string, opts IssueTableOpts) error

	Get(ctx context.Context, id string, opts IssueTableOpts) (*types.Issue, error)
	GetByIDs(ctx context.Context, ids []string, opts IssueTableOpts) ([]*types.Issue, error)

	Search(ctx context.Context, filter types.IssueFilter, opts IssueTableOpts) ([]*types.Issue, error)
}

type IssueUseCase interface {
	GetIssue(ctx context.Context, id string) (*types.Issue, error)
	UpdateIssue(ctx context.Context, id string, updates map[string]any, actor string) error
}

type IssueQueryUseCase interface {
	GetIssue(ctx context.Context, id string) (*types.Issue, error)
	GetIssuesByIDs(ctx context.Context, ids []string) ([]*types.Issue, error)
}

func NewIssueUseCase(issueRepo IssueSQLRepository) IssueUseCase {
	return nil
}

func NewIssueQueryUseCase(issueRepo IssueSQLRepository) IssueQueryUseCase {
	return nil
}
