package slackbot

import (
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

func TestConvertDecisionResponse(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	resolvedAt := time.Date(2026, 1, 15, 11, 0, 0, 0, time.UTC)

	threeOptsJSON := `[{"id":"a","label":"Alpha","description":"First option"},{"id":"b","label":"Beta","description":"Second option"},{"id":"c","label":"Gamma","description":"Third option"}]`
	twoOptsJSON := `[{"id":"a","label":"Alpha","description":"First"},{"id":"b","label":"Beta"}]`

	tests := []struct {
		name     string
		input    *rpc.DecisionResponse
		check    func(t *testing.T, d Decision)
	}{
		{
			name:  "nil input returns zero-value Decision",
			input: nil,
			check: func(t *testing.T, d Decision) {
				if d.ID != "" {
					t.Errorf("ID = %q, want empty", d.ID)
				}
				if d.Question != "" {
					t.Errorf("Question = %q, want empty", d.Question)
				}
				if d.Resolved {
					t.Error("Resolved = true, want false")
				}
				if d.ChosenIndex != 0 {
					t.Errorf("ChosenIndex = %d, want 0", d.ChosenIndex)
				}
				if len(d.Options) != 0 {
					t.Errorf("Options length = %d, want 0", len(d.Options))
				}
				if !d.RequestedAt.IsZero() {
					t.Errorf("RequestedAt = %v, want zero time", d.RequestedAt)
				}
			},
		},
		{
			name: "nil Issue with non-nil Decision",
			input: &rpc.DecisionResponse{
				Issue: nil,
				Decision: &types.DecisionPoint{
					Prompt:      "Which database?",
					Context:     "We need persistence",
					RequestedBy: "agent-1",
					Urgency:     "high",
					Options:     twoOptsJSON,
				},
			},
			check: func(t *testing.T, d Decision) {
				if d.ID != "" {
					t.Errorf("ID = %q, want empty (nil Issue)", d.ID)
				}
				if d.SemanticSlug != "" {
					t.Errorf("SemanticSlug = %q, want empty (nil Issue)", d.SemanticSlug)
				}
				if !d.RequestedAt.IsZero() {
					t.Errorf("RequestedAt = %v, want zero time (nil Issue)", d.RequestedAt)
				}
				if d.Question != "Which database?" {
					t.Errorf("Question = %q, want %q", d.Question, "Which database?")
				}
				if d.Context != "We need persistence" {
					t.Errorf("Context = %q, want %q", d.Context, "We need persistence")
				}
				if d.RequestedBy != "agent-1" {
					t.Errorf("RequestedBy = %q, want %q", d.RequestedBy, "agent-1")
				}
				if d.Urgency != "high" {
					t.Errorf("Urgency = %q, want %q", d.Urgency, "high")
				}
				if len(d.Options) != 2 {
					t.Errorf("Options length = %d, want 2", len(d.Options))
				}
			},
		},
		{
			name: "nil Decision with non-nil Issue",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{
					ID:           "issue-123",
					SemanticSlug: "proj-decision-database-choice-x7k",
					CreatedAt:    now,
					Title:        "Database Choice",
				},
				Decision: nil,
			},
			check: func(t *testing.T, d Decision) {
				if d.ID != "issue-123" {
					t.Errorf("ID = %q, want %q", d.ID, "issue-123")
				}
				if d.SemanticSlug != "proj-decision-database-choice-x7k" {
					t.Errorf("SemanticSlug = %q, want %q", d.SemanticSlug, "proj-decision-database-choice-x7k")
				}
				if !d.RequestedAt.Equal(now) {
					t.Errorf("RequestedAt = %v, want %v", d.RequestedAt, now)
				}
				// All decision-derived fields should be zero
				if d.Question != "" {
					t.Errorf("Question = %q, want empty", d.Question)
				}
				if d.Resolved {
					t.Error("Resolved = true, want false")
				}
				if d.ChosenIndex != 0 {
					t.Errorf("ChosenIndex = %d, want 0", d.ChosenIndex)
				}
				if len(d.Options) != 0 {
					t.Errorf("Options length = %d, want 0", len(d.Options))
				}
				if d.ParentBeadTitle != "" {
					t.Errorf("ParentBeadTitle = %q, want empty (nil Decision)", d.ParentBeadTitle)
				}
			},
		},
		{
			name: "full decision unresolved",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{
					ID:           "issue-456",
					SemanticSlug: "proj-decision-cache-strategy-m2p",
					CreatedAt:    now,
					Title:        "Cache Strategy",
				},
				Decision: &types.DecisionPoint{
					Prompt:       "Which caching strategy?",
					Context:      "Need sub-100ms reads",
					RequestedBy:  "agent-build",
					Urgency:      "medium",
					PriorID:      "issue-400",
					ParentBeadID: "bead-parent-1",
					RespondedAt:  nil,
					RespondedBy:  "",
					Guidance:     "",
					Options:      threeOptsJSON,
				},
			},
			check: func(t *testing.T, d Decision) {
				if d.ID != "issue-456" {
					t.Errorf("ID = %q, want %q", d.ID, "issue-456")
				}
				if d.SemanticSlug != "proj-decision-cache-strategy-m2p" {
					t.Errorf("SemanticSlug = %q, want %q", d.SemanticSlug, "proj-decision-cache-strategy-m2p")
				}
				if !d.RequestedAt.Equal(now) {
					t.Errorf("RequestedAt = %v, want %v", d.RequestedAt, now)
				}
				if d.Question != "Which caching strategy?" {
					t.Errorf("Question = %q, want %q", d.Question, "Which caching strategy?")
				}
				if d.Context != "Need sub-100ms reads" {
					t.Errorf("Context = %q, want %q", d.Context, "Need sub-100ms reads")
				}
				if d.RequestedBy != "agent-build" {
					t.Errorf("RequestedBy = %q, want %q", d.RequestedBy, "agent-build")
				}
				if d.Urgency != "medium" {
					t.Errorf("Urgency = %q, want %q", d.Urgency, "medium")
				}
				if d.PredecessorID != "issue-400" {
					t.Errorf("PredecessorID = %q, want %q", d.PredecessorID, "issue-400")
				}
				if d.ParentBeadID != "bead-parent-1" {
					t.Errorf("ParentBeadID = %q, want %q", d.ParentBeadID, "bead-parent-1")
				}
				if d.Resolved {
					t.Error("Resolved = true, want false")
				}
				if d.ResolvedBy != "" {
					t.Errorf("ResolvedBy = %q, want empty", d.ResolvedBy)
				}
				if d.Rationale != "" {
					t.Errorf("Rationale = %q, want empty", d.Rationale)
				}
				if d.ChosenIndex != 0 {
					t.Errorf("ChosenIndex = %d, want 0", d.ChosenIndex)
				}
				if len(d.Options) != 3 {
					t.Fatalf("Options length = %d, want 3", len(d.Options))
				}
				if d.Options[0].ID != "a" || d.Options[0].Label != "Alpha" || d.Options[0].Description != "First option" {
					t.Errorf("Options[0] = %+v, want {ID:a Label:Alpha Description:First option}", d.Options[0])
				}
				// ParentBeadTitle should be set since ParentBeadID is non-empty and Issue is non-nil
				if d.ParentBeadTitle != "Cache Strategy" {
					t.Errorf("ParentBeadTitle = %q, want %q", d.ParentBeadTitle, "Cache Strategy")
				}
			},
		},
		{
			name: "full decision resolved",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{
					ID:           "issue-789",
					SemanticSlug: "proj-decision-auth-method-q3r",
					CreatedAt:    now,
					Title:        "Auth Method",
				},
				Decision: &types.DecisionPoint{
					Prompt:         "Which auth method?",
					Context:        "Security audit required",
					RequestedBy:    "agent-sec",
					Urgency:        "high",
					Options:        threeOptsJSON,
					SelectedOption: "b",
					RespondedAt:    &resolvedAt,
					RespondedBy:    "human@example.com",
					Guidance:       "Beta is the best fit for our compliance needs",
				},
			},
			check: func(t *testing.T, d Decision) {
				if !d.Resolved {
					t.Error("Resolved = false, want true")
				}
				if d.ResolvedBy != "human@example.com" {
					t.Errorf("ResolvedBy = %q, want %q", d.ResolvedBy, "human@example.com")
				}
				if d.Rationale != "Beta is the best fit for our compliance needs" {
					t.Errorf("Rationale = %q, want %q", d.Rationale, "Beta is the best fit for our compliance needs")
				}
				if d.ChosenIndex != 2 {
					t.Errorf("ChosenIndex = %d, want 2 (option 'b' is second)", d.ChosenIndex)
				}
			},
		},
		{
			name: "options parsing with description",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{ID: "opt-test"},
				Decision: &types.DecisionPoint{
					Options: `[{"id":"a","label":"Alpha","description":"First"},{"id":"b","label":"Beta"}]`,
				},
			},
			check: func(t *testing.T, d Decision) {
				if len(d.Options) != 2 {
					t.Fatalf("Options length = %d, want 2", len(d.Options))
				}
				opt0 := d.Options[0]
				if opt0.ID != "a" || opt0.Label != "Alpha" || opt0.Description != "First" {
					t.Errorf("Options[0] = %+v, want {ID:a Label:Alpha Description:First}", opt0)
				}
				opt1 := d.Options[1]
				if opt1.ID != "b" || opt1.Label != "Beta" || opt1.Description != "" {
					t.Errorf("Options[1] = %+v, want {ID:b Label:Beta Description:}", opt1)
				}
			},
		},
		{
			name: "ChosenIndex maps to correct 1-based index",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{ID: "idx-test"},
				Decision: &types.DecisionPoint{
					Options:        threeOptsJSON,
					SelectedOption: "c",
					RespondedAt:    &resolvedAt,
				},
			},
			check: func(t *testing.T, d Decision) {
				if d.ChosenIndex != 3 {
					t.Errorf("ChosenIndex = %d, want 3 (option 'c' is third)", d.ChosenIndex)
				}
			},
		},
		{
			name: "ChosenIndex for first option",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{ID: "idx-first"},
				Decision: &types.DecisionPoint{
					Options:        threeOptsJSON,
					SelectedOption: "a",
					RespondedAt:    &resolvedAt,
				},
			},
			check: func(t *testing.T, d Decision) {
				if d.ChosenIndex != 1 {
					t.Errorf("ChosenIndex = %d, want 1 (option 'a' is first)", d.ChosenIndex)
				}
			},
		},
		{
			name: "SelectedOption not in options list yields ChosenIndex 0",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{ID: "idx-missing"},
				Decision: &types.DecisionPoint{
					Options:        threeOptsJSON,
					SelectedOption: "nonexistent",
					RespondedAt:    &resolvedAt,
				},
			},
			check: func(t *testing.T, d Decision) {
				if d.ChosenIndex != 0 {
					t.Errorf("ChosenIndex = %d, want 0 (option not found)", d.ChosenIndex)
				}
			},
		},
		{
			name: "empty options string yields empty Options slice",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{ID: "empty-opts"},
				Decision: &types.DecisionPoint{
					Prompt:  "A question?",
					Options: "",
				},
			},
			check: func(t *testing.T, d Decision) {
				if len(d.Options) != 0 {
					t.Errorf("Options length = %d, want 0", len(d.Options))
				}
				if d.ChosenIndex != 0 {
					t.Errorf("ChosenIndex = %d, want 0", d.ChosenIndex)
				}
				if d.Question != "A question?" {
					t.Errorf("Question = %q, want %q", d.Question, "A question?")
				}
			},
		},
		{
			name: "invalid options JSON yields empty Options slice",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{ID: "bad-json"},
				Decision: &types.DecisionPoint{
					Prompt:  "Another question?",
					Options: "not valid json {{{",
				},
			},
			check: func(t *testing.T, d Decision) {
				if len(d.Options) != 0 {
					t.Errorf("Options length = %d, want 0 (invalid JSON)", len(d.Options))
				}
				// The function should still populate other fields
				if d.Question != "Another question?" {
					t.Errorf("Question = %q, want %q", d.Question, "Another question?")
				}
			},
		},
		{
			name: "ParentBeadTitle set when ParentBeadID non-empty and Issue non-nil",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{
					ID:    "pbt-set",
					Title: "Epic: Rewrite Auth Layer",
				},
				Decision: &types.DecisionPoint{
					Prompt:       "How to handle tokens?",
					ParentBeadID: "bead-epic-42",
				},
			},
			check: func(t *testing.T, d Decision) {
				if d.ParentBeadID != "bead-epic-42" {
					t.Errorf("ParentBeadID = %q, want %q", d.ParentBeadID, "bead-epic-42")
				}
				if d.ParentBeadTitle != "Epic: Rewrite Auth Layer" {
					t.Errorf("ParentBeadTitle = %q, want %q", d.ParentBeadTitle, "Epic: Rewrite Auth Layer")
				}
			},
		},
		{
			name: "ParentBeadTitle empty when ParentBeadID empty even if Issue has Title",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{
					ID:    "pbt-empty",
					Title: "Some Title That Should Not Appear",
				},
				Decision: &types.DecisionPoint{
					Prompt:       "Something?",
					ParentBeadID: "",
				},
			},
			check: func(t *testing.T, d Decision) {
				if d.ParentBeadTitle != "" {
					t.Errorf("ParentBeadTitle = %q, want empty (ParentBeadID is empty)", d.ParentBeadTitle)
				}
			},
		},
		{
			name: "ParentBeadTitle empty when ParentBeadID set but Issue nil",
			input: &rpc.DecisionResponse{
				Issue: nil,
				Decision: &types.DecisionPoint{
					Prompt:       "Something?",
					ParentBeadID: "bead-orphan",
				},
			},
			check: func(t *testing.T, d Decision) {
				if d.ParentBeadID != "bead-orphan" {
					t.Errorf("ParentBeadID = %q, want %q", d.ParentBeadID, "bead-orphan")
				}
				if d.ParentBeadTitle != "" {
					t.Errorf("ParentBeadTitle = %q, want empty (Issue is nil)", d.ParentBeadTitle)
				}
			},
		},
		{
			name: "Recommended field on DecisionOption defaults to false",
			input: &rpc.DecisionResponse{
				Issue: &types.Issue{ID: "rec-test"},
				Decision: &types.DecisionPoint{
					Options: `[{"id":"x","label":"Option X"}]`,
				},
			},
			check: func(t *testing.T, d Decision) {
				if len(d.Options) != 1 {
					t.Fatalf("Options length = %d, want 1", len(d.Options))
				}
				if d.Options[0].Recommended {
					t.Error("Options[0].Recommended = true, want false (not stored in beads)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := convertDecisionResponse(tt.input)
			tt.check(t, d)
		})
	}
}
