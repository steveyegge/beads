// Package postgres provides the Postgres storage backend for beads.
// This file contains the Store type (implements storage.Storage) that is
// wired into the runtime connection path by cmd/bd/store_factory.go.
//
// All Storage methods currently return errNotImplemented. The full SQL
// implementation is tracked in subsequent beads (be-i3xud5 phase 1).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/steveyegge/beads/internal/storage"
	pgdsn "github.com/steveyegge/beads/internal/storage/postgres/dsn"
	"github.com/steveyegge/beads/internal/types"
)

var errNotImplemented = errors.New("postgres: not implemented")

// Store is the Postgres-backed implementation of storage.Storage.
// Methods are filled in by subsequent beads; stubs return errNotImplemented.
type Store struct {
	// dsn is the full connection string (with password) used for opening.
	dsn string
	// overrideFields is the sorted list of field names overridden by
	// BEADS_POSTGRES_* env vars. Stored for downstream reporting surfaces.
	overrideFields []string
}

// OverrideFields returns the list of DSN fields overridden by BEADS_POSTGRES_*
// env vars on this invocation, in alphabetical order. Empty when no overrides
// were applied. Exposed for bd context / bd backend status reporting.
func (s *Store) OverrideFields() []string {
	return s.overrideFields
}

// Open attempts a TCP probe against the Postgres server at the address
// encoded in fullDSN. On success it returns a Store ready for use. On
// failure it wraps the connection error with a redacted target string and
// the overrideFields list (NFR-4).
//
// fullDSN must include the password; strippedDSN (no password) is used
// only for the redacted error message.
func Open(ctx context.Context, fullDSN, strippedDSN string, overrideFields []string) (*Store, error) {
	cfg, err := pgconn.ParseConfig(fullDSN)
	if err != nil {
		return nil, fmt.Errorf("postgres: invalid DSN: %w", err)
	}

	// Quick TCP probe — cheaper than a full pgx connection for the error path.
	conn, err := pgconn.ConnectConfig(ctx, cfg)
	if err != nil {
		target := pgdsn.RenderRedacted(strippedDSN)
		msg := fmt.Sprintf("postgres unreachable: %s", target)
		if len(overrideFields) > 0 {
			msg += fmt.Sprintf(" (overrides applied: %s)", strings.Join(overrideFields, ", "))
		}
		return nil, fmt.Errorf("%s — err=%w", msg, err)
	}
	_ = conn.Close(ctx)

	return &Store{dsn: fullDSN, overrideFields: overrideFields}, nil
}

// --- storage.Storage stubs (all return errNotImplemented) ---

func (s *Store) CreateIssue(_ context.Context, _ *types.Issue, _ string) error {
	return errNotImplemented
}
func (s *Store) CreateIssues(_ context.Context, _ []*types.Issue, _ string) error {
	return errNotImplemented
}
func (s *Store) GetIssue(_ context.Context, _ string) (*types.Issue, error) {
	return nil, errNotImplemented
}
func (s *Store) GetIssueByExternalRef(_ context.Context, _ string) (*types.Issue, error) {
	return nil, errNotImplemented
}
func (s *Store) GetIssuesByIDs(_ context.Context, _ []string) ([]*types.Issue, error) {
	return nil, errNotImplemented
}
func (s *Store) UpdateIssue(_ context.Context, _ string, _ map[string]interface{}, _ string) error {
	return errNotImplemented
}
func (s *Store) ReopenIssue(_ context.Context, _ string, _ string, _ string) error {
	return errNotImplemented
}
func (s *Store) UpdateIssueType(_ context.Context, _ string, _ string, _ string) error {
	return errNotImplemented
}
func (s *Store) CloseIssue(_ context.Context, _ string, _ string, _ string, _ string) error {
	return errNotImplemented
}
func (s *Store) DeleteIssue(_ context.Context, _ string) error {
	return errNotImplemented
}
func (s *Store) SearchIssues(_ context.Context, _ string, _ types.IssueFilter) ([]*types.Issue, error) {
	return nil, errNotImplemented
}

func (s *Store) AddDependency(_ context.Context, _ *types.Dependency, _ string) error {
	return errNotImplemented
}
func (s *Store) RemoveDependency(_ context.Context, _, _ string, _ string) error {
	return errNotImplemented
}
func (s *Store) GetDependencies(_ context.Context, _ string) ([]*types.Issue, error) {
	return nil, errNotImplemented
}
func (s *Store) GetDependents(_ context.Context, _ string) ([]*types.Issue, error) {
	return nil, errNotImplemented
}
func (s *Store) GetDependenciesWithMetadata(_ context.Context, _ string) ([]*types.IssueWithDependencyMetadata, error) {
	return nil, errNotImplemented
}
func (s *Store) GetDependentsWithMetadata(_ context.Context, _ string) ([]*types.IssueWithDependencyMetadata, error) {
	return nil, errNotImplemented
}
func (s *Store) GetDependencyTree(_ context.Context, _ string, _ int, _ bool, _ bool) ([]*types.TreeNode, error) {
	return nil, errNotImplemented
}

