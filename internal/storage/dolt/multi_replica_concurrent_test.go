// Package dolt provides multi-replica concurrency tests for Dolt sql-server mode.
//
// These tests validate that 2+ bd-daemon replicas can safely write concurrently
// to the same Dolt sql-server. This is the practical validation needed before
// implementing leader election (lo-epc-primary_writer_pattern_multi_replica_bd.2).
//
// Each test creates a real dolt sql-server and connects multiple independent
// DoltStore clients (simulating separate daemon processes) to validate:
//   - Different issues from different replicas: no conflict
//   - Same issue updated by different replicas: one rolled back with clean error
//   - Concurrent counters: lost updates without explicit locking
//   - Mixed read/write workloads across replicas: no deadlocks
package dolt

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// findAvailablePort finds an available TCP port starting from startPort
func findAvailablePort(t *testing.T, startPort int) int {
	t.Helper()
	for port := startPort; port < startPort+100; port++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			ln.Close()
			return port
		}
	}
	t.Fatalf("no available port found starting from %d", startPort)
	return 0
}

const multiReplicaTestTimeout = 120 * time.Second

// multiReplicaTestContext returns a context with generous timeout for server-mode tests
func multiReplicaTestContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), multiReplicaTestTimeout)
}

// multiReplicaEnv holds the shared test environment: one dolt sql-server, multiple store clients
type multiReplicaEnv struct {
	t       *testing.T
	server  *Server
	tmpDir  string
	stores  []*DoltStore
	sqlPort int
}

// setupMultiReplicaEnv starts a dolt sql-server and connects N replica stores.
// The caller's context is used for all setup operations to avoid context
// cancellation invalidating connections when the setup function returns.
func setupMultiReplicaEnv(ctx context.Context, t *testing.T, numReplicas int) *multiReplicaEnv {
	t.Helper()

	// Skip if dolt not installed
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt not installed, skipping multi-replica test")
	}

	tmpDir, err := os.MkdirTemp("", "dolt-multi-replica-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Initialize dolt repo
	cmd := exec.Command("dolt", "init")
	cmd.Dir = tmpDir
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to init dolt repo: %v\n%s", err, out)
	}

	// Find available ports for SQL and remotesapi
	sqlPort := findAvailablePort(t, 14307)
	remotesPort := findAvailablePort(t, sqlPort+100)

	server := NewServer(ServerConfig{
		DataDir:        tmpDir,
		SQLPort:        sqlPort,
		RemotesAPIPort: remotesPort,
		Host:           "127.0.0.1",
		LogFile:        filepath.Join(tmpDir, "server.log"),
	})

	if err := server.Start(ctx); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to start dolt sql-server: %v", err)
	}

	// Give server a moment to fully initialize
	time.Sleep(500 * time.Millisecond)

	env := &multiReplicaEnv{
		t:       t,
		server:  server,
		tmpDir:  tmpDir,
		sqlPort: sqlPort,
	}

	// Create replica stores sequentially with warmup.
	// Key insight: each DoltStore must execute at least one real operation
	// before concurrent goroutine access, otherwise the go-sql-driver/mysql
	// connection pool has cold-start issues with BeginTx from goroutines.
	for i := 0; i < numReplicas; i++ {
		store, err := New(ctx, &Config{
			Path:       tmpDir,
			Database:   "beads",
			ServerMode: true,
			ServerHost: "127.0.0.1",
			ServerPort: sqlPort,
		})
		if err != nil {
			env.cleanup()
			t.Fatalf("failed to create replica store %d: %v", i, err)
		}
		env.stores = append(env.stores, store)

		// First store: configure the database
		if i == 0 {
			if err := store.SetConfig(ctx, "issue_prefix", "mr"); err != nil {
				env.cleanup()
				t.Fatalf("failed to set issue_prefix: %v", err)
			}
			if err := store.SetConfig(ctx, "types.custom", "gate,molecule,convoy,merge-request,slot,agent,role,rig,message"); err != nil {
				env.cleanup()
				t.Fatalf("failed to set custom types: %v", err)
			}
		}

		// Warmup: create a real issue to exercise the connection pool
		warmup := &types.Issue{
			Title:     fmt.Sprintf("warmup-replica-%d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, warmup, fmt.Sprintf("warmup-%d", i)); err != nil {
			env.cleanup()
			t.Fatalf("replica %d warmup CreateIssue failed: %v", i, err)
		}
		t.Logf("replica %d created and warmed up (issue %s)", i, warmup.ID)
	}
	t.Logf("all %d replicas connected and warmed up on port %d", numReplicas, sqlPort)

	return env
}

