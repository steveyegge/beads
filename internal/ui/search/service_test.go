package search

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

type stubListClient struct {
	issues   []*types.Issue
	lastArgs *rpc.ListArgs
	calls    int
}

func (s *stubListClient) List(args *rpc.ListArgs) (*rpc.Response, error) {
	s.calls++
	clone := *args
	s.lastArgs = &clone

	data, err := json.Marshal(s.issues)
	if err != nil {
		return nil, err
	}
	return &rpc.Response{
		Success: true,
		Data:    data,
	}, nil
}

type scriptedListClient struct {
	responses []*rpc.Response
	errors    []error
	calls     int
	lastArgs  *rpc.ListArgs
}

func (s *scriptedListClient) List(args *rpc.ListArgs) (*rpc.Response, error) {
	s.calls++
	clone := *args
	s.lastArgs = &clone

	if s.calls-1 < len(s.errors) && s.errors[s.calls-1] != nil {
		return nil, s.errors[s.calls-1]
	}
	if s.calls-1 < len(s.responses) {
		return s.responses[s.calls-1], nil
	}
	return nil, errors.New("unexpected call")
}

func intPtr(v int) *int {
	return &v
}

func TestServiceRanksMatchesAcrossFields(t *testing.T) {
	now := time.Date(2025, 10, 23, 8, 0, 0, 0, time.UTC)
	stub := &stubListClient{
		issues: []*types.Issue{
			{
				ID:        "daemon-keeper",
				Title:     "Keep background daemon alive",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  1,
				UpdatedAt: now.Add(-5 * time.Minute),
			},
			{
				ID:        "ui-search",
				Title:     "Command palette enhances Daemon search",
				Status:    types.StatusInProgress,
				IssueType: types.TypeFeature,
				Priority:  2,
				UpdatedAt: now.Add(-10 * time.Minute),
			},
			{
				ID:        "label-maint",
				Title:     "Label maintenance helpers",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  3,
				Labels:    []string{"daemon-tools", "ops"},
				UpdatedAt: now.Add(-15 * time.Minute),
			},
			{
				ID:          "docs-refresh",
				Title:       "Refresh docs for operators",
				Status:      types.StatusOpen,
				IssueType:   types.TypeTask,
				Priority:    2,
				Description: "Explain daemon handshake flow for new contributors.",
				UpdatedAt:   now.Add(-20 * time.Minute),
			},
		},
	}

	service := NewService(stub,
		WithCacheTTL(10*time.Minute),
		WithFetchLimit(50),
		WithClock(func() time.Time { return now }),
	)

	ctx := context.Background()
	results, err := service.Search(ctx, "daemon", 10, SortRelevance)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	if stub.calls != 1 {
		t.Fatalf("expected exactly one List call, got %d", stub.calls)
	}

	wantOrder := []string{"daemon-keeper", "ui-search", "label-maint", "docs-refresh"}
	if len(results) != len(wantOrder) {
		t.Fatalf("expected %d results, got %d", len(wantOrder), len(results))
	}
	for i, id := range wantOrder {
		if results[i].ID != id {
			t.Fatalf("result %d expected id %q, got %q", i, id, results[i].ID)
		}
	}

	snippet := results[3].Snippet
	if snippet == "" || !strings.HasPrefix(snippet, "Explain") {
		t.Fatalf("expected snippet to contain description, got %q", snippet)
	}

	if results[0].Score <= results[1].Score {
		t.Fatalf("expected ID match score to outrank title match (scores: %v vs %v)", results[0].Score, results[1].Score)
	}
	if results[1].Score <= results[2].Score {
		t.Fatalf("expected title match score to outrank label match (scores: %v vs %v)", results[1].Score, results[2].Score)
	}
	if results[2].Score <= results[3].Score {
		t.Fatalf("expected label match score to outrank description match (scores: %v vs %v)", results[2].Score, results[3].Score)
	}
}

