package issueops

import "github.com/steveyegge/beads/internal/storage/dberrors"

// queryBatchSize limits the number of IDs in a single IN (...) clause to
// stay under MySQL's max_allowed_packet. 200 matches the value used in the
// dolt package historically.
const queryBatchSize = 200

// isTableNotExistError returns true if the error indicates a missing table
// (MySQL/Dolt error 1146).
func isTableNotExistError(err error) bool {
	return dberrors.IsTableNotExist(err)
}