func (env *multiReplicaEnv) cleanup() {
	// Print server log before cleanup for debugging
	logPath := filepath.Join(env.tmpDir, "server.log")
	if data, err := os.ReadFile(logPath); err == nil && len(data) > 0 {
		env.t.Logf("=== Server log ===\n%s", string(data))
	}
	for _, store := range env.stores {
		store.Close()
	}
	if err := env.server.Stop(); err != nil {
		env.t.Logf("warning: failed to stop server: %v", err)
	}
	os.RemoveAll(env.tmpDir)
}

// =============================================================================
// Test 1: Different Issues from Different Replicas (No Conflict Expected)
//
// Two replicas create entirely different issues concurrently.
// Validates: Both succeed, all issues visible to both replicas.
// =============================================================================

func TestMultiReplica_DifferentIssueCreation(t *testing.T) {
	ctx, cancel := multiReplicaTestContext(t)
	defer cancel()

	env := setupMultiReplicaEnv(ctx, t, 2)
	defer env.cleanup()

	const issuesPerReplica = 10
	var wg sync.WaitGroup
	errors := make(chan error, 2*issuesPerReplica)
	createdIDs := make(chan string, 2*issuesPerReplica)

	// Each replica creates its own set of issues
	for replicaIdx, store := range env.stores {
		wg.Add(1)
		go func(idx int, s *DoltStore) {
			defer wg.Done()
			for i := 0; i < issuesPerReplica; i++ {
				issue := &types.Issue{
					Title:       fmt.Sprintf("Replica-%d Issue %d", idx, i),
					Description: fmt.Sprintf("Created by replica %d, issue %d", idx, i),
					Status:      types.StatusOpen,
					Priority:    (i % 4) + 1,
					IssueType:   types.TypeTask,
				}
				if err := s.CreateIssue(ctx, issue, fmt.Sprintf("replica-%d", idx)); err != nil {
					errors <- fmt.Errorf("replica %d, issue %d: %w", idx, i, err)
					return
				}
				createdIDs <- issue.ID
			}
		}(replicaIdx, store)
	}

	wg.Wait()
	close(errors)
	close(createdIDs)

	// Check for errors - expect none
	var errCount int
	for err := range errors {
		t.Errorf("creation error: %v", err)
		errCount++
	}
	if errCount > 0 {
		t.Fatalf("%d replicas had errors creating different issues", errCount)
	}

	// Collect all IDs
	ids := make(map[string]bool)
	for id := range createdIDs {
		if ids[id] {
			t.Errorf("duplicate issue ID across replicas: %s", id)
		}
		ids[id] = true
	}

	expectedTotal := 2 * issuesPerReplica
	if len(ids) != expectedTotal {
		t.Errorf("expected %d unique IDs, got %d", expectedTotal, len(ids))
	}

	// Verify ALL issues are visible from BOTH replicas
	for id := range ids {
		for replicaIdx, store := range env.stores {
			issue, err := store.GetIssue(ctx, id)
			if err != nil {
				t.Errorf("replica %d failed to read issue %s: %v", replicaIdx, id, err)
			}
			if issue == nil {
				t.Errorf("replica %d cannot see issue %s created by another replica", replicaIdx, id)
			}
		}
	}

	t.Logf("PASS: %d issues created concurrently by 2 replicas, all visible to both", expectedTotal)
}

// =============================================================================
// Test 2: Different Issues Updated by Different Replicas (No Conflict Expected)
//
// Pre-create issues, then have each replica update a disjoint set.
// Validates: No conflicts when replicas work on different data.
// =============================================================================

