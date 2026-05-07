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

// TestResolvePartialIDForWispOnPG is the regression test for be-szr.
//
// Pre-fix repro: ResolvePartialID's fast path uses SearchIssues with
// IDs filter, which on PG only consults the issues table. Wisps live
// in a separate wisps table, so a full wisp ID never resolved and
// `bd show <wisp-id>` / `gc mail peek` returned "no issue found
// matching" against PG-backed rigs.
//
// Fix (commit 16a6634f3): after SearchIssues misses, ResolvePartialID
// tries store.GetIssue(input). PG's GetIssue already falls through
// from issues→wisps on NotFound, so wisps resolve transparently.
//
// This test creates a wisp (Ephemeral=true → routed to wisps table),
// then asserts both store.GetIssue and utils.ResolvePartialID resolve
// the full wisp ID. Removing the be-szr fallback in id_parser.go (lines
// 53-60 at the time of commit 16a6634f3) makes the ResolvePartialID
// assertion fail with "no issue found matching".
func TestResolvePartialIDForWispOnPG(t *testing.T) {
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

	wisp := &types.Issue{
		Title:     "be-szr regression wisp",
		IssueType: types.TypeTask,
		Status:    types.StatusOpen,
		Priority:  2,
		Ephemeral: true,
	}
	if err := store.CreateIssue(ctx, wisp, "tester"); err != nil {
		t.Fatalf("create wisp: %v", err)
	}
	if wisp.ID == "" {
		t.Fatal("wisp ID was not assigned")
	}

	var dummy int
	if err := store.pool.QueryRow(ctx,
		`SELECT 1 FROM wisps WHERE id = $1`, wisp.ID).Scan(&dummy); err != nil {
		t.Fatalf("expected wisp %q in wisps table: %v", wisp.ID, err)
	}

	got, err := store.GetIssue(ctx, wisp.ID)
	if err != nil {
		t.Fatalf("GetIssue(%q): %v", wisp.ID, err)
	}
	if got == nil || got.ID != wisp.ID {
		t.Fatalf("GetIssue: got %+v, want ID %q", got, wisp.ID)
	}

	resolved, err := utils.ResolvePartialID(ctx, store, wisp.ID)
	if err != nil {
		t.Fatalf("ResolvePartialID(%q): %v (be-szr regression?)", wisp.ID, err)
	}
	if resolved != wisp.ID {
		t.Errorf("ResolvePartialID(%q) = %q, want %q", wisp.ID, resolved, wisp.ID)
	}
}
