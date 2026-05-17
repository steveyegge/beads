package main

import (
	"context"
	"testing"
)

// TestBuildReferencedSet_EmptyCandidates verifies the short-circuit when
// candidateIDs is nil/empty — no storage call should be made (nil store proves it).
func TestBuildReferencedSet_EmptyCandidates(t *testing.T) {
	refSet, sample, err := buildReferencedSet(context.Background(), nil /* never called */, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if refSet != nil || sample != nil {
		t.Errorf("want nil, nil; got %v, %v", refSet, sample)
	}
}
