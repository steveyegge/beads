package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/memory"
	"github.com/steveyegge/beads/internal/types"
)

// mockStatusCheckerStore wraps a memory store and adds StatusChecker capability
// for testing the sync overhead reduction optimization (gt-p1mpqx).
type mockStatusCheckerStore struct {
	*memory.MemoryStorage
	hasChanges    bool
	checkCount    int64
	commitCount   int64
	pushCount     int64
	pullCount     int64
	commitErr     error
	pushErr       error
	pullErr       error
}

func newMockStatusCheckerStore(hasChanges bool) *mockStatusCheckerStore {
	return &mockStatusCheckerStore{
		MemoryStorage: memory.New("test"),
		hasChanges:    hasChanges,
	}
}

func (m *mockStatusCheckerStore) HasUncommittedChanges(ctx context.Context) (bool, error) {
	atomic.AddInt64(&m.checkCount, 1)
	return m.hasChanges, nil
}

func (m *mockStatusCheckerStore) Commit(ctx context.Context, message string) error {
	atomic.AddInt64(&m.commitCount, 1)
	return m.commitErr
}

func (m *mockStatusCheckerStore) Push(ctx context.Context) error {
	atomic.AddInt64(&m.pushCount, 1)
	return m.pushErr
}

func (m *mockStatusCheckerStore) Pull(ctx context.Context) error {
	atomic.AddInt64(&m.pullCount, 1)
	return m.pullErr
}

// Implement remaining RemoteStorage interface methods by delegation
func (m *mockStatusCheckerStore) History(ctx context.Context, issueID string) ([]*storage.HistoryEntry, error) {
	return nil, nil
}
func (m *mockStatusCheckerStore) AsOf(ctx context.Context, issueID string, ref string) (*types.Issue, error) {
	return nil, nil
}
func (m *mockStatusCheckerStore) Diff(ctx context.Context, fromRef, toRef string) ([]*storage.DiffEntry, error) {
	return nil, nil
}
func (m *mockStatusCheckerStore) Branch(ctx context.Context, name string) error {
	return nil
}
func (m *mockStatusCheckerStore) Merge(ctx context.Context, branch string) ([]storage.Conflict, error) {
	return nil, nil
}
func (m *mockStatusCheckerStore) CurrentBranch(ctx context.Context) (string, error) {
	return "main", nil
}
func (m *mockStatusCheckerStore) ListBranches(ctx context.Context) ([]string, error) {
	return []string{"main"}, nil
}
func (m *mockStatusCheckerStore) GetCurrentCommit(ctx context.Context) (string, error) {
	return "abc123", nil
}
func (m *mockStatusCheckerStore) GetConflicts(ctx context.Context) ([]storage.Conflict, error) {
	return nil, nil
}
func (m *mockStatusCheckerStore) ResolveConflicts(ctx context.Context, table string, strategy string) error {
	return nil
}
func (m *mockStatusCheckerStore) AddRemote(ctx context.Context, name, url string) error {
	return nil
}

// TestDoltNativeExportFunc_NonRemoteStore tests that export handles non-remote stores gracefully
func TestDoltNativeExportFunc_NonRemoteStore(t *testing.T) {
	ctx := context.Background()
	store := memory.New("test") // Memory store doesn't implement RemoteStorage
	log := newTestLogger()

	// Should complete without panic when store doesn't support remote operations
	fn := createDoltNativeExportFunc(ctx, store, true, true, log)
	fn() // Should log error but not panic
}

// TestDoltNativePullFunc_NonRemoteStore tests that pull handles non-remote stores gracefully
func TestDoltNativePullFunc_NonRemoteStore(t *testing.T) {
	ctx := context.Background()
	store := memory.New("test")
	log := newTestLogger()

	// Should complete without panic when store doesn't support remote operations
	fn := createDoltNativePullFunc(ctx, store, log)
	fn() // Should log error but not panic
}

// TestDoltNativeSyncFunc_NonRemoteStore tests that sync handles non-remote stores gracefully
func TestDoltNativeSyncFunc_NonRemoteStore(t *testing.T) {
	ctx := context.Background()
	store := memory.New("test")
	log := newTestLogger()

	// Should complete without panic when store doesn't support remote operations
	fn := createDoltNativeSyncFunc(ctx, store, true, true, true, log)
	fn() // Should log error but not panic
}

// TestDoltNativeExportFunc_CancelledContext tests graceful handling of cancelled context
func TestDoltNativeExportFunc_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	store := memory.New("test")
	log := newTestLogger()

	// Should complete without panic even with cancelled context
	fn := createDoltNativeExportFunc(ctx, store, true, true, log)
	fn()
}

// TestDoltNativeSyncFunc_Timeout tests timeout handling
func TestDoltNativeSyncFunc_Timeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(5 * time.Millisecond) // Let timeout expire

	store := memory.New("test")
	log := newTestLogger()

	// Should complete without panic even with expired context
	fn := createDoltNativeSyncFunc(ctx, store, true, true, true, log)
	fn()
}

// TestDoltNativeFunctions_NoAutoCommit tests behavior when autoCommit is false
func TestDoltNativeFunctions_NoAutoCommit(t *testing.T) {
	ctx := context.Background()
	store := memory.New("test")
	log := newTestLogger()

	// With autoCommit=false, should not attempt to commit
	fn := createDoltNativeExportFunc(ctx, store, false, false, log)
	fn() // Should complete quickly without doing anything
}

// TestDoltNativeFunctions_NoPull tests behavior when autoPull is false
func TestDoltNativeFunctions_NoPull(t *testing.T) {
	ctx := context.Background()
	store := memory.New("test")
	log := newTestLogger()

	// With autoPull=false, should not attempt to pull
	fn := createDoltNativeSyncFunc(ctx, store, true, true, false, log)
	fn() // Should skip pull step
}

