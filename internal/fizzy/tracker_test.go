package fizzy

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/tracker/conformance"
	"github.com/steveyegge/beads/internal/types"
)

// testStore extends the conformance store: the pull also updates the status,
// the priority, the type and the assignee.
type testStore struct {
	*conformance.Store
}

func (s *testStore) UpdateIssue(ctx context.Context, id string, updates map[string]interface{}, actor string) error {
	if err := s.Store.UpdateIssue(ctx, id, updates, actor); err != nil {
		return err
	}
	issue := s.Issues[id]
	if value, ok := updates["status"].(string); ok {
		issue.Status = types.Status(value)
	}
	if value, ok := updates["priority"].(int); ok {
		issue.Priority = value
	}
	if value, ok := updates["issue_type"].(string); ok {
		issue.IssueType = types.IssueType(value)
	}
	if value, ok := updates["assignee"].(string); ok {
		issue.Assignee = value
	}
	issue.UpdatedAt = time.Now().UTC()
	return nil
}

func (s *testStore) ApplyIssueUpdate(ctx context.Context, id string, updates map[string]interface{}, labels []string, actor string) error {
	if err := s.UpdateIssue(ctx, id, updates, actor); err != nil {
		return err
	}
	if labels != nil {
		s.Issues[id].Labels = slices.Clone(labels)
	}
	return nil
}

func newTestStore(f *fakeFizzy, config map[string]string, issues ...*types.Issue) *testStore {
	store := &testStore{Store: conformance.NewStore()}
	store.Config["fizzy.url"] = f.server.URL
	store.Config["fizzy.account"] = testAccount
	store.Config["fizzy.board"] = testBoard
	for key, value := range config {
		store.Config[key] = value
	}
	old := time.Now().UTC().Add(-2 * time.Hour)
	for _, issue := range issues {
		if issue.CreatedAt.IsZero() {
			issue.CreatedAt, issue.UpdatedAt = old, old
		}
		store.Issues[issue.ID] = issue
	}
	return store
}

func newTestTracker(t *testing.T, store tracker.Store) *Tracker {
	t.Helper()
	t.Setenv("FIZZY_TOKEN", testToken)
	tr := &Tracker{}
	if err := tr.Init(context.Background(), store); err != nil {
		t.Fatal(err)
	}
	tr.client.Retry = RetryConfig{MaxRetries: 1}
	return tr
}

// runSync runs the sync engine with the hooks of the bd command.
func runSync(t *testing.T, store *testStore, opts tracker.SyncOptions) *tracker.SyncResult {
	t.Helper()
	ctx := context.Background()
	tr := newTestTracker(t, store)
	engine := tracker.NewEngine(tr, store, "test")
	engine.PushHooks = tr.PushHooks(ctx)
	next := 100
	engine.PullHooks = tr.PullHooks(func(_ context.Context, issue *types.Issue) error {
		if issue.ID == "" {
			issue.ID = fmt.Sprintf("bd-%d", next)
			next++
		}
		return nil
	})
	var warnings []string
	engine.OnWarning = func(msg string) { warnings = append(warnings, msg) }
	result, err := engine.Sync(ctx, opts)
	if err != nil {
		t.Fatalf("Sync() error: %v", err)
	}
	if len(warnings) > 0 {
		t.Fatalf("Sync() warnings: %v", warnings)
	}
	return result
}

func cardOf(t *testing.T, f *fakeFizzy, store *testStore, id string) *Card {
	t.Helper()
	issue := store.Issues[id]
	if issue == nil || issue.ExternalRef == nil {
		t.Fatalf("issue %s has no external_ref", id)
	}
	tr := &Tracker{}
	var number int
	if _, err := fmt.Sscan(tr.ExtractIdentifier(*issue.ExternalRef), &number); err != nil {
		t.Fatalf("external_ref %q: %v", *issue.ExternalRef, err)
	}
	return f.card(number)
}

func TestRegistered(t *testing.T) {
	tr, err := tracker.NewTracker("fizzy")
	if err != nil || tr.Name() != "fizzy" || tr.ConfigPrefix() != "fizzy" {
		t.Fatalf("NewTracker(fizzy) = %v, %v", tr, err)
	}
}

