package domain

import (
	"context"

	"github.com/steveyegge/beads/internal/types"
)

type DepDirection int

const (
	DepDirectionBoth DepDirection = iota
	DepDirectionOut
	DepDirectionIn
)

type DepInsertOpts struct {
	// UseWispsTable pivots the write to wisp_dependencies. This is the
	// "source is a wisp" path — the wisp_dependencies row's issue_id (source)
	// references wisps(id), while depends_on_issue_id still references issues(id).
	UseWispsTable bool
}

type DepListOpts struct {
	Types     []types.DependencyType
	Direction DepDirection
	// UseWispsTable picks which dependency table to read from. HasCycle and
	// other graph-walking methods always traverse both tables — only the
	// per-source list/count methods take a single-table opt.
	UseWispsTable bool
}

type DepCountsOpts struct {
	UseWispsTable bool
}

type DepBulkResult struct {
	Outgoing map[string][]*types.Dependency
	Incoming map[string][]*types.Dependency
}

type AddDependencyOpts struct {
	SkipCycleCheck bool
}

type DependencySQLRepository interface {
	Insert(ctx context.Context, dep *types.Dependency, actor string, opts DepInsertOpts) error
	// HasCycle always traverses both dependencies and wisp_dependencies via
	// UNION so cross-table cycles (e.g. issue -> wisp -> issue) are caught.
	HasCycle(ctx context.Context, issueID, dependsOnID string) (bool, error)

	ListByIssueIDs(ctx context.Context, issueIDs []string, opts DepListOpts) (DepBulkResult, error)
	CountsByIssueIDs(ctx context.Context, issueIDs []string, opts DepCountsOpts) (map[string]*types.DependencyCounts, error)
}

type DependencyUseCase interface {
	AddDependency(ctx context.Context, dep *types.Dependency, actor string, opts AddDependencyOpts) error
}

type DependencyQueryUseCase interface {
	ListByIssueIDs(ctx context.Context, issueIDs []string, opts DepListOpts) (DepBulkResult, error)
	CountsByIssueIDs(ctx context.Context, issueIDs []string) (map[string]*types.DependencyCounts, error)
}

func NewDependencyUseCase(depRepo DependencySQLRepository, issueRepo IssueSQLRepository) DependencyUseCase {
	return nil
}

func NewDependencyQueryUseCase(depRepo DependencySQLRepository) DependencyQueryUseCase {
	return nil
}
