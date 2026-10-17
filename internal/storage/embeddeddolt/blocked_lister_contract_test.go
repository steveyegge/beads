//go:build cgo

package embeddeddolt_test

import (
	"testing"

	"github.com/steveyegge/beads/backend/conformance"
)

// TestBlockedListerContract runs the BlockedLister contract against the
// embedded store.
func TestBlockedListerContract(t *testing.T) {
	skipUnlessEmbeddedDolt(t)
	te := newTestEnv(t, "blk")
	lister, err := te.store.BlockedLister()
	if err != nil {
		t.Fatalf("BlockedLister(): %v", err)
	}
	kit := newEmbeddedRoleFixtureKit(te, "blk")
	conformance.RunBlockedLister(t, t.Context(), conformance.BlockedListerFixture{
		IssuePrefix:   kit.IssuePrefix,
		BlockedLister: lister,
		CreateIssue:   kit.CreateIssue,
		CreateWisp:    kit.CreateWisp,
		AddDependency: kit.AddDependency,
	})
}
