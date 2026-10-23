package dolt

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	sqlmock "github.com/DATA-DOG/go-sqlmock"
	mysql "github.com/go-sql-driver/mysql"

	"github.com/steveyegge/beads/internal/storage/depid"
	"github.com/steveyegge/beads/internal/storage/issueops"
	"github.com/steveyegge/beads/internal/types"
	publicops "github.com/steveyegge/beads/issueops"
)

// TestGetDependentRecords verifies the target-keyed raw dependents read:
// direction correctness (rows whose target is X, not whose source is X), the
// dep-type filter, that rows span BOTH the durable and wisp dependency tables,
// row-id keyset paging with no drop/dup across the two-table boundary, and that
// it does not drop rows on source status (raw, no source hydration). Ordering is
// by the dependency row's primary id (a UUIDv5), so assertions are set-based.
func TestGetDependentRecords(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	mk := func(id string, ephemeral bool) *types.Issue {
		iss := &types.Issue{ID: id, Title: id, Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Ephemeral: ephemeral}
		if err := store.CreateIssue(ctx, iss, "tester"); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		return iss
	}
	target := mk("dr-target", false)
	block := mk("dr-s1-block", false)   // blocks -> X (durable, `dependencies`)
	childA := mk("dr-s2-childa", false) // parent-child -> X (durable)
	closed := mk("dr-s3-closed", false) // parent-child -> X, then closed (durable)
	wispDep := mk("dr-s4-wisp", true)   // blocks -> X from a WISP source (`wisp_dependencies`)
	other := mk("dr-other", false)      // X -> other (decoy: source is X)

	addDep := func(src, tgt string, typ types.DependencyType) {
		if err := store.AddDependency(ctx, &types.Dependency{IssueID: src, DependsOnID: tgt, Type: typ}, "tester"); err != nil {
			t.Fatalf("add dep %s -> %s (%s): %v", src, tgt, typ, err)
		}
	}
	addDep(block.ID, target.ID, types.DepBlocks)
	addDep(childA.ID, target.ID, types.DepParentChild)
	addDep(closed.ID, target.ID, types.DepParentChild)
	addDep(wispDep.ID, target.ID, types.DepBlocks) // wisp source -> durable target (wisp_dependencies)
	addDep(target.ID, other.ID, types.DepBlocks)   // decoy: target is the SOURCE here
	if err := store.CloseIssue(ctx, closed.ID, "done", "tester", ""); err != nil {
		t.Fatalf("close %s: %v", closed.ID, err)
	}

	srcSet := func(deps []*types.Dependency) map[string]bool {
		out := map[string]bool{}
		for _, d := range deps {
			out[d.IssueID] = true
		}
		return out
	}
	eqSet := func(t *testing.T, got map[string]bool, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("dependent sources = %v, want %v", got, want)
		}
		for _, w := range want {
			if !got[w] {
				t.Fatalf("dependent sources = %v, missing %q", got, w)
			}
		}
	}

	// Direction + raw + two-table span: all inbound edges of X regardless of
	// source status, INCLUDING the wisp source; the decoy must not appear.
	all, err := store.GetDependentRecords(ctx, target.ID, "", 100, "")
	if err != nil {
		t.Fatalf("GetDependentRecords(all): %v", err)
	}
	eqSet(t, srcSet(all), block.ID, childA.ID, closed.ID, wispDep.ID)
	for _, d := range all {
		if d.ID == "" {
			t.Fatalf("dependent row has empty ID (the keyset cursor): %+v", d)
		}
		if d.DependsOnID != target.ID {
			t.Fatalf("row %s has target %s, want %s", d.IssueID, d.DependsOnID, target.ID)
		}
		if d.IssueID == target.ID {
			t.Fatalf("direction violation: returned a row whose SOURCE is the target")
		}
	}

	// The decoy edge is discoverable from the OTHER direction (target=other).
	fromOther, err := store.GetDependentRecords(ctx, other.ID, "", 100, "")
	if err != nil {
		t.Fatalf("GetDependentRecords(other): %v", err)
	}
	eqSet(t, srcSet(fromOther), target.ID)

	// Type filter: only parent-child edges (the wisp edge is 'blocks', excluded).
	pc, err := store.GetDependentRecords(ctx, target.ID, string(types.DepParentChild), 100, "")
	if err != nil {
		t.Fatalf("GetDependentRecords(parent-child): %v", err)
	}
	eqSet(t, srcSet(pc), childA.ID, closed.ID)

	// Row-id keyset paging across the two-table boundary: page size 1 walks all
	// four inbound sources (three durable + one wisp) with no gaps or duplicates.
	seen := map[string]bool{}
	var pages int
	after := ""
	for {
		page, err := store.GetDependentRecords(ctx, target.ID, "", 1, after)
		if err != nil {
			t.Fatalf("GetDependentRecords(page after %q): %v", after, err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) != 1 {
			t.Fatalf("page size = %d, want 1", len(page))
		}
		if seen[page[0].IssueID] {
			t.Fatalf("duplicate source %q across pages (keyset drop/dup)", page[0].IssueID)
		}
		seen[page[0].IssueID] = true
		after = page[0].ID
		if pages++; pages > 10 {
			t.Fatalf("paging did not terminate")
		}
	}
	eqSet(t, seen, block.ID, childA.ID, closed.ID, wispDep.ID)
}

