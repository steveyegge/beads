//go:build cgo

package doltlite

import (
	"context"
	"database/sql"

	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
)

// GetReadyWork returns issues that are ready to work on (not blocked).
// Delegates to issueops.GetReadyWorkInTx with the shared blocked-ID computation.
func (s *DoltliteStore) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetReadyWorkInTxWithDialect(ctx, tx, filter, computeBlockedIDsWrapper, issueops.SQLDialectSQLite)
		return err
	})
	return result, err
}

// computeBlockedIDsWrapper adapts ComputeBlockedIDsInTx to the callback
// signature expected by GetReadyWorkInTx.
func computeBlockedIDsWrapper(ctx context.Context, tx *sql.Tx, includeWisps bool) ([]string, error) {
	ids, _, err := issueops.ComputeBlockedIDsInTx(ctx, tx, includeWisps)
	return ids, err
}

// GetMoleculeProgress returns progress stats for a molecule.
// Delegates to issueops.GetMoleculeProgressInTx.
func (s *DoltliteStore) GetMoleculeProgress(ctx context.Context, moleculeID string) (*types.MoleculeProgressStats, error) {
	var result *types.MoleculeProgressStats
	err := s.withConn(ctx, false, func(tx *sql.Tx) error {
		var err error
		result, err = issueops.GetMoleculeProgressInTx(ctx, tx, moleculeID)
		return err
	})
	return result, err
}
