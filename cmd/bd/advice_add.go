package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// adviceAddCmd creates a new advice bead
var adviceAddCmd = &cobra.Command{
	Use:   "add [advice text]",
	Short: "Create a new advice bead",
	Long: `Create a new advice bead with labels for agent subscriptions.

Advice is delivered to agents based on label subscriptions:
  - Global advice: use -l global (default if no targeting)
  - Rig advice: use -l rig:beads (or --rig=beads shorthand)
  - Role advice: use -l role:polecat (or --role=polecat shorthand)
  - Agent advice: use -l agent:beads/polecats/quartz (or --agent shorthand)

Agents auto-subscribe to their context labels (global, rig:X, role:Y, agent:Z).

Examples:
  # Global advice (applies to all agents)
  bd advice add "Always check for errors before proceeding" -l global

  # Rig-level advice (shorthand)
  bd advice add --rig=beads "Use go test ./... for testing"

  # Role-level advice (shorthand)
  bd advice add --role=polecat "Complete work before running gt done"

  # Agent-specific advice (shorthand)
  bd advice add --agent=beads/polecats/quartz "Focus on CLI implementation tasks"

  # Using explicit labels
  bd advice add "Security best practices" -l security -l testing

  # COMPOUND LABELS (AND/OR semantics):
  # AND: Comma-separated labels in same -l flag (must match ALL)
  bd advice add 'Beads polecat workflow' -l 'role:polecat,rig:beads'

  # OR: Separate -l flags (matches ANY group)
  bd advice add 'For polecats or crew' -l 'role:polecat' -l 'role:crew'

  # Complex: (polecat+beads) OR crew
  bd advice add 'Beads polecats or any crew' -l 'role:polecat,rig:beads' -l 'role:crew'

  # With description for more context
  bd advice add "Check hook status first" -d "Always run gt hook before announcing"

  # With stop hook (command runs at lifecycle points)
  bd advice add "Run tests before commit" --role=polecat \
    --hook-command="make test" --hook-trigger=before-commit --hook-on-failure=block

  # With session-end hook
  bd advice add "Check for uncommitted changes" \
    --hook-command="git status --porcelain" --hook-trigger=session-end`,
	Args: cobra.MaximumNArgs(1),
	Run:  runAdviceAdd,
}

