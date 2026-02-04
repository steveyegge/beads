package main

import "testing"

// TestMatchesSubscriptionsRigScoping tests that rig-scoped advice doesn't leak
func TestMatchesSubscriptionsRigScoping(t *testing.T) {
	tests := []struct {
		name          string
		adviceLabels  []string
		subscriptions []string
		want          bool
	}{
		{
			name:          "rig advice matches same rig",
			adviceLabels:  []string{"rig:gastown", "role:crew"},
			subscriptions: []string{"rig:gastown", "role:crew", "global"},
			want:          true,
		},
		{
			name:          "rig advice does NOT match different rig",
			adviceLabels:  []string{"rig:gastown", "role:crew"},
			subscriptions: []string{"rig:beads", "role:crew", "global"},
			want:          false, // Key test: role:crew matches but rig doesn't
		},
		{
			name:          "role-only advice matches any rig",
			adviceLabels:  []string{"role:crew"},
			subscriptions: []string{"rig:beads", "role:crew", "global"},
			want:          true,
		},
		{
			name:          "global advice matches everyone",
			adviceLabels:  []string{"global"},
			subscriptions: []string{"rig:beads", "role:polecat", "global"},
			want:          true,
		},
		{
			name:          "agent-specific advice requires agent match",
			adviceLabels:  []string{"agent:beads/crew/alice"},
			subscriptions: []string{"agent:beads/crew/bob", "rig:beads", "role:crew"},
			want:          false,
		},
		{
			name:          "agent-specific advice matches correct agent",
			adviceLabels:  []string{"agent:beads/crew/alice"},
			subscriptions: []string{"agent:beads/crew/alice", "rig:beads", "role:crew"},
			want:          true,
		},
		{
			name:          "topic advice matches subscription",
			adviceLabels:  []string{"testing", "go"},
			subscriptions: []string{"rig:beads", "role:crew", "testing"},
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesSubscriptions(nil, tt.adviceLabels, tt.subscriptions)
			if got != tt.want {
				t.Errorf("matchesSubscriptions(%v, %v) = %v, want %v",
					tt.adviceLabels, tt.subscriptions, got, tt.want)
			}
		})
	}
}

