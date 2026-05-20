package db

import (
	"context"
	"errors"

	"github.com/steveyegge/beads/internal/storage/domain"
)

func NewChildCounterSQLRepository(runner Runner) domain.ChildCounterSQLRepository {
	return &childCounterSQLRepositoryImpl{runner: runner}
}

type childCounterSQLRepositoryImpl struct {
	runner Runner
}

var _ domain.ChildCounterSQLRepository = (*childCounterSQLRepositoryImpl)(nil)

func (r *childCounterSQLRepositoryImpl) NextChildID(ctx context.Context, parentID string) (string, error) {
	return "", errors.New("db: ChildCounterSQLRepository.NextChildID: not implemented")
}