func init() {
	adviceAddCmd.Flags().StringP("title", "t", "", "Title for the advice (defaults to first line of advice text)")
	adviceAddCmd.Flags().StringP("description", "d", "", "Detailed description of the advice")
	adviceAddCmd.Flags().String("rig", "", "Shorthand for -l rig:X (e.g., --rig=beads adds 'rig:beads' label)")
	adviceAddCmd.Flags().String("role", "", "Shorthand for -l role:X (e.g., --role=polecat adds 'role:polecat' label)")
	adviceAddCmd.Flags().String("agent", "", "Shorthand for -l agent:X (e.g., --agent=beads/polecats/quartz)")
	adviceAddCmd.Flags().IntP("priority", "p", 2, "Priority (1=highest, 5=lowest)")
	adviceAddCmd.Flags().StringArrayP("label", "l", nil, "Labels to add (can be used multiple times)")
	// Advice hook flags (hq--uaim)
	adviceAddCmd.Flags().String("hook-command", "", "Command to execute at trigger point (e.g., 'make test')")
	adviceAddCmd.Flags().String("hook-trigger", "", "When to run hook: session-end, before-commit, before-push, before-handoff")
	adviceAddCmd.Flags().Int("hook-timeout", 0, "Hook timeout in seconds (default: 30, max: 300)")
	adviceAddCmd.Flags().String("hook-on-failure", "", "What to do if hook fails: block, warn, ignore (default: warn)")

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

	// Parse label groups - comma-separated labels in same -l get same group
	var groupedLabels []string
	for groupNum, labelArg := range labels {
		parts := strings.Split(labelArg, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part != "" {
				groupedLabels = append(groupedLabels, fmt.Sprintf("g%d:%s", groupNum, part))
			}
		}
	}
	labels = groupedLabels

	// Hook flags (hq--uaim)
	hookCommand, _ := cmd.Flags().GetString("hook-command")
	hookTrigger, _ := cmd.Flags().GetString("hook-trigger")
	hookTimeout, _ := cmd.Flags().GetInt("hook-timeout")
	hookOnFailure, _ := cmd.Flags().GetString("hook-on-failure")

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

	// Convert targeting flags to labels
	// --rig=X becomes "rig:X", --role=Y becomes "role:Y", --agent=Z becomes "agent:Z"
	if agent != "" {
		labels = append(labels, "agent:"+agent)
	}
	if role != "" {
		labels = append(labels, "role:"+role)
	}
	if rig != "" {
		labels = append(labels, "rig:"+rig)
	}

	// If no targeting specified, default to global
	hasTargeting := agent != "" || role != "" || rig != ""
	hasTargetingLabels := false
	for _, l := range labels {
		// Strip group prefix (g0:, g1:, etc.) for targeting check
		checkLabel := l
		if len(l) >= 3 && l[0] == 'g' {
			for i := 1; i < len(l); i++ {
				if l[i] == ':' && i > 1 {
					checkLabel = l[i+1:]
					break
				}
				if l[i] < '0' || l[i] > '9' {
					break
				}
			}
		}
		if checkLabel == "global" || strings.HasPrefix(checkLabel, "rig:") || strings.HasPrefix(checkLabel, "role:") || strings.HasPrefix(checkLabel, "agent:") {
			hasTargetingLabels = true
			break
		}
	}
	if !hasTargeting && !hasTargetingLabels {
		labels = append(labels, "global")
	}

	// Validate hook flags (hq--uaim)
	if hookTrigger != "" && !types.IsValidAdviceHookTrigger(hookTrigger) {
		FatalError("invalid --hook-trigger: %s (valid: %v)", hookTrigger, types.ValidAdviceHookTriggers)
	}
	if hookOnFailure != "" && !types.IsValidAdviceHookOnFailure(hookOnFailure) {
		FatalError("invalid --hook-on-failure: %s (valid: %v)", hookOnFailure, types.ValidAdviceHookOnFailure)
	}
	if hookTimeout < 0 || hookTimeout > types.AdviceHookTimeoutMax {
		FatalError("--hook-timeout must be between 0 and %d", types.AdviceHookTimeoutMax)
	}
	// Hook command requires trigger
	if hookCommand != "" && hookTrigger == "" {
		FatalError("--hook-command requires --hook-trigger")
	}

	// Use RPC (daemon is always connected)
	createArgs := &rpc.CreateArgs{
		Title:               title,
		Description:         description,
		IssueType:           "advice",
		Priority:            priority,
		Labels:              labels,
		// NOTE: Targeting now via labels (rig:X, role:Y, agent:Z, global)
		AdviceHookCommand:   hookCommand,
		AdviceHookTrigger:   hookTrigger,
		AdviceHookTimeout:   hookTimeout,
		AdviceHookOnFailure: hookOnFailure,
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
		printAdviceCreatedWithLabels(&issue, labels)
	}

	SetLastTouchedID(issue.ID)
}

// printAdviceCreatedWithLabels prints a human-readable summary of created advice
func printAdviceCreatedWithLabels(issue *types.Issue, labels []string) {
	fmt.Printf("%s Created advice: %s\n", ui.RenderPass("✓"), ui.RenderID(issue.ID))
	fmt.Printf("  Title: %s\n", issue.Title)

	// Show labels (which now include targeting)
	if len(labels) > 0 {
		fmt.Printf("  Labels: %s\n", strings.Join(labels, ", "))
	}

	// Show hook info if set (hq--uaim)
	if issue.AdviceHookCommand != "" {
		fmt.Printf("  Hook: %s @ %s", issue.AdviceHookCommand, issue.AdviceHookTrigger)
		if issue.AdviceHookOnFailure != "" {
			fmt.Printf(" (%s)", issue.AdviceHookOnFailure)
		}
		fmt.Println()
	}

	if issue.Description != "" && issue.Description != issue.Title {
		// Truncate description for display
		desc := issue.Description
		if len(desc) > 100 {
			desc = desc[:97] + "..."
		}
		fmt.Printf("  Description: %s\n", desc)
	}
}
