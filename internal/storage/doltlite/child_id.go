//go:build cgo

package doltlite

import (
	"context"
	"database/sql"

	"github.com/steveyegge/beads/internal/storage/issueops"
)

func (s *DoltliteStore) GetNextChildID(ctx context.Context, parentID string) (string, error) {
	var childID string
	err := s.withConn(ctx, true, func(tx *sql.Tx) error {
		var err error
		childID, err = issueops.GetNextChildIDTx(ctx, tx, parentID)
		return err
	})
	return childID, err
}
