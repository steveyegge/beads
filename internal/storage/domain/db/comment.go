package db

import (
	"context"
	"errors"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/types"
)

func NewCommentSQLRepository(runner Runner) domain.CommentSQLRepository {
	return &commentSQLRepositoryImpl{runner: runner}
}

type commentSQLRepositoryImpl struct {
	runner Runner
}

var _ domain.CommentSQLRepository = (*commentSQLRepositoryImpl)(nil)

func (r *commentSQLRepositoryImpl) CountsByIssueIDs(ctx context.Context, issueIDs []string) (map[string]int, error) {
	return nil, errors.New("db: CommentSQLRepository.CountsByIssueIDs: not implemented")
}

func (r *commentSQLRepositoryImpl) ListByIssueIDs(ctx context.Context, issueIDs []string) (map[string][]*types.Comment, error) {
	return nil, errors.New("db: CommentSQLRepository.ListByIssueIDs: not implemented")
}
