//go:build bench

package sqlite

import (
	"context"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// Benchmark size rationale:
// We only benchmark Large (10K) and XLarge (20K) databases because:
// - Small databases (<1K issues) perform acceptably without optimization
// - Performance issues only manifest at scale (10K+ issues)
// - Smaller benchmarks add code weight without providing optimization insights
// - Target users manage repos with thousands of issues, not hundreds

// BenchmarkGetReadyWork_Large benchmarks GetReadyWork on 10K issue database
func BenchmarkGetReadyWork_Large(b *testing.B) {
	store, cleanup := setupLargeBenchDB(b)
	defer cleanup()
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := store.GetReadyWork(ctx, types.WorkFilter{})
		if err != nil {
			b.Fatalf("GetReadyWork failed: %v", err)
		}
	}
}

// BenchmarkGetReadyWork_XLarge benchmarks GetReadyWork on 20K issue database
func BenchmarkGetReadyWork_XLarge(b *testing.B) {
	store, cleanup := setupXLargeBenchDB(b)
	defer cleanup()
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := store.GetReadyWork(ctx, types.WorkFilter{})
		if err != nil {
			b.Fatalf("GetReadyWork failed: %v", err)
		}
	}
}

// BenchmarkSearchIssues_Large_NoFilter benchmarks searching all open issues
func BenchmarkSearchIssues_Large_NoFilter(b *testing.B) {
	store, cleanup := setupLargeBenchDB(b)
	defer cleanup()
	ctx := context.Background()

	openStatus := types.StatusOpen
	filter := types.IssueFilter{
		Status: &openStatus,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			b.Fatalf("SearchIssues failed: %v", err)
		}
	}
}

// BenchmarkSearchIssues_Large_ComplexFilter benchmarks complex filtered search
func BenchmarkSearchIssues_Large_ComplexFilter(b *testing.B) {
	store, cleanup := setupLargeBenchDB(b)
	defer cleanup()
	ctx := context.Background()

	openStatus := types.StatusOpen
	filter := types.IssueFilter{
		Status:      &openStatus,
		PriorityMin: intPtr(0),
		PriorityMax: intPtr(2),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			b.Fatalf("SearchIssues failed: %v", err)
		}
	}
}

// BenchmarkCreateIssue_Large benchmarks issue creation in large database
func BenchmarkCreateIssue_Large(b *testing.B) {
	store, cleanup := setupLargeBenchDB(b)
	defer cleanup()
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		issue := &types.Issue{
			Title:       "Benchmark issue",
			Description: "Test description",
			Status:      types.StatusOpen,
			Priority:    2,
			IssueType:   types.TypeTask,
		}

		if err := store.CreateIssue(ctx, issue, "bench"); err != nil {
			b.Fatalf("CreateIssue failed: %v", err)
		}
	}
}

// BenchmarkUpdateIssue_Large benchmarks issue updates in large database
func BenchmarkUpdateIssue_Large(b *testing.B) {
	store, cleanup := setupLargeBenchDB(b)
	defer cleanup()
	ctx := context.Background()

	// Get a random issue to update
	openStatus := types.StatusOpen
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{
		Status: &openStatus,
	})
	if err != nil || len(issues) == 0 {
		b.Fatalf("Failed to get issues for update test: %v", err)
	}
	targetID := issues[0].ID

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		updates := map[string]interface{}{
			"status": types.StatusInProgress,
		}

		if err := store.UpdateIssue(ctx, targetID, updates, "bench"); err != nil {
			b.Fatalf("UpdateIssue failed: %v", err)
		}

		// Reset back to open for next iteration
		updates["status"] = types.StatusOpen
		if err := store.UpdateIssue(ctx, targetID, updates, "bench"); err != nil {
			b.Fatalf("UpdateIssue failed: %v", err)
		}
	}
}

// BenchmarkGetReadyWork_FromJSONL benchmarks ready work on JSONL-imported database
func BenchmarkGetReadyWork_FromJSONL(b *testing.B) {
	store, cleanup := setupLargeFromJSONL(b)
	defer cleanup()
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := store.GetReadyWork(ctx, types.WorkFilter{})
		if err != nil {
			b.Fatalf("GetReadyWork failed: %v", err)
		}
	}
}

// Helper function
func intPtr(i int) *int {
	return &i
}
