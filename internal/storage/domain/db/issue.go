package db

import (
	"context"
	"errors"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func NewIssueSQLRepository(runner Runner) domain.IssueSQLRepository {
	return &issueSQLRepositoryImpl{runner: runner}
}

type issueSQLRepositoryImpl struct {
	runner Runner
}

var _ domain.IssueSQLRepository = (*issueSQLRepositoryImpl)(nil)

func (r *issueSQLRepositoryImpl) Insert(ctx context.Context, issue *types.Issue, actor string, opts domain.InsertIssueOpts) error {
	return errors.New("db: IssueSQLRepository.Insert: not implemented")
}

func (r *issueSQLRepositoryImpl) InsertBatch(ctx context.Context, issues []*types.Issue, actor string, opts domain.InsertIssueOpts) error {
	return errors.New("db: IssueSQLRepository.InsertBatch: not implemented")
}

func (r *issueSQLRepositoryImpl) Update(ctx context.Context, id string, updates map[string]any, actor string) error {
	return errors.New("db: IssueSQLRepository.Update: not implemented")
}

func (r *issueSQLRepositoryImpl) Get(ctx context.Context, id string) (*types.Issue, error) {
	return nil, errors.New("db: IssueSQLRepository.Get: not implemented")
}

func (r *issueSQLRepositoryImpl) GetByIDs(ctx context.Context, ids []string) ([]*types.Issue, error) {
	return nil, errors.New("db: IssueSQLRepository.GetByIDs: not implemented")
}

func (r *issueSQLRepositoryImpl) Search(ctx context.Context, filter types.IssueFilter) ([]*types.Issue, error) {
	return nil, errors.New("db: IssueSQLRepository.Search: not implemented")
}
