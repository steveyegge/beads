package storage

import (
	"context"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// AnnotationQueryStore provides bulk comment and label queries.
type AnnotationQueryStore interface {
	AddComment(ctx context.Context, issueID, actor, comment string) error
	ImportIssueComment(ctx context.Context, issueID, author, text string, createdAt time.Time) (*types.Comment, error)
	GetCommentCounts(ctx context.Context, issueIDs []string) (map[string]int, error)
	GetCommentsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Comment, error)
	GetLabelsForIssues(ctx context.Context, issueIDs []string) (map[string][]string, error)
}
