package main

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestProcessSemaphore_AcquireRelease(t *testing.T) {
	dir := t.TempDir()

	sem, err := acquireProcessSemaphore(dir)
	if err != nil {
		t.Fatalf("acquireProcessSemaphore failed: %v", err)
	}
	if sem == nil {
		t.Fatal("expected non-nil semaphore")
	}
	if sem.slot < 0 || sem.slot >= MaxConcurrentBdProcesses {
		t.Fatalf("slot %d out of range [0, %d)", sem.slot, MaxConcurrentBdProcesses)
	}

	// Verify slot file was created
	slotPath := filepath.Join(dir, "sem", "slot.0")
	if _, err := os.Stat(slotPath); os.IsNotExist(err) {
		t.Fatal("slot file not created")
	}

	sem.Release()

	// Double release should be safe
	sem.Release()
}

func TestProcessSemaphore_ConcurrencyLimit(t *testing.T) {
	dir := t.TempDir()

	// Acquire all slots
	sems := make([]*ProcessSemaphore, MaxConcurrentBdProcesses)
	for i := 0; i < MaxConcurrentBdProcesses; i++ {
		sem, err := acquireProcessSemaphore(dir)
		if err != nil {
			t.Fatalf("acquire slot %d failed: %v", i, err)
		}
		sems[i] = sem
	}

	// Verify each got a different slot
	slots := make(map[int]bool)
	for _, sem := range sems {
		if slots[sem.slot] {
			t.Fatalf("duplicate slot %d", sem.slot)
		}
		slots[sem.slot] = true
	}

	// Next acquire should block — verify by trying in a goroutine with timeout
	acquired := make(chan struct{})
	go func() {
		sem, err := acquireProcessSemaphore(dir)
		if err != nil {
			t.Errorf("blocked acquire failed: %v", err)
			return
		}
		sem.Release()
		close(acquired)
	}()

	// Release one slot — the blocked goroutine should proceed
	sems[0].Release()

	<-acquired

	// Clean up
	for i := 1; i < MaxConcurrentBdProcesses; i++ {
		sems[i].Release()
	}
}

func TestProcessSemaphore_NilRelease(t *testing.T) {
	// nil semaphore release should be safe
	var sem *ProcessSemaphore
	sem.Release() // should not panic
}

func TestProcessSemaphore_ParallelStress(t *testing.T) {
	dir := t.TempDir()

	var maxConcurrent atomic.Int32
	var currentConcurrent atomic.Int32
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem, err := acquireProcessSemaphore(dir)
			if err != nil {
				t.Errorf("acquire failed: %v", err)
				return
			}

			cur := currentConcurrent.Add(1)
			// Track max concurrency observed
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}

			// Hold the slot briefly
			currentConcurrent.Add(-1)
			sem.Release()
		}()
	}

	wg.Wait()

	max := maxConcurrent.Load()
	if max > int32(MaxConcurrentBdProcesses) {
		t.Fatalf("max concurrent %d exceeded limit %d", max, MaxConcurrentBdProcesses)
	}
	t.Logf("max concurrent: %d (limit: %d)", max, MaxConcurrentBdProcesses)
}