// TestMatchesSubscriptionsEdgeCases tests edge cases in label matching
func TestMatchesSubscriptionsEdgeCases(t *testing.T) {
	tests := []struct {
		name          string
		adviceLabels  []string
		subscriptions []string
		want          bool
	}{
		// Edge case: Advice with no labels (orphaned)
		{
			name:          "orphaned advice with no labels matches nobody",
			adviceLabels:  []string{},
			subscriptions: []string{"rig:beads", "role:crew", "global"},
			want:          false, // No labels means no groups, so nothing to match
		},
		{
			name:          "orphaned advice nil labels matches nobody",
			adviceLabels:  nil,
			subscriptions: []string{"rig:beads", "role:crew", "global"},
			want:          false,
		},

		// Edge case: Duplicate labels
		{
			name:          "duplicate labels in advice still match",
			adviceLabels:  []string{"testing", "testing", "testing"},
			subscriptions: []string{"rig:beads", "role:crew", "testing"},
			want:          true,
		},
		{
			name:          "duplicate role labels",
			adviceLabels:  []string{"role:crew", "role:crew"},
			subscriptions: []string{"rig:beads", "role:crew", "global"},
			want:          true,
		},

		// Edge case: Conflicting scope labels (rig:A AND rig:B)
		{
			name:          "conflicting rig labels - subscriber has neither",
			adviceLabels:  []string{"rig:alpha", "rig:beta"},
			subscriptions: []string{"rig:gamma", "role:crew"},
			want:          false, // Neither rig matches
		},
		{
			name:          "conflicting rig labels - subscriber has one",
			adviceLabels:  []string{"rig:alpha", "rig:beta"},
			subscriptions: []string{"rig:alpha", "role:crew"},
			want:          false, // Must match BOTH rig labels (rig: is required)
		},
		{
			name:          "conflicting rig labels - subscriber has both",
			adviceLabels:  []string{"rig:alpha", "rig:beta"},
			subscriptions: []string{"rig:alpha", "rig:beta", "role:crew"},
			want:          true, // Both rig labels match
		},

		// Edge case: Empty string label
		{
			name:          "empty string label in advice",
			adviceLabels:  []string{""},
			subscriptions: []string{"rig:beads", "role:crew", ""},
			want:          true, // Empty string is a valid label that can match
		},
		{
			name:          "empty string label not in subscriptions",
			adviceLabels:  []string{""},
			subscriptions: []string{"rig:beads", "role:crew"},
			want:          false, // Empty string not in subscriptions
		},
		{
			name:          "mixed empty and valid labels",
			adviceLabels:  []string{"", "testing"},
			subscriptions: []string{"rig:beads", "testing"},
			want:          true, // "testing" matches, empty is separate group
		},

		// Edge case: Very long label strings
		{
			name:          "very long label string matches",
			adviceLabels:  []string{"this-is-a-very-long-label-string-that-exceeds-normal-expectations-for-what-a-label-should-be-but-we-should-handle-it-gracefully"},
			subscriptions: []string{"rig:beads", "this-is-a-very-long-label-string-that-exceeds-normal-expectations-for-what-a-label-should-be-but-we-should-handle-it-gracefully"},
			want:          true,
		},
		{
			name:          "very long label string no match",
			adviceLabels:  []string{"this-is-a-very-long-label-string-that-exceeds-normal-expectations"},
			subscriptions: []string{"rig:beads", "role:crew"},
			want:          false,
		},

		// Edge case: Special characters in labels
		{
			name:          "label with unicode characters",
			adviceLabels:  []string{"テスト"},
			subscriptions: []string{"rig:beads", "テスト"},
			want:          true,
		},
		{
			name:          "label with spaces",
			adviceLabels:  []string{"has spaces"},
			subscriptions: []string{"rig:beads", "has spaces"},
			want:          true,
		},
		{
			name:          "label with special punctuation",
			adviceLabels:  []string{"label!@#$%^&*()"},
			subscriptions: []string{"rig:beads", "label!@#$%^&*()"},
			want:          true,
		},
		{
			name:          "label with newline character",
			adviceLabels:  []string{"line1\nline2"},
			subscriptions: []string{"rig:beads", "line1\nline2"},
			want:          true,
		},
		{
			name:          "label with colon but not a known prefix",
			adviceLabels:  []string{"custom:value"},
			subscriptions: []string{"rig:beads", "custom:value"},
			want:          true, // Not rig:/agent: so treated as regular topic
		},

		// Edge case: Group prefix edge cases
		{
			name:          "g0 prefix - group zero",
			adviceLabels:  []string{"g0:label1", "g0:label2"},
			subscriptions: []string{"label1", "label2"},
			want:          true, // Both labels in group 0 match
		},
		{
			name:          "g0 prefix partial match fails",
			adviceLabels:  []string{"g0:label1", "g0:label2"},
			subscriptions: []string{"label1"},
			want:          false, // Must match ALL labels in the group
		},
		{
			name:          "malformed group prefix treated as label",
			adviceLabels:  []string{"g:label"},
			subscriptions: []string{"g:label"},
			want:          true, // "g:" without number is just a label
		},
		{
			name:          "group prefix with very large number",
			adviceLabels:  []string{"g999999:test"},
			subscriptions: []string{"test"},
			want:          true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesSubscriptions(nil, tt.adviceLabels, tt.subscriptions)
			if got != tt.want {
				t.Errorf("matchesSubscriptions(%v, %v) = %v, want %v",
					tt.adviceLabels, tt.subscriptions, got, tt.want)
			}
		})
	}
}
