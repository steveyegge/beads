package main

import (
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/types"
)

func TestBuildAutoSpecSummary(t *testing.T) {
	now := time.Now().UTC()
	entry := &spec.SpecRegistryEntry{SpecID: "specs/login.md", Title: "Login Feature"}
	beads := []*types.Issue{
		{ID: "bd-1", Title: "Implement OAuth", Status: types.StatusClosed, ClosedAt: &now},
		{ID: "bd-2", Title: "Add JWT refresh", Status: types.StatusClosed, ClosedAt: &now},
		{ID: "bd-3", Title: "Fix token expiry", Status: types.StatusClosed, ClosedAt: &now},
	}
	specText := "# Login Feature\n\n## Requirements\n- Implement OAuth\n- Add JWT refresh\n"
	summary := buildAutoSpecSummary(entry, specText, beads)
	if !strings.Contains(summary, "Login Feature.") {
		t.Fatalf("expected title sentence, got: %s", summary)
	}
	if !strings.Contains(summary, "Summary:") && !strings.Contains(summary, "Key points:") {
		t.Fatalf("expected spec highlights, got: %s", summary)
	}
	if !strings.Contains(summary, "Implemented work:") {
		t.Fatalf("expected implemented sentence, got: %s", summary)
	}
	if !strings.Contains(summary, "Completed beads: 3.") {
		t.Fatalf("expected completed count, got: %s", summary)
	}
}
