package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// fakeFallbackStore satisfies storage.DoltStorage via an embedded nil
// interface (any unimplemented method panics) and returns a configurable
// Statistics. It does NOT implement jsonlImporter, so the type assertion
// in maybeAutoImportJSONL fails and the fallback importer path is taken
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
	writeAutoImportFixtureJSONLNamed(t, dir, "issues.jsonl")
}

func writeAutoImportFixtureJSONLNamed(t *testing.T, dir, name string) {
	t.Helper()
	// Minimal valid issue line. Contents are irrelevant for the
	// skip-when-non-empty test (returns before parseJSONLFile) and are
	// only required to be parseable for the negative-control test.
	line := `{"_type":"issue","id":"unit-1","title":"unit-test-fixture","status":"open","priority":2,"issue_type":"task"}`
	if err := os.WriteFile(filepath.Join(dir, name), []byte(line+"\n"), 0o600); err != nil {
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

// TestMaybeAutoImportJSONL_FallbackImporter_SkipsWhenNonEmpty is the
// regression test for the auto-import-on-non-empty data-clobber bug
// introduced upstream by PR #3630.
//
// Pre-fix, maybeAutoImportJSONL had no top-level emptiness guard for
// stores that did not implement jsonlImporter. Every command
// unconditionally invoked importFromLocalJSONLFull, which UPSERTs JSONL
// contents on top of live Dolt rows — silently clobbering recent
// partial-update writes whose values had not yet been re-exported to
// JSONL.
//
// This test fails on the buggy code (counter == 1) and passes after the
// guard is restored (counter == 0).
func TestMaybeAutoImportJSONL_FallbackImporter_SkipsWhenNonEmpty(t *testing.T) {
	dir := t.TempDir()
	writeAutoImportFixtureJSONL(t, dir)
	count := swapFallbackImporter(t, errors.New("test importer should not run"))

	store := &fakeFallbackStore{statsTotalIssues: 5}
	maybeAutoImportJSONL(context.Background(), store, dir)

	if got := count.Load(); got != 0 {
		t.Fatalf("regression: fallback importer was invoked %d time(s) on a non-empty store; expected 0 (top-level emptiness guard missing or broken)", got)
	}
}

func TestShouldRunAutoImportJSONL(t *testing.T) {
	store := &fakeFallbackStore{}
	writeCmd := &cobra.Command{Use: "update"}

	tests := []struct {
		name        string
		cmd         *cobra.Command
		store       storage.DoltStorage
		useReadOnly bool
		globalFlag  bool
		serverMode  bool
		want        bool
	}{
		{name: "write command embedded", cmd: writeCmd, store: store, want: true},
		{name: "server mode", cmd: writeCmd, store: store, serverMode: true, want: false},
		{name: "read only", cmd: writeCmd, store: store, useReadOnly: true, want: false},
		{name: "global", cmd: writeCmd, store: store, globalFlag: true, want: false},
		{name: "import command", cmd: &cobra.Command{Use: "import"}, store: store, want: false},
		{name: "nil store", cmd: writeCmd, want: false},
		{name: "nil command", store: store, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldRunAutoImportJSONL(tt.cmd, tt.store, tt.useReadOnly, tt.globalFlag, tt.serverMode)
			if got != tt.want {
				t.Fatalf("shouldRunAutoImportJSONL() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMaybeAutoImportJSONL_FallbackImporter_SkipsWhenStatisticsNil covers
// the defensive nil-statistics guard: if the store reports no error but also
// no counts, auto-import should skip rather than panic or assume emptiness.
func TestMaybeAutoImportJSONL_FallbackImporter_SkipsWhenStatisticsNil(t *testing.T) {
	dir := t.TempDir()
	writeAutoImportFixtureJSONL(t, dir)
	count := swapFallbackImporter(t, errors.New("test importer should not run"))

	store := &fakeFallbackStore{statsNil: true}
	maybeAutoImportJSONL(context.Background(), store, dir)

	if got := count.Load(); got != 0 {
		t.Fatalf("fallback importer was invoked %d time(s) when statistics were nil; expected 0", got)
	}
}

// TestMaybeAutoImportJSONL_FallbackImporter_RunsWhenEmpty is the negative
// control. Without it, a future change that always short-circuits would
// leave the regression test above passing vacuously.
//
// The substituted importer returns an error to short-circuit before
// s.Commit is called, so the bare fakeFallbackStore (which panics on
// every other method) does not need the full DoltStorage surface.
func TestMaybeAutoImportJSONL_FallbackImporter_RunsWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	writeAutoImportFixtureJSONL(t, dir)
	count := swapFallbackImporter(t, errors.New("test importer: short-circuit before s.Commit"))

	store := &fakeFallbackStore{statsTotalIssues: 0}
	maybeAutoImportJSONL(context.Background(), store, dir)

	if got := count.Load(); got != 1 {
		t.Fatalf("fallback importer invoked %d time(s) on empty store; expected exactly 1", got)
	}
}

func TestMaybeAutoImportJSONL_UsesConfiguredImportPath(t *testing.T) {
	initConfigForTest(t)
	config.Set("import.path", "beads.jsonl")

	dir := t.TempDir()
	writeAutoImportFixtureJSONLNamed(t, dir, "beads.jsonl")

	orig := fallbackImporter
	var gotPath string
	fallbackImporter = func(_ context.Context, _ storage.DoltStorage, path string) (*importLocalResult, error) {
		gotPath = path
		return nil, errors.New("test importer: short-circuit before s.Commit")
	}
	t.Cleanup(func() { fallbackImporter = orig })

	store := &fakeFallbackStore{statsTotalIssues: 0}
	maybeAutoImportJSONL(context.Background(), store, dir)

	wantPath := filepath.Join(dir, "beads.jsonl")
	if gotPath != wantPath {
		t.Fatalf("auto-import path = %q, want %q", gotPath, wantPath)
	}
}