func TestServiceAppliesFiltersFromQuery(t *testing.T) {
	now := time.Date(2025, 10, 23, 9, 0, 0, 0, time.UTC)
	stub := &stubListClient{
		issues: []*types.Issue{
			{ID: "ui-100", Title: "Placeholder", Status: types.StatusOpen, IssueType: types.TypeFeature, UpdatedAt: now},
		},
	}

	service := NewService(stub,
		WithFetchLimit(25),
		WithClock(func() time.Time { return now }),
	)

	ctx := context.Background()
	if _, err := service.Search(ctx, "is:in_progress label:ui queue:ready query", 5, SortRelevance); err != nil {
		t.Fatalf("Search returned error: %v", err)
	}

	args := stub.lastArgs
	if args == nil {
		t.Fatalf("expected list args to be recorded")
	}

	if args.Status != string(types.StatusInProgress) {
		t.Fatalf("expected status filter %q, got %q", types.StatusInProgress, args.Status)
	}
	if len(args.Labels) != 1 || args.Labels[0] != "ui" {
		t.Fatalf("expected label filter [ui], got %v", args.Labels)
	}
	if args.Limit != 25 {
		t.Fatalf("expected fetch limit 25, got %d", args.Limit)
	}
}

func TestServiceParsesQuotedFilters(t *testing.T) {
	now := time.Date(2025, 10, 23, 9, 30, 0, 0, time.UTC)
	stub := &stubListClient{
		issues: []*types.Issue{
			{
				ID:        "ui-quoted",
				Title:     "Handle quoted label filters",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  2,
				Labels:    []string{"daemon tools"},
				UpdatedAt: now,
			},
		},
	}

	service := NewService(stub,
		WithFetchLimit(10),
		WithClock(func() time.Time { return now }),
	)

	ctx := context.Background()
	results, err := service.Search(ctx, `label:"daemon tools"`, 5, SortRelevance)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if stub.lastArgs == nil {
		t.Fatalf("expected list args to be recorded")
	}
	if len(stub.lastArgs.Labels) != 1 || stub.lastArgs.Labels[0] != "daemon tools" {
		t.Fatalf("expected label filter [\"daemon tools\"], got %v", stub.lastArgs.Labels)
	}
	if len(results) != 1 || results[0].ID != "ui-quoted" {
		t.Fatalf("expected search results to include ui-quoted, got %+v", results)
	}
}

func TestServiceAppliesSortModes(t *testing.T) {
	now := time.Date(2025, 10, 24, 12, 0, 0, 0, time.UTC)
	stub := &stubListClient{
		issues: []*types.Issue{
			{
				ID:        "quick-hero",
				Title:     "Hero search improvements",
				Status:    types.StatusOpen,
				IssueType: types.TypeTask,
				Priority:  3,
				UpdatedAt: now.Add(-30 * time.Minute),
			},
			{
				ID:        "ui-202",
				Title:     "Quick search indexing",
				Status:    types.StatusOpen,
				IssueType: types.TypeFeature,
				Priority:  1,
				UpdatedAt: now.Add(-5 * time.Minute),
			},
			{
				ID:          "ui-203",
				Title:       "Improve search responses",
				Status:      types.StatusInProgress,
				IssueType:   types.TypeTask,
				Priority:    0,
				Description: "Ensure quick fallback behaviour for palette.",
				UpdatedAt:   now.Add(-15 * time.Minute),
			},
		},
	}

	service := NewService(stub, WithClock(func() time.Time { return now }))
	ctx := context.Background()

	relevance, err := service.Search(ctx, "quick", 10, SortRelevance)
	if err != nil {
		t.Fatalf("Search relevance returned error: %v", err)
	}
	if got := extractIDs(relevance); !equalStrings(got, []string{"quick-hero", "ui-202", "ui-203"}) {
		t.Fatalf("relevance order mismatch: %v", got)
	}

	recent, err := service.Search(ctx, "quick", 10, SortRecent)
	if err != nil {
		t.Fatalf("Search recent returned error: %v", err)
	}
	if got := extractIDs(recent); !equalStrings(got, []string{"ui-202", "ui-203", "quick-hero"}) {
		t.Fatalf("recent order mismatch: %v", got)
	}

	priority, err := service.Search(ctx, "quick", 10, SortPriority)
	if err != nil {
		t.Fatalf("Search priority returned error: %v", err)
	}
	if got := extractIDs(priority); !equalStrings(got, []string{"ui-203", "ui-202", "quick-hero"}) {
		t.Fatalf("priority order mismatch: %v", got)
	}
}

