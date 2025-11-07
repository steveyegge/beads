package api

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

func TestSanitizeRefreshFilters(t *testing.T) {
	t.Parallel()

	values := url.Values{
		"status":           {"open"},
		"q":                {"status:open"},
		"labels":           {"ui", "ops"},
		"labels_any":       {"infra"},
		"sort":             {"number-desc"},
		"sort_secondary":   {"title-asc"},
		"limit":            {"50"},
		"queue":            {"ready"},
		"queue_label":      {"Ready"},
		"selected":         {"ui-1"},
		"cursor":           {"token"},
		"append":           {"1"},
		"closed_before":    {"2025-10-20T10:00:00Z"},
		"closed_before_id": {"ui-1"},
		"hx-target":        {"[data-role='issue-shell']"},
		"_internal":        {"1"},
	}

	got := sanitizeRefreshFilters(values)
	if got == nil {
		t.Fatalf("sanitizeRefreshFilters returned nil")
	}

	if got.Get("status") != "open" {
		t.Fatalf("expected status to remain, got %q", got.Get("status"))
	}
	if got.Get("q") != "status:open" {
		t.Fatalf("expected q to remain, got %q", got.Get("q"))
	}
	if !strings.Contains(got.Encode(), "labels=ui") || !strings.Contains(got.Encode(), "labels=ops") {
		t.Fatalf("expected labels to remain, got %q", got.Encode())
	}
	if got.Get("labels_any") != "infra" {
		t.Fatalf("expected labels_any to remain, got %q", got.Get("labels_any"))
	}
	if got.Get("limit") != "50" {
		t.Fatalf("expected limit to remain, got %q", got.Get("limit"))
	}
	if got.Get("sort") != "number-desc" {
		t.Fatalf("expected sort to remain, got %q", got.Get("sort"))
	}
	if got.Get("sort_secondary") != "title-asc" {
		t.Fatalf("expected sort_secondary to remain, got %q", got.Get("sort_secondary"))
	}

	for _, key := range []string{"queue", "queue_label", "selected", "cursor", "append", "closed_before", "closed_before_id"} {
		if got.Has(key) {
			t.Fatalf("expected %q to be removed, got %q", key, got.Get(key))
		}
	}
	if got.Has("hx-target") || got.Has("_internal") {
		t.Fatalf("expected internal or hx- prefixed keys to be removed, got %v", got)
	}
}

func TestDeriveListHeading(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args *rpc.ListArgs
		want string
	}{
		{
			name: "nil args",
			args: nil,
			want: "Filtered issues",
		},
		{
			name: "status takes precedence",
			args: &rpc.ListArgs{Status: "open", Query: "blocked work"},
			want: "Ready issues",
		},
		{
			name: "query fallback",
			args: &rpc.ListArgs{Query: "search text"},
			want: `Issues matching "search text"`,
		},
		{
			name: "labels all",
			args: &rpc.ListArgs{Labels: []string{"ops", "infra"}},
			want: "Issues matching all labels",
		},
		{
			name: "labels any",
			args: &rpc.ListArgs{LabelsAny: []string{"ops"}},
			want: "Issues matching any label",
		},
		{
			name: "assignee fallback",
			args: &rpc.ListArgs{Assignee: "alice"},
			want: "Issues assigned to alice",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := deriveListHeading(tc.args); got != tc.want {
				t.Fatalf("deriveListHeading() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDeriveEmptyMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		heading string
		args    *rpc.ListArgs
		want    string
	}{
		{
			name:    "no args defaults",
			heading: "Filtered issues",
			args:    nil,
			want:    "No issues match the current filters.",
		},
		{
			name:    "query specific",
			heading: `Issues matching "search"`,
			args:    &rpc.ListArgs{Query: "search"},
			want:    `No issues matched "search".`,
		},
		{
			name:    "status specific",
			heading: "Ready issues",
			args:    &rpc.ListArgs{Status: "open"},
			want:    "No ready issues found.",
		},
		{
			name:    "labels all",
			heading: "Issues matching all labels",
			args:    &rpc.ListArgs{Labels: []string{"ops"}},
			want:    "No issues matched the selected labels.",
		},
		{
			name:    "assignee specific",
			heading: "Issues assigned to bob",
			args:    &rpc.ListArgs{Assignee: "bob"},
			want:    "No issues assigned to bob.",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := deriveEmptyMessage(tc.heading, tc.args); got != tc.want {
				t.Fatalf("deriveEmptyMessage() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildDetailURL(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"":        "/fragments/issue",
		"  ":      "/fragments/issue",
		"ui-42":   "/fragments/issue?id=ui-42",
		"ui 100":  "/fragments/issue?id=ui+100",
		"ops/123": "/fragments/issue?id=ops%2F123",
	}

	for input, want := range cases {
		if got := buildDetailURL(input); got != want {
			t.Fatalf("buildDetailURL(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestBuildListIssues(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, time.October, 31, 12, 0, 0, 0, time.UTC)
	issues := []*types.Issue{
		{
			ID:        "ui-10",
			Title:     "Improve filter heading",
			Status:    types.StatusInProgress,
			IssueType: types.TypeFeature,
			Priority:  1,
			UpdatedAt: now.Add(-10 * time.Minute),
		},
		nil,
		{
			ID:        "bd-5",
			Title:     "Backfill search tests",
			Status:    types.StatusOpen,
			IssueType: types.TypeTask,
			Priority:  3,
		},
	}

	result := buildListIssues(func() time.Time { return now }, issues)

	if len(result) != 2 {
		t.Fatalf("expected 2 list items, got %d", len(result))
	}

	first := result[0]
	if first.ID != "ui-10" {
		t.Fatalf("first item id = %q, want ui-10", first.ID)
	}
	if first.IssueTypeClass != "feature" {
		t.Fatalf("expected feature class, got %q", first.IssueTypeClass)
	}
	if first.IssueTypeLabel != "Feature" {
		t.Fatalf("expected feature label, got %q", first.IssueTypeLabel)
	}
	if first.PriorityClass != "p1" || first.PriorityLabel != "P1" {
		t.Fatalf("unexpected priority badge %q/%q", first.PriorityClass, first.PriorityLabel)
	}
	if first.DetailURL != "/fragments/issue?id=ui-10" {
		t.Fatalf("unexpected detail url %q", first.DetailURL)
	}
	if first.UpdatedISO == "" || !strings.HasPrefix(first.UpdatedISO, "2025-10-31T11:50:00Z") {
		t.Fatalf("expected RFC3339 timestamp, got %q", first.UpdatedISO)
	}
	if !strings.Contains(first.UpdatedRelative, "minutes ago") {
		t.Fatalf("expected relative time, got %q", first.UpdatedRelative)
	}

	second := result[1]
	if second.ID != "bd-5" {
		t.Fatalf("second item id = %q, want bd-5", second.ID)
	}
	if second.UpdatedISO == "" || !strings.HasPrefix(second.UpdatedISO, "2025-10-31T12:00:00Z") {
		t.Fatalf("expected fallback timestamp to current time, got %q", second.UpdatedISO)
	}
	if !strings.Contains(strings.ToLower(second.IssueTypeLabel), "task") {
		t.Fatalf("expected task label, got %q", second.IssueTypeLabel)
	}
	if second.PriorityClass != "p3" || second.PriorityLabel != "P3" {
		t.Fatalf("unexpected priority formatting %q/%q", second.PriorityClass, second.PriorityLabel)
	}
}
