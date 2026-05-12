//go:build integration_pg

package postgres

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/postgres/testfixture"
	"github.com/steveyegge/beads/internal/types"
)

// heavyDatasetSize is the row count for the lite-vs-full benchmark dataset.
// 1k rows per designer §10 — large enough that the per-row scan cost
// dominates the per-query fixed overhead, so allocs/op reflects the row
// hydration path under test rather than connection plumbing.
const heavyDatasetSize = 1000

// heavyDescriptionBytes is the Description column size per fixture row.
// ~50KB per designer §10 — picks a body large enough that omitting it from
// the SELECT shape produces a measurable allocation delta even on amd64
// machines with generous heap pre-sizing. 50KB also matches the size class
// of compacted bead bodies that motivated the be-uwvs design.
const heavyDescriptionBytes = 50 * 1024

// seedHeavyBeads inserts heavyDatasetSize issues, each with a ~50KB
// Description and the other heavy columns populated. The shape mirrors what
// a populated bd database looks like after several months of work: small
// metadata, a long body, scattered identity. The benchmark target is the
// SELECT scan path, not CreateIssue, so the seed runs untimed.
func seedHeavyBeads(b *testing.B, store *PostgresStore) {
	b.Helper()
	ctx := context.Background()

	heavyBody := strings.Repeat("x", heavyDescriptionBytes)
	heavyDesign := strings.Repeat("y", heavyDescriptionBytes/4)
	heavyAccept := strings.Repeat("z", heavyDescriptionBytes/4)
	heavyNotes := strings.Repeat("n", heavyDescriptionBytes/4)

	for i := 0; i < heavyDatasetSize; i++ {
		issue := &types.Issue{
			Title:              fmt.Sprintf("Heavy bead %d", i),
			Description:        heavyBody,
			Design:             heavyDesign,
			AcceptanceCriteria: heavyAccept,
			Notes:              heavyNotes,
			Status:             types.StatusOpen,
			Priority:           (i % 4) + 1,
			IssueType:          types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "bench"); err != nil {
			b.Fatalf("seed heavy bead %d: %v", i, err)
		}
	}
}

// BenchmarkSearchIssues_Lite measures SearchIssues under filter.Lite=true on
// the heavy-bead dataset. Reports B/op and allocs/op via -benchmem.
//
// Run via:
//
//	make bench-postgres    # full bench-postgres suite (recommended)
//	go test -tags 'gms_pure_go integration_pg' -bench=BenchmarkSearchIssues_Lite -benchmem ./internal/storage/postgres/
//
// Compare against BenchmarkSearchIssues_Full on the same fixture; the lite
// path must achieve ≥50% reduction in allocs/op per the be-uwvs acceptance
// criterion. TestLiteSearchAllocReduction below enforces that ratio as a
// pass/fail gate in CI.
func BenchmarkSearchIssues_Lite(b *testing.B) {
	runBench(b, seedHeavyBeads, benchHooks{}, func(b *testing.B, store *PostgresStore) {
		ctx := context.Background()
		filter := types.IssueFilter{Lite: true}
		for i := 0; i < b.N; i++ {
			if _, err := store.SearchIssues(ctx, "", filter); err != nil {
				b.Fatalf("search lite: %v", err)
			}
		}
	})
}

// BenchmarkSearchIssues_Full measures SearchIssues under filter.Lite=false
// (the today's-default shape) on the same heavy-bead dataset. Pair with
// BenchmarkSearchIssues_Lite to read the lite-vs-full allocation delta.
func BenchmarkSearchIssues_Full(b *testing.B) {
	runBench(b, seedHeavyBeads, benchHooks{}, func(b *testing.B, store *PostgresStore) {
		ctx := context.Background()
		filter := types.IssueFilter{Lite: false}
		for i := 0; i < b.N; i++ {
			if _, err := store.SearchIssues(ctx, "", filter); err != nil {
				b.Fatalf("search full: %v", err)
			}
		}
	})
}

