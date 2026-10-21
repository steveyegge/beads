package fizzy

import (
	"slices"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func TestLaneForStatusDefaults(t *testing.T) {
	config := DefaultMappingConfig()
	config.CustomStatuses["review"] = types.CategoryWIP
	config.CustomStatuses["idea"] = types.CategoryUnspecified

	tests := []struct {
		status      types.Status
		firstColumn string
		want        string
	}{
		{types.StatusOpen, "Doing", LaneMaybe},
		{types.StatusInProgress, "Doing", "Doing"},
		{types.StatusBlocked, "Doing", "Doing"},
		{types.StatusHooked, "Doing", "Doing"},
		{types.StatusInProgress, "", LaneMaybe},
		{types.StatusDeferred, "Doing", LaneNotNow},
		{types.StatusPinned, "Doing", LaneNotNow},
		{types.StatusClosed, "Doing", LaneDone},
		{"review", "Doing", "Doing"},
		{"idea", "Doing", LaneMaybe},
	}
	for _, test := range tests {
		if got := config.LaneForStatus(test.status, test.firstColumn); got != test.want {
			t.Errorf("LaneForStatus(%q, %q) = %q, want %q", test.status, test.firstColumn, got, test.want)
		}
	}
}

func TestStatusMapRoundTrip(t *testing.T) {
	config := DefaultMappingConfig()
	config.StatusMap = map[string]string{
		"open":        "Ready",
		"in_progress": "In Progress",
		"blocked":     "Blocked",
		"review":      "In Progress", // a second status for the same column
	}

	for _, status := range []types.Status{types.StatusOpen, types.StatusInProgress, types.StatusBlocked} {
		lane := config.LaneForStatus(status, "Ready")
		if got := config.StatusForLane(lane); got != status {
			t.Errorf("StatusForLane(LaneForStatus(%q)) = %q", status, got)
		}
	}
	tests := map[string]types.Status{
		"in progress":  types.StatusInProgress, // case-insensitive, built-in status first
		LaneMaybe:      types.StatusOpen,
		LaneNotNow:     types.StatusDeferred,
		LaneDone:       types.StatusClosed,
		"Other column": types.StatusInProgress,
	}
	for lane, want := range tests {
		if got := config.StatusForLane(lane); got != want {
			t.Errorf("StatusForLane(%q) = %q, want %q", lane, got, want)
		}
	}
}

func TestCardLane(t *testing.T) {
	tests := []struct {
		card Card
		want string
	}{
		{Card{}, LaneMaybe},
		{Card{Column: &Column{Name: "Doing"}}, "Doing"},
		{Card{Postponed: true}, LaneNotNow},
		{Card{Closed: true, Column: &Column{Name: "Doing"}}, LaneDone},
	}
	for _, test := range tests {
		if got := CardLane(&test.card); got != test.want {
			t.Errorf("CardLane(%+v) = %q, want %q", test.card, got, test.want)
		}
	}
}

func TestTagsRoundTrip(t *testing.T) {
	issue := &types.Issue{Priority: 1, IssueType: types.TypeBug, Labels: []string{"Repo:Beads", "#urgent", "repo:beads"}}
	tags := TagsForIssue(issue)
	if want := []string{"p1", "repo:beads", "type::bug", "urgent"}; !slices.Equal(tags, want) {
		t.Fatalf("TagsForIssue() = %v, want %v", tags, want)
	}

	priority, issueType, labels := SplitTags(tags)
	if priority != 1 || issueType != types.TypeBug || !slices.Equal(labels, []string{"repo:beads", "urgent"}) {
		t.Errorf("SplitTags() = %d, %q, %v", priority, issueType, labels)
	}
}

func TestSplitTagsWithoutOrWithManyScopedTags(t *testing.T) {
	priority, issueType, labels := SplitTags([]string{"idea"})
	if priority != -1 || issueType != "" || !slices.Equal(labels, []string{"idea"}) {
		t.Errorf("SplitTags(idea) = %d, %q, %v", priority, issueType, labels)
	}
	priority, issueType, labels = SplitTags([]string{"p3", "p1", "type::task", "type::bug", "p9"})
	if priority != 1 || issueType != types.TypeBug || !slices.Equal(labels, []string{"p9"}) {
		t.Errorf("SplitTags(many) = %d, %q, %v", priority, issueType, labels)
	}
}

func TestContentHash(t *testing.T) {
	issue := &types.Issue{Title: "T", Description: "a\nb", Priority: 2, IssueType: types.TypeTask}
	same := &types.Issue{Title: "T", Description: "a  \r\nb\n\n", Priority: 2, IssueType: types.TypeTask}
	if contentHash(issue, "Doing") != contentHash(same, "doing") {
		t.Error("the hash must ignore line endings, trailing white space and the case of the lane")
	}
	joined := &types.Issue{Title: "T", Description: "a b", Priority: 2, IssueType: types.TypeTask}
	if contentHash(issue, "Doing") == contentHash(joined, "Doing") {
		t.Error("the hash must change with the line breaks")
	}
	other := *issue
	other.Priority = 1
	if contentHash(issue, "Doing") == contentHash(&other, "Doing") {
		t.Error("the hash must change with the priority")
	}
	if contentHash(issue, "Doing") == contentHash(issue, LaneDone) {
		t.Error("the hash must change with the lane")
	}
}
