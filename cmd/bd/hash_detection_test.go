package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// TestHashDetection_CollisionScenarios tests behavior when hash collisions occur.
// While SHA-256 collisions are astronomically unlikely, we should handle edge cases.
// Run with: go test -race -run TestHashDetection_CollisionScenarios
func TestHashDetection_CollisionScenarios(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	testStore, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()
	if err := testStore.SetConfig(ctx, "issue_prefix", "hash"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Save and restore global state
	oldStore := store
	oldDbPath := dbPath
	oldRootCtx := rootCtx
	oldStoreActive := storeActive

	store = testStore
	dbPath = testDBPath
	rootCtx = ctx
	storeMutex.Lock()
	storeActive = true
	storeMutex.Unlock()

	defer func() {
		store = oldStore
		dbPath = oldDbPath
		rootCtx = oldRootCtx
		storeMutex.Lock()
		storeActive = oldStoreActive
		storeMutex.Unlock()
	}()

	// Test that identical content produces identical hash
	t.Run("identical_content_same_hash", func(t *testing.T) {
		issues := []*types.Issue{
			{
				ID:        "hash-test001",
				Title:     "Test Issue",
				Status:    types.StatusOpen,
				Priority:  1,
				IssueType: types.TypeTask,
			},
		}

		// Write twice
		if _, err := writeJSONLAtomic(jsonlPath, issues); err != nil {
			t.Fatalf("first write failed: %v", err)
		}
		data1, _ := os.ReadFile(jsonlPath)
		hash1 := computeHash(data1)

		if _, err := writeJSONLAtomic(jsonlPath, issues); err != nil {
			t.Fatalf("second write failed: %v", err)
		}
		data2, _ := os.ReadFile(jsonlPath)
		hash2 := computeHash(data2)

		if hash1 != hash2 {
			t.Errorf("identical content produced different hashes: %s vs %s", hash1, hash2)
		}
	})

	// Test that different content produces different hash
	t.Run("different_content_different_hash", func(t *testing.T) {
		issues1 := []*types.Issue{
			{ID: "hash-test002", Title: "Version 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		}
		issues2 := []*types.Issue{
			{ID: "hash-test002", Title: "Version 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		}

		if _, err := writeJSONLAtomic(jsonlPath, issues1); err != nil {
			t.Fatalf("first write failed: %v", err)
		}
		data1, _ := os.ReadFile(jsonlPath)
		hash1 := computeHash(data1)

		if _, err := writeJSONLAtomic(jsonlPath, issues2); err != nil {
			t.Fatalf("second write failed: %v", err)
		}
		data2, _ := os.ReadFile(jsonlPath)
		hash2 := computeHash(data2)

		if hash1 == hash2 {
			t.Error("different content produced same hash (collision!)")
		}
	})

	// Test hash stability with reordering (should be sorted by ID)
	t.Run("ordering_stability", func(t *testing.T) {
		issues1 := []*types.Issue{
			{ID: "hash-aaa", Title: "First", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
			{ID: "hash-bbb", Title: "Second", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		}
		issues2 := []*types.Issue{
			{ID: "hash-bbb", Title: "Second", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
			{ID: "hash-aaa", Title: "First", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		}

		if _, err := writeJSONLAtomic(jsonlPath, issues1); err != nil {
			t.Fatalf("first write failed: %v", err)
		}
		data1, _ := os.ReadFile(jsonlPath)
		hash1 := computeHash(data1)

		if _, err := writeJSONLAtomic(jsonlPath, issues2); err != nil {
			t.Fatalf("second write failed: %v", err)
		}
		data2, _ := os.ReadFile(jsonlPath)
		hash2 := computeHash(data2)

		// writeJSONLAtomic sorts by ID, so hashes should match
		if hash1 != hash2 {
			t.Errorf("reordered content produced different hashes: %s vs %s", hash1, hash2)
		}
	})
}

// TestHashDetection_ComputeDuringModification tests hash computation while
// file is being modified concurrently.
func TestHashDetection_ComputeDuringModification(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	// Create initial content
	issues := make([]*types.Issue, 50)
	for i := 0; i < 50; i++ {
		issues[i] = &types.Issue{
			ID:          generateUniqueTestID(t,"mod", i),
			Title:       "Modification Test",
			Description: "Content for hash testing during modifications",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
		}
	}

	if _, err := writeJSONLAtomic(jsonlPath, issues); err != nil {
		t.Fatalf("failed to create initial JSONL: %v", err)
	}

	var wg sync.WaitGroup
	var hashMismatches atomic.Int64
	var computeErrors atomic.Int64
	stopChan := make(chan struct{})

	// Writer goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		version := 0
		for {
			select {
			case <-stopChan:
				return
			default:
				version++
				for i := range issues {
					issues[i].Title = "Version " + string(rune('A'+version%26))
				}
				writeJSONLAtomic(jsonlPath, issues)
				time.Sleep(time.Microsecond * 50)
			}
		}
	}()

	// Hash computation goroutines
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stopChan:
					return
				default:
				}

				// Read file
				data1, err := os.ReadFile(jsonlPath)
				if err != nil {
					computeErrors.Add(1)
					continue
				}
				hash1 := computeHash(data1)

				// Read again and compute hash
				data2, err := os.ReadFile(jsonlPath)
				if err != nil {
					computeErrors.Add(1)
					continue
				}
				hash2 := computeHash(data2)

				// If the file changed between reads, hashes will differ
				// But each hash should still be valid for its content
				if hash1 != hash2 {
					// Verify hash1 is correct for data1
					rehash1 := computeHash(data1)
					if hash1 != rehash1 {
						hashMismatches.Add(1)
					}
				}

				time.Sleep(time.Microsecond * 10)
			}
		}()
	}

	time.Sleep(100 * time.Millisecond)
	close(stopChan)
	wg.Wait()

	// Hash computation should be deterministic regardless of concurrent writes
	if hashMismatches.Load() > 0 {
		t.Errorf("detected %d hash mismatches (non-deterministic hashing)", hashMismatches.Load())
	}
	t.Logf("compute errors: %d (expected during file updates)", computeErrors.Load())
}

// TestHashDetection_StaleHashAfterCrash simulates a crash scenario where
// the stored hash doesn't match the actual file.
func TestHashDetection_StaleHashAfterCrash(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	testStore, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()
	if err := testStore.SetConfig(ctx, "issue_prefix", "stale"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Save and restore global state
	oldStore := store
	oldDbPath := dbPath
	oldRootCtx := rootCtx
	oldStoreActive := storeActive

	store = testStore
	dbPath = testDBPath
	rootCtx = ctx
	storeMutex.Lock()
	storeActive = true
	storeMutex.Unlock()

	defer func() {
		store = oldStore
		dbPath = oldDbPath
		rootCtx = oldRootCtx
		storeMutex.Lock()
		storeActive = oldStoreActive
		storeMutex.Unlock()
	}()

	// Create initial JSONL and store its hash
	issues := []*types.Issue{
		{ID: "stale-001", Title: "Initial", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
	}
	if _, err := writeJSONLAtomic(jsonlPath, issues); err != nil {
		t.Fatalf("failed to write JSONL: %v", err)
	}

	data, _ := os.ReadFile(jsonlPath)
	initialHash := computeHash(data)

	// Store the hash in the database using SetJSONLFileHash
	if err := testStore.SetJSONLFileHash(ctx, initialHash); err != nil {
		t.Fatalf("failed to store hash: %v", err)
	}

	// Simulate crash: modify JSONL without updating stored hash
	issues[0].Title = "Modified after crash"
	if _, err := writeJSONLAtomic(jsonlPath, issues); err != nil {
		t.Fatalf("failed to write modified JSONL: %v", err)
	}

	// Now the stored hash is stale
	storedHash, _ := testStore.GetJSONLFileHash(ctx)
	currentData, _ := os.ReadFile(jsonlPath)
	currentHash := computeHash(currentData)

	if storedHash == currentHash {
		t.Error("hash should be stale after simulated crash")
	}

	// The system should detect this mismatch
	// validateJSONLIntegrity should return needsFullExport=true
	needsFullExport, err := validateJSONLIntegrity(ctx, jsonlPath)
	if err != nil {
		t.Logf("validation error (may be expected): %v", err)
	}
	if !needsFullExport {
		t.Error("validateJSONLIntegrity should detect stale hash and request full export")
	}
}

// TestHashDetection_HashVsMtimeConsistency tests that hash-based detection
// is more reliable than mtime-based detection.
func TestHashDetection_HashVsMtimeConsistency(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	testDBPath := filepath.Join(beadsDir, "test.db")
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	testStore, err := sqlite.New(context.Background(), testDBPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer testStore.Close()

	ctx := context.Background()
	if err := testStore.SetConfig(ctx, "issue_prefix", "mtime"); err != nil {
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Test 1: Same content, different mtime
	t.Run("same_content_different_mtime", func(t *testing.T) {
		issues := []*types.Issue{
			{ID: "mtime-001", Title: "Test", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		}

		// Write first time
		if _, err := writeJSONLAtomic(jsonlPath, issues); err != nil {
			t.Fatalf("first write failed: %v", err)
		}
		info1, _ := os.Stat(jsonlPath)
		data1, _ := os.ReadFile(jsonlPath)
		hash1 := computeHash(data1)

		// Wait and touch the file to change mtime
		time.Sleep(10 * time.Millisecond)
		if err := os.Chtimes(jsonlPath, time.Now(), time.Now()); err != nil {
			t.Fatalf("failed to touch file: %v", err)
		}

		info2, _ := os.Stat(jsonlPath)
		data2, _ := os.ReadFile(jsonlPath)
		hash2 := computeHash(data2)

		// mtime changed but hash should be same
		if info1.ModTime().Equal(info2.ModTime()) {
			t.Error("mtime should have changed")
		}
		if hash1 != hash2 {
			t.Error("hash should be same for identical content")
		}
	})

	// Test 2: Different content, same mtime (simulated)
	t.Run("different_content_same_mtime", func(t *testing.T) {
		issues1 := []*types.Issue{
			{ID: "mtime-002", Title: "Version 1", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		}
		issues2 := []*types.Issue{
			{ID: "mtime-002", Title: "Version 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
		}

		// Write version 1
		if _, err := writeJSONLAtomic(jsonlPath, issues1); err != nil {
			t.Fatalf("first write failed: %v", err)
		}
		info1, _ := os.Stat(jsonlPath)
		data1, _ := os.ReadFile(jsonlPath)
		hash1 := computeHash(data1)
		mtime1 := info1.ModTime()

		// Write version 2
		if _, err := writeJSONLAtomic(jsonlPath, issues2); err != nil {
			t.Fatalf("second write failed: %v", err)
		}

		// Set mtime back to original (simulating git checkout or fast writes)
		if err := os.Chtimes(jsonlPath, mtime1, mtime1); err != nil {
			t.Fatalf("failed to set mtime: %v", err)
		}

		info2, _ := os.Stat(jsonlPath)
		data2, _ := os.ReadFile(jsonlPath)
		hash2 := computeHash(data2)

		// mtime is same but content changed - hash should detect it
		if !info1.ModTime().Equal(info2.ModTime()) {
			t.Error("mtime should be same (we set it)")
		}
		if hash1 == hash2 {
			t.Error("hash should be different for different content")
		}
	})

	// Test 3: Rapid writes within same mtime resolution
	t.Run("rapid_writes_same_mtime_resolution", func(t *testing.T) {
		hashes := make(map[string]bool)

		for i := 0; i < 10; i++ {
			issues := []*types.Issue{
				{ID: "mtime-003", Title: "Rapid " + string(rune('A'+i)), Status: types.StatusOpen, Priority: 1, IssueType: types.TypeTask},
			}
			if _, err := writeJSONLAtomic(jsonlPath, issues); err != nil {
				continue
			}
			data, _ := os.ReadFile(jsonlPath)
			hash := computeHash(data)
			hashes[hash] = true
			// No sleep - writes as fast as possible
		}

		// Should have 10 different hashes for 10 different contents
		if len(hashes) < 10 {
			t.Errorf("expected 10 unique hashes, got %d (hash-based detection failed)", len(hashes))
		}
	})
}

// TestHashDetection_ConcurrentHashComputation tests thread safety of hash computation.
func TestHashDetection_ConcurrentHashComputation(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create beads dir: %v", err)
	}

	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")

	// Create a moderately large file
	issues := make([]*types.Issue, 100)
	for i := 0; i < 100; i++ {
		issues[i] = &types.Issue{
			ID:          generateUniqueTestID(t,"conc", i),
			Title:       "Concurrent Hash Test",
			Description: "This is test data for concurrent hash computation testing",
			Status:      types.StatusOpen,
			Priority:    1,
			IssueType:   types.TypeTask,
		}
	}

	if _, err := writeJSONLAtomic(jsonlPath, issues); err != nil {
		t.Fatalf("failed to write JSONL: %v", err)
	}

	// Read the file once for reference
	referenceData, _ := os.ReadFile(jsonlPath)
	referenceHash := computeHash(referenceData)

	var wg sync.WaitGroup
	var mismatches atomic.Int64

	// Many goroutines computing hash concurrently
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				data, err := os.ReadFile(jsonlPath)
				if err != nil {
					continue
				}
				hash := computeHash(data)
				if hash != referenceHash {
					mismatches.Add(1)
				}
			}
		}()
	}

	wg.Wait()

	// File wasn't modified, so all hashes should match
	if mismatches.Load() > 0 {
		t.Errorf("detected %d hash mismatches (should be 0 for unchanged file)", mismatches.Load())
	}
}

// computeHash computes SHA-256 hash of data and returns hex string
func computeHash(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}