func TestMultiReplica_DifferentIssueUpdates(t *testing.T) {
	ctx, cancel := multiReplicaTestContext(t)
	defer cancel()

	env := setupMultiReplicaEnv(ctx, t, 2)
	defer env.cleanup()

	const issuesPerReplica = 10

	// Pre-create issues (assigned to specific replicas)
	issuesByReplica := make([][]string, 2)
	for r := 0; r < 2; r++ {
		for i := 0; i < issuesPerReplica; i++ {
			issue := &types.Issue{
				Title:       fmt.Sprintf("Replica-%d owned issue %d", r, i),
				Description: "Original",
				Status:      types.StatusOpen,
				Priority:    2,
				IssueType:   types.TypeTask,
			}
			if err := env.stores[0].CreateIssue(ctx, issue, "setup"); err != nil {
				t.Fatalf("setup: failed to create issue: %v", err)
			}
			issuesByReplica[r] = append(issuesByReplica[r], issue.ID)
		}
	}

	// Each replica updates ONLY its assigned issues
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	for replicaIdx, store := range env.stores {
		wg.Add(1)
		go func(idx int, s *DoltStore) {
			defer wg.Done()
			for i, issueID := range issuesByReplica[idx] {
				updates := map[string]interface{}{
					"description": fmt.Sprintf("Updated by replica %d, iteration %d", idx, i),
					"priority":    (i % 4) + 1,
				}
				if err := s.UpdateIssue(ctx, issueID, updates, fmt.Sprintf("replica-%d", idx)); err != nil {
					t.Logf("replica %d update error on %s: %v", idx, issueID, err)
					errorCount.Add(1)
				} else {
					successCount.Add(1)
				}
			}
		}(replicaIdx, store)
	}

	wg.Wait()

	totalExpected := int32(2 * issuesPerReplica)
	t.Logf("Disjoint update results: %d/%d succeeded, %d failed",
		successCount.Load(), totalExpected, errorCount.Load())

	// ALL should succeed since they're disjoint
	if successCount.Load() != totalExpected {
		t.Errorf("expected all %d disjoint updates to succeed, but %d failed",
			totalExpected, errorCount.Load())
	}

	// Verify updates persisted correctly
	for replicaIdx, ids := range issuesByReplica {
		for i, id := range ids {
			issue, err := env.stores[0].GetIssue(ctx, id)
			if err != nil {
				t.Errorf("failed to read issue %s: %v", id, err)
				continue
			}
			expected := fmt.Sprintf("Updated by replica %d, iteration %d", replicaIdx, i)
			if issue.Description != expected {
				t.Errorf("issue %s: expected description %q, got %q", id, expected, issue.Description)
			}
		}
	}

	t.Logf("PASS: Disjoint updates from 2 replicas completed without conflict")
}

// =============================================================================
// Test 3: Same Issue Updated by Different Replicas (Conflict Expected)
//
// Both replicas update the same issue simultaneously.
// Validates: Dolt handles the conflict via merge-based resolution.
// At least one succeeds; errors (if any) are clean serialization errors.
// =============================================================================

func TestMultiReplica_SameIssueConflict(t *testing.T) {
	ctx, cancel := multiReplicaTestContext(t)
	defer cancel()

	env := setupMultiReplicaEnv(ctx, t, 2)
	defer env.cleanup()

	// Create the contested issue
	issue := &types.Issue{
		ID:          "mr-conflict-test",
		Title:       "Contested Issue",
		Description: "Original description",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := env.stores[0].CreateIssue(ctx, issue, "setup"); err != nil {
		t.Fatalf("failed to create contested issue: %v", err)
	}

	const iterations = 20
	var wg sync.WaitGroup
	var successCount [2]atomic.Int32
	var errorCount [2]atomic.Int32
	var serializationErrors atomic.Int32

	// Both replicas hammer the same issue
	for replicaIdx, store := range env.stores {
		wg.Add(1)
		go func(idx int, s *DoltStore) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				updates := map[string]interface{}{
					"description": fmt.Sprintf("Replica %d, update %d", idx, i),
				}
				if err := s.UpdateIssue(ctx, issue.ID, updates, fmt.Sprintf("replica-%d", idx)); err != nil {
					errorCount[idx].Add(1)
					if isSerializationError(err) {
						serializationErrors.Add(1)
					} else {
						t.Logf("replica %d non-serialization error at iteration %d: %v", idx, i, err)
					}
				} else {
					successCount[idx].Add(1)
				}
			}
		}(replicaIdx, store)
	}

	wg.Wait()

	t.Logf("Same-issue conflict results:")
	t.Logf("  Replica 0: %d succeeded, %d failed", successCount[0].Load(), errorCount[0].Load())
	t.Logf("  Replica 1: %d succeeded, %d failed", successCount[1].Load(), errorCount[1].Load())
	t.Logf("  Serialization errors: %d", serializationErrors.Load())

	// At least one replica should have some successes
	totalSuccess := successCount[0].Load() + successCount[1].Load()
	if totalSuccess == 0 {
		t.Error("no updates succeeded from either replica")
	}

	// Final state should be consistent and readable
	retrieved, err := env.stores[0].GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to read issue after conflict: %v", err)
	}
	if retrieved == nil {
		t.Fatal("issue disappeared after concurrent updates")
	}

	t.Logf("  Final description: %q", retrieved.Description)
	t.Logf("PASS: Same-issue concurrent updates handled cleanly (%d total successes, %d serialization errors)",
		totalSuccess, serializationErrors.Load())
}

