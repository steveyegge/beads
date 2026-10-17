package dolt

import (
	"testing"

	"github.com/steveyegge/beads/backend/conformance"
)

// TestBlockedListerContract runs the BlockedLister contract against the
// server-backed store.
func TestBlockedListerContract(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()
	ctx, cancel := testContext(t)
	defer cancel()
	lister, err := store.BlockedLister()
	if err != nil {
		t.Fatalf("BlockedLister(): %v", err)
	}
	kit := newDoltRoleFixtureKit(store, "blk")
	conformance.RunBlockedLister(t, ctx, conformance.BlockedListerFixture{
		IssuePrefix:   kit.IssuePrefix,
		BlockedLister: lister,
		CreateIssue:   kit.CreateIssue,
		CreateWisp:    kit.CreateWisp,
		AddDependency: kit.AddDependency,
	})
}
