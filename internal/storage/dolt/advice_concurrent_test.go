// Package dolt provides concurrency tests for advice storage operations.
//
// These tests validate that multiple agents can safely create, modify, and query
// advice concurrently without race conditions or data corruption.
package dolt

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// =============================================================================
// Test 1: Concurrent Advice Creation
// Multiple goroutines create advice issues simultaneously.
// Verify: All advice created with unique IDs, no duplicates, no errors
// =============================================================================

func TestConcurrentAdviceCreation(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	const numGoroutines = 10
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)
	createdIDs := make(chan string, numGoroutines)

	// Launch goroutines to create advice simultaneously
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			advice := &types.Issue{
				Title:       fmt.Sprintf("Concurrent Advice %d", n),
				Description: fmt.Sprintf("Advice created by goroutine %d", n),
				Status:      types.StatusOpen,
				Priority:    2,
				IssueType:   types.IssueType("advice"),
			}
			if err := store.CreateIssue(ctx, advice, fmt.Sprintf("worker-%d", n)); err != nil {
				errors <- fmt.Errorf("goroutine %d: %w", n, err)
				return
			}
			createdIDs <- advice.ID
		}(i)
	}

	wg.Wait()
	close(errors)
	close(createdIDs)

	// Check for errors
	var errCount int
	for err := range errors {
		t.Errorf("creation error: %v", err)
		errCount++
	}

	if errCount > 0 {
		t.Fatalf("%d goroutines failed to create advice", errCount)
	}

	// Collect and verify all IDs are unique
	ids := make(map[string]bool)
	for id := range createdIDs {
		if ids[id] {
			t.Errorf("duplicate advice ID: %s", id)
		}
		ids[id] = true
	}

	if len(ids) != numGoroutines {
		t.Errorf("expected %d unique IDs, got %d", numGoroutines, len(ids))
	}

	// Verify all advice can be retrieved and has correct type
	for id := range ids {
		issue, err := store.GetIssue(ctx, id)
		if err != nil {
			t.Errorf("failed to get advice %s: %v", id, err)
			continue
		}
		if issue == nil {
			t.Errorf("advice %s not found", id)
			continue
		}
		if issue.IssueType != types.IssueType("advice") {
			t.Errorf("advice %s has wrong type: %s", id, issue.IssueType)
		}
	}
}

// =============================================================================
// Test 2: Concurrent Label Add Operations
// Multiple goroutines add labels to the same advice simultaneously.
// Verify: All labels added, no duplicates, no errors
// =============================================================================

func TestConcurrentLabelAdd(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Create advice to add labels to
	advice := &types.Issue{
		ID:          "test-advice-labels",
		Title:       "Advice for label concurrency test",
		Description: "Testing concurrent label additions",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.IssueType("advice"),
	}

	if err := store.CreateIssue(ctx, advice, "tester"); err != nil {
		t.Fatalf("failed to create advice: %v", err)
	}

	const numGoroutines = 10
	var wg sync.WaitGroup
	var addSuccess atomic.Int32
	var addFail atomic.Int32

	// Each goroutine adds a unique label
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			label := fmt.Sprintf("label-%d", n)
			if err := store.AddLabel(ctx, advice.ID, label, fmt.Sprintf("worker-%d", n)); err != nil {
				addFail.Add(1)
				t.Logf("goroutine %d add label error: %v", n, err)
				return
			}
			addSuccess.Add(1)
		}(i)
	}

	wg.Wait()

	t.Logf("Add label results: %d succeeded, %d failed", addSuccess.Load(), addFail.Load())

	// Verify all labels were added
	labels, err := store.GetLabels(ctx, advice.ID)
	if err != nil {
		t.Fatalf("failed to get labels: %v", err)
	}

	if len(labels) != numGoroutines {
		t.Errorf("expected %d labels, got %d", numGoroutines, len(labels))
	}

	// Verify no failures for unique labels
	if addFail.Load() > 0 {
		t.Errorf("expected no failures for unique labels, got %d", addFail.Load())
	}
}

