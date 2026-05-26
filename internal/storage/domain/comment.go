package domain

import (
	"context"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
)

type CommentOpts struct {
	UseWispsTable bool
}

type CommentSQLRepository interface {
	CountsByIssueIDs(ctx context.Context, issueIDs []string, opts CommentOpts) (map[string]int, error)
	ListByIssueIDs(ctx context.Context, issueIDs []string, opts CommentOpts) (map[string][]*types.Comment, error)
}

type CommentUseCase interface {
	GetCommentCounts(ctx context.Context, issueIDs []string) (map[string]int, error)
	GetCommentsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Comment, error)

	GetWispCommentCounts(ctx context.Context, wispIDs []string) (map[string]int, error)
	GetCommentsForWisps(ctx context.Context, wispIDs []string) (map[string][]*types.Comment, error)
}

func NewCommentUseCase(commentRepo CommentSQLRepository) CommentUseCase {
	return &commentUseCaseImpl{commentRepo: commentRepo}
}

type commentUseCaseImpl struct {
	commentRepo CommentSQLRepository
}

var _ CommentUseCase = (*commentUseCaseImpl)(nil)

func (u *commentUseCaseImpl) GetCommentCounts(ctx context.Context, issueIDs []string) (map[string]int, error) {
	return u.counts(ctx, issueIDs, false)
}

func (u *commentUseCaseImpl) GetWispCommentCounts(ctx context.Context, wispIDs []string) (map[string]int, error) {
	return u.counts(ctx, wispIDs, true)
}

func (u *commentUseCaseImpl) counts(ctx context.Context, ids []string, useWisp bool) (map[string]int, error) {
	if len(ids) == 0 {
		return map[string]int{}, nil
	}
	out, err := u.commentRepo.CountsByIssueIDs(ctx, ids, CommentOpts{UseWispsTable: useWisp})
	if err != nil {
		return nil, fmt.Errorf("comment counts: %w", err)
	}
	return out, nil
}

func (u *commentUseCaseImpl) GetCommentsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Comment, error) {
	return u.list(ctx, issueIDs, false)
}

func (u *commentUseCaseImpl) GetCommentsForWisps(ctx context.Context, wispIDs []string) (map[string][]*types.Comment, error) {
	return u.list(ctx, wispIDs, true)
}

func (u *commentUseCaseImpl) list(ctx context.Context, ids []string, useWisp bool) (map[string][]*types.Comment, error) {
	if len(ids) == 0 {
		return map[string][]*types.Comment{}, nil
	}
	out, err := u.commentRepo.ListByIssueIDs(ctx, ids, CommentOpts{UseWispsTable: useWisp})
	if err != nil {
		return nil, fmt.Errorf("comment list: %w", err)
	}
	return out, nil
}
