package sqlite

import "testing"

// TestIsHierarchicalID tests the IsHierarchicalID function which detects
// if an issue ID is hierarchical (has a parent) based on the .N suffix pattern.
// This test covers the fix for GH#508 where prefixes with dots were incorrectly
// flagged as hierarchical.
func TestIsHierarchicalID(t *testing.T) {
	tests := []struct {
		name             string
		id               string
		wantHierarchical bool
		wantParentID     string
	}{
		// Standard hierarchical IDs
		{
			name:             "simple child .1",
			id:               "bd-abc123.1",
			wantHierarchical: true,
			wantParentID:     "bd-abc123",
		},
		{
			name:             "child .2",
			id:               "bd-xyz789.2",
			wantHierarchical: true,
			wantParentID:     "bd-xyz789",
		},
		{
			name:             "multi-digit child .10",
			id:               "bd-test.10",
			wantHierarchical: true,
			wantParentID:     "bd-test",
		},
		{
			name:             "large child number .999",
			id:               "bd-issue.999",
			wantHierarchical: true,
			wantParentID:     "bd-issue",
		},
		{
			name:             "nested hierarchical",
			id:               "bd-parent.1.2",
			wantHierarchical: true,
			wantParentID:     "bd-parent.1",
		},

		// Non-hierarchical IDs (no suffix or non-numeric suffix)
		{
			name:             "simple top-level",
			id:               "bd-abc123",
			wantHierarchical: false,
			wantParentID:     "",
		},
		{
			name:             "no dot at all",
			id:               "test-issue",
			wantHierarchical: false,
			wantParentID:     "",
		},

		// GH#508: Prefixes with dots should NOT be detected as hierarchical
		{
			name:             "prefix with dot - my.project",
			id:               "my.project-abc123",
			wantHierarchical: false,
			wantParentID:     "",
		},
		{
			name:             "prefix with multiple dots",
			id:               "com.example.app-issue1",
			wantHierarchical: false,
			wantParentID:     "",
		},
		{
			name:             "prefix with dot AND hierarchical child",
			id:               "my.project-abc123.1",
			wantHierarchical: true,
			wantParentID:     "my.project-abc123",
		},
		{
			name:             "complex prefix with hierarchical",
			id:               "com.example.app-xyz.5",
			wantHierarchical: true,
			wantParentID:     "com.example.app-xyz",
		},

		// Edge cases
		{
			name:             "dot but non-numeric suffix",
			id:               "bd-abc.def",
			wantHierarchical: false,
			wantParentID:     "",
		},
		{
			name:             "mixed suffix (starts with digit)",
			id:               "bd-test.1abc",
			wantHierarchical: false,
			wantParentID:     "",
		},
		{
			name:             "trailing dot only",
			id:               "bd-test.",
			wantHierarchical: false,
			wantParentID:     "",
		},
		{
			name:             "empty after dot",
			id:               "bd-test.",
			wantHierarchical: false,
			wantParentID:     "",
		},
		{
			name:             "child 0",
			id:               "bd-parent.0",
			wantHierarchical: true,
			wantParentID:     "bd-parent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotHierarchical, gotParentID := IsHierarchicalID(tt.id)
			if gotHierarchical != tt.wantHierarchical {
				t.Errorf("IsHierarchicalID(%q) hierarchical = %v, want %v",
					tt.id, gotHierarchical, tt.wantHierarchical)
			}
			if gotParentID != tt.wantParentID {
				t.Errorf("IsHierarchicalID(%q) parentID = %q, want %q",
					tt.id, gotParentID, tt.wantParentID)
			}
		})
	}
}