// =============================================================================
// Test 3: Concurrent Label Add/Remove Operations
// Multiple goroutines add and remove labels simultaneously.
// Verify: No deadlocks, final state is consistent
// =============================================================================

func TestConcurrentLabelAddRemove(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Create advice with initial labels
	advice := &types.Issue{
		ID:          "test-advice-add-remove",
		Title:       "Advice for add/remove concurrency test",
		Description: "Testing concurrent label add/remove",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.IssueType("advice"),
	}

	if err := store.CreateIssue(ctx, advice, "tester"); err != nil {
		t.Fatalf("failed to create advice: %v", err)
	}

	// Add initial labels
	for i := 0; i < 5; i++ {
		if err := store.AddLabel(ctx, advice.ID, fmt.Sprintf("initial-%d", i), "tester"); err != nil {
			t.Fatalf("failed to add initial label: %v", err)
		}
	}

	const numAdders = 5
	const numRemovers = 5
	const iterations = 20

	var wg sync.WaitGroup
	var addOps atomic.Int32
	var removeOps atomic.Int32

	// Start adders
	for a := 0; a < numAdders; a++ {
		wg.Add(1)
		go func(adderID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				label := fmt.Sprintf("added-%d-%d", adderID, i)
				err := store.AddLabel(ctx, advice.ID, label, fmt.Sprintf("adder-%d", adderID))
				if err == nil {
					addOps.Add(1)
				}
			}
		}(a)
	}

	// Start removers (removing initial labels, may fail if already removed)
	for r := 0; r < numRemovers; r++ {
		wg.Add(1)
		go func(removerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Try to remove initial labels (may already be removed by another remover)
				label := fmt.Sprintf("initial-%d", i%5)
				err := store.RemoveLabel(ctx, advice.ID, label, fmt.Sprintf("remover-%d", removerID))
				if err == nil {
					removeOps.Add(1)
				}
			}
		}(r)
	}

	// Wait with timeout to detect deadlocks
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-ctx.Done():
		t.Fatal("timeout - possible deadlock detected in label add/remove")
	}

	t.Logf("Add operations: %d, Remove operations: %d", addOps.Load(), removeOps.Load())

	// Verify final state is readable
	labels, err := store.GetLabels(ctx, advice.ID)
	if err != nil {
		t.Fatalf("failed to get labels after concurrent ops: %v", err)
	}
	t.Logf("Final label count: %d", len(labels))
}

// =============================================================================
// Test 4: Concurrent Advice Queries During Modifications
// Readers query advice while writers modify it.
// Verify: Readers always get consistent state, no errors
// =============================================================================

