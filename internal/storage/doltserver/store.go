package doltserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/dbproxy/proxy"
	"github.com/steveyegge/beads/internal/storage/dbproxy/util"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/storage/schema"
	"github.com/steveyegge/beads/internal/types"
)

// validIdentifier matches safe SQL identifiers (letters, digits, underscores).
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

type DoltServerStore struct {
	serverRootDir          string
	beadsDir               string
	database               string
	committerName          string
	committerEmail         string
	serverLogFilePath      string
	serverConfigFilePath   string
	backend                proxy.Backend
	autoSyncToOriginRemote bool
	rootUser               string
	rootPassword           string
	doltBinExec            string
	db                     *sql.DB
}

var (
	_ storage.DoltStorage      = (*DoltServerStore)(nil)
	_ storage.StoreLocator     = (*DoltServerStore)(nil)
	_ storage.GarbageCollector = (*DoltServerStore)(nil)
	_ storage.Flattener        = (*DoltServerStore)(nil)
	_ storage.Compactor        = (*DoltServerStore)(nil)
)

func NewDoltServerStore(
	ctx context.Context,
	serverRootDir string,
	beadsDir string,
	database string,
	committerName string,
	committerEmail string,
	serverLogFilePath string,
	serverConfigFilePath string,
	backend proxy.Backend,
	autoSyncToOriginRemote bool,
	rootUser string,
	rootPassword string,
	doltBinExec string,
) (*DoltServerStore, error) {
	if database == "" {
		return nil, fmt.Errorf("doltserver: database name must not be empty (caller should default to %q)", "beads")
	}
	if err := backend.Validate(); err != nil {
		return nil, fmt.Errorf("doltserver: backend: %w", err)
	}
	if rootUser == "" {
		return nil, fmt.Errorf("doltserver: rootUser must not be empty")
	}
	if doltBinExec == "" {
		return nil, fmt.Errorf("doltserver: doltBinExec must not be empty")
	}

	absServerRootDir, err := filepath.Abs(serverRootDir)
	if err != nil {
		return nil, fmt.Errorf("doltserver: resolving server root dir: %w", err)
	}
	absBeadsDir, err := filepath.Abs(beadsDir)
	if err != nil {
		return nil, fmt.Errorf("doltserver: resolving beads dir: %w", err)
	}
	absDoltBinExec, err := filepath.Abs(doltBinExec)
	if err != nil {
		return nil, fmt.Errorf("doltserver: resolving dolt bin exec: %w", err)
	}

	if err := os.MkdirAll(absServerRootDir, config.BeadsDirPerm); err != nil {
		return nil, fmt.Errorf("doltserver: creating server root directory: %w", err)
	}

	s := &DoltServerStore{
		serverRootDir:          absServerRootDir,
		beadsDir:               absBeadsDir,
		database:               database,
		committerName:          committerName,
		committerEmail:         committerEmail,
		serverLogFilePath:      serverLogFilePath,
		serverConfigFilePath:   serverConfigFilePath,
		backend:                backend,
		autoSyncToOriginRemote: autoSyncToOriginRemote,
		rootUser:               rootUser,
		rootPassword:           rootPassword,
		doltBinExec:            absDoltBinExec,
	}

	ep, err := s.getDatabaseProxyEndpoint()
	if err != nil {
		return nil, fmt.Errorf("doltserver: get proxy endpoint: %w", err)
	}

	initDB, err := openDB(ctx, buildDSN(ep, "", rootUser, rootPassword))
	if err != nil {
		return nil, err
	}

	if err := s.initSchema(ctx, initDB); err != nil {
		_ = initDB.Close()
		return nil, fmt.Errorf("doltserver: init schema: %w", err)
	}
	if err := initDB.Close(); err != nil {
		return nil, fmt.Errorf("doltserver: close init db: %w", err)
	}

	db, err := openDB(ctx, buildDSN(ep, database, rootUser, rootPassword))
	if err != nil {
		return nil, err
	}

	s.db = db
	return s, nil
}