func extractIDs(results []Result) []string {
	ids := make([]string, len(results))
	for i, res := range results {
		ids[i] = res.ID
	}
	return ids
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestParsePriority(t *testing.T) {
	tests := []struct {
		raw   string
		want  int
		valid bool
	}{
		{"0", 0, true},
		{"4", 4, true},
		{"invalid", 0, false},
		{"p3", 0, false},
		{"9", 0, false},
	}

	for _, tc := range tests {
		got, err := parsePriority(tc.raw)
		if tc.valid {
			if err != nil || got != tc.want {
				t.Fatalf("parsePriority(%q) = (%d,%v), want (%d,nil)", tc.raw, got, err, tc.want)
			}
		} else if err == nil {
			t.Fatalf("expected error parsing %q", tc.raw)
		}
	}
}

func TestNormalizeSortMode(t *testing.T) {
	if mode := normalizeSortMode(SortPriority, ""); mode != SortPriority {
		t.Fatalf("expected explicit priority sort to remain")
	}
	if mode := normalizeSortMode("unknown", ""); mode != SortRecent {
		t.Fatalf("expected fallback to recent when query empty, got %s", mode)
	}
	if mode := normalizeSortMode("unknown", "quick filters"); mode != SortRelevance {
		t.Fatalf("expected fallback to relevance with query, got %s", mode)
	}
}

func TestParseQueryAdvancedFilters(t *testing.T) {
	args, terms := parseQuery(`status:blocked labels_any:"ops team" labels:backend type:Feature assignee:'Casey' priority:3 queue:recent trailing-term`)
	if args.Status != string(types.StatusBlocked) {
		t.Fatalf("expected blocked status, got %q", args.Status)
	}
	if len(args.Labels) != 1 || args.Labels[0] != "backend" {
		t.Fatalf("expected labels [backend], got %v", args.Labels)
	}
	if len(args.LabelsAny) != 1 || args.LabelsAny[0] != "ops team" {
		t.Fatalf("expected labels_any [ops team], got %v", args.LabelsAny)
	}
	if args.IssueType != "feature" {
		t.Fatalf("expected feature issue type, got %q", args.IssueType)
	}
	if args.Assignee != "Casey" {
		t.Fatalf("expected assignee Casey, got %q", args.Assignee)
	}
	if args.Priority == nil || *args.Priority != 3 {
		t.Fatalf("expected priority 3, got %v", args.Priority)
	}
	if len(terms) != 1 || terms[0] != "trailing-term" {
		t.Fatalf("expected normalized term trailing-term, got %v", terms)
	}
}

func TestParseQueryQueueFallbacks(t *testing.T) {
	if args, _ := parseQuery("queue:ready"); args.Status != string(types.StatusOpen) {
		t.Fatalf("expected queue:ready to map to open status, got %q", args.Status)
	}
	if args, _ := parseQuery("queue:in_progress"); args.Status != string(types.StatusInProgress) {
		t.Fatalf("expected queue:in_progress to map to in_progress status, got %q", args.Status)
	}
	if args, _ := parseQuery("queue:blocked"); args.Status != string(types.StatusBlocked) {
		t.Fatalf("expected queue:blocked to map to blocked status, got %q", args.Status)
	}
	if args, _ := parseQuery("queue:recent"); args.Status != "" {
		t.Fatalf("expected queue:recent to leave status unset, got %q", args.Status)
	}
	if args, _ := parseQuery("is:open queue:blocked"); args.Status != string(types.StatusOpen) {
		t.Fatalf("expected is:open to keep original status, got %q", args.Status)
	}
}

func TestApplySortTiebreakers(t *testing.T) {
	now := time.Date(2025, 10, 30, 12, 0, 0, 0, time.UTC)

	results := []Result{
		{ID: "b", Score: 1, UpdatedAt: now},
		{ID: "a", Score: 1, UpdatedAt: now},
		{ID: "c", Score: 0.5, UpdatedAt: now.Add(-time.Minute)},
	}
	applySort(results, SortRelevance)
	if results[0].ID != "a" || results[1].ID != "b" {
		t.Fatalf("expected tie on relevance to fall back to id ordering, got %+v", extractIDs(results))
	}

	recent := []Result{
		{ID: "late", Score: 2, UpdatedAt: now.Add(-time.Hour)},
		{ID: "recent", Score: 1, UpdatedAt: now.Add(-time.Hour)},
	}
	applySort(recent, SortRecent)
	if recent[0].ID != "late" {
		t.Fatalf("expected score tiebreaker for recent sort, got %+v", extractIDs(recent))
	}

	priority := []Result{
		{ID: "alpha", Priority: 1, Score: 1, UpdatedAt: now.Add(-10 * time.Minute)},
		{ID: "beta", Priority: 1, Score: 1, UpdatedAt: now.Add(-5 * time.Minute)},
		{ID: "gamma", Priority: 0, Score: 0.5, UpdatedAt: now},
	}
	applySort(priority, SortPriority)
	if !equalStrings(extractIDs(priority), []string{"gamma", "beta", "alpha"}) {
		t.Fatalf("unexpected priority ordering: %v", extractIDs(priority))
	}
}

func TestSearchRespectsContextCancellation(t *testing.T) {
	service := NewService(&stubListClient{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Search(ctx, "any", 10, SortRelevance); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}

func TestSearchLimitClampsAndSnippetFallback(t *testing.T) {
	now := time.Date(2025, 10, 31, 9, 0, 0, 0, time.UTC)
	stub := &stubListClient{
		issues: []*types.Issue{
			{ID: "ui-1", Title: "Title only", Status: types.StatusOpen, IssueType: types.TypeTask, UpdatedAt: now},
			{ID: "ui-2", Title: "Second issue", Status: types.StatusOpen, IssueType: types.TypeTask, UpdatedAt: now.Add(-time.Minute)},
			{ID: "ui-3", Title: "Third issue", Status: types.StatusOpen, IssueType: types.TypeTask, UpdatedAt: now.Add(-2 * time.Minute)},
		},
	}
	service := NewService(stub, WithClock(func() time.Time { return now }))

	results, err := service.Search(context.Background(), "", 0, SortRelevance)
	if err != nil {
		t.Fatalf("Search returned error: %v", err)
	}
	if len(results) != len(stub.issues) {
		t.Fatalf("expected all issues when limit <= 0, got %d", len(results))
	}
	if results[0].Snippet != "Title only" {
		t.Fatalf("expected snippet fallback to title, got %q", results[0].Snippet)
	}
}

func TestServiceLoadRecordsErrors(t *testing.T) {
	ctx := context.Background()
	args := &rpc.ListArgs{}

	svc := NewService(&scriptedListClient{
		errors: []error{errors.New("boom")},
	})
	if _, err := svc.loadRecords(ctx, cacheKey{}, args); err == nil || !strings.Contains(err.Error(), "list issues: boom") {
		t.Fatalf("expected wrapped error, got %v", err)
	}

	svc = NewService(&scriptedListClient{
		responses: []*rpc.Response{nil},
	})
	if _, err := svc.loadRecords(ctx, cacheKey{}, args); err == nil || !strings.Contains(err.Error(), "empty response") {
		t.Fatalf("expected empty response error, got %v", err)
	}

	svc = NewService(&scriptedListClient{
		responses: []*rpc.Response{{Success: false, Error: "invalid status"}},
	})
	if _, err := svc.loadRecords(ctx, cacheKey{}, args); err == nil || !strings.Contains(err.Error(), "invalid status") {
		t.Fatalf("expected error message to propagate, got %v", err)
	}

	svc = NewService(&scriptedListClient{
		responses: []*rpc.Response{{Success: false}},
	})
	if _, err := svc.loadRecords(ctx, cacheKey{}, args); err == nil || !strings.Contains(err.Error(), "unknown failure") {
		t.Fatalf("expected unknown failure error, got %v", err)
	}

	svc = NewService(&scriptedListClient{
		responses: []*rpc.Response{{Success: true, Data: []byte("{")}},
	})
	if _, err := svc.loadRecords(ctx, cacheKey{}, args); err == nil || !strings.Contains(err.Error(), "decode issues") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestServiceCachesRecordsWithinTTL(t *testing.T) {
	now := time.Date(2025, 10, 31, 10, 0, 0, 0, time.UTC)
	stub := &stubListClient{
		issues: []*types.Issue{
			{ID: "cache-1", Title: "Cache me", Status: types.StatusOpen, IssueType: types.TypeTask, UpdatedAt: now},
		},
	}
	svc := NewService(stub, WithCacheTTL(time.Hour), WithClock(func() time.Time { return now }))

	key := deriveCacheKey(&rpc.ListArgs{}, svc.fetchLimit)
	if _, err := svc.loadRecords(context.Background(), key, &rpc.ListArgs{}); err != nil {
		t.Fatalf("first loadRecords failed: %v", err)
	}
	if _, err := svc.loadRecords(context.Background(), key, &rpc.ListArgs{}); err != nil {
		t.Fatalf("second loadRecords failed: %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("expected cached response to avoid second list call, got %d calls", stub.calls)
	}
}

func TestDeriveCacheKeyNormalization(t *testing.T) {
	args := &rpc.ListArgs{
		Status:    " Open ",
		IssueType: "Feature",
		Assignee:  "Casey ",
		Priority:  intPtr(2),
		Labels:    []string{"ops", "backend"},
		LabelsAny: []string{"urgent", "triage"},
	}
	key := deriveCacheKey(args, 25)
	if key.status != "open" || key.issueType != "feature" || key.assignee != "casey" {
		t.Fatalf("expected normalized status/type/assignee, got %+v", key)
	}
	if key.priority != "2" {
		t.Fatalf("expected priority string 2, got %q", key.priority)
	}
	if key.labels != "backend|ops" {
		t.Fatalf("expected sorted labels backend|ops, got %q", key.labels)
	}
	if key.labelsAny != "triage|urgent" {
		t.Fatalf("expected sorted labels_any triage|urgent, got %q", key.labelsAny)
	}
	if key.limit != 25 {
		t.Fatalf("expected limit 25, got %d", key.limit)
	}
}

func TestFieldScoreVariants(t *testing.T) {
	if score, ok := fieldScore("foo", "foo", 1); !ok || score != 5 {
		t.Fatalf("expected exact match score 5, got %v %v", score, ok)
	}
	if score, ok := fieldScore("foo", "foobar", 1); !ok || score != 4 {
		t.Fatalf("expected prefix score 4, got %v %v", score, ok)
	}
	if score, ok := fieldScore("bar", "foobar", 1); !ok || score != 3 {
		t.Fatalf("expected substring score 3, got %v %v", score, ok)
	}
	if score, ok := fieldScore("fb", "foobar", 1); !ok || score != 2 {
		t.Fatalf("expected subsequence score 2, got %v %v", score, ok)
	}
	if _, ok := fieldScore("baz", "qux", 1); ok {
		t.Fatalf("expected no match for baz in qux")
	}
}

func TestIsSubsequenceCases(t *testing.T) {
	if !isSubsequence("abc", "a_b_c") {
		t.Fatalf("expected abc to be subsequence of a_b_c")
	}
	if isSubsequence("abc", "acb") {
		t.Fatalf("expected abc not to be subsequence of acb")
	}
	if !isSubsequence("", "anything") {
		t.Fatalf("empty term should be subsequence of any field")
	}
}

func TestBuildSnippetFallbacks(t *testing.T) {
	if snippet := buildSnippet("", []string{"x"}); snippet != "" {
		t.Fatalf("expected empty input to return empty snippet, got %q", snippet)
	}

	text := "This description elaborates the important matching term for snippet testing and adds more trailing content."
	snippet := buildSnippet(text, []string{"term"})
	if !strings.Contains(snippet, "term") {
		t.Fatalf("expected snippet to include search term, got %q", snippet)
	}
	if len(snippet) >= len(text) {
		t.Fatalf("expected snippet to be shorter than original text, got %q", snippet)
	}

	longText := strings.Repeat("content ", 30)
	if snippet := buildSnippet(longText, nil); snippet == "" || len(snippet) >= len(longText) {
		t.Fatalf("expected long snippet to truncate original text, got %q", snippet)
	}
}
