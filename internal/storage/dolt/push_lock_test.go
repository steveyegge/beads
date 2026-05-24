package dolt

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/lockfile"
)

func TestDoltPushLockSerializesSameCLIDir(t *testing.T) {
	tmp := t.TempDir()
	dbDir := filepath.Join(tmp, "mydb")
	if err := os.MkdirAll(filepath.Join(dbDir, ".dolt"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	s := &DoltStore{dbPath: tmp, database: "mydb"}
	first, err := s.acquireDoltPushLock(context.Background())
	if err != nil {
		t.Fatalf("first lock acquisition failed: %v", err)
	}
	defer func() {
		if first != nil {
			_ = lockfile.FlockUnlock(first)
			_ = first.Close()
		}
	}()

	t.Setenv("BEADS_DOLT_PUSH_LOCK_TIMEOUT", "10ms")
	second, err := s.acquireDoltPushLock(context.Background())
	if second != nil {
		_ = lockfile.FlockUnlock(second)
		_ = second.Close()
	}
	if !errors.Is(err, ErrDoltPushInProgress) {
		t.Fatalf("expected ErrDoltPushInProgress under contention, got %v", err)
	}

	_ = lockfile.FlockUnlock(first)
	_ = first.Close()
	first = nil

	third, err := s.acquireDoltPushLock(context.Background())
	if err != nil {
		t.Fatalf("lock should be acquirable after release: %v", err)
	}
	_ = lockfile.FlockUnlock(third)
	_ = third.Close()
}
