// Package postgres provides the Postgres storage backend for beads.
// This file contains the PGDriver (implements storage.Driver) and
// PostgresStore (implements storage.Storage) skeletons. Full SQL
// implementations will be filled in by subsequent beads.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// errNotImplemented is returned by stub methods pending full implementation.
var errNotImplemented = errors.New("postgres: not implemented")

// PostgresStore is the Postgres-backed implementation of storage.Storage.
// Methods are filled in by subsequent beads; stubs return errNotImplemented.
type PostgresStore struct {
	dsn string
}

// openStore creates a PostgresStore connected to the given configuration.
// Used by benchmarks and integration tests.
func openStore(_ context.Context, dsn string) (*PostgresStore, error) {
	if dsn == "" {
		return nil, fmt.Errorf("postgres: DSN is required")
	}
	return &PostgresStore{dsn: dsn}, nil
}

// Close releases the database connection pool.
func (s *PostgresStore) Close() error { return nil }

// Issue CRUD — stubs pending full implementation.

func (s *PostgresStore) CreateIssue(_ context.Context, issue *types.Issue, _ string) error {
	// IMPORTANT: must call ValidateWithCustom (not Validate) to honor custom types.
	// Full implementation loads customStatuses and customTypes from the DB first.
	if err := issue.ValidateWithCustom(nil, nil); err != nil {
		return err
	}
	return errNotImplemented
}

