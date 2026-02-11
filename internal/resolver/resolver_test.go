package resolver_test

import (
	"testing"

	"github.com/steveyegge/beads/internal/resolver"
	"github.com/steveyegge/beads/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestResolveBest(t *testing.T) {
	resources := []*types.Resource{
		{Identifier: "gpt-3.5", Name: "GPT-3.5 Turbo", Type: "model", Tags: []string{"cheap", "fast"}},
		{Identifier: "gpt-4", Name: "GPT-4", Type: "model", Tags: []string{"smart", "complex"}},
		{Identifier: "claude-haiku", Name: "Claude 3 Haiku", Type: "model", Tags: []string{"cheap", "fast"}},
		{Identifier: "claude-opus", Name: "Claude 3 Opus", Type: "model", Tags: []string{"smart", "complex"}},
		{Identifier: "coder", Name: "Senior Coder", Type: "agent", Tags: []string{"coding", "go", "python"}},
	}

	r := resolver.NewStandardResolver()

	tests := []struct {
		name     string
		req      resolver.Requirement
		wantID   string
		wantType string
	}{
		{
			name:   "Cheap Model",
			req:    resolver.Requirement{Type: "model", Profile: "cheap"},
			wantID: "gpt-3.5", // heuristic prefers "cheap" tag
		},
		{
			name:   "Smart Model",
			req:    resolver.Requirement{Type: "model", Profile: "performance"},
			wantID: "gpt-4", // heuristic prefers "smart" tag
		},
		{
			name:   "Coding Agent",
			req:    resolver.Requirement{Type: "agent", Tags: []string{"coding"}},
			wantID: "coder",
		},
		{
			name:   "Specific Tag",
			req:    resolver.Requirement{Tags: []string{"go"}},
			wantID: "coder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := r.ResolveBest(resources, tt.req)
			assert.NotNil(t, got)
			if tt.wantID != "" {
				// Note: ResolveBest isn't deterministic if scores are equal, but our test data + heuristics usually make it so.
				// For "Cheap Model", both gpt-3.5 and haiku match. The sort is stable or order dependent.
				// Let's check if it's one of the expected ones for ambiguous cases
				if tt.name == "Cheap Model" {
					assert.Contains(t, []string{"gpt-3.5", "claude-haiku"}, got.Identifier)
				} else if tt.name == "Smart Model" {
					assert.Contains(t, []string{"gpt-4", "claude-opus"}, got.Identifier)
				} else {
					assert.Equal(t, tt.wantID, got.Identifier)
				}
			}
		})
	}
}

func TestResolveAll(t *testing.T) {
	resources := []*types.Resource{
		{Identifier: "A", Name: "A", Tags: []string{"tag1", "tag2"}},
		{Identifier: "B", Name: "B", Tags: []string{"tag1"}},
		{Identifier: "C", Name: "C", Tags: []string{}},
	}

	r := resolver.NewStandardResolver()

	// Request tag1 + tag2. A should score 20, B score 10, C score 0
	req := resolver.Requirement{Tags: []string{"tag1", "tag2"}}
	got := r.ResolveAll(resources, req)

	assert.Len(t, got, 3)
	assert.Equal(t, "A", got[0].Identifier)
	assert.Equal(t, "B", got[1].Identifier)
	assert.Equal(t, "C", got[2].Identifier)
}