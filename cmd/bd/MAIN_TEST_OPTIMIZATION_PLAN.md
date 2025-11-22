# main_test.go Performance Optimization Plan

## Executive Summary
Tests are currently **hanging indefinitely** due to nil `rootCtx`. With fixes, we can achieve **90%+ speedup** (from ~60s+ to <5s total).

## Critical Fixes (MUST DO)

### Fix 1: Initialize rootCtx in Tests
**Impact**: Fixes hanging tests ← BLOCKING ISSUE
**Effort**: 5 minutes

Add to all tests that call `flushToJSONL()` or `autoImportIfNewer()`:

```go
func TestAutoFlushJSONLContent(t *testing.T) {
    // FIX: Initialize rootCtx for flush operations
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    oldRootCtx := rootCtx
    rootCtx = ctx
    defer func() { rootCtx = oldRootCtx }()

    // rest of test...
}
```

**Files affected**:
- TestAutoFlushOnExit
- TestAutoFlushJSONLContent
- TestAutoFlushErrorHandling
- TestAutoImportIfNewer
- TestAutoImportDisabled
- TestAutoImportWithUpdate
- TestAutoImportNoUpdate
- TestAutoImportMergeConflict
- TestAutoImportConflictMarkerFalsePositive
- TestAutoImportClosedAtInvariant

### Fix 2: Reduce Sleep Durations
**Impact**: Saves ~280ms
**Effort**: 2 minutes

```go
// BEFORE
time.Sleep(200 * time.Millisecond)

// AFTER
time.Sleep(20 * time.Millisecond)  // 10x faster, still reliable

// BEFORE
time.Sleep(100 * time.Millisecond)

// AFTER
time.Sleep(10 * time.Millisecond)  // 10x faster
```

**Rationale**: We're not testing actual timing, just sequencing. Shorter sleeps work fine.

## High-Impact Optimizations (RECOMMENDED)

### Opt 1: Share Test Fixtures
**Impact**: Saves ~1-1.5s
**Effort**: 15 minutes

Group related tests and reuse DB:

```go
func TestAutoFlushGroup(t *testing.T) {
    // Setup once
    tmpDir := t.TempDir()
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    rootCtx = ctx
    defer func() { rootCtx = nil }()

    // Subtest 1: DirtyMarking (no DB needed!)
    t.Run("DirtyMarking", func(t *testing.T) {
        autoFlushEnabled = true
        isDirty = false
        if flushTimer != nil {
            flushTimer.Stop()
            flushTimer = nil
        }

        markDirtyAndScheduleFlush()

        flushMutex.Lock()
        dirty := isDirty
        hasTimer := flushTimer != nil
        flushMutex.Unlock()

        assert(dirty && hasTimer)
    })

    // Subtest 2: Disabled (no DB needed!)
    t.Run("Disabled", func(t *testing.T) {
        autoFlushEnabled = false
        isDirty = false
        // ...
    })

    // Shared DB for remaining tests
    dbPath := filepath.Join(tmpDir, "shared.db")
    testStore := newTestStore(t, dbPath)
    store = testStore
    storeMutex.Lock()
    storeActive = true
    storeMutex.Unlock()
    defer func() {
        storeMutex.Lock()
        storeActive = false
        storeMutex.Unlock()
    }()

    t.Run("OnExit", func(t *testing.T) {
        // reuse testStore...
    })

    t.Run("JSONLContent", func(t *testing.T) {
        // reuse testStore...
    })
}
```

**Reduces DB setups from 14 to ~4-5**

### Opt 2: Use In-Memory SQLite
**Impact**: Saves ~800ms-1.2s
**Effort**: 10 minutes

```go
func newFastTestStore(t *testing.T) *sqlite.SQLiteStorage {
    t.Helper()

    // Use :memory: for speed (10-20x faster than file-based)
    store, err := sqlite.New(context.Background(), ":memory:")
    if err != nil {
        t.Fatalf("Failed to create in-memory database: %v", err)
    }

    ctx := context.Background()
    if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
        store.Close()
        t.Fatalf("Failed to set issue_prefix: %v", err)
    }

    t.Cleanup(func() { store.Close() })
    return store
}
```

**Use for tests that don't need filesystem integration**

### Opt 3: Skip TestAutoFlushDebounce
**Impact**: Re-enables skipped test with fix
**Effort**: 5 minutes

The test is currently skipped (line 93). Fix the config issue:

```go
func TestAutoFlushDebounce(t *testing.T) {
    // REMOVED: t.Skip()

    // FIX: Don't rely on config.Set during test
    // Instead, directly manipulate flushDebounce duration via test helper
    oldDebounce := flushDebounce
    flushDebounce = 20 * time.Millisecond  // Fast for testing
    defer func() { flushDebounce = oldDebounce }()

    // rest of test...
}
```

## Medium-Impact Optimizations (NICE TO HAVE)

### Opt 4: Parallel Test Execution
**Impact**: 40-60% faster with t.Parallel()
**Effort**: 20 minutes

**Careful!** Only parallelize tests that don't manipulate global state:
- TestImportOpenToClosedTransition ✓
- TestImportClosedToOpenTransition ✓
- (most can't be parallel due to global state)

```go
func TestImportOpenToClosedTransition(t *testing.T) {
    t.Parallel()  // Safe - no global state
    // ...
}
```

### Opt 5: Mock flushToJSONL() for State Tests
**Impact**: Saves ~200ms
**Effort**: 30 minutes

Tests like `TestAutoFlushDirtyMarking` don't need actual flushing:

```go
var flushToJSONLFunc = flushToJSONL  // Allow mocking

func TestAutoFlushDirtyMarking(t *testing.T) {
    flushToJSONLFunc = func() {}  // No-op
    defer func() { flushToJSONLFunc = flushToJSONL }()

    // Test just the state management...
}
```

## Expected Results

| Approach | Time Savings | Effort | Recommendation |
|----------|-------------|--------|----------------|
| Fix 1: rootCtx | Unblocks tests | 5 min | **DO NOW** |
| Fix 2: Reduce sleeps | ~280ms | 2 min | **DO NOW** |
| Opt 1: Share fixtures | ~1.2s | 15 min | **DO NOW** |
| Opt 2: In-memory DB | ~1s | 10 min | **RECOMMENDED** |
| Opt 3: Fix debounce test | Enables test | 5 min | **RECOMMENDED** |
| Opt 4: Parallel | ~2s (40%) | 20 min | Nice to have |
| Opt 5: Mock flushToJSONL | ~200ms | 30 min | Optional |

**Total speedup with Fixes + Opts 1-3: ~2.5-3s (from baseline after fixing hangs)**
**Total effort: ~40 minutes**

## Implementation Order

1. **Fix 1** (5 min) - Fixes hanging tests
2. **Fix 2** (2 min) - Quick win
3. **Opt 2** (10 min) - In-memory DBs where possible
4. **Opt 1** (15 min) - Share fixtures
5. **Opt 3** (5 min) - Fix skipped test
6. **Opt 4** (20 min) - Parallelize safe tests (optional)

## Alternative: Rewrite as Integration Tests

If tests remain slow after optimizations, consider:
- Move to `cmd/bd/integration_test` directory
- Run with `-short` flag to skip in normal CI
- Keep only smoke tests in main_test.go

**Trade-off**: Slower tests, but better integration coverage

## Validation

After changes, run:
```bash
go test -run "^TestAuto" -count=5  # Should complete in <5s consistently
go test -race -run "^TestAuto"     # Verify no race conditions
```
