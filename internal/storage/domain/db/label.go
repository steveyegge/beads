package db

import (
	"context"
	"errors"

	"github.com/steveyegge/beads/internal/storage/domain"
)

func NewLabelSQLRepository(runner Runner) domain.LabelSQLRepository {
	return &labelSQLRepositoryImpl{runner: runner}
}

type labelSQLRepositoryImpl struct {
	runner Runner
}

var _ domain.LabelSQLRepository = (*labelSQLRepositoryImpl)(nil)

func (r *labelSQLRepositoryImpl) Insert(ctx context.Context, issueID, label, actor string) error {
	return errors.New("db: LabelSQLRepository.Insert: not implemented")
}

func (r *labelSQLRepositoryImpl) List(ctx context.Context, issueID string) ([]string, error) {
	return nil, errors.New("db: LabelSQLRepository.List: not implemented")
}

func (r *labelSQLRepositoryImpl) ListByIssueIDs(ctx context.Context, issueIDs []string) (map[string][]string, error) {
	return nil, errors.New("db: LabelSQLRepository.ListByIssueIDs: not implemented")
}