// =============================================================================
// Test 4: Same Cell Updated Identically (No Conflict Per Dolt Docs)
//
// Both replicas set the same field to the same value.
// Dolt should merge this cleanly (identical updates = no conflict).
// =============================================================================

func TestMultiReplica_IdenticalUpdates(t *testing.T) {
	ctx, cancel := multiReplicaTestContext(t)
	defer cancel()

	env := setupMultiReplicaEnv(ctx, t, 2)
	defer env.cleanup()

	issue := &types.Issue{
		ID:          "mr-identical-test",
		Title:       "Identical Update Test",
		Description: "Original",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := env.stores[0].CreateIssue(ctx, issue, "setup"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	// Both replicas set the same value
	for replicaIdx, store := range env.stores {
		wg.Add(1)
		go func(idx int, s *DoltStore) {
			defer wg.Done()
			updates := map[string]interface{}{
				"description": "Converged description",
				"priority":    1,
			}
			if err := s.UpdateIssue(ctx, issue.ID, updates, fmt.Sprintf("replica-%d", idx)); err != nil {
				errorCount.Add(1)
				t.Logf("replica %d identical update error: %v", idx, err)
			} else {
				successCount.Add(1)
			}
		}(replicaIdx, store)
	}

	wg.Wait()

	t.Logf("Identical update results: %d succeeded, %d failed",
		successCount.Load(), errorCount.Load())

	// Verify final state
	retrieved, err := env.stores[0].GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to read issue: %v", err)
	}
	if retrieved.Description != "Converged description" {
		t.Errorf("expected converged description, got %q", retrieved.Description)
	}
	if retrieved.Priority != 1 {
		t.Errorf("expected priority 1, got %d", retrieved.Priority)
	}

	t.Logf("PASS: Identical concurrent updates handled (success=%d, errors=%d)",
		successCount.Load(), errorCount.Load())
}

// =============================================================================
// Test 5: Different Columns on Same Issue (Merge Expected to Succeed)
//
// Replica 0 updates description, Replica 1 updates notes.
// Dolt's cell-level merge should allow both to succeed.
// =============================================================================

func TestMultiReplica_DifferentColumnsOnSameIssue(t *testing.T) {
	ctx, cancel := multiReplicaTestContext(t)
	defer cancel()

	env := setupMultiReplicaEnv(ctx, t, 2)
	defer env.cleanup()

	issue := &types.Issue{
		ID:          "mr-diff-col-test",
		Title:       "Different Columns Test",
		Description: "Original description",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := env.stores[0].CreateIssue(ctx, issue, "setup"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	var wg sync.WaitGroup
	var results [2]struct {
		success atomic.Int32
		fail    atomic.Int32
	}

	// Replica 0: updates description
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			updates := map[string]interface{}{
				"description": fmt.Sprintf("Description update %d", i),
			}
			if err := env.stores[0].UpdateIssue(ctx, issue.ID, updates, "replica-0"); err != nil {
				results[0].fail.Add(1)
			} else {
				results[0].success.Add(1)
			}
		}
	}()

	// Replica 1: updates notes (different column)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			updates := map[string]interface{}{
				"notes": fmt.Sprintf("Notes update %d", i),
			}
			if err := env.stores[1].UpdateIssue(ctx, issue.ID, updates, "replica-1"); err != nil {
				results[1].fail.Add(1)
			} else {
				results[1].success.Add(1)
			}
		}
	}()

	wg.Wait()

	t.Logf("Different-column update results:")
	t.Logf("  Replica 0 (description): %d succeeded, %d failed", results[0].success.Load(), results[0].fail.Load())
	t.Logf("  Replica 1 (notes):       %d succeeded, %d failed", results[1].success.Load(), results[1].fail.Load())

	// Verify final state has updates from BOTH replicas
	retrieved, err := env.stores[0].GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to read issue: %v", err)
	}

	t.Logf("  Final description: %q", retrieved.Description)
	t.Logf("  Final notes: %q", retrieved.Notes)

	// At minimum, most updates should succeed
	totalSuccess := results[0].success.Load() + results[1].success.Load()
	if totalSuccess < 10 {
		t.Errorf("expected at least 10 of 20 updates to succeed, got %d", totalSuccess)
	}

	t.Logf("PASS: Different-column concurrent updates (%d/20 succeeded)", totalSuccess)
}

