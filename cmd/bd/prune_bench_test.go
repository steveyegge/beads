package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// refScanStore is a minimal mock for buildReferencedSet.
// SearchIssues returns a fixed slice; GetIssueComments returns nil.
// All other DoltStorage methods panic — they must never be called.
type refScanStore struct {
	storage.DoltStorage // nil — panics on unimplemented methods
	issues              []*types.Issue
}

func (s *refScanStore) SearchIssues(_ context.Context, _ string, _ types.IssueFilter) ([]*types.Issue, error) {
	return s.issues, nil
}

func (s *refScanStore) GetIssueComments(_ context.Context, _ string) ([]*types.Comment, error) {
	return nil, nil
}

// TestPruneLargeFixture asserts that buildReferencedSet completes within 5s on
// a 10K-open-bead × 5KB-body fixture with 100 closed candidates, 20 of which
// are cited in open bead descriptions.
//
// Skipped by go test -short.
func TestPruneLargeFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large fixture bench")
	}

	const (
		numOpen       = 10_000
		numCandidates = 100
		numReferenced = 20
		bodySize      = 5_000
	)

	// Build candidate ID set (simulates closed-bead prune targets).
	candidates := make(map[string]bool, numCandidates)
	candidateList := make([]string, numCandidates)
	for i := 0; i < numCandidates; i++ {
		id := fmt.Sprintf("fx-%05d", i)
		candidates[id] = true
		candidateList[i] = id
	}

	// Pad description to ~5KB with neutral text (no ID-like tokens).
	const filler = "Lorem ipsum dolor sit amet consectetur adipiscing elit " +
		"sed do eiusmod tempor incididunt ut labore et dolore magna aliqua " +
		"ut enim ad minim veniam quis nostrud exercitation ullamco laboris "
	var sb strings.Builder
	for sb.Len() < bodySize {
		sb.WriteString(filler)
	}
	pad := sb.String()[:bodySize]

	// Build 10K open issues; the first numReferenced each cite one candidate.
	openIssues := make([]*types.Issue, numOpen)
	for i := 0; i < numOpen; i++ {
		var desc string
		if i < numReferenced {
			desc = "See " + candidateList[i] + " §3 for the rollback path. " + pad
		} else {
			desc = pad
		}
		openIssues[i] = &types.Issue{
			ID:          fmt.Sprintf("open-%05d", i),
			Status:      types.StatusOpen,
			Description: desc,
		}
	}

	// Inject mock store and restore on exit.
	origStore := store
	store = &refScanStore{issues: openIssues}
	defer func() { store = origStore }()

	start := time.Now()
	refSet, err := buildReferencedSet(context.Background(), candidates)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("buildReferencedSet error: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("buildReferencedSet took %v; want <5s", elapsed)
	}
	if got := len(refSet); got != numReferenced {
		t.Errorf("refSet size = %d; want %d", got, numReferenced)
	}
	for i := 0; i < numReferenced; i++ {
		if !refSet[candidateList[i]] {
			t.Errorf("expected %s in refSet", candidateList[i])
		}
	}
}
