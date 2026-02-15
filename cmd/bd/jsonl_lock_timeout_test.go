package main

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestJSONLLockTimeout_ContentionStressDeterministic(t *testing.T) {
	t.Parallel()

	const workers = 8
	const holdTime = 25 * time.Millisecond

	beadsDir := t.TempDir()

	orig := lockTimeout
	lockTimeout = 2 * time.Second
	t.Cleanup(func() { lockTimeout = orig })

	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(workers)
	var wg sync.WaitGroup
	wg.Add(workers)

	errCh := make(chan error, workers)
	var acquiredCount atomic.Int32

	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			ready.Done()
			<-start

			lock := newJSONLLock(beadsDir)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			if err := lock.AcquireExclusive(ctx); err != nil {
				errCh <- err
				return
			}
			acquiredCount.Add(1)
			time.Sleep(holdTime)
			if err := lock.Release(); err != nil {
				errCh <- err
			}
		}()
	}

	ready.Wait()
	runStart := time.Now()
	close(start)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("contention test timed out (possible lock hang)")
	}

	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("unexpected contention error: %v", err)
		}
	}

	if got := acquiredCount.Load(); got != workers {
		t.Fatalf("acquired count = %d, want %d", got, workers)
	}

	elapsed := time.Since(runStart)
	if elapsed > 4*time.Second {
		t.Fatalf("contention runtime exceeded deterministic bound: %v", elapsed)
	}
}