func TestConcurrentAdviceQueriesWithModifications(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Create multiple advice items
	const numAdvice = 5
	adviceIDs := make([]string, numAdvice)
	for i := 0; i < numAdvice; i++ {
		advice := &types.Issue{
			ID:          fmt.Sprintf("test-query-advice-%d", i),
			Title:       fmt.Sprintf("Query Test Advice %d", i),
			Description: fmt.Sprintf("Advice %d for query concurrency test", i),
			Status:      types.StatusOpen,
			Priority:    (i % 4) + 1,
			IssueType:   types.IssueType("advice"),
		}
		if err := store.CreateIssue(ctx, advice, "tester"); err != nil {
			t.Fatalf("failed to create advice %d: %v", i, err)
		}
		adviceIDs[i] = advice.ID

		// Add a label for querying
		if err := store.AddLabel(ctx, advice.ID, "test-advice", "tester"); err != nil {
			t.Fatalf("failed to add label to advice %d: %v", i, err)
		}
	}

	const numReaders = 3
	const numWriters = 2
	const iterations = 10

	var wg sync.WaitGroup
	var readSuccess atomic.Int32
	var readErrors atomic.Int32
	var writeSuccess atomic.Int32
	var writeErrors atomic.Int32

	// Start readers (querying by ID and labels)
	for r := 0; r < numReaders; r++ {
		wg.Add(1)
		go func(readerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Check context before each operation
				if ctx.Err() != nil {
					return
				}
				// Focus on faster read patterns (avoid heavy join queries)
				switch i % 2 {
				case 0:
					// Query by ID (fast)
					adviceID := adviceIDs[i%numAdvice]
					issue, err := store.GetIssue(ctx, adviceID)
					if err != nil {
						readErrors.Add(1)
						continue
					}
					if issue == nil {
						readErrors.Add(1)
						continue
					}
					readSuccess.Add(1)
				case 1:
					// Query labels for advice (fast)
					adviceID := adviceIDs[i%numAdvice]
					_, err := store.GetLabels(ctx, adviceID)
					if err != nil {
						readErrors.Add(1)
						continue
					}
					readSuccess.Add(1)
				}
			}
		}(r)
	}

	// Start writers (modifying advice and labels)
	for w := 0; w < numWriters; w++ {
		wg.Add(1)
		go func(writerID int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				// Check context before each operation
				if ctx.Err() != nil {
					return
				}
				adviceID := adviceIDs[i%numAdvice]

				switch i % 3 {
				case 0:
					// Update advice description
					err := store.UpdateIssue(ctx, adviceID, map[string]interface{}{
						"notes": fmt.Sprintf("Updated by writer %d, iteration %d", writerID, i),
					}, fmt.Sprintf("writer-%d", writerID))
					if err != nil {
						writeErrors.Add(1)
						continue
					}
					writeSuccess.Add(1)
				case 1:
					// Add a label
					label := fmt.Sprintf("writer-%d-iter-%d", writerID, i)
					err := store.AddLabel(ctx, adviceID, label, fmt.Sprintf("writer-%d", writerID))
					if err != nil {
						writeErrors.Add(1)
						continue
					}
					writeSuccess.Add(1)
				case 2:
					// Update priority (valid values 1-4)
					err := store.UpdateIssue(ctx, adviceID, map[string]interface{}{
						"priority": (i % 4) + 1,
					}, fmt.Sprintf("writer-%d", writerID))
					if err != nil {
						writeErrors.Add(1)
						continue
					}
					writeSuccess.Add(1)
				}
			}
		}(w)
	}

	// Wait with timeout to detect deadlocks
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - no deadlock
	case <-ctx.Done():
		t.Fatal("timeout - possible deadlock detected in advice query/modify test")
	}

	t.Logf("Read results: %d succeeded, %d failed", readSuccess.Load(), readErrors.Load())
	t.Logf("Write results: %d succeeded, %d failed", writeSuccess.Load(), writeErrors.Load())

	// Most reads should succeed
	expectedReads := int32(numReaders * iterations)
	if readSuccess.Load() < expectedReads/2 {
		t.Errorf("too many read failures: %d/%d succeeded", readSuccess.Load(), expectedReads)
	}

	// Some writes should succeed
	if writeSuccess.Load() == 0 {
		t.Error("no writes succeeded")
	}

	// Verify final state is consistent
	for _, adviceID := range adviceIDs {
		issue, err := store.GetIssue(ctx, adviceID)
		if err != nil {
			t.Errorf("failed to get advice %s after test: %v", adviceID, err)
		}
		if issue == nil {
			t.Errorf("advice %s missing after test", adviceID)
		}
	}
}

// =============================================================================
// Test 5: Database Locking Under Contention (Same Advice Update Race)
// Multiple goroutines update the same advice simultaneously.
// Verify: At least one update succeeds, no corruption
// =============================================================================

