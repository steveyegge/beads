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

type DepListOpts struct {
	Types     []types.DependencyType
	Direction DepDirection
}

type DepBulkResult struct {
	Outgoing map[string][]*types.Dependency
	Incoming map[string][]*types.Dependency
}

type AddDependencyOpts struct {
	SkipCycleCheck bool
}

type DependencySQLRepository interface {
	Insert(ctx context.Context, dep *types.Dependency, actor string) error
	HasCycle(ctx context.Context, issueID, dependsOnID string) (bool, error)

	ListByIssueIDs(ctx context.Context, issueIDs []string, opts DepListOpts) (DepBulkResult, error)
	CountsByIssueIDs(ctx context.Context, issueIDs []string) (map[string]*types.DependencyCounts, error)
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
