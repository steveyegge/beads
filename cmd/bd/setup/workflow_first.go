package setup

import "github.com/steveyegge/beads/internal/recipes"

// UseWorkflowFirst toggles optional workflow-first addendum for setup templates.
var UseWorkflowFirst bool

func withWorkflowFirst(base string) string {
	if !UseWorkflowFirst {
		return base
	}
	return base + "\n" + recipes.WorkflowFirstAddendum
}
