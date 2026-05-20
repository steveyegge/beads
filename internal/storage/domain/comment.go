package domain

import (
	"context"

	"github.com/steveyegge/beads/internal/types"
)

type CommentSQLRepository interface {
	CountsByIssueIDs(ctx context.Context, issueIDs []string) (map[string]int, error)
	ListByIssueIDs(ctx context.Context, issueIDs []string) (map[string][]*types.Comment, error)
}

type CommentQueryUseCase interface {
	GetCommentCounts(ctx context.Context, issueIDs []string) (map[string]int, error)
	GetCommentsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Comment, error)
}

func NewCommentQueryUseCase(commentRepo CommentSQLRepository) CommentQueryUseCase {
	return nil
}
