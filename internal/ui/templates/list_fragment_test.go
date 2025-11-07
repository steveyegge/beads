package templates_test

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/ui/templates"
)

func TestListFragmentRendersIssues(t *testing.T) {
	now := time.Date(2025, 10, 22, 22, 0, 0, 0, time.UTC)

	data := templates.ListFragmentData{
		Heading:         "Blocked issues",
		SelectedIssueID: "ui-02",
		Filters: url.Values{
			"status": {"blocked"},
		},
		Issues: []templates.ListIssue{
			{
				ID:              "ui-01",
				Title:           "First issue",
				Status:          "open",
				IssueType:       "feature",
				IssueTypeLabel:  "Feature",
				IssueTypeClass:  "feature",
				Priority:        1,
				PriorityLabel:   "P1",
				PriorityClass:   "p1",
				UpdatedISO:      now.Add(-15 * time.Minute).Format(time.RFC3339),
				UpdatedRelative: templates.RelativeTimeString(now, now.Add(-15*time.Minute)),
				Active:          false,
				Index:           0,
				DetailURL:       "/fragments/issue?id=ui-01",
			},
			{
				ID:              "ui-02",
				Title:           "Second issue",
				Status:          "in_progress",
				IssueType:       "task",
				IssueTypeLabel:  "Task",
				IssueTypeClass:  "task",
				Priority:        2,
				PriorityLabel:   "P2",
				PriorityClass:   "p2",
				UpdatedISO:      now.Add(-2 * time.Hour).Format(time.RFC3339),
				UpdatedRelative: templates.RelativeTimeString(now, now.Add(-2*time.Hour)),
				Active:          true,
				Index:           1,
				DetailURL:       "/fragments/issue?id=ui-02",
			},
		},
	}

	html, err := templates.RenderListFragment(data)
	if err != nil {
		t.Fatalf("RenderListFragment: %v", err)
	}

	output := string(html)
	for _, snippet := range []string{
		`data-role="issue-list-rows"`,
		`hx-trigger="events:update from:body"`,
		`data-issue-id="ui-01"`,
		`data-issue-id="ui-02"`,
		`class="ui-issue-row is-active"`,
		`data-testid="issue-id-pill" aria-hidden="true">ui-02</span>`,
		`hx-get="/fragments/issue?id=ui-02"`,
		`data-role="issue-load-more"`,
		`hidden`,
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected fragment to contain %q\noutput=%s", snippet, output)
		}
	}

	if !strings.Contains(output, `hx-get="/fragments/issues?status=blocked&amp;selected=ui-02"`) {
		t.Fatalf("expected refresh URL to include filters and selected id\noutput=%s", output)
	}
}

func TestListFragmentRefreshURLIncludesFilters(t *testing.T) {
	data := templates.ListFragmentData{
		Heading:         "Custom filters",
		SelectedIssueID: "ui 42",
		Filters: url.Values{
			"labels": {"needs-review", "customer/enterprise"},
			"q":      {"status:open priority>1"},
		},
	}

	html, err := templates.RenderListFragment(data)
	if err != nil {
		t.Fatalf("RenderListFragment: %v", err)
	}

	output := string(html)
	fragments := []string{
		`labels=customer%2Fenterprise`,
		`labels=needs-review`,
		`q=status%3Aopen&#43;priority%3E1`,
		`selected=ui&#43;42`,
	}
	for _, fragment := range fragments {
		if !strings.Contains(output, fragment) {
			t.Fatalf("expected refresh URL to include %q\noutput=%s", fragment, output)
		}
	}
}

func TestListFragmentEmptyState(t *testing.T) {
	data := templates.ListFragmentData{
		Heading: "Filtered issues",
	}

	html, err := templates.RenderListFragment(data)
	if err != nil {
		t.Fatalf("RenderListFragment: %v", err)
	}

	output := string(html)
	if !strings.Contains(output, "No matching issues") {
		t.Fatalf("expected empty placeholder heading, got %s", output)
	}
	if !strings.Contains(output, "No issues match the current filters.") {
		t.Fatalf("expected default empty message, got %s", output)
	}
}

