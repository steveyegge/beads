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
// Compare against BenchmarkSearchIssues_Full on the same fixture.
// TestLiteSearchAllocReduction enforces a compound bytes gate (≥90% reduction)
// + allocs sanity-check gate (≥15% reduction) as pass/fail in CI.
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
// be-uwvs.4 acceptance criteria via a compound two-gate check:
//
//   - Bytes gate (primary): lite B/op ≤ 10% of full B/op (≥90% reduction).
//     The lite SELECT eliminates 4 heavy text columns (each 12–50 KB) from the
//     wire; on the 1k×50KB fixture that must shrink heap bytes by ≥90%.
//
//   - Allocs gate (sanity check): lite allocs/row ≤ 85% of full allocs/row
//     (≥15% reduction). Verifies the lite SELECT branch is actually active;
//     if filter.Lite is being ignored this gate catches it.
//
// Implementation: drives testing.Benchmark on both modes against a single
// shared PG fixture (so dataset shape is identical between samples), then
// asserts ratios on the results.
//
// Why not bench-only: a benchmark prints metrics, it does not fail. Without
// this test, a regression would surface only in human review of bench output.
//
// Why not a benchcmp golden file: the absolute allocation counts depend on
// the testcontainer image, Go version, pgx version. Ratios are portable.
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
	if full.AllocedBytesPerOp() == 0 {
		t.Fatalf("full-mode B/op = 0; benchmark broken (no bytes measured)")
	}

	// ── bytes gate (primary) ──────────────────────────────────────────────
	// The lite shape eliminates 4 heavy text columns (each 12–50 KB) from the
	// SELECT and scan path. On a 1k × 50KB fixture this produces ≥90% heap
	// bytes reduction — the load-bearing GC-pressure claim of be-uwvs.
	fullBytesPerOp := float64(full.AllocedBytesPerOp())
	liteBytesPerOp := float64(lite.AllocedBytesPerOp())
	bytesRatio := liteBytesPerOp / fullBytesPerOp
	t.Logf("lite/full B/op ratio: %.4f (target: ≤ 0.10, i.e. ≥90%% bytes reduction)", bytesRatio)
	if bytesRatio > 0.10 {
		t.Errorf("be-uwvs.4 bytes gate: lite B/op is %.1f%% of full (want ≤ 10%%). "+
			"Lite=%d B/op, Full=%d B/op. "+
			"The lite SELECT shape is not dropping the heavy columns from the wire.",
			bytesRatio*100, lite.AllocedBytesPerOp(), full.AllocedBytesPerOp())
	}

	// ── allocs gate (sanity check) ────────────────────────────────────────
	// Verifies the lite SELECT branch is active: dropping 4 populated heavy
	// text columns must save ≥15% of per-row allocations. If this fails,
	// filter.Lite is being ignored.
	allocRatio := liteAllocsPerRow / fullAllocsPerRow
	t.Logf("lite/full per-row alloc ratio: %.2f (target: ≤ 0.85, i.e. ≥15%% reduction)", allocRatio)
	if allocRatio > 0.85 {
		t.Errorf("be-uwvs.4 allocs gate: lite allocs/row is %.0f%% of full (want ≤ 85%%). "+
			"Lite=%.2f allocs/row, Full=%.2f allocs/row. "+
			"The lite SELECT shape may not be active (check filter.Lite branching).",
			allocRatio*100, liteAllocsPerRow, fullAllocsPerRow)
	}
}