func TestInitNeedsTokenAccountAndBoard(t *testing.T) {
	for _, missing := range []string{"token", "account", "board"} {
		t.Run(missing, func(t *testing.T) {
			store := conformance.NewStore()
			store.Config["fizzy.account"] = testAccount
			store.Config["fizzy.board"] = testBoard
			t.Setenv("FIZZY_TOKEN", testToken)
			switch missing {
			case "token":
				t.Setenv("FIZZY_TOKEN", "")
			default:
				delete(store.Config, "fizzy."+missing)
			}
			err := (&Tracker{}).Init(context.Background(), store)
			if err == nil || !strings.Contains(err.Error(), missing) {
				t.Errorf("Init() error = %v, want an error about the %s", err, missing)
			}
		})
	}
}

func TestInitReadsStatusMapAndCustomStatuses(t *testing.T) {
	f := newFakeFizzy(t)
	store := newTestStore(f, map[string]string{
		"fizzy.status_map.open": " Ready ",
		"status.custom":         "review:wip",
	})
	tr := newTestTracker(t, store)
	if tr.config.StatusMap["open"] != "Ready" || tr.config.CustomStatuses["review"] != types.CategoryWIP {
		t.Errorf("config = %+v", tr.config)
	}
}

func TestExternalRefs(t *testing.T) {
	f := newFakeFizzy(t)
	tr := newTestTracker(t, newTestStore(f, nil))
	ref := tr.BuildExternalRef(&tracker.TrackerIssue{Identifier: "42", URL: "http://other-host/123/cards/42"})
	if ref != f.server.URL+"/123/cards/42" {
		t.Errorf("BuildExternalRef() = %q, want the configured base URL", ref)
	}
	tests := map[string]bool{
		ref: true,
		strings.ToUpper(f.server.URL) + "/123/cards/42": true,
		"fizzy:42":                         true,
		f.server.URL + "/999/cards/42":     false, // other account
		"https://github.com/o/r/issues/42": false,
		"https://example.com/123/cards/42": false,
	}
	for ref, want := range tests {
		if got := tr.IsExternalRef(ref); got != want {
			t.Errorf("IsExternalRef(%q) = %v, want %v", ref, got, want)
		}
	}
	if got := tr.ExtractIdentifier(ref); got != "42" {
		t.Errorf("ExtractIdentifier() = %q", got)
	}
	if got := tr.ExtractIdentifier("fizzy:7"); got != "7" {
		t.Errorf("ExtractIdentifier(fizzy:7) = %q", got)
	}
}

func TestSyncPushesIssuesThenSkipsThem(t *testing.T) {
	f := newFakeFizzy(t, "Doing")
	closedAt := time.Now().UTC().Add(-time.Hour)
	store := newTestStore(f, nil,
		&types.Issue{ID: "bd-1", Title: "Open bug", Description: "Line 1\nLine 2", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeBug, Labels: []string{"repo:a"}},
		&types.Issue{ID: "bd-2", Title: "Work", Status: types.StatusInProgress, Priority: 2, IssueType: types.TypeTask},
		&types.Issue{ID: "bd-3", Title: "Later", Status: types.StatusDeferred, Priority: 4, IssueType: types.TypeTask},
		&types.Issue{ID: "bd-4", Title: "Done", Status: types.StatusClosed, Priority: 2, IssueType: types.TypeTask, ClosedAt: &closedAt},
	)

	result := runSync(t, store, tracker.SyncOptions{Push: true})
	if result.Stats.Pushed != 4 {
		t.Fatalf("Pushed = %d, want 4 (%+v)", result.Stats.Pushed, result.Stats)
	}
	lanes := map[string]string{"bd-1": LaneMaybe, "bd-2": "Doing", "bd-3": LaneNotNow, "bd-4": LaneDone}
	for id, want := range lanes {
		if got := CardLane(cardOf(t, f, store, id)); got != want {
			t.Errorf("lane of %s = %q, want %q", id, got, want)
		}
	}
	card := cardOf(t, f, store, "bd-1")
	if card.Title != "Open bug" || card.Description != "Line 1\nLine 2" || !sameTags(card.Tags, []string{"p1", "type::bug", "repo:a"}) {
		t.Errorf("card of bd-1 = %+v", card)
	}

	f.resetMutations()
	runSync(t, store, tracker.SyncOptions{Push: true})
	if calls := f.mutations(); len(calls) != 0 {
		t.Errorf("second push changed Fizzy: %v", calls)
	}
}

