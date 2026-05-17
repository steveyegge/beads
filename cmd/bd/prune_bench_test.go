//go:build cgo

package main

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// benchStorage is a minimal storage.Storage for bench fixtures.
// Only SearchIssues, GetIssueComments, and Close are implemented;
// any other call panics (the embedded nil interface propagates).
type benchStorage struct {
	storage.Storage // nil — panics on unimplemented methods
	open            []*types.Issue
	comments        map[string][]*types.Comment
}

func (s *benchStorage) SearchIssues(_ context.Context, _ string, filter types.IssueFilter) ([]*types.Issue, error) {
	// Return all open issues regardless of the filter (bench fixture is
	// pre-built to contain only open issues).
	return s.open, nil
}

func (s *benchStorage) GetIssueComments(_ context.Context, issueID string) ([]*types.Comment, error) {
	return s.comments[issueID], nil
}

func (s *benchStorage) Close() error { return nil }

// buildPruneBenchFixture creates an in-memory storage rig with nOpen open
// beads and nClosed closed candidate beads. Open bead descriptions are ~5KB
// with 0–3 random references to closed bead IDs; 10% of closed IDs form the
// referenced pool. Returns a storage.Storage and the closed candidateIDs map.
// Fixture build is in-memory — no Dolt overhead — and completes in < 1s.
// Skips if testing.Short() is set.
func buildPruneBenchFixture(t testing.TB, nOpen, nClosed int) (storage.Storage, map[string]bool, func()) {
	t.Helper()
	if testing.Short() {
		t.Skip("NFR-02 bench: skipping slow fixture in short mode")
	}

	start := time.Now()

	// Build ~5KB lorem body base.
	bodyBase := strings.Repeat(
		"Lorem ipsum dolor sit amet, consectetur adipiscing elit. Sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. ",
		40, // 40 × ~120 chars ≈ 4.8 KB
	)

	// 10% of closed IDs form the referenced pool.
	refPoolSize := nClosed / 10
	if refPoolSize < 1 {
		refPoolSize = 1
	}

	// Pre-generate closed IDs.
	closedIDs := make([]string, nClosed)
	candidateIDs := make(map[string]bool, nClosed)
	for i := range nClosed {
		id := fmt.Sprintf("pb-c%06d", i)
		closedIDs[i] = id
		candidateIDs[id] = true
	}
	refPool := closedIDs[:refPoolSize]

	rng := rand.New(rand.NewSource(42))
	now := time.Now()

	// Build open issues with ~5KB bodies and 0–3 bead-ID refs each.
	open := make([]*types.Issue, nOpen)
	for i := range nOpen {
		nRefs := rng.Intn(4) // 0–3
		body := bodyBase
		for range nRefs {
			body += " " + refPool[rng.Intn(len(refPool))]
		}
		open[i] = &types.Issue{
			ID:          fmt.Sprintf("pb-o%06d", i),
			Title:       fmt.Sprintf("Open bead %d — bench fixture", i),
			Description: body,
			IssueType:   types.TypeTask,
			Priority:    2,
			Status:      types.StatusOpen,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
	}

	// Add 1–2 comments (~1 KB each) to 2% of open beads.
	commentBase := strings.Repeat("Technical discussion and implementation notes for review. ", 18) // ~1 KB
	comments := make(map[string][]*types.Comment)
	commentLimit := nOpen / 50
	if commentLimit < 1 {
		commentLimit = 1
	}
	for i, iss := range open {
		if i >= commentLimit {
			break
		}
		nComments := 1 + rng.Intn(2)
		issComments := make([]*types.Comment, nComments)
		for j := range nComments {
			ref := refPool[rng.Intn(len(refPool))]
			issComments[j] = &types.Comment{
				ID:   fmt.Sprintf("%s-c%d", iss.ID, j),
				Text: commentBase + " " + ref,
			}
		}
		comments[iss.ID] = issComments
	}

	s := &benchStorage{open: open, comments: comments}

	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Skipf("bench fixture build unexpectedly slow (%v); skipping", elapsed)
	}

	return s, candidateIDs, func() {}
}

// BenchmarkPruneScan_10K measures the reference scan (candidates loaded →
// referenced set built) on a 10K-open / 1K-closed in-memory fixture.
// Benchmark scope: from candidateIDs map ready to refSet returned.
// Skips under go test -short.
func BenchmarkPruneScan_10K(b *testing.B) {
	s, candidateIDs, cleanup := buildPruneBenchFixture(b, 10_000, 1_000)
	defer cleanup()

	ctx := context.Background()

	b.ResetTimer()
	for range b.N {
		refSet, _, err := buildReferencedSet(ctx, s, candidateIDs)
		if err != nil {
			b.Fatalf("buildReferencedSet: %v", err)
		}
		_ = refSet
	}
	b.ReportAllocs()
}

// TestPruneScan_NFR02_Under5s asserts the reference scan over a 10K-open /
// 1K-closed fixture completes in < 5s (NFR-02, per be-5sn done-when criteria).
// Uses an in-memory storage to isolate scan latency from storage I/O.
// Skips under go test -short.
func TestPruneScan_NFR02_Under5s(t *testing.T) {
	s, candidateIDs, cleanup := buildPruneBenchFixture(t, 10_000, 1_000)
	defer cleanup()

	start := time.Now()
	refSet, _, err := buildReferencedSet(context.Background(), s, candidateIDs)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("buildReferencedSet: %v", err)
	}
	_ = refSet

	if elapsed > 5*time.Second {
		t.Errorf("NFR-02 FAIL: prune scan took %v; want < 5s", elapsed)
	}
}
