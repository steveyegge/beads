package main

import (
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/rpc"
)

// MatchOp defines the comparison operation for a match condition.
type MatchOp int

const (
	OpEqual    MatchOp = iota // Exact equality
	OpContains                // Substring match (uses ~= syntax)
)

// MatchCondition is a single field comparison.
type MatchCondition struct {
	Field string
	Value string
	Op    MatchOp
}

// EventMatcher holds a set of conditions that must ALL match (AND logic).
type EventMatcher struct {
	Conditions []MatchCondition
}

// ParseMatcher parses a comma-separated list of key=value conditions.
// Supports = (exact) and ~= (contains) operators.
// Example: "type=create,issue_type=gate" or "title~=deploy,new_status=closed"
func ParseMatcher(expr string) (*EventMatcher, error) {
	if expr == "" {
		return &EventMatcher{}, nil
	}

	var conditions []MatchCondition
	for _, part := range strings.Split(expr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		var cond MatchCondition

		// Check for ~= (contains) first, then = (equal)
		if idx := strings.Index(part, "~="); idx > 0 {
			cond.Field = strings.TrimSpace(part[:idx])
			cond.Value = strings.TrimSpace(part[idx+2:])
			cond.Op = OpContains
		} else if idx := strings.Index(part, "="); idx > 0 {
			cond.Field = strings.TrimSpace(part[:idx])
			cond.Value = strings.TrimSpace(part[idx+1:])
			cond.Op = OpEqual
		} else {
			return nil, fmt.Errorf("invalid condition %q: expected key=value or key~=value", part)
		}

		if cond.Field == "" {
			return nil, fmt.Errorf("empty field name in condition %q", part)
		}

		// Normalize field aliases
		switch cond.Field {
		case "issue", "issue_id":
			cond.Field = "issue_id"
		case "label":
			cond.Field = "label"
		default:
			// Validate field name
			if !isMatchableField(cond.Field) {
				return nil, fmt.Errorf("unknown matchable field %q (valid: type, issue_id, title, assignee, actor, old_status, new_status, parent_id, issue_type, await_type, label)", cond.Field)
			}
		}

		conditions = append(conditions, cond)
	}

	return &EventMatcher{Conditions: conditions}, nil
}

// isMatchableField returns true if the field name can be used in a match condition.
func isMatchableField(field string) bool {
	switch field {
	case "type", "issue_id", "title", "assignee", "actor",
		"old_status", "new_status", "parent_id",
		"issue_type", "await_type", "label":
		return true
	}
	return false
}

// Matches returns true if the event satisfies ALL conditions in the matcher.
func (m *EventMatcher) Matches(evt rpc.MutationEvent) bool {
	for _, cond := range m.Conditions {
		if !matchCondition(cond, evt) {
			return false
		}
	}
	return true
}

// IsEmpty returns true if the matcher has no conditions.
func (m *EventMatcher) IsEmpty() bool {
	return len(m.Conditions) == 0
}

// matchCondition checks a single condition against an event.
func matchCondition(cond MatchCondition, evt rpc.MutationEvent) bool {
	fieldValue := getEventField(cond.Field, evt)

	// Special case: "label" matches against any label in the list
	if cond.Field == "label" {
		for _, l := range evt.Labels {
			if matchValue(cond.Op, l, cond.Value) {
				return true
			}
		}
		return false
	}

	return matchValue(cond.Op, fieldValue, cond.Value)
}

// matchValue compares a field value against a condition value using the given operator.
func matchValue(op MatchOp, fieldValue, condValue string) bool {
	switch op {
	case OpEqual:
		return fieldValue == condValue
	case OpContains:
		return strings.Contains(fieldValue, condValue)
	}
	return false
}

// getEventField extracts a named field from a MutationEvent.
func getEventField(field string, evt rpc.MutationEvent) string {
	switch field {
	case "type":
		return evt.Type
	case "issue_id":
		return evt.IssueID
	case "title":
		return evt.Title
	case "assignee":
		return evt.Assignee
	case "actor":
		return evt.Actor
	case "old_status":
		return evt.OldStatus
	case "new_status":
		return evt.NewStatus
	case "parent_id":
		return evt.ParentID
	case "issue_type":
		return evt.IssueType
	case "await_type":
		return evt.AwaitType
	}
	return ""
}
