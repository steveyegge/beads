//go:build integration_pg

package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/postgres/testfixture"
	"github.com/steveyegge/beads/internal/types"
)

// startPG returns a per-test PG DSN. It delegates to the shared testfixture
// helper, which spins up postgres:14-alpine via testcontainers-go (or
// honors BEADS_TEST_POSTGRES_DSN when CI provides a service container) and
// creates a fresh `bd_test_<rand>` database with DROP-on-cleanup.
//
// The returned closure is now a no-op — fixture cleanup is registered
// directly with t.Cleanup by ForTest. Callers may still call it to keep
// pre-helper call sites readable.
func startPG(t *testing.T) (string, func()) {
	t.Helper()
	return testfixture.ForTest(t), func() {}
}

// TestSmokePath runs the bd command-equivalent sequence end-to-end against PG
// using internal/storage/postgres directly. Mirrors the acceptance criterion
// in bead be-6fk.3 / ADR be-l7t.3.
func TestSmokePath(t *testing.T) {
	dsn, stop := startPG(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	// init: set the issue prefix.
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("set issue_prefix: %v", err)
	}

	// create #1
	parent := &types.Issue{
		Title:     "Parent",
		IssueType: types.TypeTask,
		Priority:  1,
		Status:    types.StatusOpen,
	}
	if err := store.CreateIssue(ctx, parent, "tester"); err != nil {
		t.Fatalf("create parent: %v", err)
	}
	if parent.ID == "" {
		t.Fatal("parent ID was not assigned")
	}

	// create #2
	child := &types.Issue{
		Title:     "Child",
		IssueType: types.TypeTask,
		Priority:  2,
		Status:    types.StatusOpen,
	}
	if err := store.CreateIssue(ctx, child, "tester"); err != nil {
		t.Fatalf("create child: %v", err)
	}

	// dep add: parent blocks child
	dep := &types.Dependency{
		IssueID:     child.ID,
		DependsOnID: parent.ID,
		Type:        types.DepBlocks,
	}
	if err := store.AddDependency(ctx, dep, "tester"); err != nil {
		t.Fatalf("add dependency: %v", err)
	}

	// ready: parent should be ready, child should not.
	ready, err := store.GetReadyWork(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("get ready work: %v", err)
	}
	readyIDs := map[string]bool{}
	for _, r := range ready {
		readyIDs[r.ID] = true
	}
	if !readyIDs[parent.ID] {
		t.Errorf("expected parent %s in ready, got %v", parent.ID, readyIDs)
	}
	if readyIDs[child.ID] {
		t.Errorf("did NOT expect blocked child %s in ready", child.ID)
	}

	// update --claim
	if err := store.ClaimIssue(ctx, parent.ID, "tester"); err != nil {
		t.Fatalf("claim parent: %v", err)
	}
	got, err := store.GetIssue(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get after claim: %v", err)
	}
	if got.Status != types.StatusInProgress {
		t.Errorf("expected status in_progress after claim, got %s", got.Status)
	}
	if got.Assignee != "tester" {
		t.Errorf("expected assignee tester after claim, got %q", got.Assignee)
	}

	// claim contention: a different actor must be rejected
	if err := store.ClaimIssue(ctx, parent.ID, "interloper"); !errors.Is(err, storage.ErrAlreadyClaimed) {
		t.Errorf("expected ErrAlreadyClaimed, got %v", err)
	}

	// comment add
	c, err := store.AddIssueComment(ctx, parent.ID, "tester", "first comment")
	if err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if c.ID == "" {
		t.Error("comment ID was not assigned")
	}
	comments, err := store.GetIssueComments(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get comments: %v", err)
	}
	if len(comments) != 1 || comments[0].Text != "first comment" {
		t.Errorf("expected one comment 'first comment', got %v", comments)
	}

	// close
	if err := store.CloseIssue(ctx, parent.ID, "done", "tester", "session-1"); err != nil {
		t.Fatalf("close parent: %v", err)
	}

	// child should now appear in ready
	ready, err = store.GetReadyWork(ctx, types.WorkFilter{})
	if err != nil {
		t.Fatalf("get ready work after close: %v", err)
	}
	readyIDs = map[string]bool{}
	for _, r := range ready {
		readyIDs[r.ID] = true
	}
	if !readyIDs[child.ID] {
		t.Errorf("expected child %s in ready after parent close, got %v", child.ID, readyIDs)
	}

	// export-equivalent reads
	if _, err := store.GetAllConfig(ctx); err != nil {
		t.Errorf("get all config: %v", err)
	}
	got, err = store.GetIssue(ctx, parent.ID)
	if err != nil {
		t.Fatalf("get parent post-close: %v", err)
	}
	if got.Status != types.StatusClosed {
		t.Errorf("expected closed parent, got %s", got.Status)
	}
	deps, err := store.GetDependencies(ctx, child.ID)
	if err != nil {
		t.Fatalf("get child deps: %v", err)
	}
	if len(deps) != 1 || deps[0].ID != parent.ID {
		t.Errorf("expected child dep on %s, got %v", parent.ID, deps)
	}
	events, err := store.GetEvents(ctx, parent.ID, 10)
	if err != nil {
		t.Fatalf("get events: %v", err)
	}
	if len(events) == 0 {
		t.Error("expected at least one audit event for parent")
	}
}