func TestListFragmentLoadMoreButton(t *testing.T) {
	data := templates.ListFragmentData{
		Heading:     "Closed issues",
		HasMore:     true,
		LoadMoreURL: "/fragments/issues?status=closed&limit=5",
		LoadMoreVals: map[string]any{
			"append": "1",
			"cursor": "2025-10-30T18:00:00Z|ui-50",
		},
	}

	html, err := templates.RenderListFragment(data)
	if err != nil {
		t.Fatalf("RenderListFragment: %v", err)
	}

	output := string(html)
	for _, snippet := range []string{
		`data-role="issue-load-more"`,
		`hx-get="/fragments/issues?status=closed&amp;limit=5"`,
		`&#34;append&#34;:&#34;1&#34;`,
		"Load more results",
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected load more markup to contain %q\noutput=%s", snippet, output)
		}
	}
}

func TestRenderListAppendIncludesOOBDirectives(t *testing.T) {
	data := templates.ListAppendData{
		Issues: []templates.ListIssue{
			{
				ID:             "ui-500",
				Title:          "Closed issue",
				Status:         "closed",
				IssueType:      "task",
				IssueTypeLabel: "Task",
				IssueTypeClass: "task",
				Priority:       2,
				PriorityLabel:  "P2",
				PriorityClass:  "p2",
				UpdatedISO:     time.Now().UTC().Format(time.RFC3339),
				DetailURL:      "/fragments/issue?id=ui-500",
			},
		},
		HasMore:     true,
		LoadMoreURL: "/fragments/issues?status=closed",
		LoadMoreVals: map[string]any{
			"append": "1",
			"cursor": "cursor-token",
		},
	}

	html, err := templates.RenderListAppend(data)
	if err != nil {
		t.Fatalf("RenderListAppend: %v", err)
	}

	output := string(html)
	for _, snippet := range []string{
		`hx-swap-oob="beforeend:[data-role='issue-list-items']"`,
		`ui-500`,
		`hx-swap-oob="outerHTML:[data-role='issue-load-more']"`,
	} {
		if !strings.Contains(output, snippet) {
			t.Fatalf("expected append fragment to contain %q\noutput=%s", snippet, output)
		}
	}
}

func TestRelativeTimeStringCoversIntervals(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, time.October, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name     string
		then     time.Time
		expected string
	}{
		{"just now", now.Add(-30 * time.Second), "just now"},
		{"under a minute", now.Add(-50 * time.Second), "less than a minute ago"},
		{"one minute", now.Add(-1 * time.Minute), "1 minute ago"},
		{"several minutes", now.Add(-5 * time.Minute), "5 minutes ago"},
		{"one hour", now.Add(-1 * time.Hour), "1 hour ago"},
		{"many hours", now.Add(-3 * time.Hour), "3 hours ago"},
		{"future hours treated as past", now.Add(2 * time.Hour), "2 hours ago"},
		{"one day", now.Add(-24 * time.Hour), "1 day ago"},
		{"several days", now.Add(-5 * 24 * time.Hour), "5 days ago"},
		{"one month", now.Add(-30 * 24 * time.Hour), "1 month ago"},
		{"many months", now.Add(-6 * 30 * 24 * time.Hour), "6 months ago"},
		{"one year", now.Add(-365 * 24 * time.Hour), "1 year ago"},
		{"many years", now.Add(-3 * 365 * 24 * time.Hour), "3 years ago"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := templates.RelativeTimeString(now, tc.then)
			if got != tc.expected {
				t.Fatalf("RelativeTimeString(%s, %s) = %q, want %q", now, tc.then, got, tc.expected)
			}
		})
	}
}
