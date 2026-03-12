//go:build embeddeddolt

package embeddeddolt

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

func (s *EmbeddedDoltStore) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	if issue == nil {
		return fmt.Errorf("issue must not be nil")
	}
	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		bc, err := issueops.NewBatchContext(ctx, tx, storage.BatchCreateOptions{})
		if err != nil {
			return err
		}
		return issueops.CreateIssueInTx(ctx, tx, bc, issue, actor)
	})
}

func (s *EmbeddedDoltStore) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	return s.CreateIssuesWithFullOptions(ctx, issues, actor, storage.BatchCreateOptions{
		OrphanHandling:       storage.OrphanAllow,
		SkipPrefixValidation: false,
	})
}

func (s *EmbeddedDoltStore) CreateIssuesWithFullOptions(ctx context.Context, issues []*types.Issue, actor string, opts storage.BatchCreateOptions) error {
	if len(issues) == 0 {
		return nil
	}

	// All-ephemeral fast path: create each wisp individually.
	if issueops.AllEphemeral(issues) {
		for _, issue := range issues {
			issue.Ephemeral = true
			if err := s.CreateIssue(ctx, issue, actor); err != nil {
				return err
			}
		}
		return nil
	}

	return s.withConn(ctx, true, func(tx *sql.Tx) error {
		return issueops.CreateIssuesInTx(ctx, tx, issues, actor, opts)
	})
}