// TestConcurrentReads opens N=20 goroutines and confirms the connection
// pool serves them without errors. The architect's NFR-2 budget owns the
// hard quantitative threshold; here we just verify nothing blows up under
// realistic concurrency.
func TestConcurrentReads(t *testing.T) {
	dsn, stop := startPG(t)
	defer stop()

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
	seed := &types.Issue{Title: "Seed", IssueType: types.TypeTask, Priority: 2, Status: types.StatusOpen}
	if err := store.CreateIssue(ctx, seed, "tester"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	const fanout = 20
	errs := make(chan error, fanout)
	for i := 0; i < fanout; i++ {
		go func() {
			if _, err := store.GetIssue(ctx, seed.ID); err != nil {
				errs <- err
				return
			}
			if _, err := store.GetReadyWork(ctx, types.WorkFilter{}); err != nil {
				errs <- err
				return
			}
			errs <- nil
		}()
	}
	for i := 0; i < fanout; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent reader %d: %v", i, err)
		}
	}
}

// TestRoundtripIdempotency ensures rerunning openStore against an existing
// migration set is a no-op (advisory lock + bd_schema_migrations bookkeeping).
func TestRoundtripIdempotency(t *testing.T) {
	dsn, stop := startPG(t)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for i := 0; i < 3; i++ {
		store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
		if err != nil {
			t.Fatalf("attempt %d: open: %v", i, err)
		}
		store.Close()
	}
}

// TestAddCommentWritesEvent regression-tests be-b8p Finding #1: prior to the
// fix, AddIssueComment used s.pool.QueryRow directly without opening a
// transaction or recording an EventCommented row. The fix wraps the insert
// in RunInTransaction and writes the event from inside the same tx, so a
// rollback in either step rolls back both. We assert the event lands by
// counting events of type EventCommented before/after a single AddComment.
func TestAddCommentWritesEvent(t *testing.T) {
	dsn, stop := startPG(t)
	defer stop()

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

	issue := &types.Issue{Title: "for comment test", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2}
	if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
		t.Fatalf("create: %v", err)
	}

	commentedBefore := countEventsOfType(t, ctx, store, issue.ID, types.EventCommented)
	if _, err := store.AddIssueComment(ctx, issue.ID, "tester", "audited"); err != nil {
		t.Fatalf("add issue comment: %v", err)
	}
	commentedAfter := countEventsOfType(t, ctx, store, issue.ID, types.EventCommented)
	if commentedAfter != commentedBefore+1 {
		t.Errorf("expected one new EventCommented row, before=%d after=%d", commentedBefore, commentedAfter)
	}

	if err := store.AddComment(ctx, issue.ID, "tester", "second"); err != nil {
		t.Fatalf("add comment: %v", err)
	}
	if c := countEventsOfType(t, ctx, store, issue.ID, types.EventCommented); c != commentedAfter+1 {
		t.Errorf("AnnotationStore.AddComment did not write event, got %d expected %d", c, commentedAfter+1)
	}
}

