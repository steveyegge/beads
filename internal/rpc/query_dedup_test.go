package rpc

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueryDeduplicator_CoalescesIdenticalQueries(t *testing.T) {
	dedup := NewQueryDeduplicator(500 * time.Millisecond)

	var executionCount atomic.Int32
	args := json.RawMessage(`{"query":"test"}`)

	var wg sync.WaitGroup
	results := make([]Response, 10)

	// Launch 10 concurrent identical queries
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			resp, _ := dedup.Execute(OpList, args, func() Response {
				executionCount.Add(1)
				time.Sleep(50 * time.Millisecond) // Simulate query execution
				return Response{Success: true, Data: json.RawMessage(`{"count":42}`)}
			})
			results[idx] = resp
		}(i)
	}

	wg.Wait()

	// Should have only executed once
	if count := executionCount.Load(); count != 1 {
		t.Errorf("Expected 1 execution, got %d", count)
	}

	// All results should be successful and identical
	for i, resp := range results {
		if !resp.Success {
			t.Errorf("Result %d: expected success", i)
		}
	}
}

func TestQueryDeduplicator_DifferentQueriesExecuteSeparately(t *testing.T) {
	dedup := NewQueryDeduplicator(500 * time.Millisecond)

	var executionCount atomic.Int32

	var wg sync.WaitGroup

	// Launch queries with different args
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			args := json.RawMessage(`{"idx":` + string(rune('0'+idx)) + `}`)
			dedup.Execute(OpList, args, func() Response {
				executionCount.Add(1)
				time.Sleep(10 * time.Millisecond)
				return Response{Success: true}
			})
		}(i)
	}

	wg.Wait()

	// Each unique query should execute separately
	if count := executionCount.Load(); count != 5 {
		t.Errorf("Expected 5 executions for different queries, got %d", count)
	}
}

func TestQueryDeduplicator_NonDedupableOpsExecuteDirectly(t *testing.T) {
	dedup := NewQueryDeduplicator(500 * time.Millisecond)

	var executionCount atomic.Int32
	args := json.RawMessage(`{"title":"test"}`)

	var wg sync.WaitGroup

	// Launch concurrent create operations (not dedupable)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dedup.Execute(OpCreate, args, func() Response {
				executionCount.Add(1)
				time.Sleep(10 * time.Millisecond)
				return Response{Success: true}
			})
		}()
	}

	wg.Wait()

	// All create operations should execute (no dedup for writes)
	if count := executionCount.Load(); count != 5 {
		t.Errorf("Expected 5 executions for non-dedupable ops, got %d", count)
	}
}

func TestQueryDeduplicator_TimeoutFallsBackToDirectExecution(t *testing.T) {
	// Very short timeout
	dedup := NewQueryDeduplicator(10 * time.Millisecond)

	var executionCount atomic.Int32
	args := json.RawMessage(`{"query":"slow"}`)

	var wg sync.WaitGroup

	// First query takes a long time
	wg.Add(1)
	go func() {
		defer wg.Done()
		dedup.Execute(OpList, args, func() Response {
			executionCount.Add(1)
			time.Sleep(100 * time.Millisecond) // Longer than timeout
			return Response{Success: true}
		})
	}()

	// Wait a bit for first query to start
	time.Sleep(5 * time.Millisecond)

	// Second query should timeout waiting and execute directly
	wg.Add(1)
	go func() {
		defer wg.Done()
		dedup.Execute(OpList, args, func() Response {
			executionCount.Add(1)
			return Response{Success: true}
		})
	}()

	wg.Wait()

	// Both should have executed due to timeout
	if count := executionCount.Load(); count != 2 {
		t.Errorf("Expected 2 executions (timeout fallback), got %d", count)
	}
}

func TestQueryDeduplicator_Stats(t *testing.T) {
	dedup := NewQueryDeduplicator(500 * time.Millisecond)

	// Initially no inflight queries
	stats := dedup.Stats()
	if stats.InflightQueries != 0 {
		t.Errorf("Expected 0 inflight queries, got %d", stats.InflightQueries)
	}

	// Start a slow query
	done := make(chan struct{})
	go func() {
		dedup.Execute(OpList, json.RawMessage(`{}`), func() Response {
			<-done // Wait for signal
			return Response{Success: true}
		})
	}()

	// Wait for query to be registered
	time.Sleep(10 * time.Millisecond)

	stats = dedup.Stats()
	if stats.InflightQueries != 1 {
		t.Errorf("Expected 1 inflight query, got %d", stats.InflightQueries)
	}

	// Release the query
	close(done)
	time.Sleep(20 * time.Millisecond)

	stats = dedup.Stats()
	if stats.InflightQueries != 0 {
		t.Errorf("Expected 0 inflight queries after completion, got %d", stats.InflightQueries)
	}
}

func TestQueryDeduplicator_ReturnsWasDeduped(t *testing.T) {
	dedup := NewQueryDeduplicator(500 * time.Millisecond)

	args := json.RawMessage(`{"test":true}`)
	barrier := make(chan struct{})
	firstStarted := make(chan struct{})

	var wg sync.WaitGroup
	var firstDeduped, secondDeduped bool

	// First query
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, wasDeduped := dedup.Execute(OpList, args, func() Response {
			close(firstStarted) // Signal that first query started
			<-barrier           // Wait for second query to register
			return Response{Success: true}
		})
		firstDeduped = wasDeduped
	}()

	// Wait for first to start
	<-firstStarted
	time.Sleep(5 * time.Millisecond)

	// Second query — starts while first query is blocked, so it gets deduped.
	// We use a generous sleep to ensure the goroutine enters Execute before
	// we unblock the first query.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, wasDeduped := dedup.Execute(OpList, args, func() Response {
			return Response{Success: true}
		})
		secondDeduped = wasDeduped
	}()
	// Give goroutine time to reach Execute and register as a waiter
	time.Sleep(50 * time.Millisecond)
	close(barrier) // Now allow first query to complete

	wg.Wait()

	if firstDeduped {
		t.Error("First query should not be deduped")
	}
	if !secondDeduped {
		t.Error("Second query should be deduped")
	}
}

func TestDedupableOps_ContainsExpectedOps(t *testing.T) {
	// Verify expected read operations are dedupable
	expectedDedupable := []string{
		OpList, OpCount, OpReady, OpBlocked, OpShow,
		OpStats, OpStale, OpHealth, OpStatus, OpMetrics,
	}

	for _, op := range expectedDedupable {
		if !DedupableOps[op] {
			t.Errorf("Expected %s to be dedupable", op)
		}
	}

	// Verify write operations are NOT dedupable
	notDedupable := []string{
		OpCreate, OpUpdate, OpClose, OpDelete,
		OpDepAdd, OpDepRemove, OpLabelAdd, OpLabelRemove,
		OpImport, OpExport, OpShutdown,
	}

	for _, op := range notDedupable {
		if DedupableOps[op] {
			t.Errorf("Expected %s to NOT be dedupable", op)
		}
	}
}