func (s *PostgresStore) CreateIssues(_ context.Context, _ []*types.Issue, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) GetIssue(_ context.Context, _ string) (*types.Issue, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetIssueByExternalRef(_ context.Context, _ string) (*types.Issue, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetIssuesByIDs(_ context.Context, _ []string) ([]*types.Issue, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) UpdateIssue(_ context.Context, _ string, _ map[string]interface{}, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) ReopenIssue(_ context.Context, _ string, _ string, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) UpdateIssueType(_ context.Context, _ string, _ string, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) CloseIssue(_ context.Context, _ string, _ string, _ string, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) DeleteIssue(_ context.Context, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) SearchIssues(_ context.Context, _ string, _ types.IssueFilter) ([]*types.Issue, error) {
	return nil, errNotImplemented
}

// Dependencies — stubs.

func (s *PostgresStore) AddDependency(_ context.Context, _ *types.Dependency, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) RemoveDependency(_ context.Context, _, _ string, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) GetDependencies(_ context.Context, _ string) ([]*types.Issue, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetDependents(_ context.Context, _ string) ([]*types.Issue, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetDependenciesWithMetadata(_ context.Context, _ string) ([]*types.IssueWithDependencyMetadata, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetDependentsWithMetadata(_ context.Context, _ string) ([]*types.IssueWithDependencyMetadata, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetDependencyTree(_ context.Context, _ string, _ int, _, _ bool) ([]*types.TreeNode, error) {
	return nil, errNotImplemented
}

// Labels — stubs.

func (s *PostgresStore) AddLabel(_ context.Context, _, _, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) RemoveLabel(_ context.Context, _, _, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) GetLabels(_ context.Context, _ string) ([]string, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetIssuesByLabel(_ context.Context, _ string) ([]*types.Issue, error) {
	return nil, errNotImplemented
}

// Work queries — stubs.

func (s *PostgresStore) GetReadyWork(_ context.Context, _ types.WorkFilter) ([]*types.Issue, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetBlockedIssues(_ context.Context, _ types.WorkFilter) ([]*types.BlockedIssue, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetEpicsEligibleForClosure(_ context.Context) ([]*types.EpicStatus, error) {
	return nil, errNotImplemented
}

// Wisps — stub.

func (s *PostgresStore) ListWisps(_ context.Context, _ types.WispFilter) ([]*types.Issue, error) {
	return nil, errNotImplemented
}

// Comments and events — stubs.

func (s *PostgresStore) AddIssueComment(_ context.Context, _, _, _ string) (*types.Comment, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetIssueComments(_ context.Context, _ string) ([]*types.Comment, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetEvents(_ context.Context, _ string, _ int) ([]*types.Event, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) GetAllEventsSince(_ context.Context, _ time.Time) ([]*types.Event, error) {
	return nil, errNotImplemented
}

// Statistics — stub.

func (s *PostgresStore) GetStatistics(_ context.Context) (*types.Statistics, error) {
	return nil, errNotImplemented
}

// Configuration — stubs.

func (s *PostgresStore) SetConfig(_ context.Context, _, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) GetConfig(_ context.Context, _ string) (string, error) {
	return "", errNotImplemented
}

func (s *PostgresStore) GetAllConfig(_ context.Context) (map[string]string, error) {
	return nil, errNotImplemented
}

// Local metadata — stubs.

func (s *PostgresStore) SetLocalMetadata(_ context.Context, _, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) GetLocalMetadata(_ context.Context, _ string) (string, error) {
	return "", errNotImplemented
}

// Transactions — stub.

func (s *PostgresStore) RunInTransaction(_ context.Context, _ string, _ func(storage.Transaction) error) error {
	return errNotImplemented
}

// MergeSlot — stubs.

func (s *PostgresStore) MergeSlotCreate(_ context.Context, _ string) (*types.Issue, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) MergeSlotCheck(_ context.Context) (*storage.MergeSlotStatus, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) MergeSlotAcquire(_ context.Context, _, _ string, _ bool) (*storage.MergeSlotResult, error) {
	return nil, errNotImplemented
}

func (s *PostgresStore) MergeSlotRelease(_ context.Context, _, _ string) error {
	return errNotImplemented
}

// Metadata slots — stubs.

func (s *PostgresStore) SlotSet(_ context.Context, _, _, _, _ string) error {
	return errNotImplemented
}

func (s *PostgresStore) SlotGet(_ context.Context, _, _ string) (string, error) {
	return "", errNotImplemented
}

func (s *PostgresStore) SlotClear(_ context.Context, _, _, _ string) error {
	return errNotImplemented
}

// PGDriver wraps PostgresStore and implements storage.Driver.
type PGDriver struct {
	store *PostgresStore
}

var _ storage.Driver = (*PGDriver)(nil)

// Name returns "postgres".
func (d *PGDriver) Name() string { return "postgres" }

// Capabilities returns the Postgres capability set.
func (d *PGDriver) Capabilities() storage.CapabilitySet { return storage.PostgresCapabilities }

// Open parses the DSN from cfg.Options["dsn"] and opens the connection pool.
func (d *PGDriver) Open(ctx context.Context, cfg storage.DriverConfig) error {
	dsn := cfg.Options["dsn"]
	store, err := openStore(ctx, dsn)
	if err != nil {
		return fmt.Errorf("pg driver open: %w", err)
	}
	d.store = store
	return nil
}

// Close releases the PG connection pool.
func (d *PGDriver) Close() error {
	if d.store == nil {
		return nil
	}
	return d.store.Close()
}

// Ping verifies the PG server is reachable.
func (d *PGDriver) Ping(_ context.Context) error { return errNotImplemented }

// SchemaVersion returns the current PG schema version.
func (d *PGDriver) SchemaVersion(_ context.Context) (int, error) { return 0, errNotImplemented }

// InitSchema creates the PG schema tables.
func (d *PGDriver) InitSchema(_ context.Context) error { return errNotImplemented }

// MigrateSchema runs PG migrations to targetVersion.
func (d *PGDriver) MigrateSchema(_ context.Context, _ int) error { return errNotImplemented }

// Storage delegation — all Storage methods forward to the embedded PostgresStore.

func (d *PGDriver) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	return d.store.CreateIssue(ctx, issue, actor)
}

func (d *PGDriver) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	return d.store.CreateIssues(ctx, issues, actor)
}

func (d *PGDriver) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	return d.store.GetIssue(ctx, id)
}

func (d *PGDriver) GetIssueByExternalRef(ctx context.Context, ref string) (*types.Issue, error) {
	return d.store.GetIssueByExternalRef(ctx, ref)
}

func (d *PGDriver) GetIssuesByIDs(ctx context.Context, ids []string) ([]*types.Issue, error) {
	return d.store.GetIssuesByIDs(ctx, ids)
}

func (d *PGDriver) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	return d.store.UpdateIssue(ctx, id, updates, actor)
}

func (d *PGDriver) ReopenIssue(ctx context.Context, id string, reason string, actor string) error {
	return d.store.ReopenIssue(ctx, id, reason, actor)
}

func (d *PGDriver) UpdateIssueType(ctx context.Context, id string, issueType string, actor string) error {
	return d.store.UpdateIssueType(ctx, id, issueType, actor)
}

func (d *PGDriver) CloseIssue(ctx context.Context, id string, reason string, actor string, session string) error {
	return d.store.CloseIssue(ctx, id, reason, actor, session)
}

func (d *PGDriver) DeleteIssue(ctx context.Context, id string) error {
	return d.store.DeleteIssue(ctx, id)
}

func (d *PGDriver) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	return d.store.SearchIssues(ctx, query, filter)
}

func (d *PGDriver) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	return d.store.AddDependency(ctx, dep, actor)
}

func (d *PGDriver) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	return d.store.RemoveDependency(ctx, issueID, dependsOnID, actor)
}

func (d *PGDriver) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return d.store.GetDependencies(ctx, issueID)
}

func (d *PGDriver) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	return d.store.GetDependents(ctx, issueID)
}

func (d *PGDriver) GetDependenciesWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	return d.store.GetDependenciesWithMetadata(ctx, issueID)
}

func (d *PGDriver) GetDependentsWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	return d.store.GetDependentsWithMetadata(ctx, issueID)
}

