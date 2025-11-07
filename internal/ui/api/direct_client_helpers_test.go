package api

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

func TestUpdatesFromRPCArgsIncludesSetFields(t *testing.T) {
	t.Parallel()

	title := "Updated title"
	description := "Updated description"
	status := string(types.StatusInProgress)
	priority := 2
	design := "New design"
	ac := "Acceptance criteria"
	notes := "Additional notes"
	assignee := "alice"

	args := rpc.UpdateArgs{
		Title:              &title,
		Description:        &description,
		Status:             &status,
		Priority:           &priority,
		Design:             &design,
		AcceptanceCriteria: &ac,
		Notes:              &notes,
		Assignee:           &assignee,
	}

	updates := updatesFromRPCArgs(args)

	if len(updates) != 8 {
		t.Fatalf("expected 8 update fields, got %d: %#v", len(updates), updates)
	}
	assertUpdateValue(t, updates, "title", title)
	assertUpdateValue(t, updates, "description", description)
	assertUpdateValue(t, updates, "status", status)
	assertUpdateValue(t, updates, "priority", priority)
	assertUpdateValue(t, updates, "design", design)
	assertUpdateValue(t, updates, "acceptance_criteria", ac)
	assertUpdateValue(t, updates, "notes", notes)
	assertUpdateValue(t, updates, "assignee", assignee)
}

func TestUpdatesFromRPCArgsSkipsNilPointers(t *testing.T) {
	t.Parallel()

	updates := updatesFromRPCArgs(rpc.UpdateArgs{})
	if len(updates) != 0 {
		t.Fatalf("expected no updates, got %#v", updates)
	}
}

func TestConvertListArgsToFilterCopiesFields(t *testing.T) {
	t.Parallel()

	priority := 1
	args := &rpc.ListArgs{
		Status:    string(types.StatusBlocked),
		Priority:  &priority,
		IssueType: string(types.TypeBug),
		Assignee:  "bob",
		Labels:    []string{"critical", "ui"},
		LabelsAny: []string{"backend"},
		IDs:       []string{"ui-1", "ui-2"},
		IDPrefix:  "ui-",
		Limit:     50,
		Order:     "updated-desc,title-asc",
	}

	filter := convertListArgsToFilter(args)

	if filter.Limit != 50 {
		t.Fatalf("filter limit = %d, want 50", filter.Limit)
	}
	if filter.Priority == nil || filter.Priority != args.Priority {
		t.Fatalf("priority pointer not propagated: %v vs %v", filter.Priority, args.Priority)
	}
	if filter.Status == nil || *filter.Status != types.StatusBlocked {
		t.Fatalf("status = %v, want %v", filter.Status, types.StatusBlocked)
	}
	if filter.IssueType == nil || *filter.IssueType != types.TypeBug {
		t.Fatalf("issue type = %v, want %v", filter.IssueType, types.TypeBug)
	}
	if filter.Assignee == nil || *filter.Assignee != "bob" {
		t.Fatalf("assignee = %v, want bob", filter.Assignee)
	}
	if len(filter.Labels) != 2 || filter.Labels[0] != "critical" || filter.Labels[1] != "ui" {
		t.Fatalf("labels = %#v, want %v", filter.Labels, args.Labels)
	}
	if len(filter.LabelsAny) != 1 || filter.LabelsAny[0] != "backend" {
		t.Fatalf("labels any = %#v, want %v", filter.LabelsAny, args.LabelsAny)
	}
	if len(filter.IDs) != 2 || filter.IDs[0] != "ui-1" {
		t.Fatalf("ids = %#v, want %v", filter.IDs, args.IDs)
	}
	if filter.IDPrefix != "ui-" {
		t.Fatalf("id prefix = %q, want %q", filter.IDPrefix, args.IDPrefix)
	}
	if len(filter.Sort) != 2 {
		t.Fatalf("expected two sort options, got %#v", filter.Sort)
	}
	if filter.Sort[0].Field != types.SortFieldUpdated || filter.Sort[0].Direction != types.SortDesc {
		t.Fatalf("unexpected primary sort %+v", filter.Sort[0])
	}
	if filter.Sort[1].Field != types.SortFieldTitle || filter.Sort[1].Direction != types.SortAsc {
		t.Fatalf("unexpected secondary sort %+v", filter.Sort[1])
	}
}

func TestConvertListArgsToFilterHandlesEmptyOptionalFields(t *testing.T) {
	t.Parallel()

	args := &rpc.ListArgs{
		Labels: []string{},
		Limit:  0,
	}

	filter := convertListArgsToFilter(args)

	if filter.Priority != nil {
		t.Fatalf("expected nil priority, got %v", filter.Priority)
	}
	if filter.Status != nil {
		t.Fatalf("expected nil status, got %v", filter.Status)
	}
	if filter.IssueType != nil {
		t.Fatalf("expected nil issue type, got %v", filter.IssueType)
	}
	if filter.Assignee != nil {
		t.Fatalf("expected nil assignee, got %v", filter.Assignee)
	}
	if len(filter.Labels) != 0 {
		t.Fatalf("expected empty labels slice, got %#v", filter.Labels)
	}
}

func TestConvertListArgsToFilterHandlesClosedCursor(t *testing.T) {
	t.Parallel()

	now := time.Date(2025, 10, 30, 18, 0, 0, 0, time.UTC)
	cursor := now.Format(time.RFC3339Nano) + "|ui-77"
	args := &rpc.ListArgs{
		Order:  closedQueueOrder,
		Cursor: cursor,
		Limit:  5,
	}

	filter := convertListArgsToFilter(args)

	if !filter.OrderClosed {
		t.Fatalf("expected OrderClosed to be true")
	}
	if filter.ClosedBefore == nil {
		t.Fatalf("expected ClosedBefore to be populated")
	}
	if filter.ClosedBeforeID != "ui-77" {
		t.Fatalf("expected ClosedBeforeID ui-77, got %q", filter.ClosedBeforeID)
	}
	if filter.ClosedBefore != nil && !filter.ClosedBefore.Equal(now) {
		t.Fatalf("expected ClosedBefore time %v, got %v", now, filter.ClosedBefore)
	}
}

func assertUpdateValue(t *testing.T, updates map[string]interface{}, key string, want interface{}) {
	t.Helper()

	got, ok := updates[key]
	if !ok {
		t.Fatalf("expected key %q to be present", key)
	}
	switch v := want.(type) {
	case string:
		s, ok := got.(string)
		if !ok || s != v {
			t.Fatalf("key %q = %v, want %v", key, got, v)
		}
	case int:
		i, ok := got.(int)
		if !ok || i != v {
			t.Fatalf("key %q = %v, want %v", key, got, v)
		}
	default:
		t.Fatalf("unexpected want type %T for key %q", want, key)
	}
}
