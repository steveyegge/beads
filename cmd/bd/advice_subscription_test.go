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
