//go:build integration_pg

// Package postgres benchmarks against the PG backend. Three named
// benchmarks back the bd CLI hot paths: BenchmarkBdReady, BenchmarkBdList,
// BenchmarkBdCreate. All benchmarks share a small extensible wrapper so a
// follow-up bead can inject memory snapshots, error-rate counters, or
// recovery-time metrics without rewriting each benchmark body (per mayor
// 2026-05-02 addendum).
//
// Run via `make bench-postgres`. Each sample uses a fresh per-sample
// database via testfixture.ForTest(b) so accumulated row state cannot
// saturate the shared container across `-count=N` iterations.
package postgres

import (
	"context"
	"fmt"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/postgres/testfixture"
	"github.com/steveyegge/beads/internal/types"
)

// benchHooks is the per-benchmark extension point per mayor 2026-05-02
// addendum. Each hook fires at a well-defined phase of runBench so a
// follow-up can layer memory snapshots, lock-contention counters, or
// error-rate probes without touching the individual benchmark bodies.
//
// Today every hook is nil; the first non-nil hook lands in the follow-up
// bead for memory + reliability metrics.
type benchHooks struct {
	// AfterSetup runs once after the store is opened and seeded but
	// before BeforeTimedLoop. Use for snapshotting initial heap/RSS.
	AfterSetup func(b *testing.B, store *PostgresStore)

	// BeforeTimedLoop runs immediately before b.ResetTimer. Distinct
	// from AfterSetup so a future memory probe can sample at exactly
	// the t=0 of the timed region without conflating seed cost.
	BeforeTimedLoop func(b *testing.B, store *PostgresStore)

	// AfterTimedLoop runs after b.StopTimer. Use for delta metrics —
	// heap growth, recorded errors, lock waits, or any counter the
	// caller wishes to b.ReportMetric.
	AfterTimedLoop func(b *testing.B, store *PostgresStore)
}

// runBench drives every PG benchmark. It opens a fresh per-sample
// database via testfixture, runs the optional seed, fires AfterSetup +
// BeforeTimedLoop, resets the timer, runs body, stops the timer, then
// fires AfterTimedLoop. -benchmem reports B/op + allocs/op; future hooks
// add metrics via b.ReportMetric without changing the body shape.
func runBench(
	b *testing.B,
	seed func(b *testing.B, store *PostgresStore),
	hooks benchHooks,
	body func(b *testing.B, store *PostgresStore),
) {
	b.Helper()

	dsn := testfixture.ForTest(b)
	ctx := context.Background()
	store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		b.Fatalf("open: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bench"); err != nil {
		b.Fatalf("set issue_prefix: %v", err)
	}

	if seed != nil {
		seed(b, store)
	}
	if hooks.AfterSetup != nil {
		hooks.AfterSetup(b, store)
	}
	if hooks.BeforeTimedLoop != nil {
		hooks.BeforeTimedLoop(b, store)
	}

	b.ResetTimer()
	body(b, store)
	b.StopTimer()

	if hooks.AfterTimedLoop != nil {
		hooks.AfterTimedLoop(b, store)
	}
}

// seedReadyMix populates the store with 50 parent + 50 child issues,
// half of which are linked by a blocks dependency. Seed shape is
// shared between BenchmarkBdReady and BenchmarkBdList so the two are
// directly comparable.
func seedReadyMix(b *testing.B, store *PostgresStore) {
	b.Helper()
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		parent := &types.Issue{
			ID:        fmt.Sprintf("bench-parent-%d", i),
			Title:     fmt.Sprintf("Bench Parent %d", i),
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, parent, "bench"); err != nil {
			b.Fatalf("seed parent %d: %v", i, err)
		}
		child := &types.Issue{
			ID:        fmt.Sprintf("bench-child-%d", i),
			Title:     fmt.Sprintf("Bench Child %d", i),
			Status:    types.StatusOpen,
			Priority:  2,
			IssueType: types.TypeTask,
		}
		if err := store.CreateIssue(ctx, child, "bench"); err != nil {
			b.Fatalf("seed child %d: %v", i, err)
		}
		if i%2 == 0 {
			dep := &types.Dependency{
				IssueID:     child.ID,
				DependsOnID: parent.ID,
				Type:        types.DepBlocks,
			}
			if err := store.AddDependency(ctx, dep, "bench"); err != nil {
				b.Fatalf("seed dep %d: %v", i, err)
			}
		}
	}
}

// BenchmarkBdReady measures GetReadyWork — the storage call that backs
// `bd ready`. Seed has 50 unblocked parents and 25 unblocked children;
// the other 25 children are blocked by their parent.
func BenchmarkBdReady(b *testing.B) {
	runBench(b, seedReadyMix, benchHooks{}, func(b *testing.B, store *PostgresStore) {
		ctx := context.Background()
		for i := 0; i < b.N; i++ {
			if _, err := store.GetReadyWork(ctx, types.WorkFilter{}); err != nil {
				b.Fatalf("get ready: %v", err)
			}
		}
	})
}

// BenchmarkBdList measures SearchIssues with an empty query — the storage
// call that backs `bd list` (default invocation, no --ready/--filter).
func BenchmarkBdList(b *testing.B) {
	runBench(b, seedReadyMix, benchHooks{}, func(b *testing.B, store *PostgresStore) {
		ctx := context.Background()
		for i := 0; i < b.N; i++ {
			if _, err := store.SearchIssues(ctx, "", types.IssueFilter{}); err != nil {
				b.Fatalf("search: %v", err)
			}
		}
	})
}

// BenchmarkBdCreate measures CreateIssue against an already-seeded store
// so each measured insert pays the realistic per-row index-maintenance
// cost rather than the empty-table cost.
func BenchmarkBdCreate(b *testing.B) {
	runBench(b, seedReadyMix, benchHooks{}, func(b *testing.B, store *PostgresStore) {
		ctx := context.Background()
		for i := 0; i < b.N; i++ {
			issue := &types.Issue{
				Title:       fmt.Sprintf("BdCreate %d", i),
				Description: "bd create benchmark",
				Status:      types.StatusOpen,
				Priority:    (i % 4) + 1,
				IssueType:   types.TypeTask,
			}
			if err := store.CreateIssue(ctx, issue, "bench"); err != nil {
				b.Fatalf("create: %v", err)
			}
		}
	})
}