func (s *DoltServerStore) getDatabaseProxyEndpoint() (proxy.Endpoint, error) {
	return proxy.GetCreateDatabaseProxyServerEndpoint(s.serverRootDir, proxy.OpenOpts{
		Backend:        s.backend,
		ConfigFilePath: s.serverConfigFilePath,
		LogFilePath:    s.serverLogFilePath,
		DoltBinPath:    s.doltBinExec,
		IdleTimeout:    30 * time.Second,
	})
}

func buildDSN(ep proxy.Endpoint, database, user, password string) string {
	return util.DoltServerDSN{
		Host:     ep.Host,
		Port:     ep.Port,
		User:     user,
		Password: password,
		Database: database,
	}.String()
}

func openDB(ctx context.Context, dsn string) (*sql.DB, error) {
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("doltserver: open db: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Join(fmt.Errorf("doltserver: ping db: %w", err), db.Close())
	}
	return db, nil
}

func withReadTxOn(ctx context.Context, db *sql.DB, fn func(ctx context.Context, tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("doltserver: begin read tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	return fn(ctx, tx)
}

func withWriteTxOn(ctx context.Context, db *sql.DB, fn func(ctx context.Context, tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("doltserver: begin write tx: %w", err)
	}
	if err := fn(ctx, tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("doltserver: commit: %w", err)
	}
	return nil
}

func (s *DoltServerStore) withReadTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	return withReadTxOn(ctx, s.db, fn)
}

func (s *DoltServerStore) withWriteTx(ctx context.Context, fn func(ctx context.Context, tx *sql.Tx) error) error {
	return withWriteTxOn(ctx, s.db, fn)
}

func (s *DoltServerStore) initSchema(ctx context.Context, db *sql.DB) error {
	if !validIdentifier.MatchString(s.database) {
		return fmt.Errorf("doltserver: invalid database name: %q", s.database)
	}
	dbIdent := "`" + s.database + "`"

	return withWriteTxOn(ctx, db, func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+dbIdent); err != nil {
			return fmt.Errorf("doltserver: creating database: %w", err)
		}
		if _, err := tx.ExecContext(ctx, "USE "+dbIdent); err != nil {
			return fmt.Errorf("doltserver: switching to database: %w", err)
		}

		if err := schema.EnsureIgnoredTables(ctx, tx); err != nil {
			return fmt.Errorf("ensure ignored tables before migration: %w", err)
		}

		applied, err := schema.MigrateUp(ctx, tx)
		if err != nil {
			return err
		}

		if applied > 0 {
			if _, err := tx.ExecContext(ctx, "CALL DOLT_ADD('-A')"); err != nil {
				return fmt.Errorf("dolt add after migrations: %w", err)
			}

			if _, err := tx.ExecContext(ctx, "CALL DOLT_COMMIT('-m', 'schema: apply migrations')"); err != nil {
				if !strings.Contains(err.Error(), "nothing to commit") {
					return fmt.Errorf("dolt commit after migrations: %w", err)
				}
			}
		}

		return nil
	})
}

// Storage — issue CRUD