func (d *PGDriver) GetDependencyTree(ctx context.Context, issueID string, maxDepth int, showAllPaths bool, reverse bool) ([]*types.TreeNode, error) {
	return d.store.GetDependencyTree(ctx, issueID, maxDepth, showAllPaths, reverse)
}

func (d *PGDriver) AddLabel(ctx context.Context, issueID, label, actor string) error {
	return d.store.AddLabel(ctx, issueID, label, actor)
}

func (d *PGDriver) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	return d.store.RemoveLabel(ctx, issueID, label, actor)
}

func (d *PGDriver) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	return d.store.GetLabels(ctx, issueID)
}

func (d *PGDriver) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	return d.store.GetIssuesByLabel(ctx, label)
}

func (d *PGDriver) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	return d.store.GetReadyWork(ctx, filter)
}

func (d *PGDriver) GetBlockedIssues(ctx context.Context, filter types.WorkFilter) ([]*types.BlockedIssue, error) {
	return d.store.GetBlockedIssues(ctx, filter)
}

func (d *PGDriver) GetEpicsEligibleForClosure(ctx context.Context) ([]*types.EpicStatus, error) {
	return d.store.GetEpicsEligibleForClosure(ctx)
}

func (d *PGDriver) ListWisps(ctx context.Context, filter types.WispFilter) ([]*types.Issue, error) {
	return d.store.ListWisps(ctx, filter)
}

func (d *PGDriver) AddIssueComment(ctx context.Context, issueID, author, text string) (*types.Comment, error) {
	return d.store.AddIssueComment(ctx, issueID, author, text)
}

func (d *PGDriver) GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	return d.store.GetIssueComments(ctx, issueID)
}

func (d *PGDriver) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	return d.store.GetEvents(ctx, issueID, limit)
}

func (d *PGDriver) GetAllEventsSince(ctx context.Context, since time.Time) ([]*types.Event, error) {
	return d.store.GetAllEventsSince(ctx, since)
}

func (d *PGDriver) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	return d.store.GetStatistics(ctx)
}

func (d *PGDriver) SetConfig(ctx context.Context, key, value string) error {
	return d.store.SetConfig(ctx, key, value)
}

func (d *PGDriver) GetConfig(ctx context.Context, key string) (string, error) {
	return d.store.GetConfig(ctx, key)
}

func (d *PGDriver) GetAllConfig(ctx context.Context) (map[string]string, error) {
	return d.store.GetAllConfig(ctx)
}

func (d *PGDriver) SetLocalMetadata(ctx context.Context, key, value string) error {
	return d.store.SetLocalMetadata(ctx, key, value)
}

func (d *PGDriver) GetLocalMetadata(ctx context.Context, key string) (string, error) {
	return d.store.GetLocalMetadata(ctx, key)
}

func (d *PGDriver) RunInTransaction(ctx context.Context, commitMsg string, fn func(storage.Transaction) error) error {
	return d.store.RunInTransaction(ctx, commitMsg, fn)
}

func (d *PGDriver) MergeSlotCreate(ctx context.Context, actor string) (*types.Issue, error) {
	return d.store.MergeSlotCreate(ctx, actor)
}

func (d *PGDriver) MergeSlotCheck(ctx context.Context) (*storage.MergeSlotStatus, error) {
	return d.store.MergeSlotCheck(ctx)
}

func (d *PGDriver) MergeSlotAcquire(ctx context.Context, holder, actor string, wait bool) (*storage.MergeSlotResult, error) {
	return d.store.MergeSlotAcquire(ctx, holder, actor, wait)
}

func (d *PGDriver) MergeSlotRelease(ctx context.Context, holder, actor string) error {
	return d.store.MergeSlotRelease(ctx, holder, actor)
}

func (d *PGDriver) SlotSet(ctx context.Context, issueID, key, value, actor string) error {
	return d.store.SlotSet(ctx, issueID, key, value, actor)
}

func (d *PGDriver) SlotGet(ctx context.Context, issueID, key string) (string, error) {
	return d.store.SlotGet(ctx, issueID, key)
}

func (d *PGDriver) SlotClear(ctx context.Context, issueID, key, actor string) error {
	return d.store.SlotClear(ctx, issueID, key, actor)
}

func init() {
	storage.RegisterDriver("postgres", func() (storage.Driver, error) {
		return &PGDriver{}, nil
	})
}