func (s *Store) AddLabel(_ context.Context, _, _, _ string) error    { return errNotImplemented }
func (s *Store) RemoveLabel(_ context.Context, _, _, _ string) error { return errNotImplemented }
func (s *Store) GetLabels(_ context.Context, _ string) ([]string, error) {
	return nil, errNotImplemented
}
func (s *Store) GetIssuesByLabel(_ context.Context, _ string) ([]*types.Issue, error) {
	return nil, errNotImplemented
}

func (s *Store) GetReadyWork(_ context.Context, _ types.WorkFilter) ([]*types.Issue, error) {
	return nil, errNotImplemented
}
func (s *Store) GetBlockedIssues(_ context.Context, _ types.WorkFilter) ([]*types.BlockedIssue, error) {
	return nil, errNotImplemented
}
func (s *Store) GetEpicsEligibleForClosure(_ context.Context) ([]*types.EpicStatus, error) {
	return nil, errNotImplemented
}
func (s *Store) ListWisps(_ context.Context, _ types.WispFilter) ([]*types.Issue, error) {
	return nil, errNotImplemented
}

func (s *Store) AddIssueComment(_ context.Context, _, _, _ string) (*types.Comment, error) {
	return nil, errNotImplemented
}
func (s *Store) GetIssueComments(_ context.Context, _ string) ([]*types.Comment, error) {
	return nil, errNotImplemented
}
func (s *Store) GetEvents(_ context.Context, _ string, _ int) ([]*types.Event, error) {
	return nil, errNotImplemented
}
func (s *Store) GetAllEventsSince(_ context.Context, _ time.Time) ([]*types.Event, error) {
	return nil, errNotImplemented
}

func (s *Store) IterIssues(_ context.Context, _ string, _ types.IssueFilter) (storage.Iter[types.Issue], error) {
	return nil, errNotImplemented
}
func (s *Store) IterDependentsWithMetadata(_ context.Context, _ string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	return nil, errNotImplemented
}
func (s *Store) IterDependenciesWithMetadata(_ context.Context, _ string) (storage.Iter[types.IssueWithDependencyMetadata], error) {
	return nil, errNotImplemented
}
func (s *Store) IterIssueComments(_ context.Context, _ string) (storage.Iter[types.Comment], error) {
	return nil, errNotImplemented
}
func (s *Store) IterEvents(_ context.Context, _ string, _ int) (storage.Iter[types.Event], error) {
	return nil, errNotImplemented
}
func (s *Store) IterAllEventsSince(_ context.Context, _ time.Time) (storage.Iter[types.Event], error) {
	return nil, errNotImplemented
}
func (s *Store) IterReadyWork(_ context.Context, _ types.WorkFilter) (storage.Iter[types.Issue], error) {
	return nil, errNotImplemented
}
func (s *Store) IterBlockedIssues(_ context.Context, _ types.WorkFilter) (storage.Iter[types.BlockedIssue], error) {
	return nil, errNotImplemented
}
func (s *Store) IterWisps(_ context.Context, _ types.WispFilter) (storage.Iter[types.Issue], error) {
	return nil, errNotImplemented
}

func (s *Store) GetStatistics(_ context.Context) (*types.Statistics, error) {
	return nil, errNotImplemented
}

func (s *Store) SetConfig(_ context.Context, _, _ string) error { return errNotImplemented }
func (s *Store) GetConfig(_ context.Context, _ string) (string, error) {
	return "", errNotImplemented
}
func (s *Store) GetAllConfig(_ context.Context) (map[string]string, error) {
	return nil, errNotImplemented
}

func (s *Store) SetLocalMetadata(_ context.Context, _, _ string) error { return errNotImplemented }
func (s *Store) GetLocalMetadata(_ context.Context, _ string) (string, error) {
	return "", errNotImplemented
}

func (s *Store) RunInTransaction(_ context.Context, _ string, _ func(tx storage.Transaction) error) error {
	return errNotImplemented
}

func (s *Store) MergeSlotCreate(_ context.Context, _ string) (*types.Issue, error) {
	return nil, errNotImplemented
}
func (s *Store) MergeSlotCheck(_ context.Context) (*storage.MergeSlotStatus, error) {
	return nil, errNotImplemented
}
func (s *Store) MergeSlotAcquire(_ context.Context, _, _ string, _ bool) (*storage.MergeSlotResult, error) {
	return nil, errNotImplemented
}
func (s *Store) MergeSlotRelease(_ context.Context, _, _ string) error { return errNotImplemented }

func (s *Store) SlotSet(_ context.Context, _, _, _, _ string) error { return errNotImplemented }
func (s *Store) SlotGet(_ context.Context, _, _ string) (string, error) {
	return "", errNotImplemented
}
func (s *Store) SlotClear(_ context.Context, _, _, _ string) error { return errNotImplemented }

func (s *Store) Close() error { return nil }

// compile-time check that Store implements storage.Storage
var _ storage.Storage = (*Store)(nil)