func (s *DoltServerStore) CreateIssue(ctx context.Context, issue *types.Issue, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) CreateIssues(ctx context.Context, issues []*types.Issue, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) GetIssue(ctx context.Context, id string) (*types.Issue, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetIssueByExternalRef(ctx context.Context, externalRef string) (*types.Issue, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetIssuesByIDs(ctx context.Context, ids []string) ([]*types.Issue, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) ReopenIssue(ctx context.Context, id string, reason string, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) UpdateIssueType(ctx context.Context, id string, issueType string, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) CloseIssue(ctx context.Context, id string, reason string, actor string, session string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) DeleteIssue(ctx context.Context, id string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) SearchIssues(ctx context.Context, query string, filter types.IssueFilter) ([]*types.Issue, error) {
	panic("unimplemented")
}

// Storage — dependencies

func (s *DoltServerStore) AddDependency(ctx context.Context, dep *types.Dependency, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) RemoveDependency(ctx context.Context, issueID, dependsOnID string, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) GetDependencies(ctx context.Context, issueID string) ([]*types.Issue, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetDependents(ctx context.Context, issueID string) ([]*types.Issue, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetDependenciesWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetDependentsWithMetadata(ctx context.Context, issueID string) ([]*types.IssueWithDependencyMetadata, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetDependencyTree(ctx context.Context, issueID string, maxDepth int, showAllPaths bool, reverse bool) ([]*types.TreeNode, error) {
	panic("unimplemented")
}

// Storage — labels

func (s *DoltServerStore) AddLabel(ctx context.Context, issueID, label, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) RemoveLabel(ctx context.Context, issueID, label, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) GetLabels(ctx context.Context, issueID string) ([]string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetIssuesByLabel(ctx context.Context, label string) ([]*types.Issue, error) {
	panic("unimplemented")
}

// Storage — work queries

func (s *DoltServerStore) GetReadyWork(ctx context.Context, filter types.WorkFilter) ([]*types.Issue, error) {
	var result []*types.Issue
	err := s.withReadTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		result, err = issueops.GetReadyWorkInTx(ctx, tx, filter, doltServerComputeBlockedIDs)
		return err
	})
	return result, err
}

// doltServerComputeBlockedIDs adapts ComputeBlockedIDsInTx to the callback
// signature expected by GetReadyWorkInTx.
func doltServerComputeBlockedIDs(ctx context.Context, tx *sql.Tx, includeWisps bool) ([]string, error) {
	ids, _, err := issueops.ComputeBlockedIDsInTx(ctx, tx, includeWisps)
	return ids, err
}

func (s *DoltServerStore) GetBlockedIssues(ctx context.Context, filter types.WorkFilter) ([]*types.BlockedIssue, error) {
	var result []*types.BlockedIssue
	err := s.withReadTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		result, err = issueops.GetBlockedIssuesInTx(ctx, tx, filter)
		return err
	})
	return result, err
}

func (s *DoltServerStore) GetEpicsEligibleForClosure(ctx context.Context) ([]*types.EpicStatus, error) {
	panic("unimplemented")
}

// Storage — wisp queries

func (s *DoltServerStore) ListWisps(ctx context.Context, filter types.WispFilter) ([]*types.Issue, error) {
	panic("unimplemented")
}

// Storage — comments and events

func (s *DoltServerStore) AddIssueComment(ctx context.Context, issueID, author, text string) (*types.Comment, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetIssueComments(ctx context.Context, issueID string) ([]*types.Comment, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetEvents(ctx context.Context, issueID string, limit int) ([]*types.Event, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetAllEventsSince(ctx context.Context, since time.Time) ([]*types.Event, error) {
	panic("unimplemented")
}

// Storage — statistics

func (s *DoltServerStore) GetStatistics(ctx context.Context) (*types.Statistics, error) {
	panic("unimplemented")
}

// Storage — configuration

func (s *DoltServerStore) SetConfig(ctx context.Context, key, value string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) GetConfig(ctx context.Context, key string) (string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetAllConfig(ctx context.Context) (map[string]string, error) {
	panic("unimplemented")
}

// Storage — local metadata

func (s *DoltServerStore) SetLocalMetadata(ctx context.Context, key, value string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) GetLocalMetadata(ctx context.Context, key string) (string, error) {
	panic("unimplemented")
}

// Storage — transactions

func (s *DoltServerStore) RunInTransaction(ctx context.Context, commitMsg string, fn func(tx storage.Transaction) error) error {
	panic("unimplemented")
}

// Storage — merge slot

func (s *DoltServerStore) MergeSlotCreate(ctx context.Context, actor string) (*types.Issue, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) MergeSlotCheck(ctx context.Context) (*storage.MergeSlotStatus, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) MergeSlotAcquire(ctx context.Context, holder, actor string, wait bool) (*storage.MergeSlotResult, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) MergeSlotRelease(ctx context.Context, holder, actor string) error {
	panic("unimplemented")
}

// Storage — metadata slots

func (s *DoltServerStore) SlotSet(ctx context.Context, issueID, key, value, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) SlotGet(ctx context.Context, issueID, key string) (string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) SlotClear(ctx context.Context, issueID, key, actor string) error {
	panic("unimplemented")
}

// Storage — lifecycle

func (s *DoltServerStore) Close() error {
	panic("unimplemented")
}

// VersionControl

func (s *DoltServerStore) Branch(ctx context.Context, name string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) Checkout(ctx context.Context, branch string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) CurrentBranch(ctx context.Context) (string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) DeleteBranch(ctx context.Context, branch string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) ListBranches(ctx context.Context) ([]string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) Commit(ctx context.Context, message string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) CommitWithConfig(ctx context.Context, message string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) CommitPending(ctx context.Context, actor string) (bool, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) CommitExists(ctx context.Context, commitHash string) (bool, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetCurrentCommit(ctx context.Context) (string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) Status(ctx context.Context) (*storage.Status, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) Log(ctx context.Context, limit int) ([]storage.CommitInfo, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) Merge(ctx context.Context, branch string) ([]storage.Conflict, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetConflicts(ctx context.Context) ([]storage.Conflict, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) ResolveConflicts(ctx context.Context, table string, strategy string) error {
	panic("unimplemented")
}

// HistoryViewer

func (s *DoltServerStore) History(ctx context.Context, issueID string) ([]*storage.HistoryEntry, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) AsOf(ctx context.Context, issueID string, ref string) (*types.Issue, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) Diff(ctx context.Context, fromRef, toRef string) ([]*storage.DiffEntry, error) {
	panic("unimplemented")
}

// RemoteStore

func (s *DoltServerStore) AddRemote(ctx context.Context, name, url string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) RemoveRemote(ctx context.Context, name string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) HasRemote(ctx context.Context, name string) (bool, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) ListRemotes(ctx context.Context) ([]storage.RemoteInfo, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) Push(ctx context.Context) error {
	panic("unimplemented")
}

func (s *DoltServerStore) Pull(ctx context.Context) error {
	panic("unimplemented")
}

func (s *DoltServerStore) ForcePush(ctx context.Context) error {
	panic("unimplemented")
}

func (s *DoltServerStore) PushRemote(ctx context.Context, remote string, force bool) error {
	panic("unimplemented")
}

func (s *DoltServerStore) PullRemote(ctx context.Context, remote string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) Fetch(ctx context.Context, peer string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) PushTo(ctx context.Context, peer string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) PullFrom(ctx context.Context, peer string) ([]storage.Conflict, error) {
	panic("unimplemented")
}

// SyncStore

func (s *DoltServerStore) Sync(ctx context.Context, peer string, strategy string) (*storage.SyncResult, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) SyncStatus(ctx context.Context, peer string) (*storage.SyncStatus, error) {
	panic("unimplemented")
}

// FederationStore

func (s *DoltServerStore) AddFederationPeer(ctx context.Context, peer *storage.FederationPeer) error {
	panic("unimplemented")
}

func (s *DoltServerStore) GetFederationPeer(ctx context.Context, name string) (*storage.FederationPeer, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) ListFederationPeers(ctx context.Context) ([]*storage.FederationPeer, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) RemoveFederationPeer(ctx context.Context, name string) error {
	panic("unimplemented")
}

// BulkIssueStore

func (s *DoltServerStore) CreateIssuesWithFullOptions(ctx context.Context, issues []*types.Issue, actor string, opts storage.BatchCreateOptions) error {
	panic("unimplemented")
}

func (s *DoltServerStore) DeleteIssues(ctx context.Context, ids []string, cascade bool, force bool, dryRun bool) (*types.DeleteIssuesResult, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) DeleteIssuesBySourceRepo(ctx context.Context, sourceRepo string) (int, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) UpdateIssueID(ctx context.Context, oldID, newID string, issue *types.Issue, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) ClaimIssue(ctx context.Context, id string, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) ClaimReadyIssue(ctx context.Context, filter types.WorkFilter, actor string) (*types.Issue, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) PromoteFromEphemeral(ctx context.Context, id string, actor string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) GetNextChildID(ctx context.Context, parentID string) (string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) RenameCounterPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	panic("unimplemented")
}

// DependencyQueryStore

func (s *DoltServerStore) GetDependencyRecords(ctx context.Context, issueID string) ([]*types.Dependency, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetDependencyRecordsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Dependency, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetAllDependencyRecords(ctx context.Context) (map[string][]*types.Dependency, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetDependencyCounts(ctx context.Context, issueIDs []string) (map[string]*types.DependencyCounts, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetBlockingInfoForIssues(ctx context.Context, issueIDs []string) (map[string][]string, map[string][]string, map[string]string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) IsBlocked(ctx context.Context, issueID string) (bool, []string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetNewlyUnblockedByClose(ctx context.Context, closedIssueID string) ([]*types.Issue, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) DetectCycles(ctx context.Context) ([][]*types.Issue, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) FindWispDependentsRecursive(ctx context.Context, ids []string) (map[string]bool, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) RenameDependencyPrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	panic("unimplemented")
}

// AnnotationStore

func (s *DoltServerStore) AddComment(ctx context.Context, issueID, actor, comment string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) ImportIssueComment(ctx context.Context, issueID, author, text string, createdAt time.Time) (*types.Comment, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetCommentCounts(ctx context.Context, issueIDs []string) (map[string]int, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetCommentsForIssues(ctx context.Context, issueIDs []string) (map[string][]*types.Comment, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetLabelsForIssues(ctx context.Context, issueIDs []string) (map[string][]string, error) {
	panic("unimplemented")
}

// ConfigMetadataStore

func (s *DoltServerStore) GetMetadata(ctx context.Context, key string) (string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) SetMetadata(ctx context.Context, key, value string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) DeleteConfig(ctx context.Context, key string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) GetCustomStatuses(ctx context.Context) ([]string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetCustomStatusesDetailed(ctx context.Context) ([]types.CustomStatus, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetCustomTypes(ctx context.Context) ([]string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetInfraTypes(ctx context.Context) map[string]bool {
	panic("unimplemented")
}

func (s *DoltServerStore) IsInfraTypeCtx(ctx context.Context, t types.IssueType) bool {
	panic("unimplemented")
}

// CompactionStore

func (s *DoltServerStore) CheckEligibility(ctx context.Context, issueID string, tier int) (bool, string, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) ApplyCompaction(ctx context.Context, issueID string, tier int, originalSize int, compactedSize int, commitHash string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) GetTier1Candidates(ctx context.Context) ([]*types.CompactionCandidate, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetTier2Candidates(ctx context.Context) ([]*types.CompactionCandidate, error) {
	panic("unimplemented")
}

// AdvancedQueryStore

func (s *DoltServerStore) GetRepoMtime(ctx context.Context, repoPath string) (int64, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) SetRepoMtime(ctx context.Context, repoPath, jsonlPath string, mtimeNs int64) error {
	panic("unimplemented")
}

func (s *DoltServerStore) ClearRepoMtime(ctx context.Context, repoPath string) error {
	panic("unimplemented")
}

func (s *DoltServerStore) GetMoleculeProgress(ctx context.Context, moleculeID string) (*types.MoleculeProgressStats, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetMoleculeLastActivity(ctx context.Context, moleculeID string) (*types.MoleculeLastActivity, error) {
	panic("unimplemented")
}

func (s *DoltServerStore) GetStaleIssues(ctx context.Context, filter types.StaleFilter) ([]*types.Issue, error) {
	panic("unimplemented")
}

// StoreLocator

func (s *DoltServerStore) Path() string {
	panic("unimplemented")
}

func (s *DoltServerStore) CLIDir() string {
	panic("unimplemented")
}

// GarbageCollector

func (s *DoltServerStore) DoltGC(ctx context.Context) error {
	panic("unimplemented")
}

// Flattener

func (s *DoltServerStore) Flatten(ctx context.Context) error {
	panic("unimplemented")
}

// Compactor

func (s *DoltServerStore) Compact(ctx context.Context, initialHash, boundaryHash string, oldCommits int, recentHashes []string) error {
	panic("unimplemented")
}