// TestGetSyncModeDoltNative verifies sync mode detection
func TestGetSyncModeDoltNative(t *testing.T) {
	// Reset config to avoid dolt-native mode from repo config
	config.ResetForTesting()

	ctx := context.Background()
	store := memory.New("test")

	// Default should be git-portable when neither database nor config.yaml has a value
	mode := GetSyncMode(ctx, store)
	if mode != SyncModeGitPortable {
		t.Errorf("expected default mode %s, got %s", SyncModeGitPortable, mode)
	}

	// Set dolt-native mode
	if err := SetSyncMode(ctx, store, SyncModeDoltNative); err != nil {
		t.Fatalf("failed to set sync mode: %v", err)
	}

	// Should now return dolt-native
	mode = GetSyncMode(ctx, store)
	if mode != SyncModeDoltNative {
		t.Errorf("expected mode %s, got %s", SyncModeDoltNative, mode)
	}
}

// TestShouldExportJSONL_DoltNative verifies JSONL is skipped for dolt-native
func TestShouldExportJSONL_DoltNative(t *testing.T) {
	// Reset config to ensure default sync mode (not dolt-native from repo config)
	config.ResetForTesting()

	ctx := context.Background()
	store := memory.New("test")

	// Default should export JSONL
	if !ShouldExportJSONL(ctx, store) {
		t.Error("expected ShouldExportJSONL=true for default mode")
	}

	// Set dolt-native mode
	if err := SetSyncMode(ctx, store, SyncModeDoltNative); err != nil {
		t.Fatalf("failed to set sync mode: %v", err)
	}

	// Should NOT export JSONL in dolt-native
	if ShouldExportJSONL(ctx, store) {
		t.Error("expected ShouldExportJSONL=false for dolt-native mode")
	}
}

// TestShouldUseDoltRemote_DoltNative verifies Dolt remote is used for dolt-native
func TestShouldUseDoltRemote_DoltNative(t *testing.T) {
	ctx := context.Background()
	store := memory.New("test")

	// Default should NOT use Dolt remote
	if ShouldUseDoltRemote(ctx, store) {
		t.Error("expected ShouldUseDoltRemote=false for default mode")
	}

	// Set dolt-native mode
	if err := SetSyncMode(ctx, store, SyncModeDoltNative); err != nil {
		t.Fatalf("failed to set sync mode: %v", err)
	}

	// Should use Dolt remote in dolt-native
	if !ShouldUseDoltRemote(ctx, store) {
		t.Error("expected ShouldUseDoltRemote=true for dolt-native mode")
	}
}

// =============================================================================
// StatusChecker Optimization Tests (gt-p1mpqx)
// =============================================================================
// These tests verify the sync overhead reduction optimization that checks
// for uncommitted changes before attempting expensive commit/push operations.

// TestDoltNativeSyncFunc_SkipsCommitWhenNoChanges verifies that commit/push
// are skipped when HasUncommittedChanges returns false.
func TestDoltNativeSyncFunc_SkipsCommitWhenNoChanges(t *testing.T) {
	ctx := context.Background()
	store := newMockStatusCheckerStore(false) // No uncommitted changes
	log := newTestLogger()

	fn := createDoltNativeSyncFunc(ctx, store, true, true, true, log)
	fn()

	// Should have checked status
	if store.checkCount == 0 {
		t.Error("expected HasUncommittedChanges to be called")
	}

	// Should NOT have attempted commit or push since no changes
	if store.commitCount > 0 {
		t.Errorf("expected no commits when no changes, got %d", store.commitCount)
	}
	if store.pushCount > 0 {
		t.Errorf("expected no pushes when no changes, got %d", store.pushCount)
	}
}

// TestDoltNativeSyncFunc_CommitsWhenChangesExist verifies that commit/push
// are called when HasUncommittedChanges returns true.
func TestDoltNativeSyncFunc_CommitsWhenChangesExist(t *testing.T) {
	ctx := context.Background()
	store := newMockStatusCheckerStore(true) // Has uncommitted changes
	log := newTestLogger()

	fn := createDoltNativeSyncFunc(ctx, store, true, true, true, log)
	fn()

	// Should have checked status
	if store.checkCount == 0 {
		t.Error("expected HasUncommittedChanges to be called")
	}

	// Should have attempted commit and push since there are changes
	if store.commitCount == 0 {
		t.Error("expected commit when changes exist")
	}
	if store.pushCount == 0 {
		t.Error("expected push when changes exist")
	}
}

// TestDoltNativeSyncFunc_PullAlwaysAttempted verifies that pull is attempted
// regardless of uncommitted changes status (we want fresh remote data).
func TestDoltNativeSyncFunc_PullAlwaysAttempted(t *testing.T) {
	ctx := context.Background()
	store := newMockStatusCheckerStore(false) // No uncommitted changes
	log := newTestLogger()

	fn := createDoltNativeSyncFunc(ctx, store, true, true, true, log)
	fn()

	// Pull should still be attempted even when no local changes
	if store.pullCount == 0 {
		t.Error("expected pull to be attempted even when no local changes")
	}
}

// TestStatusCheckerInterface verifies the mock implements StatusChecker
func TestStatusCheckerInterface(t *testing.T) {
	store := newMockStatusCheckerStore(true)

	// Verify it can be cast to StatusChecker
	sc, ok := storage.AsStatusChecker(store)
	if !ok {
		t.Fatal("mockStatusCheckerStore should implement StatusChecker")
	}

	ctx := context.Background()
	hasChanges, err := sc.HasUncommittedChanges(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !hasChanges {
		t.Error("expected hasChanges=true")
	}
}
