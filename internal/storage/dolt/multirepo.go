//go:build cgo

package dolt

import "context"

// ExportToMultiRepo is a no-op for Dolt backend.
// Dolt uses native version control (push/pull/merge) instead of JSONL-based multi-repo sync.
// Returns nil, nil to indicate multi-repo mode is not active.
func (s *DoltStore) ExportToMultiRepo(_ context.Context) (map[string]int, error) {
	return nil, nil
}

// HydrateFromMultiRepo is a no-op for Dolt backend.
// Dolt uses native version control (push/pull/merge) instead of JSONL-based multi-repo sync.
// Returns nil, nil to indicate multi-repo mode is not active.
func (s *DoltStore) HydrateFromMultiRepo(_ context.Context) (map[string]int, error) {
	return nil, nil
}