func countEventsOfType(t *testing.T, ctx context.Context, store *PostgresStore, issueID string, kind types.EventType) int {
	t.Helper()
	var n int
	err := store.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM events WHERE issue_id = $1 AND event_type = $2`,
		issueID, string(kind)).Scan(&n)
	if err != nil {
		t.Fatalf("count events: %v", err)
	}
	return n
}

// TestConnectionUsesUTC asserts ADR be-l7t.3 §3.5: every new pool connection
// has its session timezone set to UTC by AfterConnect, so NOW() and
// CURRENT_TIMESTAMP write UTC into our timezone-naive TIMESTAMP columns and
// round-trip the same value across processes regardless of the host's
// configured TZ.
func TestConnectionUsesUTC(t *testing.T) {
	dsn, stop := startPG(t)
	defer stop()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()

	var tz string
	if err := store.pool.QueryRow(ctx, "SHOW TIME ZONE").Scan(&tz); err != nil {
		t.Fatalf("show time zone: %v", err)
	}
	if tz != "UTC" {
		t.Errorf("expected session timezone UTC, got %q", tz)
	}
}

// TestSearchIssuesFilters is the regression guard for be-ucslk4. Six
// WHERE-clause cases (Labels, LabelsAny, ExcludeLabels, LabelPattern,
// LabelRegex, TitleContains) had previously been silent no-ops on the PG
// path; the fix at 1f12523af wired them through PostgresStore.SearchIssues.
// Each subtest seeds the same corpus and asserts BOTH inclusion and
// exclusion — the prior bug would have passed any inclusion-only assertion
// because the filter was dropped and the full queue was returned.
//
// Bead: be-8skfsh.
func TestSearchIssuesFilters(t *testing.T) {
	dsn, stop := startPG(t)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("set issue_prefix: %v", err)
	}

	// Corpus: 6 beads with mixed labels/titles. Designed so each filter has
	// at least one matching and one non-matching bead, and so the OR-vs-AND
	// distinction is observable for LabelsAny (ids[0] has only backend,
	// ids[1] has only frontend, ids[2] has both — an AND interpretation
	// would surface only ids[2]).
	type seed struct {
		title  string
		labels []string
	}
	corpus := []seed{
		{"alpha with foo bar", []string{"backend"}},               // 0
		{"beta has FoOd inside", []string{"frontend"}},            // 1
		{"gamma neutral", []string{"backend", "frontend"}},        // 2
		{"delta back-end test", []string{"back-end", "back-log"}}, // 3
		{"epsilon random", []string{"random"}},                    // 4
		{"zeta no labels", nil},                                   // 5
	}
	ids := make([]string, len(corpus))
	for i, s := range corpus {
		issue := &types.Issue{
			Title:     s.title,
			IssueType: types.TypeTask,
			Status:    types.StatusOpen,
			Priority:  2,
			Labels:    s.labels,
		}
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if issue.ID == "" {
			t.Fatalf("issue %d: ID was not assigned", i)
		}
		ids[i] = issue.ID
	}

	runSearch := func(t *testing.T, filter types.IssueFilter) map[string]bool {
		t.Helper()
		got, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			t.Fatalf("SearchIssues: %v", err)
		}
		out := make(map[string]bool, len(got))
		for _, issue := range got {
			out[issue.ID] = true
		}
		return out
	}

	assertSet := func(t *testing.T, name string, got map[string]bool, want, notWant []string) {
		t.Helper()
		for _, id := range want {
			if !got[id] {
				t.Errorf("%s: expected %s in result, got %v", name, id, sortedKeys(got))
			}
		}
		for _, id := range notWant {
			if got[id] {
				t.Errorf("%s: did NOT expect %s in result, got %v", name, id, sortedKeys(got))
			}
		}
	}

	t.Run("Labels_single", func(t *testing.T) {
		// --label=backend: only beads with the backend label.
		got := runSearch(t, types.IssueFilter{Labels: []string{"backend"}})
		assertSet(t, "Labels=[backend]", got,
			[]string{ids[0], ids[2]},
			[]string{ids[1], ids[3], ids[4], ids[5]},
		)
	})

	t.Run("LabelsAny_OR", func(t *testing.T) {
		// --label-any=backend,frontend: OR semantics. ids[0] has only
		// backend, ids[1] has only frontend, ids[2] has both — all three
		// must appear. ids[3..5] have neither and must be excluded.
		got := runSearch(t, types.IssueFilter{LabelsAny: []string{"backend", "frontend"}})
		assertSet(t, "LabelsAny=[backend,frontend]", got,
			[]string{ids[0], ids[1], ids[2]},
			[]string{ids[3], ids[4], ids[5]},
		)
	})

	t.Run("ExcludeLabels", func(t *testing.T) {
		// --exclude-label=backend: no bead carrying the backend label.
		got := runSearch(t, types.IssueFilter{ExcludeLabels: []string{"backend"}})
		assertSet(t, "ExcludeLabels=[backend]", got,
			[]string{ids[1], ids[3], ids[4], ids[5]},
			[]string{ids[0], ids[2]},
		)
	})

	t.Run("LabelPattern_glob", func(t *testing.T) {
		// --label-pattern='back*': any label starting with back —
		// matches backend, back-end, back-log; rejects frontend, random.
		got := runSearch(t, types.IssueFilter{LabelPattern: "back*"})
		assertSet(t, "LabelPattern=back*", got,
			[]string{ids[0], ids[2], ids[3]},
			[]string{ids[1], ids[4], ids[5]},
		)
	})

	t.Run("LabelRegex_anchored", func(t *testing.T) {
		// --label-regex='^back-(end|log)$': anchored POSIX ERE. Matches
		// the hyphenated back-end and back-log on ids[3]; the unhyphenated
		// "backend" on ids[0]/ids[2] must NOT match the anchored hyphen.
		got := runSearch(t, types.IssueFilter{LabelRegex: "^back-(end|log)$"})
		assertSet(t, "LabelRegex=^back-(end|log)$", got,
			[]string{ids[3]},
			[]string{ids[0], ids[1], ids[2], ids[4], ids[5]},
		)
	})

	t.Run("TitleContains_caseInsensitive", func(t *testing.T) {
		// --title-contains=foo: ILIKE substring. The mixed-case "FoOd"
		// on ids[1] proves the case-insensitivity contract.
		got := runSearch(t, types.IssueFilter{TitleContains: "foo"})
		assertSet(t, "TitleContains=foo", got,
			[]string{ids[0], ids[1]},
			[]string{ids[2], ids[3], ids[4], ids[5]},
		)
	})
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// === be-nl46qf: TestSearchIssuesApplies* ============================
//
// Validator coverage for the 12 new filter-clause branches added to
// PostgresStore.SearchIssues by be-jdeief (commit ef082b72a). The fix
// extends the be-ucslk4 wire-up to the rest of the canonical Dolt
// filter set: NoLabels, ExcludeTypes, IsTemplate, Pinned,
// MetadataFields, HasMetadataKey, ParentID, NoParent, MolType,
// WispType, Deferred + DeferAfter/DeferBefore, DueAfter/DueBefore +
// Overdue, TitleSearch, DescriptionContains, NotesContains,
// ExternalRefContains.
//
// Each test seeds a small focused corpus and asserts BOTH inclusion
// AND exclusion — the pre-fix behavior dropped these filters silently,
// so an inclusion-only assertion would have passed against the
// unfiltered result set. Verified by reverting the SearchIssues block
// to the pre-be-jdeief state: every test fails with the expected
// pattern (excluded IDs surface in the full result set).
//
// Bead: be-nl46qf. Spec source: bd show be-nl46qf and be-jdeief.

// runSearchAssertingSet runs SearchIssues with the given filter and
// asserts the result set contains every ID in want and excludes every
// ID in notWant. The label is included in error messages so failures
// from t.Run subtests are debuggable.
func runSearchAssertingSet(t *testing.T, ctx context.Context, store *PostgresStore, label string, filter types.IssueFilter, want, notWant []string) {
	t.Helper()
	got, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("%s: SearchIssues: %v", label, err)
	}
	have := make(map[string]bool, len(got))
	for _, issue := range got {
		have[issue.ID] = true
	}
	for _, id := range want {
		if !have[id] {
			t.Errorf("%s: expected %s in result, got %v", label, id, sortedKeys(have))
		}
	}
	for _, id := range notWant {
		if have[id] {
			t.Errorf("%s: did NOT expect %s in result, got %v", label, id, sortedKeys(have))
		}
	}
}

// newSearchStore opens a fresh per-test PG store with the issue prefix
// configured. Returns the store + a cancel-aware context to drive
// CreateIssue calls.
func newSearchStore(t *testing.T) (*PostgresStore, context.Context) {
	t.Helper()
	dsn, _ := startPG(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	store, err := openStore(ctx, storage.ConnectionConfig{DSN: dsn})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("set issue_prefix: %v", err)
	}
	return store, ctx
}

// createForSearch persists each issue via CreateIssue and returns the
// generated ID slice. Fails the test with full context on any error so
// callers don't need per-call err checks.
func createForSearch(t *testing.T, ctx context.Context, store *PostgresStore, issues []*types.Issue) []string {
	t.Helper()
	ids := make([]string, len(issues))
	for i, issue := range issues {
		if err := store.CreateIssue(ctx, issue, "tester"); err != nil {
			t.Fatalf("create #%d (%q): %v", i, issue.Title, err)
		}
		if issue.ID == "" {
			t.Fatalf("issue #%d (%q): ID was not assigned", i, issue.Title)
		}
		ids[i] = issue.ID
	}
	return ids
}

// metaJSON marshals a key/value map into the canonical JSON shape that
// the issues.metadata JSONB column expects. Used to set MetadataFields
// / HasMetadataKey corpus state.
func metaJSON(t *testing.T, m map[string]string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("metaJSON: %v", err)
	}
	return b
}

// TestSearchIssuesAppliesNoLabels covers issues.go:449. Filter
// NoLabels=true must surface only beads with zero label rows in the
// labels table.
func TestSearchIssuesAppliesNoLabels(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "labeled", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, Labels: []string{"x"}},
		{Title: "unlabeled-1", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "unlabeled-2", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	runSearchAssertingSet(t, ctx, store, "NoLabels=true",
		types.IssueFilter{NoLabels: true},
		[]string{ids[1], ids[2]},
		[]string{ids[0]},
	)
}

// TestSearchIssuesAppliesExcludeTypes covers issues.go:453. The clause
// renders to issue_type NOT IN (...). Seed one of each built-in type
// the spec touches and exclude bug; the bug must drop while the rest
// surface.
func TestSearchIssuesAppliesExcludeTypes(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "a-bug", IssueType: types.TypeBug, Status: types.StatusOpen, Priority: 2},
		{Title: "a-task", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "a-feature", IssueType: types.TypeFeature, Status: types.StatusOpen, Priority: 2},
	})
	runSearchAssertingSet(t, ctx, store, "ExcludeTypes=[bug]",
		types.IssueFilter{ExcludeTypes: []types.IssueType{types.TypeBug}},
		[]string{ids[1], ids[2]},
		[]string{ids[0]},
	)
}

// TestSearchIssuesAppliesIsTemplate covers issues.go:462. The clause
// uses TRUE/FALSE literals; the negative path is NULL-safe via
// (is_template = FALSE OR is_template IS NULL). Both directions are
// asserted on a shared corpus.
func TestSearchIssuesAppliesIsTemplate(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "template", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, IsTemplate: true},
		{Title: "non-template-1", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "non-template-2", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	tt := true
	ff := false
	t.Run("true", func(t *testing.T) {
		runSearchAssertingSet(t, ctx, store, "IsTemplate=true",
			types.IssueFilter{IsTemplate: &tt},
			[]string{ids[0]},
			[]string{ids[1], ids[2]},
		)
	})
	t.Run("false", func(t *testing.T) {
		runSearchAssertingSet(t, ctx, store, "IsTemplate=false",
			types.IssueFilter{IsTemplate: &ff},
			[]string{ids[1], ids[2]},
			[]string{ids[0]},
		)
	})
}

// TestSearchIssuesAppliesPinned covers issues.go:470. Same TRUE/FALSE
// + NULL-safe negative pattern as IsTemplate; we assert both
// directions to pin the negative branch (was previously a silent
// no-op so a query for unpinned beads would have returned the pinned
// one too).
func TestSearchIssuesAppliesPinned(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "pinned", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, Pinned: true},
		{Title: "unpinned-1", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "unpinned-2", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	tt := true
	ff := false
	t.Run("true", func(t *testing.T) {
		runSearchAssertingSet(t, ctx, store, "Pinned=true",
			types.IssueFilter{Pinned: &tt},
			[]string{ids[0]},
			[]string{ids[1], ids[2]},
		)
	})
	t.Run("false", func(t *testing.T) {
		runSearchAssertingSet(t, ctx, store, "Pinned=false",
			types.IssueFilter{Pinned: &ff},
			[]string{ids[1], ids[2]},
			[]string{ids[0]},
		)
	})
}

// TestSearchIssuesAppliesMetadataFields covers issues.go:478. The
// clause renders one `metadata->>'key' = $N` per entry, joined by AND
// with stable key ordering. Three subtests pin the contract:
//   - single key narrows to beads with that key=value
//   - multi-key AND requires all key=value pairs (rejects partial
//     matches that match only one of two)
//   - non-matching value returns nothing even when the key exists
func TestSearchIssuesAppliesMetadataFields(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "k1=v1+k2=v2", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, Metadata: metaJSON(t, map[string]string{"k1": "v1", "k2": "v2"})},
		{Title: "k1=v1+k2=v3", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, Metadata: metaJSON(t, map[string]string{"k1": "v1", "k2": "v3"})},
		{Title: "k1=v1 only", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, Metadata: metaJSON(t, map[string]string{"k1": "v1"})},
		{Title: "no metadata", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	t.Run("singleKey", func(t *testing.T) {
		runSearchAssertingSet(t, ctx, store, "MetadataFields={k1:v1}",
			types.IssueFilter{MetadataFields: map[string]string{"k1": "v1"}},
			[]string{ids[0], ids[1], ids[2]},
			[]string{ids[3]},
		)
	})
	t.Run("multiKey_AND", func(t *testing.T) {
		// Both keys must match; ids[1] has k1=v1 but k2=v3 (wrong) and
		// must drop. ids[2] is missing k2 entirely and must drop.
		runSearchAssertingSet(t, ctx, store, "MetadataFields={k1:v1,k2:v2}",
			types.IssueFilter{MetadataFields: map[string]string{"k1": "v1", "k2": "v2"}},
			[]string{ids[0]},
			[]string{ids[1], ids[2], ids[3]},
		)
	})
	t.Run("nonMatchingValue", func(t *testing.T) {
		runSearchAssertingSet(t, ctx, store, "MetadataFields={k1:other}",
			types.IssueFilter{MetadataFields: map[string]string{"k1": "other"}},
			nil,
			[]string{ids[0], ids[1], ids[2], ids[3]},
		)
	})
}

// TestSearchIssuesAppliesHasMetadataKey covers issues.go:492. Renders
// to `metadata ? $N` (jsonb key existence). The key existence check is
// independent of the value, so a bead with the key present (any value)
// matches, while a bead without the key drops.
func TestSearchIssuesAppliesHasMetadataKey(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "has key1", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, Metadata: metaJSON(t, map[string]string{"key1": "any"})},
		{Title: "has key2", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, Metadata: metaJSON(t, map[string]string{"key2": "any"})},
		{Title: "no metadata", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	runSearchAssertingSet(t, ctx, store, "HasMetadataKey=key1",
		types.IssueFilter{HasMetadataKey: "key1"},
		[]string{ids[0]},
		[]string{ids[1], ids[2]},
	)
}

// TestSearchIssuesAppliesParentID covers issues.go:499. The clause
// uses EXISTS over the dependencies table filtered to type
// 'parent-child' and depends_on_id = the supplied parent ID. Only
// children of the named parent should surface; the parent itself and
// unrelated beads must drop.
func TestSearchIssuesAppliesParentID(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "parent", IssueType: types.TypeEpic, Status: types.StatusOpen, Priority: 2},
		{Title: "child", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "unrelated", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	if err := store.AddDependency(ctx, &types.Dependency{IssueID: ids[1], DependsOnID: ids[0], Type: types.DepParentChild}, "tester"); err != nil {
		t.Fatalf("add parent-child dep: %v", err)
	}
	parentID := ids[0]
	runSearchAssertingSet(t, ctx, store, "ParentID=parent",
		types.IssueFilter{ParentID: &parentID},
		[]string{ids[1]},
		[]string{ids[0], ids[2]},
	)
}

// TestSearchIssuesAppliesNoParent covers issues.go:502. The NOT EXISTS
// counterpart returns beads that are NOT children of any
// parent-child dependency. Both the parent itself and the unrelated
// bead surface; only the child drops.
func TestSearchIssuesAppliesNoParent(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "parent", IssueType: types.TypeEpic, Status: types.StatusOpen, Priority: 2},
		{Title: "child", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "unrelated", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	if err := store.AddDependency(ctx, &types.Dependency{IssueID: ids[1], DependsOnID: ids[0], Type: types.DepParentChild}, "tester"); err != nil {
		t.Fatalf("add parent-child dep: %v", err)
	}
	runSearchAssertingSet(t, ctx, store, "NoParent=true",
		types.IssueFilter{NoParent: true},
		[]string{ids[0], ids[2]},
		[]string{ids[1]},
	)
}

// TestSearchIssuesAppliesMolType covers issues.go:506. Renders to
// `mol_type = $N`. We seed three beads with distinct mol_type values
// (swarm, work, empty) and filter on swarm; only the swarm bead
// surfaces. The empty mol_type bead must drop because the column will
// equal the empty string and 'swarm' won't match.
func TestSearchIssuesAppliesMolType(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "swarm-mol", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, MolType: types.MolTypeSwarm},
		{Title: "work-mol", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, MolType: types.MolTypeWork},
		{Title: "no-mol", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	want := types.MolTypeSwarm
	runSearchAssertingSet(t, ctx, store, "MolType=swarm",
		types.IssueFilter{MolType: &want},
		[]string{ids[0]},
		[]string{ids[1], ids[2]},
	)
}

// TestSearchIssuesAppliesWispType covers issues.go:509. Renders to
// `wisp_type = $N`. The wisp_type column is settable on persistent
// issues table rows even though wisps live in a separate table; the
// PG SearchIssues query targets the issues table directly so we cover
// the column equality there.
func TestSearchIssuesAppliesWispType(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "heartbeat-wisp", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, WispType: types.WispTypeHeartbeat},
		{Title: "error-wisp", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, WispType: types.WispTypeError},
		{Title: "no-wisp", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	want := types.WispTypeHeartbeat
	runSearchAssertingSet(t, ctx, store, "WispType=heartbeat",
		types.IssueFilter{WispType: &want},
		[]string{ids[0]},
		[]string{ids[1], ids[2]},
	)
}

// TestSearchIssuesAppliesDeferred covers issues.go:513. The clause
// surfaces beads that are scheduled later — defer_until set OR explicit
// status='deferred'. We seed three: one with status=deferred but no
// defer_until, one with defer_until set in the future but status=open,
// and a regular open bead. The first two must surface; the regular
// bead must drop.
func TestSearchIssuesAppliesDeferred(t *testing.T) {
	store, ctx := newSearchStore(t)
	future := time.Now().UTC().Add(24 * time.Hour)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "status-deferred", IssueType: types.TypeTask, Status: types.StatusDeferred, Priority: 2},
		{Title: "defer-until-set", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, DeferUntil: &future},
		{Title: "regular", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	runSearchAssertingSet(t, ctx, store, "Deferred=true",
		types.IssueFilter{Deferred: true},
		[]string{ids[0], ids[1]},
		[]string{ids[2]},
	)
}

// TestSearchIssuesAppliesDeferRange covers issues.go:517. The clauses
// render to `defer_until > $N` and `defer_until < $N` respectively
// (both bind UTC times). Subtests share a corpus seeded at fixed
// offsets relative to a reference time T so the boundary conditions
// are unambiguous.
func TestSearchIssuesAppliesDeferRange(t *testing.T) {
	store, ctx := newSearchStore(t)
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "defer-past", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, DeferUntil: &past},
		{Title: "defer-future", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, DeferUntil: &future},
		{Title: "no-defer", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	t.Run("DeferAfter", func(t *testing.T) {
		// > now — only the future bead matches; the past bead and the
		// IS NULL bead drop.
		runSearchAssertingSet(t, ctx, store, "DeferAfter=now",
			types.IssueFilter{DeferAfter: &now},
			[]string{ids[1]},
			[]string{ids[0], ids[2]},
		)
	})
	t.Run("DeferBefore", func(t *testing.T) {
		// < now — only the past bead matches; the future bead and the
		// IS NULL bead drop.
		runSearchAssertingSet(t, ctx, store, "DeferBefore=now",
			types.IssueFilter{DeferBefore: &now},
			[]string{ids[0]},
			[]string{ids[1], ids[2]},
		)
	})
}

// TestSearchIssuesAppliesDueRange covers issues.go:523-525. The
// DueAfter / DueBefore clauses render to `due_at > $N` / `due_at < $N`
// respectively. Unlike Overdue, neither has a status guard, so a
// closed bead with due_at < now still surfaces under DueBefore.
func TestSearchIssuesAppliesDueRange(t *testing.T) {
	store, ctx := newSearchStore(t)
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "due-past-open", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, DueAt: &past},
		{Title: "due-future-open", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, DueAt: &future},
		{Title: "no-due", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	t.Run("DueAfter", func(t *testing.T) {
		runSearchAssertingSet(t, ctx, store, "DueAfter=now",
			types.IssueFilter{DueAfter: &now},
			[]string{ids[1]},
			[]string{ids[0], ids[2]},
		)
	})
	t.Run("DueBefore", func(t *testing.T) {
		runSearchAssertingSet(t, ctx, store, "DueBefore=now",
			types.IssueFilter{DueBefore: &now},
			[]string{ids[0]},
			[]string{ids[1], ids[2]},
		)
	})
}

// TestSearchIssuesAppliesOverdue covers issues.go:529. The clause is
// (due_at IS NOT NULL AND due_at < NOW() AND status != 'closed'). The
// status guard distinguishes Overdue from a plain DueBefore=NOW: a
// closed bead with due_at < now must NOT surface under Overdue. We
// seed both an open and a closed bead at the same past due_at to pin
// that branch.
func TestSearchIssuesAppliesOverdue(t *testing.T) {
	store, ctx := newSearchStore(t)
	now := time.Now().UTC()
	past := now.Add(-24 * time.Hour)
	future := now.Add(24 * time.Hour)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "due-past-open", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, DueAt: &past},
		{Title: "due-past-closed", IssueType: types.TypeTask, Status: types.StatusClosed, Priority: 2, DueAt: &past},
		{Title: "due-future-open", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, DueAt: &future},
		{Title: "no-due-open", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	runSearchAssertingSet(t, ctx, store, "Overdue=true",
		types.IssueFilter{Overdue: true},
		[]string{ids[0]},
		[]string{ids[1], ids[2], ids[3]},
	)
}

// TestSearchIssuesAppliesTitleSearch covers issues.go:533. ILIKE
// substring; case-insensitive. Distinct from TitleContains (covered by
// be-8skfsh's TestSearchIssuesFilters) — TitleSearch is its own
// branch, also wired by be-jdeief.
func TestSearchIssuesAppliesTitleSearch(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "alpha plain", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "beta TARGET upper", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "gamma target lower", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "delta neutral", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	// Both upper/lower-cased title rows must surface; case differences
	// must not exclude them.
	runSearchAssertingSet(t, ctx, store, "TitleSearch=target",
		types.IssueFilter{TitleSearch: "target"},
		[]string{ids[1], ids[2]},
		[]string{ids[0], ids[3]},
	)
}

// TestSearchIssuesAppliesDescriptionContains covers issues.go:536.
// ILIKE substring on the description column. Pin case-insensitivity
// with a mixed-case marker.
func TestSearchIssuesAppliesDescriptionContains(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "a", Description: "this MARKER is here", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "b", Description: "MaRkEr in mixed case", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "c", Description: "no match here", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "d", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	runSearchAssertingSet(t, ctx, store, "DescriptionContains=marker",
		types.IssueFilter{DescriptionContains: "marker"},
		[]string{ids[0], ids[1]},
		[]string{ids[2], ids[3]},
	)
}

// TestSearchIssuesAppliesNotesContains covers issues.go:539. ILIKE
// substring on the notes column.
func TestSearchIssuesAppliesNotesContains(t *testing.T) {
	store, ctx := newSearchStore(t)
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "a", Notes: "MARKER in notes", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "b", Notes: "marker mixed CaSe inside", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "c", Notes: "no match", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "d", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	runSearchAssertingSet(t, ctx, store, "NotesContains=marker",
		types.IssueFilter{NotesContains: "marker"},
		[]string{ids[0], ids[1]},
		[]string{ids[2], ids[3]},
	)
}

// TestSearchIssuesAppliesExternalRefContains covers issues.go:542.
// ILIKE substring on the external_ref column. The external_ref column
// is nullable and stored as TEXT; ILIKE against NULL never matches, so
// the no-ref bead must drop.
func TestSearchIssuesAppliesExternalRefContains(t *testing.T) {
	store, ctx := newSearchStore(t)
	refTarget := "gh-123-TARGET"
	refOther := "gh-456-other"
	refMixed := "jira-target-mixed"
	ids := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "a", ExternalRef: &refTarget, IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "b", ExternalRef: &refMixed, IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "c", ExternalRef: &refOther, IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
		{Title: "d", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	runSearchAssertingSet(t, ctx, store, "ExternalRefContains=target",
		types.IssueFilter{ExternalRefContains: "target"},
		[]string{ids[0], ids[1]},
		[]string{ids[2], ids[3]},
	)
}

// === be-2clc: TestSearchIssuesMergesWisps_NoHistory =================
//
// PostgresStore.SearchIssues currently queries only the issues table.
// Dolt's SearchIssuesInTx (internal/storage/issueops/search.go:40-56)
// also queries the wisps table and merges results when filter.Ephemeral
// is nil or *filter.Ephemeral == false, so NoHistory beads (stored in
// the wisps table with ephemeral=0 per GH#3649 / GH#3659) survive the
// default non-ephemeral guard. The PG path lacks that merge, so
// label-filtered listings silently drop NoHistory beads on PG-backed
// cities.
//
// This test pins the regression in CI under integration_pg. Each
// subtest fails on the pre-fix code with "expected <id> in result, got
// [...]" for the NoHistory wisp(s) that should have surfaced. Sibling
// existing tests (TestSearchIssuesFilters, TestSearchIssuesApplies*)
// continue to pass without modification — they exercise the issues
// table only.
//
// Bead: be-2clc. Spec: bd show be-2clc; mirror at
// internal/storage/issueops/search.go:40-56.
func TestSearchIssuesMergesWisps_NoHistory(t *testing.T) {
	store, ctx := newSearchStore(t)

	// Persistent issues table — control rows.
	persistent := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "persistent labeled", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, Labels: []string{"needs-pm"}},
		{Title: "persistent unlabeled", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2},
	})
	persistentLabeled := persistent[0]
	persistentUnlabeled := persistent[1]

	// Wisps table — exercises both NoHistory (ephemeral=0) and true
	// ephemeral (ephemeral=1) rows so the merge correctness can be
	// observed against the per-row ephemeral guard.
	wisps := createForSearch(t, ctx, store, []*types.Issue{
		{Title: "no-history labeled", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, NoHistory: true, Labels: []string{"needs-pm"}},
		{Title: "no-history unlabeled", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, NoHistory: true},
		{Title: "true ephemeral labeled", IssueType: types.TypeTask, Status: types.StatusOpen, Priority: 2, Ephemeral: true, Labels: []string{"needs-pm"}},
	})
	noHistoryLabeled := wisps[0]
	noHistoryUnlabeled := wisps[1]
	trueEphemeralLabeled := wisps[2]

	t.Run("NilEphemeral_LabelFilter_MergesNoHistoryWisp", func(t *testing.T) {
		// Default search (filter.Ephemeral == nil) with --label needs-pm.
		// Must surface the persistent labeled issue AND the NoHistory
		// labeled wisp. Must NOT surface the true ephemeral wisp (default
		// non-ephemeral guard) nor any unlabeled bead. This is the
		// user-visible repro from the bead: `bd list --label X` on a
		// PG-backed city silently drops NoHistory beads pre-fix.
		runSearchAssertingSet(t, ctx, store, "Labels=[needs-pm],Ephemeral=nil",
			types.IssueFilter{Labels: []string{"needs-pm"}},
			[]string{persistentLabeled, noHistoryLabeled},
			[]string{persistentUnlabeled, noHistoryUnlabeled, trueEphemeralLabeled},
		)
	})

	t.Run("EphemeralFalse_LabelFilter_MergesNoHistoryWisp", func(t *testing.T) {
		// Explicit Ephemeral=false. Symmetric with the nil case: NoHistory
		// rows (ephemeral=0) MUST appear; true ephemeral wisps
		// (ephemeral=1) MUST be filtered out by the per-row ephemeral
		// column check applied uniformly to issues and wisps queries.
		eph := false
		runSearchAssertingSet(t, ctx, store, "Labels=[needs-pm],Ephemeral=false",
			types.IssueFilter{Labels: []string{"needs-pm"}, Ephemeral: &eph},
			[]string{persistentLabeled, noHistoryLabeled},
			[]string{persistentUnlabeled, noHistoryUnlabeled, trueEphemeralLabeled},
		)
	})

	t.Run("NilEphemeral_NoLabelFilter_MergesNoHistoryWisp", func(t *testing.T) {
		// No filters beyond the default ephemeral guard. Both persistent
		// rows AND both NoHistory rows must surface; true ephemeral wisps
		// must be filtered out by the per-row ephemeral check. Catches
		// the broader gap where any unfiltered SearchIssues call (e.g.
		// `bd list`) misses NoHistory beads on PG.
		runSearchAssertingSet(t, ctx, store, "Ephemeral=nil",
			types.IssueFilter{},
			[]string{persistentLabeled, persistentUnlabeled, noHistoryLabeled, noHistoryUnlabeled},
			[]string{trueEphemeralLabeled},
		)
	})
}

// avoid unused-import errors when the test build tag is off
var _ = strings.HasPrefix
