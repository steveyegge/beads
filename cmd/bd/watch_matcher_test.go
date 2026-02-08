package main

import (
	"testing"

	"github.com/steveyegge/beads/internal/rpc"
)

func TestParseMatcher_Empty(t *testing.T) {
	m, err := ParseMatcher("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.IsEmpty() {
		t.Fatal("expected empty matcher")
	}
}

func TestParseMatcher_SingleEqual(t *testing.T) {
	m, err := ParseMatcher("type=create")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(m.Conditions))
	}
	if m.Conditions[0].Field != "type" || m.Conditions[0].Value != "create" || m.Conditions[0].Op != OpEqual {
		t.Fatalf("unexpected condition: %+v", m.Conditions[0])
	}
}

func TestParseMatcher_MultipleConditions(t *testing.T) {
	m, err := ParseMatcher("type=status,new_status=closed,issue_type=gate")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Conditions) != 3 {
		t.Fatalf("expected 3 conditions, got %d", len(m.Conditions))
	}
}

func TestParseMatcher_Contains(t *testing.T) {
	m, err := ParseMatcher("title~=deploy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Conditions[0].Op != OpContains {
		t.Fatal("expected OpContains")
	}
}

func TestParseMatcher_IssueAlias(t *testing.T) {
	m, err := ParseMatcher("issue=bd-42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Conditions[0].Field != "issue_id" {
		t.Fatalf("expected issue_id, got %s", m.Conditions[0].Field)
	}
}

func TestParseMatcher_InvalidField(t *testing.T) {
	_, err := ParseMatcher("bogus=value")
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
}

func TestParseMatcher_InvalidSyntax(t *testing.T) {
	_, err := ParseMatcher("noequalssign")
	if err == nil {
		t.Fatal("expected error for missing = sign")
	}
}

func TestEventMatcher_Matches_SimpleType(t *testing.T) {
	m, _ := ParseMatcher("type=create")
	evt := rpc.MutationEvent{Type: rpc.MutationCreate}
	if !m.Matches(evt) {
		t.Fatal("expected match")
	}

	evt2 := rpc.MutationEvent{Type: rpc.MutationUpdate}
	if m.Matches(evt2) {
		t.Fatal("expected no match")
	}
}

func TestEventMatcher_Matches_MultipleAND(t *testing.T) {
	m, _ := ParseMatcher("type=status,new_status=closed")
	evt := rpc.MutationEvent{Type: rpc.MutationStatus, NewStatus: "closed"}
	if !m.Matches(evt) {
		t.Fatal("expected match")
	}

	evt2 := rpc.MutationEvent{Type: rpc.MutationStatus, NewStatus: "in_progress"}
	if m.Matches(evt2) {
		t.Fatal("expected no match")
	}
}

func TestEventMatcher_Matches_Label(t *testing.T) {
	m, _ := ParseMatcher("label=urgent")
	evt := rpc.MutationEvent{Labels: []string{"urgent", "p0"}}
	if !m.Matches(evt) {
		t.Fatal("expected match - label 'urgent' is present")
	}

	evt2 := rpc.MutationEvent{Labels: []string{"p1", "feature"}}
	if m.Matches(evt2) {
		t.Fatal("expected no match - label 'urgent' not present")
	}
}

func TestEventMatcher_Matches_LabelContains(t *testing.T) {
	m, _ := ParseMatcher("label~=urg")
	evt := rpc.MutationEvent{Labels: []string{"urgent"}}
	if !m.Matches(evt) {
		t.Fatal("expected match - label contains 'urg'")
	}
}

func TestEventMatcher_Matches_IssueType(t *testing.T) {
	m, _ := ParseMatcher("issue_type=gate,await_type=decision")
	evt := rpc.MutationEvent{IssueType: "gate", AwaitType: "decision"}
	if !m.Matches(evt) {
		t.Fatal("expected match")
	}
}

func TestEventMatcher_Matches_TitleContains(t *testing.T) {
	m, _ := ParseMatcher("title~=deploy to prod")
	evt := rpc.MutationEvent{Title: "Should we deploy to production?"}
	if !m.Matches(evt) {
		t.Fatal("expected match")
	}
}

func TestEventMatcher_Matches_EmptyMatcher(t *testing.T) {
	m, _ := ParseMatcher("")
	evt := rpc.MutationEvent{Type: "anything"}
	if !m.Matches(evt) {
		t.Fatal("empty matcher should match everything")
	}
}