func TestSyncPushesChangesWithStatusMap(t *testing.T) {
	f := newFakeFizzy(t, "Ready", "In Progress", "Blocked")
	statusMap := map[string]string{
		"fizzy.status_map.open":        "Ready",
		"fizzy.status_map.in_progress": "In Progress",
		"fizzy.status_map.blocked":     "Blocked",
	}
	store := newTestStore(f, statusMap,
		&types.Issue{ID: "bd-1", Title: "Task", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask, Labels: []string{"a"}},
	)
	runSync(t, store, tracker.SyncOptions{Push: true})
	if got := CardLane(cardOf(t, f, store, "bd-1")); got != "Ready" {
		t.Fatalf("lane = %q, want Ready", got)
	}

	issue := store.Issues["bd-1"]
	issue.Status, issue.Priority, issue.Labels, issue.Title = types.StatusBlocked, 0, []string{"b"}, "Task, renamed"
	issue.UpdatedAt = time.Now().UTC().Add(time.Minute)
	runSync(t, store, tracker.SyncOptions{Push: true})

	card := cardOf(t, f, store, "bd-1")
	if CardLane(card) != "Blocked" || card.Title != "Task, renamed" || !sameTags(card.Tags, []string{"p0", "type::task", "b"}) {
		t.Errorf("card after the change = %+v (lane %q)", card, CardLane(card))
	}
}

func TestSyncPullsLaneChangesAndKeepsLocalFields(t *testing.T) {
	f := newFakeFizzy(t, "Ready", "In Progress")
	statusMap := map[string]string{"fizzy.status_map.open": "Ready", "fizzy.status_map.in_progress": "In Progress"}
	store := newTestStore(f, statusMap,
		&types.Issue{ID: "bd-1", Title: "Task", Description: "Text\n\nwith paragraphs", Status: types.StatusOpen, Priority: 1, IssueType: types.TypeFeature, Assignee: "claude"},
	)
	runSync(t, store, tracker.SyncOptions{})

	number := cardOf(t, f, store, "bd-1").Number
	f.edit(number, func(card *Card) {
		card.Column = &Column{ID: "col-2", Name: "In Progress"}
		card.Title = "Task, renamed in Fizzy"
	})
	runSync(t, store, tracker.SyncOptions{})

	issue := store.Issues["bd-1"]
	if issue.Status != types.StatusInProgress || issue.Title != "Task, renamed in Fizzy" {
		t.Errorf("issue after the pull = status %q, title %q", issue.Status, issue.Title)
	}
	if issue.Assignee != "claude" || issue.Priority != 1 || issue.IssueType != types.TypeFeature || issue.Description != "Text\n\nwith paragraphs" {
		t.Errorf("the pull did not keep the local fields: %+v", issue)
	}
}

func TestSyncKeepsStatusWhenCardStaysInItsLane(t *testing.T) {
	f := newFakeFizzy(t, "Doing")
	// Without a status map, blocked and in_progress both go to the first column.
	store := newTestStore(f, nil, &types.Issue{ID: "bd-1", Title: "Blocked task", Status: types.StatusBlocked, Priority: 2, IssueType: types.TypeTask})
	runSync(t, store, tracker.SyncOptions{})

	f.edit(cardOf(t, f, store, "bd-1").Number, func(card *Card) { card.Title = "Blocked task, renamed" })
	runSync(t, store, tracker.SyncOptions{})

	issue := store.Issues["bd-1"]
	if issue.Status != types.StatusBlocked || issue.Title != "Blocked task, renamed" {
		t.Errorf("issue = status %q, title %q, want blocked and the new title", issue.Status, issue.Title)
	}
}

