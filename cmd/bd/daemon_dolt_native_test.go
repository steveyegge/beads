package main

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage/memory"
)

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
	ctx := context.Background()
	store := memory.New("test")

	// Initialize config and clear sync.mode to test default behavior
	// This ensures the test is isolated from environment config
	if err := config.Initialize(); err != nil {
		t.Fatalf("failed to initialize config: %v", err)
	}
	config.Set("sync.mode", "")

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
