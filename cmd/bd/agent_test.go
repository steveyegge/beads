package main

import (
	"testing"
)

func TestValidAgentStates(t *testing.T) {
	// Test that all expected states are valid
	expectedStates := []string{
		"idle", "spawning", "running", "working",
		"stuck", "done", "stopped", "dead",
	}

	for _, state := range expectedStates {
		if !validAgentStates[state] {
			t.Errorf("expected state %q to be valid, but it's not", state)
		}
	}
}

func TestInvalidAgentStates(t *testing.T) {
	// Test that invalid states are rejected
	invalidStates := []string{
		"starting", "waiting", "active", "inactive",
		"unknown", "error", "RUNNING", "Idle",
	}

	for _, state := range invalidStates {
		if validAgentStates[state] {
			t.Errorf("expected state %q to be invalid, but it's valid", state)
		}
	}
}

func TestAgentStateCount(t *testing.T) {
	// Verify we have exactly 8 valid states
	expectedCount := 8
	actualCount := len(validAgentStates)
	if actualCount != expectedCount {
		t.Errorf("expected %d valid states, got %d", expectedCount, actualCount)
	}
}

func TestFormatTimeOrNil(t *testing.T) {
	// Test nil case
	result := formatTimeOrNil(nil)
	if result != nil {
		t.Errorf("expected nil for nil time, got %v", result)
	}
}

func TestParseAgentIDFields(t *testing.T) {
	tests := []struct {
		name         string
		agentID      string
		wantRoleType string
		wantRig      string
	}{
		// Town-level roles
		{
			name:         "town-level mayor",
			agentID:      "gt-mayor",
			wantRoleType: "mayor",
			wantRig:      "",
		},
		{
			name:         "town-level deacon",
			agentID:      "bd-deacon",
			wantRoleType: "deacon",
			wantRig:      "",
		},
		// Per-rig singleton roles
		{
			name:         "rig-level witness",
			agentID:      "gt-gastown-witness",
			wantRoleType: "witness",
			wantRig:      "gastown",
		},
		{
			name:         "rig-level refinery",
			agentID:      "bd-beads-refinery",
			wantRoleType: "refinery",
			wantRig:      "beads",
		},
		// Per-rig named roles
		{
			name:         "named polecat",
			agentID:      "gt-gastown-polecat-nux",
			wantRoleType: "polecat",
			wantRig:      "gastown",
		},
		{
			name:         "named crew",
			agentID:      "bd-beads-crew-dave",
			wantRoleType: "crew",
			wantRig:      "beads",
		},
		{
			name:         "polecat with hyphenated name",
			agentID:      "gt-gastown-polecat-nux-123",
			wantRoleType: "polecat",
			wantRig:      "gastown",
		},
		// Hyphenated rig names (GH#868)
		{
			name:         "polecat with hyphenated rig",
			agentID:      "gt-my-project-polecat-nux",
			wantRoleType: "polecat",
			wantRig:      "my-project",
		},
		{
			name:         "crew with hyphenated rig",
			agentID:      "bd-infra-dashboard-crew-alice",
			wantRoleType: "crew",
			wantRig:      "infra-dashboard",
		},
		{
			name:         "witness with hyphenated rig",
			agentID:      "gt-my-cool-rig-witness",
			wantRoleType: "witness",
			wantRig:      "my-cool-rig",
		},
		{
			name:         "refinery with hyphenated rig",
			agentID:      "bd-super-long-rig-name-refinery",
			wantRoleType: "refinery",
			wantRig:      "super-long-rig-name",
		},
		{
			name:         "polecat with multi-hyphen rig and name",
			agentID:      "gt-my-awesome-project-polecat-worker-1",
			wantRoleType: "polecat",
			wantRig:      "my-awesome-project",
		},
		// Edge cases
		{
			name:         "no hyphen",
			agentID:      "invalid",
			wantRoleType: "",
			wantRig:      "",
		},
		{
			name:         "empty string",
			agentID:      "",
			wantRoleType: "",
			wantRig:      "",
		},
		{
			name:         "unknown role",
			agentID:      "gt-gastown-unknown",
			wantRoleType: "",
			wantRig:      "",
		},
		{
			name:         "prefix only",
			agentID:      "gt-",
			wantRoleType: "",
			wantRig:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotRoleType, gotRig := parseAgentIDFields(tt.agentID)
			if gotRoleType != tt.wantRoleType {
				t.Errorf("parseAgentIDFields(%q) roleType = %q, want %q", tt.agentID, gotRoleType, tt.wantRoleType)
			}
			if gotRig != tt.wantRig {
				t.Errorf("parseAgentIDFields(%q) rig = %q, want %q", tt.agentID, gotRig, tt.wantRig)
			}
		})
	}
}

