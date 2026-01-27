package main

import (
	"github.com/spf13/cobra"
)

// decisionCmd is the parent command for decision point operations
var decisionCmd = &cobra.Command{
	Use:     "decision",
	GroupID: "issues",
	Short:   "Manage decision points (human-in-the-loop choices)",
	Long: `Decision points are gates that wait for structured human input via external notifications.

A decision point presents options to a human (via email, SMS, web) and blocks workflow
until they select an option or provide custom text guidance.

Commands:
  create    Create a new decision point with options
  respond   Record a human response to a decision point
  list      List pending decision points
  show      Show decision point details

Decision points are specialized gates with await_type="decision". They support:
  - Single-select from predefined options
  - Custom text input (can trigger iterative refinement)
  - Timeout with default option
  - Notification via email/SMS/webhook

Examples:
  bd decision create --prompt="Which approach?" --options='[{"id":"a","label":"Option A"}]'
  bd decision respond <id> --select=a
  bd decision list --pending
  bd decision show <id>`,
}

func init() {
	rootCmd.AddCommand(decisionCmd)
}
