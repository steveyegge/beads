package postgres

import (
	"context"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// stubs.go collects methods that satisfy the compile-time interface assertions
// in store.go but are not part of the v1 mayor scope (locally-runnable PG
// backend). They return errNotImplemented from errors.go so callers see a
// typed sentinel and CI can grep for `bd_phase=v2` work later.

// --- BulkIssueStore (most landed in claim.go / idgen.go / issues.go) ---

func (s *PostgresStore) DeleteIssues(ctx context.Context, ids []string, cascade, force, dryRun bool) (*types.DeleteIssuesResult, error) {
	return nil, notImplemented("DeleteIssues")
}

func (s *PostgresStore) DeleteIssuesBySourceRepo(ctx context.Context, sourceRepo string) (int, error) {
	return 0, notImplemented("DeleteIssuesBySourceRepo")
}

func (s *PostgresStore) UpdateIssueID(ctx context.Context, oldID, newID string, issue *types.Issue, actor string) error {
	return notImplemented("UpdateIssueID")
}

func (s *PostgresStore) PromoteFromEphemeral(ctx context.Context, id, actor string) error {
	return notImplemented("PromoteFromEphemeral")
}

// --- MergeSlot (Storage) ---

func (s *PostgresStore) MergeSlotCreate(ctx context.Context, actor string) (*types.Issue, error) {
	return nil, notImplemented("MergeSlotCreate")
}

func (s *PostgresStore) MergeSlotCheck(ctx context.Context) (*storage.MergeSlotStatus, error) {
	return nil, notImplemented("MergeSlotCheck")
}

func (s *PostgresStore) MergeSlotAcquire(ctx context.Context, holder, actor string, wait bool) (*storage.MergeSlotResult, error) {
	return nil, notImplemented("MergeSlotAcquire")
}

func (s *PostgresStore) MergeSlotRelease(ctx context.Context, holder, actor string) error {
	return notImplemented("MergeSlotRelease")
}

// --- Metadata slots (Storage) ---

func (s *PostgresStore) SlotSet(ctx context.Context, issueID, key, value, actor string) error {
	return notImplemented("SlotSet")
}

func (s *PostgresStore) SlotGet(ctx context.Context, issueID, key string) (string, error) {
	return "", notImplemented("SlotGet")
}

func (s *PostgresStore) SlotClear(ctx context.Context, issueID, key, actor string) error {
	return notImplemented("SlotClear")
}

// --- CompactionStore (entire surface deferred to v2) ---

func (s *PostgresStore) CheckEligibility(ctx context.Context, issueID string, tier int) (bool, string, error) {
	return false, "", notImplemented("CheckEligibility")
}

func (s *PostgresStore) ApplyCompaction(ctx context.Context, issueID string, tier int, originalSize, compactedSize int, commitHash string) error {
	return notImplemented("ApplyCompaction")
}

func (s *PostgresStore) GetTier1Candidates(ctx context.Context) ([]*types.CompactionCandidate, error) {
	return nil, notImplemented("GetTier1Candidates")
}

func (s *PostgresStore) GetTier2Candidates(ctx context.Context) ([]*types.CompactionCandidate, error) {
	return nil, notImplemented("GetTier2Candidates")
}

// nowUTC is a small helper that returns the current time in UTC. Avoids
// scattering `.UTC()` calls.
func nowUTC() time.Time { return time.Now().UTC() }
