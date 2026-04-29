//go:build cgo

package doltlite

import (
	"context"
	"database/sql"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

func (s *DoltliteStore) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	var issue *types.Issue
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		issue, err = issueops.GetIssueInTx(ctx, tx, id)
		return err
	})
	return issue, err
}