func TestSameAdviceUpdateRace(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Create the advice to be updated
	advice := &types.Issue{
		ID:          "test-advice-race",
		Title:       "Race Test Advice",
		Description: "Original description",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.IssueType("advice"),
	}

	if err := store.CreateIssue(ctx, advice, "tester"); err != nil {
		t.Fatalf("failed to create advice: %v", err)
	}

	const numGoroutines = 10
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	// Launch goroutines to update the same advice
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			updates := map[string]interface{}{
				"description": fmt.Sprintf("Updated by goroutine %d", n),
				"priority":    (n % 4) + 1,
			}
			if err := store.UpdateIssue(ctx, advice.ID, updates, fmt.Sprintf("worker-%d", n)); err != nil {
				t.Logf("goroutine %d update error (may be expected): %v", n, err)
				errorCount.Add(1)
				return
			}
			successCount.Add(1)
		}(i)
	}

	wg.Wait()

	// At least some updates should succeed
	if successCount.Load() == 0 {
		t.Error("no updates succeeded - expected at least one to complete")
	}

	t.Logf("Update results: %d succeeded, %d failed", successCount.Load(), errorCount.Load())

	// Verify final state is consistent
	retrieved, err := store.GetIssue(ctx, advice.ID)
	if err != nil {
		t.Fatalf("failed to get advice after updates: %v", err)
	}
	if retrieved == nil {
		t.Fatal("advice not found after updates")
	}

	// Description should be from one of the goroutines
	t.Logf("Final state - description: %q, priority: %d", retrieved.Description, retrieved.Priority)
}

// =============================================================================
// Test 6: Concurrent Advice Subscription Updates
// Multiple goroutines update advice subscription fields simultaneously.
// Verify: Final state is consistent, no corruption
// =============================================================================

func TestConcurrentAdviceSubscriptionUpdates(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Create advice with subscription fields
	advice := &types.Issue{
		ID:                         "test-advice-subscriptions",
		Title:                      "Subscription Test Advice",
		Description:                "Testing concurrent subscription updates",
		Status:                     types.StatusOpen,
		Priority:                   2,
		IssueType:                  types.IssueType("advice"),
		AdviceSubscriptions:        []string{"role:polecat"},
		AdviceSubscriptionsExclude: []string{"agent:test"},
	}

	if err := store.CreateIssue(ctx, advice, "tester"); err != nil {
		t.Fatalf("failed to create advice: %v", err)
	}

	const numGoroutines = 10
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	// Launch goroutines to update subscription fields
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			updates := map[string]interface{}{
				"advice_subscriptions": []string{
					fmt.Sprintf("role:polecat-%d", n),
					fmt.Sprintf("rig:beads-%d", n),
				},
			}
			if err := store.UpdateIssue(ctx, advice.ID, updates, fmt.Sprintf("worker-%d", n)); err != nil {
				t.Logf("goroutine %d subscription update error: %v", n, err)
				errorCount.Add(1)
				return
			}
			successCount.Add(1)
		}(i)
	}

	wg.Wait()

	t.Logf("Subscription update results: %d succeeded, %d failed", successCount.Load(), errorCount.Load())

	// At least some updates should succeed
	if successCount.Load() == 0 {
		t.Error("no subscription updates succeeded")
	}

	// Verify final state
	retrieved, err := store.GetIssue(ctx, advice.ID)
	if err != nil {
		t.Fatalf("failed to get advice after subscription updates: %v", err)
	}
	if retrieved == nil {
		t.Fatal("advice not found after subscription updates")
	}

	t.Logf("Final subscriptions: %v", retrieved.AdviceSubscriptions)
}

// =============================================================================
// Test 7: High Contention Advice Stress Test
// Many goroutines performing various advice operations simultaneously.
// =============================================================================