// TestCountDependentRecords verifies the un-paged total matches the paged read's
// membership, spans both tables, and honors the type filter.
func TestCountDependentRecords(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	mk := func(id string, ephemeral bool) {
		iss := &types.Issue{ID: id, Title: id, Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Ephemeral: ephemeral}
		if err := store.CreateIssue(ctx, iss, "tester"); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	mk("cd-target", false)
	mk("cd-s1", false)
	mk("cd-s2", false)
	mk("cd-w", true)
	add := func(src string, typ types.DependencyType) {
		if err := store.AddDependency(ctx, &types.Dependency{IssueID: src, DependsOnID: "cd-target", Type: typ}, "tester"); err != nil {
			t.Fatalf("add dep %s: %v", src, err)
		}
	}
	add("cd-s1", types.DepBlocks)
	add("cd-s2", types.DepParentChild)
	add("cd-w", types.DepBlocks) // wisp source (wisp_dependencies)

	if n, err := store.CountDependentRecords(ctx, "cd-target", ""); err != nil {
		t.Fatalf("CountDependentRecords: %v", err)
	} else if n != 3 {
		t.Fatalf("CountDependentRecords(all) = %d, want 3 (2 durable + 1 wisp)", n)
	}
	if n, err := store.CountDependentRecords(ctx, "cd-target", string(types.DepBlocks)); err != nil {
		t.Fatalf("CountDependentRecords(blocks): %v", err)
	} else if n != 2 {
		t.Fatalf("CountDependentRecords(blocks) = %d, want 2 (cd-s1 durable + cd-w wisp)", n)
	}
	if n, err := store.CountDependentRecords(ctx, "cd-target", string(types.DepParentChild)); err != nil {
		t.Fatalf("CountDependentRecords(parent-child): %v", err)
	} else if n != 1 {
		t.Fatalf("CountDependentRecords(parent-child) = %d, want 1", n)
	}
	// A target with no dependents counts zero, not an error.
	if n, err := store.CountDependentRecords(ctx, "cd-s1", ""); err != nil {
		t.Fatalf("CountDependentRecords(leaf): %v", err)
	} else if n != 0 {
		t.Fatalf("CountDependentRecords(leaf) = %d, want 0", n)
	}
}

// TestDependentRecordsCrossTableCollision seeds the SAME logical edge in BOTH
// the durable and wisp dependency tables — they share one depid-derived id
// because depid.New keys on (issue_id, target) and omits the table — and proves
// the target-keyed reads treat it as a single inbound edge: GetDependentRecords
// returns one row per id (the durable copy), CountDependentRecords equals the
// distinct total (not the sum of two per-table COUNTs), and keyset paging is
// stable across the collision (no dup, no drop). A collision like this arises
// from a wisp promoted to durable or two Dolt clones merged.
func TestDependentRecordsCrossTableCollision(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	mk := func(id string, ephemeral bool) {
		iss := &types.Issue{ID: id, Title: id, Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Ephemeral: ephemeral}
		if err := store.CreateIssue(ctx, iss, "tester"); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	mk("cc-target", false)
	mk("cc-a", false) // durable source of the collision edge
	mk("cc-c", false) // durable-only dependent
	mk("cc-w", true)  // genuine wisp-only dependent

	add := func(src string) {
		if err := store.AddDependency(ctx, &types.Dependency{IssueID: src, DependsOnID: "cc-target", Type: types.DepBlocks}, "tester"); err != nil {
			t.Fatalf("add dep %s: %v", src, err)
		}
	}
	add("cc-a") // -> dependencies, id = depid.New("cc-a", "cc-target")
	add("cc-c") // -> dependencies
	add("cc-w") // wisp source -> wisp_dependencies

	// Force the collision: the SAME (cc-a, cc-target) edge into wisp_dependencies
	// with the same depid. FK checks are relaxed because cc-a is not a wisp — the
	// point is to reproduce the post-merge/promotion state where one edge exists
	// in both tables under one id.
	collisionID := depid.New("cc-a", "cc-target")
	if _, err := store.db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		t.Fatalf("disable FK checks: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		"INSERT INTO wisp_dependencies (id, issue_id, depends_on_issue_id, type, created_by) VALUES (?, ?, ?, 'blocks', 'tester')",
		collisionID, "cc-a", "cc-target"); err != nil {
		t.Fatalf("seed collision row: %v", err)
	}
	if _, err := store.db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		t.Fatalf("re-enable FK checks: %v", err)
	}

	// One row per id: cc-a appears once (its durable copy), never twice.
	all, err := store.GetDependentRecords(ctx, "cc-target", "", 100, "")
	if err != nil {
		t.Fatalf("GetDependentRecords: %v", err)
	}
	ids := map[string]int{}
	srcs := map[string]bool{}
	for _, d := range all {
		ids[d.ID]++
		srcs[d.IssueID] = true
	}
	for id, n := range ids {
		if n != 1 {
			t.Fatalf("id %s appears %d times in one page; want exactly 1 (collision not de-duped)", id, n)
		}
	}
	if len(srcs) != 3 || !srcs["cc-a"] || !srcs["cc-c"] || !srcs["cc-w"] {
		t.Fatalf("dependent sources = %v, want {cc-a, cc-c, cc-w}", srcs)
	}

	// Count equals the distinct total (3), not the sum of two per-table COUNTs (4).
	if n, err := store.CountDependentRecords(ctx, "cc-target", ""); err != nil {
		t.Fatalf("CountDependentRecords: %v", err)
	} else if n != 3 {
		t.Fatalf("CountDependentRecords = %d, want 3 (distinct); a sum-of-counts would report 4", n)
	}

	// Keyset paging (size 1) is stable across the collision: 3 distinct sources,
	// each once, no drop.
	seen := map[string]bool{}
	after := ""
	for i := 0; i < 10; i++ {
		page, err := store.GetDependentRecords(ctx, "cc-target", "", 1, after)
		if err != nil {
			t.Fatalf("page after %q: %v", after, err)
		}
		if len(page) == 0 {
			break
		}
		if len(page) != 1 {
			t.Fatalf("page size = %d, want 1", len(page))
		}
		if seen[page[0].IssueID] {
			t.Fatalf("duplicate source %q across pages (collision drop/dup)", page[0].IssueID)
		}
		seen[page[0].IssueID] = true
		after = page[0].ID
	}
	if len(seen) != 3 || !seen["cc-a"] || !seen["cc-c"] || !seen["cc-w"] {
		t.Fatalf("paged sources = %v, want 3 distinct {cc-a, cc-c, cc-w}", seen)
	}
}

// TestGetDependentRecordsLimitClamp verifies the self-clamp (default 100, cap 500).
func TestGetDependentRecordsLimitClamp(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	if err := store.CreateIssue(ctx, &types.Issue{ID: "lc-target", Title: "t", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}, "tester"); err != nil {
		t.Fatalf("create target: %v", err)
	}
	// 120 durable dependents -> more than the default clamp (100), fewer than
	// the cap (500), so we can observe the default kick in without seeding 500.
	const n = 120
	for i := 0; i < n; i++ {
		id := "lc-s" + pad(i)
		if err := store.CreateIssue(ctx, &types.Issue{ID: id, Title: id, Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask}, "tester"); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
		if err := store.AddDependency(ctx, &types.Dependency{IssueID: id, DependsOnID: "lc-target", Type: types.DepBlocks}, "tester"); err != nil {
			t.Fatalf("add dep %s: %v", id, err)
		}
	}

	// limit <= 0 => issueops.defaultDependentRecordsLimit (100).
	def, err := store.GetDependentRecords(ctx, "lc-target", "", 0, "")
	if err != nil {
		t.Fatalf("GetDependentRecords(limit=0): %v", err)
	}
	if len(def) != 100 {
		t.Fatalf("default clamp: got %d rows, want 100", len(def))
	}
	// A limit above the total returns the total (and is under the 500 cap).
	full, err := store.GetDependentRecords(ctx, "lc-target", "", 100000, "")
	if err != nil {
		t.Fatalf("GetDependentRecords(limit=100000): %v", err)
	}
	if len(full) != n {
		t.Fatalf("clamp with limit>total: got %d rows, want %d", len(full), n)
	}
}

// TestGetDependentRecordsForIssues verifies the BATCHED target-keyed dependents
// read: it keys inbound edges by target across a SET of targets in ONE call,
// spans both dependency tables (durable + wisp sources), returns the FULL dep-type
// set (blocks, waits-for, conditional-blocks, parent-child — no type is dropped,
// unlike GetBlockingInfoForIssues which restricts to blocks/parent-child), keeps
// each row's real dep_type, and never surfaces an edge whose target is not the
// key (a decoy where the id is the SOURCE stays out).
func TestGetDependentRecordsForIssues(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	mk := func(id string, ephemeral bool) {
		iss := &types.Issue{ID: id, Title: id, Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Ephemeral: ephemeral}
		if err := store.CreateIssue(ctx, iss, "tester"); err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}
	mk("bt-x", false)     // target X
	mk("bt-y", false)     // target Y
	mk("bt-blk", false)   // blocks -> X (durable)
	mk("bt-wait", false)  // waits-for -> X (durable)
	mk("bt-cond", true)   // conditional-blocks -> X from a WISP source (wisp_dependencies)
	mk("bt-child", false) // parent-child -> X (durable)
	mk("bt-yblk", false)  // blocks -> Y (durable)
	mk("bt-z", false)     // decoy target: X -> Z (X is the SOURCE)

	addDep := func(src, tgt string, typ types.DependencyType) {
		if err := store.AddDependency(ctx, &types.Dependency{IssueID: src, DependsOnID: tgt, Type: typ}, "tester"); err != nil {
			t.Fatalf("add dep %s -> %s (%s): %v", src, tgt, typ, err)
		}
	}
	addDep("bt-blk", "bt-x", types.DepBlocks)
	addDep("bt-wait", "bt-x", types.DepWaitsFor)
	addDep("bt-cond", "bt-x", types.DepConditionalBlocks) // wisp source -> wisp_dependencies
	addDep("bt-child", "bt-x", types.DepParentChild)
	addDep("bt-yblk", "bt-y", types.DepBlocks)
	addDep("bt-x", "bt-z", types.DepBlocks) // decoy: X is the SOURCE here

	typesBySrc := func(deps []*types.Dependency, target string) map[string]types.DependencyType {
		out := map[string]types.DependencyType{}
		for _, d := range deps {
			if d.DependsOnID != target {
				t.Fatalf("row keyed under %s has target %s (must equal the key)", target, d.DependsOnID)
			}
			if d.ID == "" {
				t.Fatalf("dependent row has empty ID (the row primary key): %+v", d)
			}
			out[d.IssueID] = d.Type
		}
		return out
	}

	byTarget, err := store.GetDependentRecordsForIssues(ctx, []string{"bt-x", "bt-y"})
	if err != nil {
		t.Fatalf("GetDependentRecordsForIssues: %v", err)
	}

	// X: all four inbound edges, keyed by target, spanning both tables, with the
	// FULL type set and real dep_type preserved (no drop of waits-for/conditional-
	// blocks, no drop of the wisp-source or the parent-child edge).
	x := typesBySrc(byTarget["bt-x"], "bt-x")
	if len(x) != 4 {
		t.Fatalf("X dependents = %v, want 4 (blocks, waits-for, conditional-blocks[wisp], parent-child)", x)
	}
	if x["bt-blk"] != types.DepBlocks || x["bt-wait"] != types.DepWaitsFor ||
		x["bt-cond"] != types.DepConditionalBlocks || x["bt-child"] != types.DepParentChild {
		t.Fatalf("X dependents lost a real dep_type or dropped a wisp/blocking edge: %v", x)
	}
	if _, bad := x["bt-z"]; bad {
		t.Fatalf("decoy edge X->Z surfaced as an inbound edge of X: %v", x)
	}

	// Y: its single inbound blocks edge, keyed separately in the same batch.
	y := typesBySrc(byTarget["bt-y"], "bt-y")
	if len(y) != 1 || y["bt-yblk"] != types.DepBlocks {
		t.Fatalf("Y dependents = %v, want {bt-yblk: blocks}", y)
	}

	// A batch element with no inbound edges is simply absent from the map (bt-blk
	// only has an OUTGOING edge), never an error or a phantom key.
	leaf, err := store.GetDependentRecordsForIssues(ctx, []string{"bt-blk"})
	if err != nil {
		t.Fatalf("GetDependentRecordsForIssues(leaf): %v", err)
	}
	if got, present := leaf["bt-blk"]; present {
		t.Fatalf("source-only node bt-blk has inbound edges %v, want absent", got)
	}

	// Empty input is a valid empty map, not an error.
	if got, err := store.GetDependentRecordsForIssues(ctx, nil); err != nil {
		t.Fatalf("GetDependentRecordsForIssues(nil): %v", err)
	} else if len(got) != 0 {
		t.Fatalf("GetDependentRecordsForIssues(nil) = %v, want empty", got)
	}
}

// pad renders i as a zero-padded 4-digit string for stable id construction.
func pad(i int) string {
	s := ""
	for _, d := range []int{1000, 100, 10, 1} {
		s += string(rune('0' + (i/d)%10))
	}
	return s
}

// seedTwoChildEpic creates epic mr-epic with parent-child children mr-c01
// (closed) and mr-c02 (open), all durable, so both edges land in
// `dependencies`.
func seedTwoChildEpic(t *testing.T, store *DoltStore) {
	t.Helper()
	ctx, cancel := testContext(t)
	defer cancel()

	for _, iss := range []*types.Issue{
		{ID: "mr-epic", Title: "epic", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeEpic},
		{ID: "mr-c01", Title: "child 1", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "mr-c02", Title: "child 2", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
	} {
		if err := store.CreateIssue(ctx, iss, "tester"); err != nil {
			t.Fatalf("create %s: %v", iss.ID, err)
		}
	}
	for _, child := range []string{"mr-c01", "mr-c02"} {
		if err := store.AddDependency(ctx, &types.Dependency{IssueID: child, DependsOnID: "mr-epic", Type: types.DepParentChild}, "tester"); err != nil {
			t.Fatalf("add parent-child %s: %v", child, err)
		}
	}
	if err := store.CloseIssue(ctx, "mr-c01", "done", "tester", ""); err != nil {
		t.Fatalf("close mr-c01: %v", err)
	}
}

// requireEpicChildren asserts got, the dependents read names, is exactly mr-c01
// and mr-c02, each once and parent-child, with closed the one closed child: the
// rows `bd show` renders as CHILDREN with 1/2 progress. A table read has no
// ORDER BY, and the two children can share a created_at, so the check is
// set-based.
func requireEpicChildren(t *testing.T, read string, got []*types.IssueWithDependencyMetadata, closed string) {
	t.Helper()
	if len(got) != 2 {
		t.Fatalf("%s = %d rows, want 2 (one per child)", read, len(got))
	}
	seen := map[string]*types.IssueWithDependencyMetadata{}
	for _, d := range got {
		seen[d.ID] = d
	}
	c1, c2 := seen["mr-c01"], seen["mr-c02"]
	if c1 == nil || c2 == nil {
		t.Fatalf("dependents = %v, want mr-c01 and mr-c02 once each", seen)
	}
	for _, d := range []*types.IssueWithDependencyMetadata{c1, c2} {
		if d.DependencyType != types.DepParentChild {
			t.Fatalf("dependent %s type = %s, want parent-child", d.ID, d.DependencyType)
		}
		if (d.Status == types.StatusClosed) != (d.ID == closed) {
			t.Fatalf("child status mr-c01=%s mr-c02=%s, want 1/2 complete with %s closed", c1.Status, c2.Status, closed)
		}
	}
}

// requireEdgeReadsOnce asserts the other three both-table readers read each
// edge once: mr-epic's dependents by id, and mr-c01's single outbound edge
// (to its epic) by id and with metadata. DoltStore.GetDependenciesWithMetadata
// reads one plane, so the issueops reader is driven directly.
func requireEdgeReadsOnce(t *testing.T, store *DoltStore, wantDependents int) {
	t.Helper()
	ctx, cancel := testContext(t)
	defer cancel()

	if got, err := store.GetDependents(ctx, "mr-epic"); err != nil {
		t.Fatalf("GetDependents(mr-epic): %v", err)
	} else if len(got) != wantDependents {
		t.Fatalf("GetDependents(mr-epic) = %d, want %d", len(got), wantDependents)
	}
	if got, err := store.GetDependencies(ctx, "mr-c01"); err != nil {
		t.Fatalf("GetDependencies(mr-c01): %v", err)
	} else if len(got) != 1 || got[0].ID != "mr-epic" {
		t.Fatalf("GetDependencies(mr-c01) = %d, want [mr-epic]", len(got))
	}
	var withMeta []*types.IssueWithDependencyMetadata
	if err := store.withReadTx(ctx, func(tx *sql.Tx) error {
		var err error
		withMeta, err = issueops.GetDependenciesWithMetadataInTx(ctx, tx, "mr-c01")
		return err
	}); err != nil {
		t.Fatalf("GetDependenciesWithMetadataInTx(mr-c01): %v", err)
	}
	if len(withMeta) != 1 || withMeta[0].ID != "mr-epic" || withMeta[0].DependencyType != types.DepParentChild {
		t.Fatalf("GetDependenciesWithMetadataInTx(mr-c01) = %d rows, want [mr-epic/parent-child]", len(withMeta))
	}
}

// TestDependentsReadWithoutWispDependenciesTable reproduces GH#3445's first
// state: in server mode the dolt-ignored wisp_dependencies can be absent from
// the working set, and the dependents read must treat it as empty rather than
// fail, or `bd show <epic>` (which reads dependents best-effort) drops CHILDREN.
// The counts must answer too: --json publishes dependent_count 0 when the count
// fails.
func TestDependentsReadWithoutWispDependenciesTable(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	seedTwoChildEpic(t, store)
	if _, err := store.db.ExecContext(ctx, "DROP TABLE wisp_dependencies"); err != nil {
		t.Fatalf("drop wisp_dependencies: %v", err)
	}

	got, err := store.GetDependentsWithMetadata(ctx, "mr-epic")
	if err != nil {
		t.Fatalf("GetDependentsWithMetadata with wisp_dependencies missing: %v", err)
	}
	requireEpicChildren(t, "GetDependentsWithMetadata(mr-epic)", got, "mr-c01")
	requireEdgeReadsOnce(t, store, 2)
	requireEdgeCounts(t, store, 2)
	requireDetailDependents(t, store, "", "mr-c01")
}

// mirrorIntoWispDependencies copies the durable edge src -> target into
// wisp_dependencies under the same depid with the given type: the state a wisp
// promoted to durable, or two merged clones, leave behind. FK checks are
// relaxed because src is not a wisp.
func mirrorIntoWispDependencies(t *testing.T, store *DoltStore, src, target string, depType types.DependencyType) {
	t.Helper()
	ctx, cancel := testContext(t)
	defer cancel()

	if _, err := store.db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 0"); err != nil {
		t.Fatalf("disable FK checks: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		"INSERT INTO wisp_dependencies (id, issue_id, depends_on_issue_id, type, created_by) VALUES (?, ?, ?, ?, 'tester')",
		depid.New(src, target), src, target, string(depType)); err != nil {
		t.Fatalf("mirror %s -> %s: %v", src, target, err)
	}
	if _, err := store.db.ExecContext(ctx, "SET FOREIGN_KEY_CHECKS = 1"); err != nil {
		t.Fatalf("re-enable FK checks: %v", err)
	}
}

// requireEdgeCounts asserts CountDependents(mr-epic) and
// CountDependencies(mr-c01) equal the listings above. `bd show` prints
// count - len(listing) as edges "whose far end has no row", and --json
// publishes the counts, so a count that sums a mirrored edge twice reports a
// phantom unresolvable dependent.
func requireEdgeCounts(t *testing.T, store *DoltStore, wantDependents int64) {
	t.Helper()
	ctx, cancel := testContext(t)
	defer cancel()

	if n, err := store.CountDependents(ctx, "mr-epic"); err != nil {
		t.Fatalf("CountDependents(mr-epic): %v", err)
	} else if n != wantDependents {
		t.Fatalf("CountDependents(mr-epic) = %d, want %d (each far end once)", n, wantDependents)
	}
	if n, err := store.CountDependencies(ctx, "mr-c01"); err != nil {
		t.Fatalf("CountDependencies(mr-c01): %v", err)
	} else if n != 1 {
		t.Fatalf("CountDependencies(mr-c01) = %d, want 1 (its epic once)", n)
	}
}

// TestDependentsReadEachMirroredEdgeOnce reproduces GH#3445's third state: an
// edge stored in both dependency tables is one edge, so `bd show <epic>` lists
// each child once (not twice, which also doubled the progress rollup), with the
// durable row's type when the mirror's type conflicts, and counts it once.
func TestDependentsReadEachMirroredEdgeOnce(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	seedTwoChildEpic(t, store)
	mirrorIntoWispDependencies(t, store, "mr-c01", "mr-epic", types.DepBlocks)
	mirrorIntoWispDependencies(t, store, "mr-c02", "mr-epic", types.DepParentChild)

	got, err := store.GetDependentsWithMetadata(ctx, "mr-epic")
	if err != nil {
		t.Fatalf("GetDependentsWithMetadata: %v", err)
	}
	requireEpicChildren(t, "GetDependentsWithMetadata(mr-epic)", got, "mr-c01")
	requireEdgeReadsOnce(t, store, 2)
	requireEdgeCounts(t, store, 2)
	requireDetailDependents(t, store, "", "mr-c01")

	// A genuine wisp-only dependent (its edge lives only in wisp_dependencies)
	// is still read, after the durable rows.
	wisp := &types.Issue{ID: "mr-w", Title: "wisp", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Ephemeral: true}
	if err := store.CreateIssue(ctx, wisp, "tester"); err != nil {
		t.Fatalf("create mr-w: %v", err)
	}
	if err := store.AddDependency(ctx, &types.Dependency{IssueID: "mr-w", DependsOnID: "mr-epic", Type: types.DepBlocks}, "tester"); err != nil {
		t.Fatalf("add mr-w -> mr-epic: %v", err)
	}
	got, err = store.GetDependentsWithMetadata(ctx, "mr-epic")
	if err != nil {
		t.Fatalf("GetDependentsWithMetadata with a wisp dependent: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("GetDependentsWithMetadata(mr-epic) = %d rows, want 3 (2 children + mr-w)", len(got))
	}
	if last := got[2]; last.ID != "mr-w" || last.DependencyType != types.DepBlocks {
		t.Fatalf("last dependent = %s/%s, want the wisp-only mr-w/blocks after the durable rows", last.ID, last.DependencyType)
	}
	requireEpicChildren(t, "GetDependentsWithMetadata(mr-epic)", got[:2], "mr-c01")
	requireEdgeReadsOnce(t, store, 3)
	requireEdgeCounts(t, store, 3)
	requireDetailDependents(t, store, "mr-w", "mr-c01")
}

// requireDetailDependents asserts what `bd show --json --include-dependents`
// and `bd serve`'s include_dependents publish for mr-epic. That detail view
// streams its dependents through DoltStore.IterDependentsWithMetadata, not the
// slice path the other helpers drive, so it must list mr-c01 and mr-c02 once
// each, as requireEpicChildren checks them with closed the closed child, then
// wantWisp last when it is set. The dependent count must equal that list, so
// no edge is announced as unresolvable, and the epic rollup must be 1/2.
func requireDetailDependents(t *testing.T, store *DoltStore, wantWisp, closed string) {
	t.Helper()
	ctx, cancel := testContext(t)
	defer cancel()

	reader, err := store.IssueReader()
	if err != nil {
		t.Fatalf("IssueReader(): %v", err)
	}
	details, err := reader.Get(ctx, publicops.GetRequest{ID: "mr-epic", IncludeDependents: true})
	if err != nil {
		t.Fatalf("Get(mr-epic, IncludeDependents): %v", err)
	}
	children := details.Dependents
	if wantWisp != "" {
		if len(children) != 3 || children[2].ID != wantWisp {
			t.Fatalf("Get(mr-epic, IncludeDependents) dependents = %v, want mr-c01 and mr-c02, then %s", dependentIDs(children), wantWisp)
		}
		children = children[:2]
	}
	requireEpicChildren(t, "Get(mr-epic, IncludeDependents) dependents", children, closed)
	if details.DependentCount == nil || *details.DependentCount != int64(len(details.Dependents)) {
		t.Fatalf("dependent_count = %v, want %d (one per listed dependent)", details.DependentCount, len(details.Dependents))
	}
	if details.UnresolvableDependents != nil {
		t.Fatalf("unresolvable_dependents = %d, want none", *details.UnresolvableDependents)
	}
	if details.EpicTotalChildren == nil || details.EpicClosedChildren == nil ||
		*details.EpicTotalChildren != 2 || *details.EpicClosedChildren != 1 {
		t.Fatalf("epic rollup = %v/%v closed/total, want 1/2", details.EpicClosedChildren, details.EpicTotalChildren)
	}
}

func dependentIDs(deps []*types.IssueWithDependencyMetadata) []string {
	ids := make([]string, len(deps))
	for i, d := range deps {
		ids[i] = d.ID
	}
	return ids
}

// dependentRow is what the slice and the streaming dependents reads must agree
// on for one dependent: its edge type, and the fields of the issue row it was
// read from that the detail view and the epic rollup publish.
type dependentRow struct {
	depType     types.DependencyType
	status      types.Status
	title       string
	ephemeral   bool
	closeReason string
	closedAt    string
}

func dependentRowOf(d *types.IssueWithDependencyMetadata) dependentRow {
	row := dependentRow{
		depType:     d.DependencyType,
		status:      d.Status,
		title:       d.Title,
		ephemeral:   d.Ephemeral,
		closeReason: d.CloseReason,
	}
	if d.ClosedAt != nil {
		row.closedAt = d.ClosedAt.UTC().Format(time.RFC3339Nano)
	}
	return row
}

// requireStreamMatchesSlice asserts DoltStore.IterDependentsWithMetadata, the
// detail view's read, streams the dependents of mr-epic the slice path
// returned in slice, each once, with the same type and from the same issue row.
// The two reads order the dependents differently, so the check is by id.
func requireStreamMatchesSlice(t *testing.T, store *DoltStore, slice []*types.IssueWithDependencyMetadata) {
	t.Helper()
	ctx, cancel := testContext(t)
	defer cancel()

	want := make(map[string]dependentRow, len(slice))
	for _, d := range slice {
		want[d.ID] = dependentRowOf(d)
	}
	iter, err := store.IterDependentsWithMetadata(ctx, "mr-epic")
	if err != nil {
		t.Fatalf("IterDependentsWithMetadata(mr-epic): %v", err)
	}
	defer iter.Close() //nolint:errcheck // read-only iterator
	got := make(map[string]dependentRow, len(slice))
	for iter.Next(ctx) {
		d := iter.Value()
		if _, dup := got[d.ID]; dup {
			t.Fatalf("IterDependentsWithMetadata(mr-epic) streamed %s twice", d.ID)
		}
		got[d.ID] = dependentRowOf(d)
	}
	if err := iter.Err(); err != nil {
		t.Fatalf("IterDependentsWithMetadata(mr-epic): %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IterDependentsWithMetadata(mr-epic) = %+v, want the slice path's %+v", got, want)
	}
}

// TestDependentsStreamADualResidentChildOnce reproduces the mirror on the state
// the detail view's streaming read can see: both children are also resident in
// `wisps`, the replication residual publicops.GetRequest documents, with their
// edges in both dependency tables. The stream's wisp query joins the wisp
// copies, so before the stream de-duplicated, `bd show --json
// --include-dependents` listed mr-c01 a second time (as blocks) and mr-c02 a
// second time, which doubled it in the rollup. Each child must stream once,
// with its durable type, agreeing with the slice path and the count.
//
// The slice path reads an id resident in both planes from `wisps`
// (GetIssuesByIDsInTx), so the stream must stream that row too. The wisp
// copies therefore differ from the durable rows: their titles name the copy,
// and the closed child is mr-c02, not mr-c01. Text `bd show` and the detail
// view then report the same child closed.
func TestDependentsStreamADualResidentChildOnce(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	seedTwoChildEpic(t, store)
	closedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	for _, child := range []struct {
		id          string
		status      types.Status
		closeReason string
		closedAt    *time.Time
	}{{"mr-c01", types.StatusOpen, "", nil}, {"mr-c02", types.StatusClosed, "done in the wisp", &closedAt}} {
		if _, err := store.db.ExecContext(ctx,
			"INSERT INTO wisps (id, title, description, design, acceptance_criteria, notes, status, priority, issue_type, ephemeral, close_reason, closed_at) VALUES (?, ?, '', '', '', '', ?, 2, ?, 1, ?, ?)",
			child.id, child.id+" wisp copy", child.status, types.TypeTask, child.closeReason, child.closedAt); err != nil {
			t.Fatalf("seed wisp copy of %s: %v", child.id, err)
		}
	}
	mirrorIntoWispDependencies(t, store, "mr-c01", "mr-epic", types.DepBlocks)
	mirrorIntoWispDependencies(t, store, "mr-c02", "mr-epic", types.DepParentChild)

	got, err := store.GetDependentsWithMetadata(ctx, "mr-epic")
	if err != nil {
		t.Fatalf("GetDependentsWithMetadata: %v", err)
	}
	requireEpicChildren(t, "GetDependentsWithMetadata(mr-epic)", got, "mr-c02")
	for _, d := range got {
		if d.Title != d.ID+" wisp copy" || !d.Ephemeral {
			t.Fatalf("GetDependentsWithMetadata(mr-epic) read %s as %q (ephemeral=%v), want its wisp copy", d.ID, d.Title, d.Ephemeral)
		}
	}
	requireStreamMatchesSlice(t, store, got)
	requireEdgeCounts(t, store, 2)
	requireDetailDependents(t, store, "", "mr-c02")
}

// TestDependentsStreamAChildLinkedOnlyByItsMirror pins the other out-of-step
// join. On GH#3445's third state, `bd dep remove` of a durable child deletes
// only its `dependencies` row (RemoveDependency routes by the source's plane),
// so the child's edge survives as its wisp_dependencies mirror. The slice
// path lists the child, read from `issues`, with the mirror's type, and the
// count includes it, so the stream must list it too. Otherwise the detail view
// reports it as an edge whose far end has no row.
func TestDependentsStreamAChildLinkedOnlyByItsMirror(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	seedTwoChildEpic(t, store)
	mirrorIntoWispDependencies(t, store, "mr-c02", "mr-epic", types.DepParentChild)
	if err := store.RemoveDependency(ctx, "mr-c02", "mr-epic", "tester"); err != nil {
		t.Fatalf("remove mr-c02 -> mr-epic: %v", err)
	}

	got, err := store.GetDependentsWithMetadata(ctx, "mr-epic")
	if err != nil {
		t.Fatalf("GetDependentsWithMetadata: %v", err)
	}
	requireEpicChildren(t, "GetDependentsWithMetadata(mr-epic)", got, "mr-c01")
	requireStreamMatchesSlice(t, store, got)
	requireEdgeCounts(t, store, 2)
	requireDetailDependents(t, store, "", "mr-c01")
}

// iterDependentRows answers one IterDependentsWithMetadata query: each
// (id, type) pair becomes a minimal open issue row for the dependent, titled
// with the issue table the query reads, so a test can tell which join a
// streamed dependent came from, followed by its edge type.
func iterDependentRows(issueTable string, ends ...[2]string) *sqlmock.Rows {
	cols := splitCommaList(prefixedIssueColumns("i"))
	rows := sqlmock.NewRows(append(append([]string(nil), cols...), "type"))
	for _, end := range ends {
		values := make([]driver.Value, 0, len(cols)+1)
		for _, col := range cols {
			switch col[strings.LastIndex(col, ".")+1:] {
			case "id":
				values = append(values, end[0])
			case "title":
				values = append(values, issueTable)
			case "description", "design", "acceptance_criteria", "notes":
				values = append(values, "")
			case "status":
				values = append(values, string(types.StatusOpen))
			case "priority":
				values = append(values, 2)
			case "issue_type":
				values = append(values, string(types.TypeTask))
			case "compaction_level":
				values = append(values, 0)
			default:
				values = append(values, nil)
			}
		}
		rows.AddRow(append(values, end[1])...)
	}
	return rows
}

// TestIterDependentsWithMetadataReadsBothTables pins the four joins behind the
// streaming dependents read, which the Dolt-backed tests above cover only on a
// server. They run in one read transaction: `dependencies` before
// `wisp_dependencies`, and for each, `wisps` before `issues`. A dependent
// streams once, with the first row returned for it, so it keeps its durable
// type, and its wisps row when its id is resident in both planes, as the slice
// path reads it. A missing wisps or wisp_dependencies table means no rows from
// the joins that name it (GH#3445). Any other error fails the read, a missing
// leases table included: it is in every join's FROM clause too, and the
// tolerance is for the optional wisp tables alone.
func TestIterDependentsWithMetadataReadsBothTables(t *testing.T) {
	// The joins in the order they run, each with the issue table it reads.
	joins := []struct{ query, issueTable string }{
		{`FROM wisps i\s+JOIN dependencies d ON d\.issue_id = i\.id`, "wisps"},
		{`FROM issues i\s+JOIN dependencies d ON d\.issue_id = i\.id`, "issues"},
		{`FROM wisps i\s+JOIN wisp_dependencies d ON d\.issue_id = i\.id`, "wisps"},
		{`FROM issues i\s+JOIN wisp_dependencies d ON d\.issue_id = i\.id`, "issues"},
	}
	missingTable := func(table string) error {
		return &mysql.MySQLError{Number: 1146, Message: "table not found: " + table}
	}
	errRead := errors.New("connection reset")
	errMissingLeases := missingTable("leases")
	durable := [][2]string{{"c1", "parent-child"}, {"c2", "parent-child"}}
	// durableRows is how the durable dependents stream: from issues.
	durableRows := [][3]string{{"c1", "parent-child", "issues"}, {"c2", "parent-child", "issues"}}

	type arm struct {
		ends [][2]string
		err  error
	}
	cases := []struct {
		name string
		// arms answer the joins in order. A join after the one that fails the
		// read never runs, so it has no arm.
		arms []arm
		// want is each streamed dependent: its id, its type, and the issue
		// table its row came from.
		want [][3]string
		// wantCallErr is the call's error when the first join fails the read;
		// wantErr is the error Err returns after the rows in want otherwise.
		wantCallErr, wantErr error
	}{
		{
			name: "a dependent streams once, with its durable type and its wisps row",
			arms: []arm{
				{ends: [][2]string{{"c1", "parent-child"}}},
				{ends: durable},
				{ends: [][2]string{{"c1", "blocks"}, {"w1", "blocks"}}},
				{ends: [][2]string{{"c1", "blocks"}, {"c2", "related"}, {"i1", "related"}}},
			},
			want: [][3]string{
				{"c1", "parent-child", "wisps"},
				{"c2", "parent-child", "issues"},
				{"w1", "blocks", "wisps"},
				{"i1", "related", "issues"},
			},
		},
		{
			name: "missing wisp_dependencies reads as empty",
			arms: []arm{
				{},
				{ends: durable},
				{err: missingTable("wisp_dependencies")},
				{err: missingTable("wisp_dependencies")},
			},
			want: durableRows,
		},
		{
			name: "missing wisps reads as empty",
			arms: []arm{
				{err: missingTable("wisps")},
				{ends: durable},
				{err: missingTable("wisps")},
				{ends: [][2]string{{"i1", "related"}}},
			},
			want: append(append([][3]string(nil), durableRows...), [3]string{"i1", "related", "issues"}),
		},
		{
			name:        "missing leases fails the call",
			arms:        []arm{{err: errMissingLeases}},
			wantCallErr: errMissingLeases,
		},
		{
			name: "a later join's error fails the read",
			arms: []arm{
				{},
				{ends: durable},
				{ends: [][2]string{{"w1", "blocks"}}},
				{err: errRead},
			},
			want:    append(append([][3]string(nil), durableRows...), [3]string{"w1", "blocks", "wisps"}),
			wantErr: errRead,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatalf("sqlmock: %v", err)
			}
			t.Cleanup(func() { _ = db.Close() })
			mock.ExpectBegin()
			for i, a := range tc.arms {
				q := mock.ExpectQuery(joins[i].query).WithArgs("near")
				if a.err != nil {
					q.WillReturnError(a.err)
					continue
				}
				q.WillReturnRows(iterDependentRows(joins[i].issueTable, a.ends...))
			}
			mock.ExpectRollback()

			ctx := context.Background()
			iter, err := (&DoltStore{db: db}).IterDependentsWithMetadata(ctx, "near")
			if tc.wantCallErr != nil {
				if !errors.Is(err, tc.wantCallErr) || iter != nil {
					t.Fatalf("IterDependentsWithMetadata = %v, %v; want no iterator and an error wrapping %v", iter, err, tc.wantCallErr)
				}
			} else {
				if err != nil {
					t.Fatalf("IterDependentsWithMetadata: %v", err)
				}
				var got [][3]string
				for iter.Next(ctx) {
					d := iter.Value()
					got = append(got, [3]string{d.ID, string(d.DependencyType), d.Title})
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("streamed %v, want %v", got, tc.want)
				}
				if err := iter.Err(); !errors.Is(err, tc.wantErr) {
					t.Fatalf("Err() = %v, want %v", err, tc.wantErr)
				}
				_ = iter.Close()
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("unmet SQL expectations: %v", err)
			}
		})
	}
}