func TestFormatAgentDescription(t *testing.T) {
	tests := []struct {
		name   string
		fields AgentFields
		want   string
	}{
		{
			name:   "empty fields",
			fields: AgentFields{},
			want:   "",
		},
		{
			name: "role_type only",
			fields: AgentFields{
				RoleType: "polecat",
			},
			want: "role_type: polecat",
		},
		{
			name: "all basic fields",
			fields: AgentFields{
				RoleType: "polecat",
				Rig:      "beads",
			},
			want: "role_type: polecat\nrig: beads",
		},
		{
			name: "with advice subscriptions",
			fields: AgentFields{
				RoleType:            "polecat",
				Rig:                 "beads",
				AdviceSubscriptions: []string{"security", "testing"},
			},
			want: "role_type: polecat\nrig: beads\nadvice_subscriptions: security,testing",
		},
		{
			name: "with advice subscriptions exclude",
			fields: AgentFields{
				RoleType:                   "polecat",
				Rig:                        "beads",
				AdviceSubscriptionsExclude: []string{"deprecated", "wip"},
			},
			want: "role_type: polecat\nrig: beads\nadvice_subscriptions_exclude: deprecated,wip",
		},
		{
			name: "all fields",
			fields: AgentFields{
				RoleType:                   "crew",
				Rig:                        "gastown",
				AdviceSubscriptions:        []string{"security", "performance"},
				AdviceSubscriptionsExclude: []string{"deprecated"},
			},
			want: "role_type: crew\nrig: gastown\nadvice_subscriptions: security,performance\nadvice_subscriptions_exclude: deprecated",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatAgentDescription(tt.fields)
			if got != tt.want {
				t.Errorf("FormatAgentDescription() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseAgentFields(t *testing.T) {
	tests := []struct {
		name        string
		description string
		want        AgentFields
	}{
		{
			name:        "empty description",
			description: "",
			want:        AgentFields{},
		},
		{
			name:        "role_type only",
			description: "role_type: polecat",
			want: AgentFields{
				RoleType: "polecat",
			},
		},
		{
			name:        "all basic fields",
			description: "role_type: polecat\nrig: beads",
			want: AgentFields{
				RoleType: "polecat",
				Rig:      "beads",
			},
		},
		{
			name:        "with advice subscriptions",
			description: "role_type: polecat\nrig: beads\nadvice_subscriptions: security,testing",
			want: AgentFields{
				RoleType:            "polecat",
				Rig:                 "beads",
				AdviceSubscriptions: []string{"security", "testing"},
			},
		},
		{
			name:        "with advice subscriptions exclude",
			description: "role_type: polecat\nadvice_subscriptions_exclude: deprecated,wip",
			want: AgentFields{
				RoleType:                   "polecat",
				AdviceSubscriptionsExclude: []string{"deprecated", "wip"},
			},
		},
		{
			name:        "whitespace tolerance",
			description: "  role_type:   crew  \n  rig:  gastown  \nadvice_subscriptions:  sec , test  ",
			want: AgentFields{
				RoleType:            "crew",
				Rig:                 "gastown",
				AdviceSubscriptions: []string{"sec", "test"},
			},
		},
		{
			name:        "ignores unknown fields",
			description: "role_type: polecat\nunknown_field: value\nrig: beads",
			want: AgentFields{
				RoleType: "polecat",
				Rig:      "beads",
			},
		},
		{
			name:        "handles lines without colon",
			description: "role_type: polecat\nsome text without colon\nrig: beads",
			want: AgentFields{
				RoleType: "polecat",
				Rig:      "beads",
			},
		},
		{
			name:        "empty advice subscriptions value",
			description: "role_type: polecat\nadvice_subscriptions:",
			want: AgentFields{
				RoleType: "polecat",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAgentFields(tt.description)
			if got.RoleType != tt.want.RoleType {
				t.Errorf("ParseAgentFields().RoleType = %q, want %q", got.RoleType, tt.want.RoleType)
			}
			if got.Rig != tt.want.Rig {
				t.Errorf("ParseAgentFields().Rig = %q, want %q", got.Rig, tt.want.Rig)
			}
			if !strSlicesEqual(got.AdviceSubscriptions, tt.want.AdviceSubscriptions) {
				t.Errorf("ParseAgentFields().AdviceSubscriptions = %v, want %v", got.AdviceSubscriptions, tt.want.AdviceSubscriptions)
			}
			if !strSlicesEqual(got.AdviceSubscriptionsExclude, tt.want.AdviceSubscriptionsExclude) {
				t.Errorf("ParseAgentFields().AdviceSubscriptionsExclude = %v, want %v", got.AdviceSubscriptionsExclude, tt.want.AdviceSubscriptionsExclude)
			}
		})
	}
}

func TestAgentFieldsRoundTrip(t *testing.T) {
	tests := []struct {
		name   string
		fields AgentFields
	}{
		{
			name:   "empty",
			fields: AgentFields{},
		},
		{
			name: "basic fields",
			fields: AgentFields{
				RoleType: "witness",
				Rig:      "beads",
			},
		},
		{
			name: "with subscriptions",
			fields: AgentFields{
				RoleType:            "polecat",
				Rig:                 "gastown",
				AdviceSubscriptions: []string{"security", "testing", "performance"},
			},
		},
		{
			name: "with exclusions",
			fields: AgentFields{
				RoleType:                   "crew",
				Rig:                        "beads",
				AdviceSubscriptionsExclude: []string{"deprecated", "wip"},
			},
		},
		{
			name: "all fields",
			fields: AgentFields{
				RoleType:                   "polecat",
				Rig:                        "my-project",
				AdviceSubscriptions:        []string{"security", "go"},
				AdviceSubscriptionsExclude: []string{"deprecated"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatted := FormatAgentDescription(tt.fields)
			parsed := ParseAgentFields(formatted)

			if parsed.RoleType != tt.fields.RoleType {
				t.Errorf("Round trip RoleType: got %q, want %q", parsed.RoleType, tt.fields.RoleType)
			}
			if parsed.Rig != tt.fields.Rig {
				t.Errorf("Round trip Rig: got %q, want %q", parsed.Rig, tt.fields.Rig)
			}
			if !strSlicesEqual(parsed.AdviceSubscriptions, tt.fields.AdviceSubscriptions) {
				t.Errorf("Round trip AdviceSubscriptions: got %v, want %v", parsed.AdviceSubscriptions, tt.fields.AdviceSubscriptions)
			}
			if !strSlicesEqual(parsed.AdviceSubscriptionsExclude, tt.fields.AdviceSubscriptionsExclude) {
				t.Errorf("Round trip AdviceSubscriptionsExclude: got %v, want %v", parsed.AdviceSubscriptionsExclude, tt.fields.AdviceSubscriptionsExclude)
			}
		})
	}
}

// strSlicesEqual compares two string slices for equality
func strSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}