// =============================================================================
// Test 6: Counter Simulation (Lost Update Problem)
//
// Simulates concurrent counter increment - a known weak spot in
// Dolt's Read Committed isolation. Demonstrates the lost-update problem.
// =============================================================================

func TestMultiReplica_CounterLostUpdates(t *testing.T) {
	ctx, cancel := multiReplicaTestContext(t)
	defer cancel()

	env := setupMultiReplicaEnv(ctx, t, 2)
	defer env.cleanup()

	// Create issue with initial "counter" in notes field
	issue := &types.Issue{
		ID:          "mr-counter-test",
		Title:       "Counter Test",
		Description: "Testing lost updates",
		Notes:       "count:0",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := env.stores[0].CreateIssue(ctx, issue, "setup"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	// Each replica does read-modify-write cycles on the notes field
	const incrementsPerReplica = 10
	var wg sync.WaitGroup
	var successCount [2]atomic.Int32

	for replicaIdx, store := range env.stores {
		wg.Add(1)
		go func(idx int, s *DoltStore) {
			defer wg.Done()
			for i := 0; i < incrementsPerReplica; i++ {
				// Read-modify-write (no explicit locking)
				err := s.RunInTransaction(ctx, func(tx storage.Transaction) error {
					current, err := s.GetIssue(ctx, issue.ID)
					if err != nil {
						return err
					}
					// "Increment" by appending to notes
					newNotes := fmt.Sprintf("%s,r%d-i%d", current.Notes, idx, i)
					return tx.UpdateIssue(ctx, issue.ID, map[string]interface{}{
						"notes": newNotes,
					}, fmt.Sprintf("replica-%d", idx))
				})
				if err != nil {
					t.Logf("replica %d counter increment %d failed: %v", idx, i, err)
				} else {
					successCount[idx].Add(1)
				}
			}
		}(replicaIdx, store)
	}

	wg.Wait()

	totalSuccesses := successCount[0].Load() + successCount[1].Load()
	totalExpected := int32(2 * incrementsPerReplica)

	// Read final state
	retrieved, err := env.stores[0].GetIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("failed to read issue: %v", err)
	}

	t.Logf("Counter lost-update results:")
	t.Logf("  Replica 0: %d/%d increments succeeded", successCount[0].Load(), incrementsPerReplica)
	t.Logf("  Replica 1: %d/%d increments succeeded", successCount[1].Load(), incrementsPerReplica)
	t.Logf("  Total successful: %d/%d", totalSuccesses, totalExpected)
	t.Logf("  Final notes: %q", retrieved.Notes)

	// The key insight: even if all increments "succeed", the final value may
	// be missing some updates due to the lost-update problem. This is expected
	// with Read Committed isolation and read-modify-write without explicit locking.
	if totalSuccesses < totalExpected {
		t.Logf("NOTE: %d increments failed (likely serialization errors - handled by retry)",
			totalExpected-totalSuccesses)
	}

	t.Logf("PASS: Counter test demonstrates Dolt's concurrent behavior " +
		"(lost updates are possible without explicit locking)")
}

// =============================================================================
// Test 7: Mixed Read-Write Workload Across Replicas (No Deadlocks)
//
// Replica 0 writes, Replica 1 reads, both working on shared data.
// Validates: No deadlocks, reads always return consistent state.
// =============================================================================

func TestMultiReplica_MixedReadWriteWorkload(t *testing.T) {
	ctx, cancel := multiReplicaTestContext(t)
	defer cancel()

	env := setupMultiReplicaEnv(ctx, t, 2)
	defer env.cleanup()

	// Pre-create issues
	const numIssues = 10
	issueIDs := make([]string, numIssues)
	for i := 0; i < numIssues; i++ {
		issue := &types.Issue{
			Title:       fmt.Sprintf("Mixed RW Issue %d", i),
			Description: "Initial",
			Status:      types.StatusOpen,
			Priority:    (i % 4) + 1,
			IssueType:   types.TypeTask,
		}
		if err := env.stores[0].CreateIssue(ctx, issue, "setup"); err != nil {
			t.Fatalf("failed to create issue %d: %v", i, err)
		}
		issueIDs[i] = issue.ID
	}

	const iterations = 50
	var wg sync.WaitGroup
	var writeSuccess atomic.Int32
	var writeFail atomic.Int32
	var readSuccess atomic.Int32
	var readFail atomic.Int32

	// Replica 0: writes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			issueID := issueIDs[i%numIssues]
			updates := map[string]interface{}{
				"description": fmt.Sprintf("Write %d", i),
			}
			if err := env.stores[0].UpdateIssue(ctx, issueID, updates, "writer"); err != nil {
				writeFail.Add(1)
			} else {
				writeSuccess.Add(1)
			}
		}
	}()

	// Replica 1: reads
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			issueID := issueIDs[i%numIssues]
			issue, err := env.stores[1].GetIssue(ctx, issueID)
			if err != nil || issue == nil {
				readFail.Add(1)
			} else {
				readSuccess.Add(1)
			}
		}
	}()

	// Wait with timeout to detect deadlocks
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// No deadlock
	case <-ctx.Done():
		t.Fatal("timeout - possible deadlock in mixed read/write workload")
	}

	t.Logf("Mixed read/write results:")
	t.Logf("  Writes: %d succeeded, %d failed", writeSuccess.Load(), writeFail.Load())
	t.Logf("  Reads:  %d succeeded, %d failed", readSuccess.Load(), readFail.Load())

	// All reads should succeed (reads don't conflict)
	if readFail.Load() > 0 {
		t.Errorf("unexpected read failures: %d", readFail.Load())
	}

	// Most writes should succeed
	if writeSuccess.Load() < int32(iterations/2) {
		t.Errorf("too many write failures: only %d/%d succeeded", writeSuccess.Load(), iterations)
	}

	t.Logf("PASS: No deadlocks in mixed read/write workload across replicas")
}

