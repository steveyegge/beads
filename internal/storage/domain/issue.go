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

type IssueSQLRepository interface {
	Insert(ctx context.Context, issue *types.Issue, actor string, opts InsertIssueOpts) error
	InsertBatch(ctx context.Context, issues []*types.Issue, actor string, opts InsertIssueOpts) error
	Update(ctx context.Context, id string, updates map[string]any, actor string) error

	Get(ctx context.Context, id string) (*types.Issue, error)
	GetByIDs(ctx context.Context, ids []string) ([]*types.Issue, error)

	Search(ctx context.Context, filter types.IssueFilter) ([]*types.Issue, error)
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
