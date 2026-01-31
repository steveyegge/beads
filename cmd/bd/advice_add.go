package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/idgen"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// adviceAddCmd creates a new advice bead
var adviceAddCmd = &cobra.Command{
	Use:   "add [advice text]",
	Short: "Create a new advice bead",
	Long: `Create a new advice bead targeting agents, roles, or rigs.

Advice is hierarchical guidance that agents receive during priming:
  - Global advice applies to all agents (no target flags)
  - Rig advice applies to all agents in a rig (--rig)
  - Role advice applies to a role type in a rig (--role, requires --rig)
  - Agent advice applies to a specific agent (--agent)

More specific advice takes precedence over less specific advice.

Examples:
  # Global advice (applies to all agents)
  bd advice add "Always check for errors before proceeding"

  # Rig-level advice
  bd advice add --rig=beads "Use go test ./... for testing"

  # Role-level advice (role within a rig)
  bd advice add --rig=beads --role=polecat "Complete work before running gt done"

  # Agent-specific advice
  bd advice add --agent=beads/polecats/quartz "Focus on CLI implementation tasks"

  # With description for more context
  bd advice add "Check hook status first" -d "When starting a session, always run gt hook to check if work is assigned before announcing yourself"`,
	Args: cobra.MaximumNArgs(1),
	Run:  runAdviceAdd,
}

func init() {
	adviceAddCmd.Flags().StringP("title", "t", "", "Title for the advice (defaults to first line of advice text)")
	adviceAddCmd.Flags().StringP("description", "d", "", "Detailed description of the advice")
	adviceAddCmd.Flags().String("rig", "", "Target rig for advice (e.g., 'beads', 'gastown')")
	adviceAddCmd.Flags().String("role", "", "Target role for advice (e.g., 'polecat', 'crew'). Requires --rig")
	adviceAddCmd.Flags().String("agent", "", "Target agent ID for advice (e.g., 'beads/polecats/quartz')")
	adviceAddCmd.Flags().IntP("priority", "p", 2, "Priority (1=highest, 5=lowest)")
	adviceAddCmd.Flags().StringArrayP("label", "l", nil, "Labels to add (can be used multiple times)")

	adviceCmd.AddCommand(adviceAddCmd)
}

func runAdviceAdd(cmd *cobra.Command, args []string) {
	CheckReadonly("advice add")

	// Get flags
	title, _ := cmd.Flags().GetString("title")
	description, _ := cmd.Flags().GetString("description")
	rig, _ := cmd.Flags().GetString("rig")
	role, _ := cmd.Flags().GetString("role")
	agent, _ := cmd.Flags().GetString("agent")
	priority, _ := cmd.Flags().GetInt("priority")
	labels, _ := cmd.Flags().GetStringArray("label")

	// Get advice text from args or title
	var adviceText string
	if len(args) > 0 {
		adviceText = args[0]
	}

	// Validate: either advice text or title must be provided
	if adviceText == "" && title == "" {
		FatalError("advice text or --title is required")
	}

	// Default title to advice text if not provided
	if title == "" {
		title = truncateTitle(adviceText, 80)
	}

	// Default description to advice text if not provided
	if description == "" && adviceText != "" {
		description = adviceText
	}

	// Validate role requires rig
	if role != "" && rig == "" {
		FatalError("--role requires --rig to specify which rig the role belongs to")
	}

	// Validate agent cannot be combined with rig/role
	if agent != "" && (rig != "" || role != "") {
		FatalError("--agent cannot be combined with --rig or --role (agent includes rig/role info)")
	}

	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := rootCtx

	// If daemon is running, use RPC
	if daemonClient != nil {
		createArgs := &rpc.CreateArgs{
			Title:             title,
			Description:       description,
			IssueType:         "advice",
			Priority:          priority,
			Labels:            labels,
			AdviceTargetRig:   rig,
			AdviceTargetRole:  role,
			AdviceTargetAgent: agent,
		}

		resp, err := daemonClient.Create(createArgs)
		if err != nil {
			FatalError("%v", err)
		}

		var issue types.Issue
		if err := json.Unmarshal(resp.Data, &issue); err != nil {
			FatalError("parsing response: %v", err)
		}

		// Run create hook
		if hookRunner != nil {
			hookRunner.Run(hooks.EventCreate, &issue)
		}

		if jsonOutput {
			fmt.Println(string(resp.Data))
		} else {
			printAdviceCreated(&issue)
		}

		SetLastTouchedID(issue.ID)
		return
	}

	// Direct mode - generate ID and create issue
	prefix, _ := store.GetConfig(ctx, "issue_prefix")
	if prefix == "" {
		prefix = "bd"
	}

	// Generate semantic ID for advice
	now := time.Now()
	issueID := idgen.GenerateHashID(prefix, title, "advice", actor, now, 6, 0)

	// Check for collision
	for i := 1; i <= 100; i++ {
		existing, err := store.GetIssue(ctx, issueID)
		if err != nil {
			FatalError("checking for ID collision: %v", err)
		}
		if existing == nil {
			break
		}
		issueID = idgen.GenerateHashID(prefix, title, "advice", actor, now, 6, i)
	}

	issue := &types.Issue{
		ID:                issueID,
		Title:             title,
		Description:       description,
		Status:            types.StatusOpen,
		Priority:          priority,
		IssueType:         types.IssueType("advice"),
		AdviceTargetRig:   rig,
		AdviceTargetRole:  role,
		AdviceTargetAgent: agent,
		CreatedBy:         getActorWithGit(),
		Owner:             getOwner(),
	}

	if err := store.CreateIssue(ctx, issue, actor); err != nil {
		FatalError("creating advice: %v", err)
	}

	// Add labels
	for _, label := range labels {
		if err := store.AddLabel(ctx, issueID, label, actor); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to add label %s: %v\n", label, err)
		}
	}

	markDirtyAndScheduleFlush()

	// Run create hook
	if hookRunner != nil {
		hookRunner.Run(hooks.EventCreate, issue)
	}

	if jsonOutput {
		outputJSON(issue)
	} else {
		printAdviceCreated(issue)
	}

	SetLastTouchedID(issue.ID)
}

// printAdviceCreated prints a human-readable summary of created advice
func printAdviceCreated(issue *types.Issue) {
	fmt.Printf("%s Created advice: %s\n", ui.RenderPass("✓"), ui.RenderID(issue.ID))
	fmt.Printf("  Title: %s\n", issue.Title)

	// Show target scope
	scope := "Global"
	if issue.AdviceTargetAgent != "" {
		scope = fmt.Sprintf("Agent: %s", issue.AdviceTargetAgent)
	} else if issue.AdviceTargetRole != "" {
		scope = fmt.Sprintf("Role: %s/%s", issue.AdviceTargetRig, issue.AdviceTargetRole)
	} else if issue.AdviceTargetRig != "" {
		scope = fmt.Sprintf("Rig: %s", issue.AdviceTargetRig)
	}
	fmt.Printf("  Scope: %s\n", scope)

	if issue.Description != "" && issue.Description != issue.Title {
		// Truncate description for display
		desc := issue.Description
		if len(desc) > 100 {
			desc = desc[:97] + "..."
		}
		fmt.Printf("  Description: %s\n", desc)
	}
}
