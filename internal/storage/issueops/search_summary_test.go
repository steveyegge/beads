package issueops

import "testing"

func TestBuildSummaryOrderBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		sortBy  string
		reverse bool
		want    string
		wantErr bool
	}{
		{"default", "", false, "ORDER BY priority ASC, created_at DESC, id ASC", false},
		{"default ignores reverse", "", true, "ORDER BY priority ASC, created_at DESC, id ASC", false},

		{"priority asc", "priority", false, "ORDER BY priority ASC, id ASC", false},
		{"priority reverse", "priority", true, "ORDER BY priority DESC, id ASC", false},

		{"created desc", "created", false, "ORDER BY created_at DESC, id ASC", false},
		{"created reverse", "created", true, "ORDER BY created_at ASC, id ASC", false},

		{"updated desc", "updated", false, "ORDER BY updated_at DESC, id ASC", false},
		{"closed desc", "closed", false, "ORDER BY closed_at DESC, id ASC", false},
		{"status asc", "status", false, "ORDER BY status ASC, id ASC", false},
		{"id asc", "id", false, "ORDER BY id ASC", false},
		{"id reverse", "id", true, "ORDER BY id DESC", false},
		{"title asc", "title", false, "ORDER BY lower(title) ASC, id ASC", false},
		{"type asc", "type", false, "ORDER BY issue_type ASC, id ASC", false},
		{"assignee asc", "assignee", false, "ORDER BY assignee ASC, id ASC", false},

		{"unknown field", "bogus", false, "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := BuildSummaryOrderBy(tt.sortBy, tt.reverse)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for sortBy=%q, got nil", tt.sortBy)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("BuildSummaryOrderBy(%q, %v) = %q, want %q", tt.sortBy, tt.reverse, got, tt.want)
			}
		})
	}
}