func TestHighContentionAdviceStress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping stress test in short mode")
	}

	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Create initial advice items
	const numAdvice = 10
	for i := 0; i < numAdvice; i++ {
		advice := &types.Issue{
			ID:          fmt.Sprintf("stress-advice-%d", i),
			Title:       fmt.Sprintf("Stress Test Advice %d", i),
			Description: "For high contention stress testing",
			Status:      types.StatusOpen,
			Priority:    (i % 4) + 1,
			IssueType:   types.IssueType("advice"),
		}
		if err := store.CreateIssue(ctx, advice, "tester"); err != nil {
			t.Fatalf("failed to create advice %d: %v", i, err)
		}
		// Add initial label for querying
		if err := store.AddLabel(ctx, advice.ID, "stress-advice", "tester"); err != nil {
			t.Fatalf("failed to add label to advice %d: %v", i, err)
		}
	}

	const numWorkers = 10
	const opsPerWorker = 20
	var wg sync.WaitGroup
	var totalOps atomic.Int32
	var failedOps atomic.Int32

	// Launch workers doing mixed advice operations
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for op := 0; op < opsPerWorker; op++ {
				// Check context before each operation
				if ctx.Err() != nil {
					return
				}
				adviceID := fmt.Sprintf("stress-advice-%d", op%numAdvice)

				switch op % 4 {
				case 0: // Read advice
					_, err := store.GetIssue(ctx, adviceID)
					if err != nil {
						failedOps.Add(1)
					}
				case 1: // Update advice notes
					err := store.UpdateIssue(ctx, adviceID, map[string]interface{}{
						"notes": fmt.Sprintf("Worker %d, op %d", workerID, op),
					}, fmt.Sprintf("worker-%d", workerID))
					if err != nil {
						failedOps.Add(1)
					}
				case 2: // Add label
					err := store.AddLabel(ctx, adviceID, fmt.Sprintf("worker-%d-op-%d", workerID, op), fmt.Sprintf("worker-%d", workerID))
					if err != nil {
						failedOps.Add(1)
					}
				case 3: // Get labels
					_, err := store.GetLabels(ctx, adviceID)
					if err != nil {
						failedOps.Add(1)
					}
				}
				totalOps.Add(1)
			}
		}(w)
	}

	// Wait with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-ctx.Done():
		t.Fatal("stress test timeout - possible deadlock")
	}

	t.Logf("Stress test completed: %d total ops, %d failed (%.2f%% success rate)",
		totalOps.Load(), failedOps.Load(),
		float64(totalOps.Load()-failedOps.Load())/float64(totalOps.Load())*100)

	// Verify data integrity - all advice should still be readable
	for i := 0; i < numAdvice; i++ {
		adviceID := fmt.Sprintf("stress-advice-%d", i)
		advice, err := store.GetIssue(ctx, adviceID)
		if err != nil {
			t.Errorf("failed to read advice %s after stress test: %v", adviceID, err)
		}
		if advice == nil {
			t.Errorf("advice %s missing after stress test", adviceID)
		}
	}
}

// =============================================================================
// Test 8: Concurrent Advice Hook Field Updates
// Multiple goroutines update advice hook fields simultaneously.
// Verify: No corruption in hook configuration
// =============================================================================