func TestSyncPullsNewCards(t *testing.T) {
	f := newFakeFizzy(t, "Doing")
	f.addCard(Card{Title: "Idea from Fizzy", Description: "Details", Tags: []string{"p0", "type::feature", "idea"}})
	f.addCard(Card{Title: "Draft", Status: "drafted"})
	store := newTestStore(f, nil)

	result := runSync(t, store, tracker.SyncOptions{Pull: true})
	if result.Stats.Created != 1 {
		t.Fatalf("Created = %d, want 1 (%+v)", result.Stats.Created, result.Stats)
	}
	var issue *types.Issue
	for _, candidate := range store.Issues {
		issue = candidate
	}
	if issue.Title != "Idea from Fizzy" || issue.Priority != 0 || issue.IssueType != types.TypeFeature ||
		issue.Status != types.StatusOpen || !slices.Equal(issue.Labels, []string{"idea"}) {
		t.Errorf("pulled issue = %+v", issue)
	}
	if issue.ExternalRef == nil || *issue.ExternalRef != f.server.URL+"/123/cards/1" {
		t.Errorf("external_ref = %v", issue.ExternalRef)
	}
}

func TestSyncReportsMissingColumn(t *testing.T) {
	f := newFakeFizzy(t, "Doing")
	store := newTestStore(f, map[string]string{"fizzy.status_map.open": "Ready"},
		&types.Issue{ID: "bd-1", Title: "Task", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask})
	tr := newTestTracker(t, store)
	_, err := tr.CreateIssue(context.Background(), store.Issues["bd-1"])
	if err == nil || !strings.Contains(err.Error(), `no column "Ready"`) {
		t.Errorf("CreateIssue() error = %v, want a missing column error", err)
	}
	if len(f.mutations()) != 0 {
		t.Errorf("CreateIssue() changed Fizzy before the error: %v", f.mutations())
	}
}

func TestValidateBaseURL(t *testing.T) {
	valid := []string{"", "https://app.fizzy.do", "http://localhost:3007", "http://127.0.0.1:3006", "http://app.fizzy.localhost:3006"}
	for _, raw := range valid {
		if err := ValidateBaseURL(raw); err != nil {
			t.Errorf("ValidateBaseURL(%q) = %v", raw, err)
		}
	}
	for _, raw := range []string{"http://fizzy.example.com", "ftp://host", "not a url"} {
		if err := ValidateBaseURL(raw); err == nil {
			t.Errorf("ValidateBaseURL(%q) = nil, want an error", raw)
		}
	}
}

// A pull changes the issue, then the issue goes back to its last pushed state.
// The push must move the card again: the stored push hash must describe the
// state of the pull, not the state of the last push.
func TestSyncPushesARevertAfterAPull(t *testing.T) {
	f := newFakeFizzy(t, "Ready", "In Progress", "Blocked")
	statusMap := map[string]string{
		"fizzy.status_map.open":        "Ready",
		"fizzy.status_map.in_progress": "In Progress",
		"fizzy.status_map.blocked":     "Blocked",
	}
	store := newTestStore(f, statusMap,
		&types.Issue{ID: "bd-1", Title: "Task", Status: types.StatusInProgress, Priority: 2, IssueType: types.TypeTask})
	runSync(t, store, tracker.SyncOptions{})

	number := cardOf(t, f, store, "bd-1").Number
	f.edit(number, func(card *Card) { card.Column = &Column{ID: "col-3", Name: "Blocked"} })
	runSync(t, store, tracker.SyncOptions{Pull: true})
	if got := store.Issues["bd-1"].Status; got != types.StatusBlocked {
		t.Fatalf("status after the pull = %q, want blocked", got)
	}

	issue := store.Issues["bd-1"]
	issue.Status = types.StatusInProgress
	issue.UpdatedAt = time.Now().UTC().Add(2 * time.Minute)
	runSync(t, store, tracker.SyncOptions{Push: true})
	if got := CardLane(f.card(number)); got != "In Progress" {
		t.Errorf("lane after the push of the revert = %q, want In Progress", got)
	}
}
