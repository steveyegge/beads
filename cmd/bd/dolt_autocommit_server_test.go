//go:build cgo

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

type fakeCommitPendingStore struct {
	storage.DoltStorage
	calls int
	actor string
	err   error
}

func (f *fakeCommitPendingStore) CommitPending(_ context.Context, actor string) (bool, error) {
	f.calls++
	f.actor = actor
	return true, f.err
}

func saveStorageMode(t *testing.T) {
	t.Helper()
	oldServerMode := serverMode
	oldProxiedServerMode := proxiedServerMode
	oldCmdCtx := cmdCtx
	oldUseGlobals := testModeUseGlobals
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "0")
	testModeUseGlobals = true
	cmdCtx = nil
	t.Cleanup(func() {
		serverMode = oldServerMode
		proxiedServerMode = oldProxiedServerMode
		cmdCtx = oldCmdCtx
		testModeUseGlobals = oldUseGlobals
	})
}

func TestCommitPendingIfEmbeddedSkipsServerMode(t *testing.T) {
	saveStorageMode(t)
	serverMode = true

	fake := &fakeCommitPendingStore{}
	if err := commitPendingIfEmbedded(context.Background(), fake, "tester"); err != nil {
		t.Fatalf("commitPendingIfEmbedded: %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("CommitPending calls = %d, want 0 in server mode", fake.calls)
	}
}

func TestCommitPendingIfEmbeddedFlushesEmbeddedMode(t *testing.T) {
	saveStorageMode(t)
	serverMode = false
	proxiedServerMode = false

	fake := &fakeCommitPendingStore{}
	if err := commitPendingIfEmbedded(context.Background(), fake, "tester"); err != nil {
		t.Fatalf("commitPendingIfEmbedded: %v", err)
	}
	if fake.calls != 1 {
		t.Fatalf("CommitPending calls = %d, want 1 in embedded mode", fake.calls)
	}
	if fake.actor != "tester" {
		t.Fatalf("actor = %q, want tester", fake.actor)
	}
}

func TestCommitPendingIfEmbeddedPropagatesEmbeddedError(t *testing.T) {
	saveStorageMode(t)
	serverMode = false

	want := errors.New("commit failed")
	fake := &fakeCommitPendingStore{err: want}
	if err := commitPendingIfEmbedded(context.Background(), fake, "tester"); !errors.Is(err, want) {
		t.Fatalf("commitPendingIfEmbedded error = %v, want %v", err, want)
	}
}

func TestShouldCommitCreatePostWritesSkipsNoHistoryWispsInServerMode(t *testing.T) {
	saveStorageMode(t)
	serverMode = true

	if shouldCommitCreatePostWrites(&types.Issue{NoHistory: true}, true) {
		t.Fatal("no-history create post-writes should not issue a Dolt commit in server mode")
	}
	if shouldCommitCreatePostWrites(&types.Issue{Ephemeral: true}, true) {
		t.Fatal("ephemeral create post-writes should not issue a Dolt commit in server mode")
	}
	if !shouldCommitCreatePostWrites(&types.Issue{}, true) {
		t.Fatal("persistent create post-writes should issue a Dolt commit in server mode")
	}
	if shouldCommitCreatePostWrites(&types.Issue{}, false) {
		t.Fatal("create without post-writes should not issue an extra Dolt commit in server mode")
	}
}

func TestShouldCommitCreatePostWritesPreservesEmbeddedFlush(t *testing.T) {
	saveStorageMode(t)
	serverMode = false

	if !shouldCommitCreatePostWrites(&types.Issue{NoHistory: true}, true) {
		t.Fatal("embedded create should still flush pending writes")
	}
	if !shouldCommitCreatePostWrites(&types.Issue{}, false) {
		t.Fatal("embedded create should keep the existing commit behavior")
	}
}
