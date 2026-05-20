package db

import (
	"context"
	"errors"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func NewDependencySQLRepository(runner Runner) domain.DependencySQLRepository {
	return &dependencySQLRepositoryImpl{runner: runner}
}

type dependencySQLRepositoryImpl struct {
	runner Runner
}

var _ domain.DependencySQLRepository = (*dependencySQLRepositoryImpl)(nil)

func (r *dependencySQLRepositoryImpl) Insert(ctx context.Context, dep *types.Dependency, actor string) error {
	return errors.New("db: DependencySQLRepository.Insert: not implemented")
}

func (r *dependencySQLRepositoryImpl) HasCycle(ctx context.Context, issueID, dependsOnID string) (bool, error) {
	return false, errors.New("db: DependencySQLRepository.HasCycle: not implemented")
}

func (r *dependencySQLRepositoryImpl) ListByIssueIDs(ctx context.Context, issueIDs []string, opts domain.DepListOpts) (domain.DepBulkResult, error) {
	return domain.DepBulkResult{}, errors.New("db: DependencySQLRepository.ListByIssueIDs: not implemented")
}

func (r *dependencySQLRepositoryImpl) CountsByIssueIDs(ctx context.Context, issueIDs []string) (map[string]*types.DependencyCounts, error) {
	return nil, errors.New("db: DependencySQLRepository.CountsByIssueIDs: not implemented")
}