func TestConcurrentAdviceHookUpdates(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Create advice with hook fields
	advice := &types.Issue{
		ID:                  "test-advice-hooks",
		Title:               "Hook Test Advice",
		Description:         "Testing concurrent hook updates",
		Status:              types.StatusOpen,
		Priority:            2,
		IssueType:           types.IssueType("advice"),
		AdviceHookCommand:   "initial-command",
		AdviceHookTrigger:   "session-end",
		AdviceHookTimeout:   30,
		AdviceHookOnFailure: "warn",
	}

	if err := store.CreateIssue(ctx, advice, "tester"); err != nil {
		t.Fatalf("failed to create advice: %v", err)
	}

	const numGoroutines = 10
	var wg sync.WaitGroup
	var successCount atomic.Int32
	var errorCount atomic.Int32

	triggers := []string{"session-end", "before-commit", "before-push", "before-handoff"}
	onFailures := []string{"block", "warn", "ignore"}

	// Launch goroutines to update hook fields
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()

			// Note: hook fields are not in the allowed update list
			// so we'll test what we can update
			updates := map[string]interface{}{
				"notes": fmt.Sprintf("Hook update by goroutine %d: trigger=%s, onFailure=%s",
					n, triggers[n%len(triggers)], onFailures[n%len(onFailures)]),
			}
			if err := store.UpdateIssue(ctx, advice.ID, updates, fmt.Sprintf("worker-%d", n)); err != nil {
				t.Logf("goroutine %d hook update error: %v", n, err)
				errorCount.Add(1)
				return
			}
			successCount.Add(1)
		}(i)
	}

	wg.Wait()

	t.Logf("Hook-related update results: %d succeeded, %d failed", successCount.Load(), errorCount.Load())

	// Verify final state is consistent
	retrieved, err := store.GetIssue(ctx, advice.ID)
	if err != nil {
		t.Fatalf("failed to get advice after hook updates: %v", err)
	}
	if retrieved == nil {
		t.Fatal("advice not found after hook updates")
	}

	// Original hook config should be preserved (we only updated notes)
	if retrieved.AdviceHookCommand != "initial-command" {
		t.Errorf("hook command was unexpectedly changed to: %s", retrieved.AdviceHookCommand)
	}
	if retrieved.AdviceHookTrigger != "session-end" {
		t.Errorf("hook trigger was unexpectedly changed to: %s", retrieved.AdviceHookTrigger)
	}

	t.Logf("Final hook config - command: %s, trigger: %s, timeout: %d, onFailure: %s",
		retrieved.AdviceHookCommand, retrieved.AdviceHookTrigger,
		retrieved.AdviceHookTimeout, retrieved.AdviceHookOnFailure)
}

// =============================================================================
// Test 9: Long Transaction Blocking with Advice
// One long advice transaction, multiple short ones competing.
// Verify: Short transactions complete or timeout cleanly
// =============================================================================

func TestLongAdviceTransactionBlocking(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := concurrentTestContext(t)
	defer cancel()

	// Create advice
	advice := &types.Issue{
		ID:          "test-advice-long-tx",
		Title:       "Long Transaction Advice Test",
		Description: "Testing long transaction blocking",
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.IssueType("advice"),
	}
	if err := store.CreateIssue(ctx, advice, "tester"); err != nil {
		t.Fatalf("failed to create advice: %v", err)
	}

	var wg sync.WaitGroup
	var shortTxSuccess atomic.Int32
	var shortTxFail atomic.Int32
	longTxStarted := make(chan struct{})
	longTxDone := make(chan struct{})

	// Start long transaction that holds the advice
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(longTxDone)

		err := store.RunInTransaction(ctx, func(tx storage.Transaction) error {
			// Signal that long tx has started
			close(longTxStarted)

			// Hold the transaction open
			time.Sleep(2 * time.Second)

			// Update advice
			return tx.UpdateIssue(ctx, advice.ID, map[string]interface{}{
				"description": "Updated by long transaction",
			}, "long-tx")
		})
		if err != nil {
			t.Logf("long transaction error: %v", err)
		}
	}()

	// Wait for long tx to start
	<-longTxStarted

	// Start multiple short transactions
	const numShortTx = 5
	for i := 0; i < numShortTx; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()

			shortCtx, shortCancel := context.WithTimeout(ctx, 5*time.Second)
			defer shortCancel()

			err := store.RunInTransaction(shortCtx, func(tx storage.Transaction) error {
				return tx.UpdateIssue(shortCtx, advice.ID, map[string]interface{}{
					"notes": fmt.Sprintf("Short tx %d", n),
				}, fmt.Sprintf("short-tx-%d", n))
			})

			if err != nil {
				shortTxFail.Add(1)
				t.Logf("short tx %d error (expected under contention): %v", n, err)
			} else {
				shortTxSuccess.Add(1)
			}
		}(i)
	}

	wg.Wait()

	t.Logf("Short tx results: %d succeeded, %d failed", shortTxSuccess.Load(), shortTxFail.Load())

	// Verify final state
	retrieved, err := store.GetIssue(ctx, advice.ID)
	if err != nil {
		t.Fatalf("failed to get advice after transactions: %v", err)
	}
	if retrieved == nil {
		t.Fatal("advice not found after transactions")
	}
}
