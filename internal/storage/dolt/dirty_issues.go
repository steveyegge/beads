//go:build cgo

package dolt

import "context"

// ClearDirtyIssuesByID clears the dirty flag for the given issue IDs.
// This was used by the old JSONL sync/auto-flush system. In the Dolt-only world,
// dirty tracking is no longer needed since Dolt handles persistence natively.
// Kept as a no-op for compatibility with callers that haven't been updated yet.
func (s *DoltStore) ClearDirtyIssuesByID(_ context.Context, _ []string) error {
	return nil // No-op: Dolt handles persistence natively
}

// SetJSONLFileHash stores the hash of the JSONL file for integrity validation.
// This was used by the old JSONL sync system. Kept as a no-op stub.
func (s *DoltStore) SetJSONLFileHash(_ context.Context, _ string) error {
	return nil // No-op: Dolt handles persistence natively
}
