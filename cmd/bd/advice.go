package main

import (
	"github.com/spf13/cobra"
)

// adviceCmd is the parent command for advice management operations
var adviceCmd = &cobra.Command{
	Use:     "advice",
	GroupID: "issues",
	Short:   "Manage agent advice beads",
	Long: `Advice beads provide hierarchical guidance for agents.

Advice is scoped to specific targets:
  - Global: Applies to all agents (no target specified)
  - Rig: Applies to all agents in a rig (--rig)
  - Role: Applies to a role type (--role)
  - Agent: Applies to a specific agent (--agent)

More specific advice takes precedence over less specific advice.

Commands:
  add      Create a new advice bead
  list     List advice beads by scope
  remove   Remove an advice bead

Examples:
  bd advice add "Always run tests before committing"
  bd advice add --rig=beads "Use go test ./... for testing"
  bd advice add --role=polecat "Complete work before running gt done"
  bd advice add --agent=beads/polecats/quartz "Focus on CLI implementation"
  bd advice list
  bd advice list --rig=beads
  bd advice remove gt-abc123`,
}

func init() {
	rootCmd.AddCommand(adviceCmd)
}