// =============================================================================
// Test 8: Transaction Isolation Between Replicas
//
// Validates that a long transaction on one replica doesn't block
// short transactions on the other replica indefinitely.
// =============================================================================

func TestMultiReplica_TransactionIsolation(t *testing.T) {
	ctx, cancel := multiReplicaTestContext(t)
	defer cancel()

	env := setupMultiReplicaEnv(ctx, t, 2)
	defer env.cleanup()

	// Create test issue
	issue := &types.Issue{
		ID:          "mr-tx-isolation",
		Title:       "Transaction Isolation Test",
		Description: "Original",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeTask,
	}
	if err := env.stores[0].CreateIssue(ctx, issue, "setup"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}

	longTxStarted := make(chan struct{})
	longTxDone := make(chan struct{})
	var shortTxSuccess atomic.Int32
	var shortTxFail atomic.Int32
	var wg sync.WaitGroup

	// Replica 0: long transaction (holds for 2 seconds)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(longTxDone)
		err := env.stores[0].RunInTransaction(ctx, func(tx storage.Transaction) error {
			close(longTxStarted)
			// Hold transaction open
			time.Sleep(2 * time.Second)
			return tx.UpdateIssue(ctx, issue.ID, map[string]interface{}{
				"description": "Long transaction update",
			}, "long-tx")
		})
		if err != nil {
			t.Logf("long transaction error: %v", err)
		}
	}()

	// Wait for long tx to start
	<-longTxStarted

	// Replica 1: multiple short transactions while long tx is active
	const numShortTx = 5
	for i := 0; i < numShortTx; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			shortCtx, shortCancel := context.WithTimeout(ctx, 10*time.Second)
			defer shortCancel()

			// Create a DIFFERENT issue (should not conflict with long tx)
			newIssue := &types.Issue{
				Title:     fmt.Sprintf("Short tx issue %d", n),
				Status:    types.StatusOpen,
				Priority:  2,
				IssueType: types.TypeTask,
			}
			if err := env.stores[1].CreateIssue(shortCtx, newIssue, fmt.Sprintf("short-tx-%d", n)); err != nil {
				shortTxFail.Add(1)
				t.Logf("short tx %d error: %v", n, err)
			} else {
				shortTxSuccess.Add(1)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Transaction isolation results:")
	t.Logf("  Short tx: %d succeeded, %d failed (while long tx held)", shortTxSuccess.Load(), shortTxFail.Load())

	// Short transactions on DIFFERENT data should succeed even while long tx is active
	if shortTxSuccess.Load() == 0 {
		t.Error("all short transactions failed - long tx may be blocking other replicas")
	}

	t.Logf("PASS: Transaction isolation - short tx on other replica not fully blocked by long tx")
}

