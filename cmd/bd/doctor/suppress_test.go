package doctor

import (
	"testing"
)

func TestCheckNameToSlug(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"Git Hooks", "git-hooks"},
		{"CLI Version", "cli-version"},
		{"Pending Migrations", "pending-migrations"},
		{"Remote Consistency", "remote-consistency"},
		{"Role Configuration", "role-configuration"},
		{"Stale Closed Issues", "stale-closed-issues"},
		{"Large Database", "large-database"},
		{"Dolt Format", "dolt-format"},
		{"Lock Files", "lock-files"},
		{"Merge Artifacts", "merge-artifacts"},
		{"Orphaned Dependencies", "orphaned-dependencies"},
		{"Duplicate Issues", "duplicate-issues"},
		{"Multi-Repo Types", "multi-repo-types"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CheckNameToSlug(tt.name)
			if got != tt.want {
				t.Errorf("CheckNameToSlug(%q) = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestFilterSuppressedChecks(t *testing.T) {
	checks := []DoctorCheck{
		{Name: "Git Hooks", Status: StatusWarning, Message: "missing hooks"},
		{Name: "CLI Version", Status: StatusOK, Message: "up to date"},
		{Name: "Pending Migrations", Status: StatusWarning, Message: "1 available"},
		{Name: "Role Configuration", Status: StatusWarning, Message: "not set"},
		{Name: "Large Database", Status: StatusOK, Message: "ok"},
	}

	t.Run("no suppressions", func(t *testing.T) {
		filtered, count := FilterSuppressedChecks(checks, nil)
		if len(filtered) != len(checks) {
			t.Errorf("expected %d checks, got %d", len(checks), len(filtered))
		}
		if count != 0 {
			t.Errorf("expected 0 suppressed, got %d", count)
		}
	})

	t.Run("suppress warning check", func(t *testing.T) {
		suppressed := map[string]bool{"git-hooks": true}
		filtered, count := FilterSuppressedChecks(checks, suppressed)
		if count != 1 {
			t.Errorf("expected 1 suppressed, got %d", count)
		}
		if len(filtered) != len(checks)-1 {
			t.Errorf("expected %d checks, got %d", len(checks)-1, len(filtered))
		}
		for _, c := range filtered {
			if c.Name == "Git Hooks" {
				t.Error("Git Hooks should have been filtered out")
			}
		}
	})

	t.Run("ok checks are not suppressed", func(t *testing.T) {
		suppressed := map[string]bool{"cli-version": true}
		filtered, count := FilterSuppressedChecks(checks, suppressed)
		if count != 0 {
			t.Errorf("expected 0 suppressed (OK checks not filtered), got %d", count)
		}
		if len(filtered) != len(checks) {
			t.Errorf("expected %d checks, got %d", len(checks), len(filtered))
		}
	})

	t.Run("multiple suppressions", func(t *testing.T) {
		suppressed := map[string]bool{
			"git-hooks":          true,
			"pending-migrations": true,
			"role-configuration": true,
		}
		filtered, count := FilterSuppressedChecks(checks, suppressed)
		if count != 3 {
			t.Errorf("expected 3 suppressed, got %d", count)
		}
		if len(filtered) != 2 {
			t.Errorf("expected 2 checks, got %d", len(filtered))
		}
	})
}
