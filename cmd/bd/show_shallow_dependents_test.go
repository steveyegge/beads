package main

import (
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

// TestShallowDependentsForJSON_StripsHeavyFields is the regression guard for
// be-4d36f2: bd show --json must not embed the heavy free-form fields
// (Description, Design, Notes, AcceptanceCriteria) of every dependent into
// the output. On hub beads with thousands of dependents this previously
// allocated 5-13 GB.
func TestShallowDependentsForJSON_StripsHeavyFields(t *testing.T) {
	heavy := strings.Repeat("x", 1024) // simulate a long free-form field
	raw := []*types.IssueWithDependencyMetadata{
		{
			Issue: types.Issue{
				ID:                 "be-1",
				Status:             types.StatusOpen,
				IssueType:          types.TypeTask,
				Priority:           1,
				Title:              "title 1",
				Description:        heavy,
				Design:             heavy,
				Notes:              heavy,
				AcceptanceCriteria: heavy,
			},
			DependencyType: types.DepBlocks,
		},
		{
			Issue: types.Issue{
				ID:          "be-2",
				Status:      types.StatusClosed,
				IssueType:   types.TypeBug,
				Priority:    2,
				Title:       "title 2",
				Description: heavy,
			},
			DependencyType: types.DepParentChild,
		},
	}

	got := shallowDependentsForJSON(raw)
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}

	for i, dep := range got {
		if dep == nil {
			t.Fatalf("entry %d nil", i)
		}
		// Identity-and-shape fields preserved
		if dep.Issue.ID != raw[i].Issue.ID {
			t.Errorf("entry %d: ID got %q want %q", i, dep.Issue.ID, raw[i].Issue.ID)
		}
		if dep.Issue.Status != raw[i].Issue.Status {
			t.Errorf("entry %d: Status got %q want %q", i, dep.Issue.Status, raw[i].Issue.Status)
		}
		if dep.Issue.IssueType != raw[i].Issue.IssueType {
			t.Errorf("entry %d: IssueType got %q want %q", i, dep.Issue.IssueType, raw[i].Issue.IssueType)
		}
		if dep.Issue.Priority != raw[i].Issue.Priority {
			t.Errorf("entry %d: Priority got %d want %d", i, dep.Issue.Priority, raw[i].Issue.Priority)
		}
		if dep.Issue.Title != raw[i].Issue.Title {
			t.Errorf("entry %d: Title got %q want %q", i, dep.Issue.Title, raw[i].Issue.Title)
		}
		if dep.DependencyType != raw[i].DependencyType {
			t.Errorf("entry %d: DependencyType got %q want %q", i, dep.DependencyType, raw[i].DependencyType)
		}
		// Heavy fields stripped
		if dep.Issue.Description != "" {
			t.Errorf("entry %d: Description not stripped (len=%d)", i, len(dep.Issue.Description))
		}
		if dep.Issue.Design != "" {
			t.Errorf("entry %d: Design not stripped (len=%d)", i, len(dep.Issue.Design))
		}
		if dep.Issue.Notes != "" {
			t.Errorf("entry %d: Notes not stripped (len=%d)", i, len(dep.Issue.Notes))
		}
		if dep.Issue.AcceptanceCriteria != "" {
			t.Errorf("entry %d: AcceptanceCriteria not stripped (len=%d)", i, len(dep.Issue.AcceptanceCriteria))
		}
	}
}

func TestShallowDependentsForJSON_NilAndEmpty(t *testing.T) {
	if got := shallowDependentsForJSON(nil); got != nil {
		t.Errorf("nil input: got non-nil (%d entries)", len(got))
	}
	if got := shallowDependentsForJSON([]*types.IssueWithDependencyMetadata{}); got != nil {
		t.Errorf("empty input: got non-nil (%d entries)", len(got))
	}
}

func TestShallowDependentsForJSON_SkipsNilEntries(t *testing.T) {
	raw := []*types.IssueWithDependencyMetadata{
		nil,
		{Issue: types.Issue{ID: "be-1"}, DependencyType: types.DepBlocks},
		nil,
		{Issue: types.Issue{ID: "be-2"}, DependencyType: types.DepBlocks},
	}
	got := shallowDependentsForJSON(raw)
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (nils skipped)", len(got))
	}
	if got[0].Issue.ID != "be-1" || got[1].Issue.ID != "be-2" {
		t.Errorf("unexpected IDs: %q, %q", got[0].Issue.ID, got[1].Issue.ID)
	}
}
