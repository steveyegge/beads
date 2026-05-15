//go:build integration_pg

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/utils"
)

// TestResolvePartialIDForWispOnPG_PartialHash is the be-e62iag repro.
//
// Mirrors TestResolvePartialID_Wisp/partial_hash from id_parser_test.go
// (Dolt) against PG. Expected to FAIL on PG until SearchIssues routes
// Ephemeral=true queries to the wisps table.
//
// Substring path in ResolvePartialID at internal/utils/id_parser.go:167-196
// calls store.SearchIssues(ctx, hashPart, IssueFilter{Ephemeral: &true}).
// PG SearchIssues only queries the issues table (with ephemeral=TRUE
// clause → 0 rows) when Ephemeral=true; the wisp merge gate is FALSE
// so wisps are never queried. Dolt's SearchIssuesInTx at
// internal/storage/issueops/search.go:17-26 routes Ephemeral=true to
// wisps first, then falls through.
func TestResolvePartialIDForWispOnPG_PartialHash(t *testing.T) {
	dsn, _ := startPG(t)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("set issue_prefix: %v", err)
	}

	// Hand-crafted ID with a recognizable hash so the partial substring
	// in the call to ResolvePartialID is unambiguous.
	wisp := &types.Issue{
		ID:        "bd-wisp-t3st",
		Title:     "be-e62iag partial-hash repro wisp",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Priority:  2,
		Ephemeral: true,
	}
	if err := store.CreateIssue(ctx, wisp, "tester"); err != nil {
		t.Fatalf("create wisp: %v", err)
	}

	var dummy int
	if err := store.pool.QueryRow(ctx,
		`SELECT 1 FROM wisps WHERE id = $1`, wisp.ID).Scan(&dummy); err != nil {
		t.Fatalf("expected wisp %q in wisps table: %v", wisp.ID, err)
	}

	const partial = "t3st"
	resolved, err := utils.ResolvePartialID(ctx, store, partial)
	if err != nil {
		t.Fatalf("ResolvePartialID(%q): %v (be-e62iag: PG SearchIssues with Ephemeral=true does not consult wisps table)", partial, err)
	}
	if resolved != wisp.ID {
		t.Errorf("ResolvePartialID(%q) = %q, want %q", partial, resolved, wisp.ID)
	}
}
