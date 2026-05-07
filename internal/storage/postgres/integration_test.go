//go:build integration_pg

package postgres

import (
	"context"
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

// avoid unused-import errors when the test build tag is off
var _ = strings.HasPrefix
