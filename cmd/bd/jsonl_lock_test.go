package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofrs/flock"
)

func TestJSONLLockTimeout_ImmediateWhenConfiguredZero(t *testing.T) {
	t.Parallel()

	beadsDir := t.TempDir()
	lockPath := filepath.Join(beadsDir, jsonlLockFileName)
	holder := flock.New(lockPath)
	locked, err := holder.TryLock()
	if err != nil {
		t.Fatalf("holder lock failed: %v", err)
	}
	if !locked {
		t.Fatal("expected holder lock to succeed")
	}
	defer func() { _ = holder.Unlock() }()

	orig := lockTimeout
	lockTimeout = 0
	t.Cleanup(func() { lockTimeout = orig })

	start := time.Now()
	err = newJSONLLock(beadsDir).AcquireExclusive(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected immediate timeout error")
	}
	if !strings.Contains(err.Error(), "after 0s") {
		t.Fatalf("expected 0s timeout message, got: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("expected fast failure, got elapsed=%v", elapsed)
	}
}

func TestJSONLLockTimeout_UsesConfiguredBound(t *testing.T) {
	t.Parallel()

	beadsDir := t.TempDir()
	lockPath := filepath.Join(beadsDir, jsonlLockFileName)
	holder := flock.New(lockPath)
	locked, err := holder.TryLock()
	if err != nil {
		t.Fatalf("holder lock failed: %v", err)
	}
	if !locked {
		t.Fatal("expected holder lock to succeed")
	}
	defer func() { _ = holder.Unlock() }()

	orig := lockTimeout
	lockTimeout = 120 * time.Millisecond
	t.Cleanup(func() { lockTimeout = orig })

	start := time.Now()
	err = newJSONLLock(beadsDir).AcquireExclusive(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error while lock is held")
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("expected bounded retry window near configured timeout, got elapsed=%v", elapsed)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("expected timeout near configured bound, got elapsed=%v", elapsed)
	}
}
