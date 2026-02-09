package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestDebouncer_BatchesMultipleTriggers(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Trigger()
	debouncer.Trigger()
	debouncer.Trigger()

	time.Sleep(20 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 0 {
		t.Errorf("action fired too early: got %d, want 0", got)
	}

	time.Sleep(35 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("action should have fired once: got %d, want 1", got)
	}
}

func TestDebouncer_ResetsTimerOnSubsequentTriggers(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Trigger()
	time.Sleep(20 * time.Millisecond)

	debouncer.Trigger()
	time.Sleep(20 * time.Millisecond)

	if got := atomic.LoadInt32(&count); got != 0 {
		t.Errorf("action fired too early after timer reset: got %d, want 0", got)
	}

	time.Sleep(35 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("action should have fired once after final timer: got %d, want 1", got)
	}
}

func TestDebouncer_CancelDuringWait(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Trigger()
	time.Sleep(10 * time.Millisecond)

	debouncer.Cancel()

	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 0 {
		t.Errorf("action should not have fired after cancel: got %d, want 0", got)
	}
}

func TestDebouncer_CancelWithNoPendingAction(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Cancel()

	debouncer.Trigger()
	// Use longer wait to account for Windows timer imprecision
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 1 {
		t.Errorf("action should fire normally after cancel with no pending action: got %d, want 1", got)
	}
}

func TestDebouncer_ThreadSafety(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			debouncer.Trigger()
		}()
	}

	close(start)
	wg.Wait()

	time.Sleep(70 * time.Millisecond)

	got := atomic.LoadInt32(&count)
	if got != 1 {
		t.Errorf("all concurrent triggers should batch to exactly 1 action: got %d, want 1", got)
	}
}

func TestDebouncer_ConcurrentCancelAndTrigger(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	var wg sync.WaitGroup
	numGoroutines := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			if index%2 == 0 {
				debouncer.Trigger()
			} else {
				debouncer.Cancel()
			}
		}(i)
	}

	wg.Wait()
	debouncer.Cancel()

	time.Sleep(100 * time.Millisecond)

	got := atomic.LoadInt32(&count)
	if got != 0 && got != 1 {
		t.Errorf("unexpected action count with concurrent cancel/trigger: got %d, want 0 or 1", got)
	}
}

func TestDebouncer_MultipleSequentialTriggerCycles(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(30*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	awaitCount := func(want int32) {
		deadline := time.Now().Add(500 * time.Millisecond)
		for time.Now().Before(deadline) {
			if got := atomic.LoadInt32(&count); got >= want {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		got := atomic.LoadInt32(&count)
		t.Fatalf("timeout waiting for count=%d (got %d)", want, got)
	}

	debouncer.Trigger()
	awaitCount(1)

	debouncer.Trigger()
	awaitCount(2)

	debouncer.Trigger()
	awaitCount(3)
}

func TestDebouncer_CancelAndWaitDrainsInFlight(t *testing.T) {
	started := make(chan struct{})
	done := make(chan struct{})
	debouncer := NewDebouncer(10*time.Millisecond, func() {
		close(started)
		// Simulate a long-running sync operation
		time.Sleep(200 * time.Millisecond)
		close(done)
	})

	debouncer.Trigger()

	// Wait for the action to start executing
	select {
	case <-started:
	case <-time.After(1 * time.Second):
		t.Fatal("action did not start in time")
	}

	// CancelAndWait should block until the action finishes
	debouncer.CancelAndWait()

	// Verify the action completed
	select {
	case <-done:
		// Success - action completed before CancelAndWait returned
	default:
		t.Error("CancelAndWait returned before in-flight action completed")
	}
}

func TestDebouncer_CancelAndWaitNoPending(t *testing.T) {
	debouncer := NewDebouncer(50*time.Millisecond, func() {})

	// CancelAndWait with nothing pending should return immediately
	waitDone := make(chan struct{})
	go func() {
		debouncer.CancelAndWait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-time.After(1 * time.Second):
		t.Fatal("CancelAndWait blocked with nothing pending")
	}
}

func TestDebouncer_CancelAndWaitWithPendingTimer(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(5*time.Second, func() {
		// This should never fire - timer should be stopped
		atomic.AddInt32(&count, 1)
	})

	debouncer.Trigger()

	// CancelAndWait should stop the timer and return immediately
	waitDone := make(chan struct{})
	go func() {
		debouncer.CancelAndWait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
	case <-time.After(1 * time.Second):
		t.Fatal("CancelAndWait blocked with only a pending (not yet fired) timer")
	}

	if got := atomic.LoadInt32(&count); got != 0 {
		t.Errorf("action should not have fired after CancelAndWait: got %d, want 0", got)
	}
}

func TestDebouncer_CancelImmediatelyAfterTrigger(t *testing.T) {
	var count int32
	debouncer := NewDebouncer(50*time.Millisecond, func() {
		atomic.AddInt32(&count, 1)
	})
	t.Cleanup(debouncer.Cancel)

	debouncer.Trigger()
	debouncer.Cancel()

	time.Sleep(60 * time.Millisecond)
	if got := atomic.LoadInt32(&count); got != 0 {
		t.Errorf("action should not fire after immediate cancel: got %d, want 0", got)
	}
}