// TestLiteSearchAllocReduction is the CI-runnable gate that enforces the
// be-uwvs.4 acceptance criterion: lite-mode SearchIssues must allocate at
// most half as much per call as full-mode SearchIssues on the heavy-bead
// dataset.
//
// Implementation: drives testing.Benchmark on both modes against a single
// shared PG fixture (so dataset shape is identical between samples), then
// asserts allocs/op_lite ≤ 0.5 × allocs/op_full.
//
// Why not bench-only: a benchmark prints metrics, it does not fail. Without
// this test, a regression that raised the lite-mode allocations back to
// full-mode levels would surface only in human review of bench output. The
// designer's done-when (§10) calls for the 50% threshold as a hard gate, so
// we wire it via testing.Benchmark + math on the result.
//
// Why not a benchcmp golden file: the absolute allocation counts depend on
// the testcontainer image, Go version, pgx version. A ratio is portable.
//
// Skipped under -short to keep the default test suite snappy; the heavy
// seed plus 2× Benchmark invocations runs well over a minute.
func TestLiteSearchAllocReduction(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy-bead alloc-reduction benchmark — skipped under -short")
	}

	dsn := testfixture.ForTest(t)
	ctx := context.Background()
	store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	if err := store.SetConfig(ctx, "issue_prefix", "bench"); err != nil {
		t.Fatalf("set issue_prefix: %v", err)
	}

	heavyBody := strings.Repeat("x", heavyDescriptionBytes)
	heavyDesign := strings.Repeat("y", heavyDescriptionBytes/4)
	heavyAccept := strings.Repeat("z", heavyDescriptionBytes/4)
	heavyNotes := strings.Repeat("n", heavyDescriptionBytes/4)
	for i := 0; i < heavyDatasetSize; i++ {
		issue := &types.Issue{
			Title:              fmt.Sprintf("Heavy bead %d", i),
			Description:        heavyBody,
			Design:             heavyDesign,
			AcceptanceCriteria: heavyAccept,
			Notes:              heavyNotes,
			Status:             types.StatusOpen,
			Priority:           (i % 4) + 1,
			IssueType:          types.TypeTask,
		}
		if err := store.CreateIssue(ctx, issue, "bench"); err != nil {
			t.Fatalf("seed heavy bead %d: %v", i, err)
		}
	}

	full := testing.Benchmark(func(b *testing.B) {
		ctx := context.Background()
		filter := types.IssueFilter{Lite: false}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := store.SearchIssues(ctx, "", filter); err != nil {
				b.Fatalf("search full: %v", err)
			}
		}
	})

	lite := testing.Benchmark(func(b *testing.B) {
		ctx := context.Background()
		filter := types.IssueFilter{Lite: true}
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := store.SearchIssues(ctx, "", filter); err != nil {
				b.Fatalf("search lite: %v", err)
			}
		}
	})

	if full.N == 0 || lite.N == 0 {
		t.Fatalf("benchmark produced no samples: full N=%d lite N=%d", full.N, lite.N)
	}

	// Normalize to per-row allocations so the ratio is independent of the
	// row count (heavyDatasetSize). Each Benchmark op scans all rows; divide
	// to compare the per-row scan cost the SELECT shape change targets.
	fullAllocsPerRow := float64(full.AllocsPerOp()) / float64(heavyDatasetSize)
	liteAllocsPerRow := float64(lite.AllocsPerOp()) / float64(heavyDatasetSize)

	t.Logf("full mode: %d allocs/op (≈ %.2f allocs/row, %d ops)",
		full.AllocsPerOp(), fullAllocsPerRow, full.N)
	t.Logf("lite mode: %d allocs/op (≈ %.2f allocs/row, %d ops)",
		lite.AllocsPerOp(), liteAllocsPerRow, lite.N)
	t.Logf("full mode: %d B/op", full.AllocedBytesPerOp())
	t.Logf("lite mode: %d B/op", lite.AllocedBytesPerOp())

	if fullAllocsPerRow <= 0 {
		t.Fatalf("full-mode allocs/row = %.2f; benchmark broken (no allocations measured)", fullAllocsPerRow)
	}

	ratio := liteAllocsPerRow / fullAllocsPerRow
	t.Logf("lite/full per-row allocation ratio: %.2f (target: ≤ 0.50)", ratio)

	// Be-uwvs §10 acceptance: ≥50% reduction in per-row scan allocations
	// under lite. Equivalent: lite allocs/row ≤ 0.5 × full allocs/row.
	if ratio > 0.5 {
		t.Errorf("be-uwvs.4 acceptance violated: lite per-row allocations are %.0f%% of full "+
			"(want ≤ 50%%). Lite=%d allocs/op, Full=%d allocs/op over %d rows.",
			ratio*100, lite.AllocsPerOp(), full.AllocsPerOp(), heavyDatasetSize)
	}
}