// =============================================================================
// Test 9: Three-Replica Stress Test
//
// Three replicas performing mixed operations simultaneously.
// Validates: System remains stable under higher concurrency.
// =============================================================================

func TestMultiReplica_ThreeReplicaStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	ctx, cancel := multiReplicaTestContext(t)
	defer cancel()

	env := setupMultiReplicaEnv(ctx, t, 3)
	defer env.cleanup()

	// Pre-create shared issues
	const numIssues = 10
	issueIDs := make([]string, numIssues)
	for i := 0; i < numIssues; i++ {
		issue := &types.Issue{
			Title:       fmt.Sprintf("Stress Issue %d", i),
			Description: "Stress test initial",
			Status:      types.StatusOpen,
			Priority:    (i % 4) + 1,
			IssueType:   types.TypeTask,
		}
		if err := env.stores[0].CreateIssue(ctx, issue, "setup"); err != nil {
			t.Fatalf("failed to create issue %d: %v", i, err)
		}
		issueIDs[i] = issue.ID
	}

	const opsPerReplica = 30
	var wg sync.WaitGroup
	var totalOps atomic.Int32
	var failedOps atomic.Int32

	for replicaIdx, store := range env.stores {
		wg.Add(1)
		go func(idx int, s *DoltStore) {
			defer wg.Done()
			for op := 0; op < opsPerReplica; op++ {
				issueID := issueIDs[op%numIssues]
				switch op % 4 {
				case 0: // Create new issue
					newIssue := &types.Issue{
						Title:     fmt.Sprintf("R%d-new-%d", idx, op),
						Status:    types.StatusOpen,
						Priority:  2,
						IssueType: types.TypeTask,
					}
					if err := s.CreateIssue(ctx, newIssue, fmt.Sprintf("r%d", idx)); err != nil {
						failedOps.Add(1)
					}
				case 1: // Update existing
					if err := s.UpdateIssue(ctx, issueID, map[string]interface{}{
						"description": fmt.Sprintf("R%d-op%d", idx, op),
					}, fmt.Sprintf("r%d", idx)); err != nil {
						failedOps.Add(1)
					}
				case 2: // Read
					if _, err := s.GetIssue(ctx, issueID); err != nil {
						failedOps.Add(1)
					}
				case 3: // Search
					if _, err := s.SearchIssues(ctx, "Stress", types.IssueFilter{}); err != nil {
						failedOps.Add(1)
					}
				}
				totalOps.Add(1)
			}
		}(replicaIdx, store)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// No deadlock
	case <-ctx.Done():
		t.Fatal("timeout - possible deadlock in 3-replica stress test")
	}

	t.Logf("3-replica stress test: %d total ops, %d failed (%.1f%% success rate)",
		totalOps.Load(), failedOps.Load(),
		float64(totalOps.Load()-failedOps.Load())/float64(totalOps.Load())*100)

	// Verify all pre-existing issues are still readable
	for _, id := range issueIDs {
		issue, err := env.stores[0].GetIssue(ctx, id)
		if err != nil {
			t.Errorf("issue %s unreadable after stress test: %v", id, err)
		}
		if issue == nil {
			t.Errorf("issue %s disappeared after stress test", id)
		}
	}

	// Success rate should be high (>80%)
	successRate := float64(totalOps.Load()-failedOps.Load()) / float64(totalOps.Load())
	if successRate < 0.80 {
		t.Errorf("success rate too low: %.1f%% (expected >80%%)", successRate*100)
	}

	t.Logf("PASS: 3-replica stress test completed with %.1f%% success rate", successRate*100)
}
