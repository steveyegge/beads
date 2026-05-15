package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// fakeFallbackStore satisfies storage.DoltStorage via an embedded nil
// interface (any unimplemented method panics) and returns a configurable
// Statistics. It does NOT implement jsonlImporter, so the type assertion
// in maybeAutoImportJSONL fails and the server-mode fallback path is taken
// — exactly the path that lacked an emptiness guard prior to the fix.
type fakeFallbackStore struct {
	storage.DoltStorage // nil — panics on any non-overridden method
	statsTotalIssues    int
	statsNil            bool
}

func (f *fakeFallbackStore) GetStatistics(_ context.Context) (*types.Statistics, error) {
	if f.statsNil {
		return nil, nil
	}
	return &types.Statistics{TotalIssues: f.statsTotalIssues}, nil
}

func writeAutoImportFixtureJSONL(t *testing.T, dir string) {
	t.Helper()
	// Minimal valid issue line. Contents are irrelevant for the
	// skip-when-non-empty test (returns before parseJSONLFile) and are
	// only required to be parseable for the negative-control test.
	line := `{"_type":"issue","id":"unit-1","title":"unit-test-fixture","status":"open","priority":2,"issue_type":"task"}`
	if err := os.WriteFile(filepath.Join(dir, "issues.jsonl"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func swapFallbackImporter(t *testing.T, returnErr error) *atomic.Int32 {
	t.Helper()
	orig := fallbackImporter
	var count atomic.Int32
	fallbackImporter = func(_ context.Context, _ storage.DoltStorage, _ string) (*importLocalResult, error) {
		count.Add(1)
		if returnErr != nil {
			return nil, returnErr
		}
		return &importLocalResult{}, nil
	}
	t.Cleanup(func() { fallbackImporter = orig })
	return &count
}

// TestMaybeAutoImportJSONL_ServerModeFallback_SkipsWhenNonEmpty is the
// regression test for the auto-import-on-non-empty data-clobber bug
// introduced upstream by PR #3630.
//
// Pre-fix, maybeAutoImportJSONL had no top-level emptiness guard for
// stores that did not implement jsonlImporter (i.e. server-mode dolt).
// Every command unconditionally invoked importFromLocalJSONLFull, which
// UPSERTs JSONL contents on top of live Dolt rows — silently clobbering
// recent partial-update writes whose values had not yet been re-exported
// to JSONL.
//
// This test fails on the buggy code (counter == 1) and passes after the
// guard is restored (counter == 0).
func TestMaybeAutoImportJSONL_ServerModeFallback_SkipsWhenNonEmpty(t *testing.T) {
	dir := t.TempDir()
	writeAutoImportFixtureJSONL(t, dir)
	count := swapFallbackImporter(t, errors.New("test importer should not run"))

	store := &fakeFallbackStore{statsTotalIssues: 5}
	maybeAutoImportJSONL(context.Background(), store, dir)

	if got := count.Load(); got != 0 {
		t.Fatalf("regression: server-mode fallback importer was invoked %d time(s) on a non-empty store; expected 0 (top-level emptiness guard missing or broken)", got)
	}
}

// TestMaybeAutoImportJSONL_ServerModeFallback_SkipsWhenStatisticsNil covers
// the defensive nil-statistics guard: if the store reports no error but also
// no counts, auto-import should skip rather than panic or assume emptiness.
func TestMaybeAutoImportJSONL_ServerModeFallback_SkipsWhenStatisticsNil(t *testing.T) {
	dir := t.TempDir()
	writeAutoImportFixtureJSONL(t, dir)
	count := swapFallbackImporter(t, errors.New("test importer should not run"))

	store := &fakeFallbackStore{statsNil: true}
	maybeAutoImportJSONL(context.Background(), store, dir)

	if got := count.Load(); got != 0 {
		t.Fatalf("server-mode fallback importer was invoked %d time(s) when statistics were nil; expected 0", got)
	}
}

// TestMaybeAutoImportJSONL_ServerModeFallback_RunsWhenEmpty is the negative
// control. Without it, a future change that always short-circuits would
// leave the regression test above passing vacuously.
//
// The substituted importer returns an error to short-circuit before
// s.Commit is called, so the bare fakeFallbackStore (which panics on
// every other method) does not need the full DoltStorage surface.
func TestMaybeAutoImportJSONL_ServerModeFallback_RunsWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	writeAutoImportFixtureJSONL(t, dir)
	count := swapFallbackImporter(t, errors.New("test importer: short-circuit before s.Commit"))

	store := &fakeFallbackStore{statsTotalIssues: 0}
	maybeAutoImportJSONL(context.Background(), store, dir)

	if got := count.Load(); got != 1 {
		t.Fatalf("server-mode fallback importer invoked %d time(s) on empty store; expected exactly 1", got)
	}
}

// captureOptsStore is a storage.DoltStorage that records the
// BatchCreateOptions handed to CreateIssuesWithFullOptions. Every other
// method panics (embedded nil interface); the import plumbing under test
// only touches CreateIssuesWithFullOptions, GetConfig and SetConfig.
type captureOptsStore struct {
	storage.DoltStorage // nil — panics on any non-overridden method
	prefix              string
	gotOpts             storage.BatchCreateOptions
}

func (c *captureOptsStore) CreateIssuesWithFullOptions(_ context.Context, _ []*types.Issue, _ string, opts storage.BatchCreateOptions) error {
	c.gotOpts = opts
	return nil
}

func (c *captureOptsStore) GetConfig(_ context.Context, _ string) (string, error) {
	return c.prefix, nil
}

func (c *captureOptsStore) SetConfig(_ context.Context, _, _ string) error {
	return nil
}

// TestImportIssuesCoreThreadsConflictSkip verifies the GH#3955 plumbing:
// ImportOptions.ConflictSkip maps onto storage.BatchCreateOptions.ConflictSkip,
// and the default (explicit `bd import`) path keeps UPSERT semantics.
func TestImportIssuesCoreThreadsConflictSkip(t *testing.T) {
	issues := []*types.Issue{{ID: "unit-1", Title: "t", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}}

	t.Run("ConflictSkip true threads through", func(t *testing.T) {
		s := &captureOptsStore{}
		if _, err := importIssuesCore(context.Background(), "", s, issues, ImportOptions{SkipPrefixValidation: true, ConflictSkip: true}); err != nil {
			t.Fatalf("importIssuesCore: %v", err)
		}
		if !s.gotOpts.ConflictSkip {
			t.Fatalf("ConflictSkip not threaded into BatchCreateOptions: got %+v", s.gotOpts)
		}
		if !s.gotOpts.SkipPrefixValidation {
			t.Errorf("SkipPrefixValidation should still thread through: got %+v", s.gotOpts)
		}
	})

	t.Run("default keeps UPSERT", func(t *testing.T) {
		s := &captureOptsStore{}
		if _, err := importIssuesCore(context.Background(), "", s, issues, ImportOptions{SkipPrefixValidation: true}); err != nil {
			t.Fatalf("importIssuesCore: %v", err)
		}
		if s.gotOpts.ConflictSkip {
			t.Fatalf("explicit-import path must keep UPSERT (ConflictSkip=false); got true")
		}
	})
}

// TestAutoImportFallbackSeamUsesConflictSkip verifies which importer each
// caller is wired to: the auto-import server-mode fallback
// (importFromLocalJSONLConflictSkip, the fallbackImporter seam) requests
// conflict-skip, while importFromLocalJSONLFull — used by `bd bootstrap`
// and `bd init --from-jsonl` — keeps UPSERT. This is the scope boundary
// for GH#3955.
func TestAutoImportFallbackSeamUsesConflictSkip(t *testing.T) {
	dir := t.TempDir()
	writeAutoImportFixtureJSONL(t, dir)
	jsonlPath := filepath.Join(dir, "issues.jsonl")

	t.Run("auto-import fallback uses conflict-skip", func(t *testing.T) {
		s := &captureOptsStore{prefix: "unit"}
		if _, err := importFromLocalJSONLConflictSkip(context.Background(), s, jsonlPath); err != nil {
			t.Fatalf("importFromLocalJSONLConflictSkip: %v", err)
		}
		if !s.gotOpts.ConflictSkip {
			t.Fatalf("auto-import fallback must set ConflictSkip=true; got %+v", s.gotOpts)
		}
	})

	t.Run("explicit recovery path keeps UPSERT", func(t *testing.T) {
		s := &captureOptsStore{prefix: "unit"}
		if _, err := importFromLocalJSONLFull(context.Background(), s, jsonlPath); err != nil {
			t.Fatalf("importFromLocalJSONLFull: %v", err)
		}
		if s.gotOpts.ConflictSkip {
			t.Fatalf("bd bootstrap / init --from-jsonl must keep UPSERT (ConflictSkip=false); got true")
		}
	})
}
