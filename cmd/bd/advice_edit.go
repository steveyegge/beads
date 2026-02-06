package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

// adviceEditCmd modifies an existing advice bead
var adviceEditCmd = &cobra.Command{
	Use:   "edit <id>",
	Short: "Edit an existing advice bead",
	Long: `Edit an existing advice bead's title, description, labels, hooks, or priority.

At least one modification flag must be provided. Labels can be added or removed
individually using --add-label and --remove-label. Shorthand flags --rig, --role,
and --agent add the corresponding targeting labels.

Examples:
  # Change the title
  bd advice edit gt-abc123 -t "New title for this advice"

  # Update description
  bd advice edit gt-abc123 -d "Updated detailed description"

  # Change priority
  bd advice edit gt-abc123 -p 1

  # Add a rig targeting label (shorthand for --add-label rig:beads)
  bd advice edit gt-abc123 --rig=beads

  # Add and remove labels
  bd advice edit gt-abc123 --add-label security --remove-label deprecated

  # Update hook configuration
  bd advice edit gt-abc123 --hook-command="make lint" --hook-trigger=before-commit

  # Clear hook command
  bd advice edit gt-abc123 --hook-command=""`,
	Args: cobra.ExactArgs(1),
	Run:  runAdviceEdit,
}

func init() {
	adviceEditCmd.Flags().StringP("title", "t", "", "New title")
	adviceEditCmd.Flags().StringP("description", "d", "", "New description")
	adviceEditCmd.Flags().StringArray("add-label", nil, "Add labels (can repeat)")
	adviceEditCmd.Flags().StringArray("remove-label", nil, "Remove labels (can repeat)")
	adviceEditCmd.Flags().String("rig", "", "Shorthand: add rig:X label")
	adviceEditCmd.Flags().String("role", "", "Shorthand: add role:X label")
	adviceEditCmd.Flags().String("agent", "", "Shorthand: add agent:X label")
	adviceEditCmd.Flags().IntP("priority", "p", 0, "New priority (1-5)")
	adviceEditCmd.Flags().String("hook-command", "", "New hook command")
	adviceEditCmd.Flags().String("hook-trigger", "", "New hook trigger")
	adviceEditCmd.Flags().Int("hook-timeout", -1, "New hook timeout (-1 = unchanged)")
	adviceEditCmd.Flags().String("hook-on-failure", "", "New on-failure mode")
	adviceEditCmd.ValidArgsFunction = issueIDCompletion

	adviceCmd.AddCommand(adviceEditCmd)
}

func runAdviceEdit(cmd *cobra.Command, args []string) {
	CheckReadonly("advice edit")

	id := args[0]

	// Check that at least one flag was provided
	hasChanges := cmd.Flags().Changed("title") ||
		cmd.Flags().Changed("description") ||
		cmd.Flags().Changed("add-label") ||
		cmd.Flags().Changed("remove-label") ||
		cmd.Flags().Changed("rig") ||
		cmd.Flags().Changed("role") ||
		cmd.Flags().Changed("agent") ||
		cmd.Flags().Changed("priority") ||
		cmd.Flags().Changed("hook-command") ||
		cmd.Flags().Changed("hook-trigger") ||
		cmd.Flags().Changed("hook-timeout") ||
		cmd.Flags().Changed("hook-on-failure")

	if !hasChanges {
		FatalError("at least one edit flag must be provided (e.g., --title, --description, --add-label, --priority)")
	}

	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	ctx := rootCtx

	// Resolve partial ID
	var resolvedID string
	if daemonClient != nil {
		resolveArgs := &rpc.ResolveIDArgs{ID: id}
		resp, err := daemonClient.ResolveID(resolveArgs)
		if err != nil {
			FatalError("resolving ID %s: %v", id, err)
		}
		if err := json.Unmarshal(resp.Data, &resolvedID); err != nil {
			FatalError("unmarshaling resolved ID: %v", err)
		}
	} else {
		resolved, err := utils.ResolvePartialID(ctx, store, id)
		if err != nil {
			FatalError("resolving ID %s: %v", id, err)
		}
		resolvedID = resolved
	}

	// Fetch the existing issue and verify it is advice
	var issue *types.Issue
	if daemonClient != nil {
		showArgs := &rpc.ShowArgs{ID: resolvedID}
		resp, err := daemonClient.Show(showArgs)
		if err != nil {
			FatalError("getting issue %s: %v", resolvedID, err)
		}
		if err := json.Unmarshal(resp.Data, &issue); err != nil {
			FatalError("parsing issue: %v", err)
		}
	} else {
		var err error
		issue, err = store.GetIssue(ctx, resolvedID)
		if err != nil {
			FatalError("getting issue %s: %v", resolvedID, err)
		}
	}

	if issue == nil {
		FatalError("issue %s not found", resolvedID)
	}

	if issue.IssueType != types.IssueType("advice") {
		FatalError("issue %s is not an advice bead (type: %s)", resolvedID, issue.IssueType)
	}

	// Collect label additions/removals
	var addLabels, removeLabels []string
	if cmd.Flags().Changed("add-label") {
		addLabels, _ = cmd.Flags().GetStringArray("add-label")
	}
	if cmd.Flags().Changed("remove-label") {
		removeLabels, _ = cmd.Flags().GetStringArray("remove-label")
	}

	// Convert --rig/--role/--agent shorthands to add-label equivalents
	if cmd.Flags().Changed("rig") {
		rig, _ := cmd.Flags().GetString("rig")
		if rig != "" {
			addLabels = append(addLabels, "rig:"+rig)
		}
	}
	if cmd.Flags().Changed("role") {
		role, _ := cmd.Flags().GetString("role")
		if role != "" {
			addLabels = append(addLabels, "role:"+role)
		}
	}
	if cmd.Flags().Changed("agent") {
		agent, _ := cmd.Flags().GetString("agent")
		if agent != "" {
			addLabels = append(addLabels, "agent:"+agent)
		}
	}

	// Validate hook flags
	if cmd.Flags().Changed("hook-trigger") {
		hookTrigger, _ := cmd.Flags().GetString("hook-trigger")
		if hookTrigger != "" && !types.IsValidAdviceHookTrigger(hookTrigger) {
			FatalError("invalid --hook-trigger: %s (valid: %v)", hookTrigger, types.ValidAdviceHookTriggers)
		}
	}
	if cmd.Flags().Changed("hook-on-failure") {
		hookOnFailure, _ := cmd.Flags().GetString("hook-on-failure")
		if hookOnFailure != "" && !types.IsValidAdviceHookOnFailure(hookOnFailure) {
			FatalError("invalid --hook-on-failure: %s (valid: %v)", hookOnFailure, types.ValidAdviceHookOnFailure)
		}
	}
	if cmd.Flags().Changed("hook-timeout") {
		hookTimeout, _ := cmd.Flags().GetInt("hook-timeout")
		// -1 means unchanged (default), any other value must be in range
		if hookTimeout != -1 && (hookTimeout < 0 || hookTimeout > types.AdviceHookTimeoutMax) {
			FatalError("--hook-timeout must be between 0 and %d", types.AdviceHookTimeoutMax)
		}
	}
	if cmd.Flags().Changed("priority") {
		priority, _ := cmd.Flags().GetInt("priority")
		if priority < 1 || priority > 5 {
			FatalError("--priority must be between 1 and 5")
		}
	}

	// If daemon is running, use RPC
	if daemonClient != nil {
		updateArgs := &rpc.UpdateArgs{ID: resolvedID}

		if cmd.Flags().Changed("title") {
			title, _ := cmd.Flags().GetString("title")
			updateArgs.Title = &title
		}
		if cmd.Flags().Changed("description") {
			description, _ := cmd.Flags().GetString("description")
			updateArgs.Description = &description
		}
		if cmd.Flags().Changed("priority") {
			priority, _ := cmd.Flags().GetInt("priority")
			updateArgs.Priority = &priority
		}

		// Hook fields
		if cmd.Flags().Changed("hook-command") {
			hookCommand, _ := cmd.Flags().GetString("hook-command")
			updateArgs.AdviceHookCommand = &hookCommand
		}
		if cmd.Flags().Changed("hook-trigger") {
			hookTrigger, _ := cmd.Flags().GetString("hook-trigger")
			updateArgs.AdviceHookTrigger = &hookTrigger
		}
		if cmd.Flags().Changed("hook-timeout") {
			hookTimeout, _ := cmd.Flags().GetInt("hook-timeout")
			if hookTimeout != -1 {
				updateArgs.AdviceHookTimeout = &hookTimeout
			}
		}
		if cmd.Flags().Changed("hook-on-failure") {
			hookOnFailure, _ := cmd.Flags().GetString("hook-on-failure")
			updateArgs.AdviceHookOnFailure = &hookOnFailure
		}

		// Labels via UpdateArgs
		if len(addLabels) > 0 {
			updateArgs.AddLabels = addLabels
		}
		if len(removeLabels) > 0 {
			updateArgs.RemoveLabels = removeLabels
		}

		resp, err := daemonClient.Update(updateArgs)
		if err != nil {
			FatalError("updating advice: %v", err)
		}

		var updatedIssue types.Issue
		if err := json.Unmarshal(resp.Data, &updatedIssue); err != nil {
			FatalError("parsing updated issue: %v", err)
		}

		// Run update hook
		if hookRunner != nil {
			hookRunner.Run(hooks.EventUpdate, &updatedIssue)
		}

		if jsonOutput {
			fmt.Println(string(resp.Data))
		} else {
			printAdviceEdited(&updatedIssue, addLabels, removeLabels)
		}

		SetLastTouchedID(resolvedID)
		return
	}

	// Direct mode
	updates := make(map[string]interface{})

	if cmd.Flags().Changed("title") {
		title, _ := cmd.Flags().GetString("title")
		updates["title"] = title
	}
	if cmd.Flags().Changed("description") {
		description, _ := cmd.Flags().GetString("description")
		updates["description"] = description
	}
	if cmd.Flags().Changed("priority") {
		priority, _ := cmd.Flags().GetInt("priority")
		updates["priority"] = priority
	}

	// Hook fields
	if cmd.Flags().Changed("hook-command") {
		hookCommand, _ := cmd.Flags().GetString("hook-command")
		updates["advice_hook_command"] = hookCommand
	}
	if cmd.Flags().Changed("hook-trigger") {
		hookTrigger, _ := cmd.Flags().GetString("hook-trigger")
		updates["advice_hook_trigger"] = hookTrigger
	}
	if cmd.Flags().Changed("hook-timeout") {
		hookTimeout, _ := cmd.Flags().GetInt("hook-timeout")
		if hookTimeout != -1 {
			updates["advice_hook_timeout"] = hookTimeout
		}
	}
	if cmd.Flags().Changed("hook-on-failure") {
		hookOnFailure, _ := cmd.Flags().GetString("hook-on-failure")
		updates["advice_hook_on_failure"] = hookOnFailure
	}

	// Apply field updates
	if len(updates) > 0 {
		if err := store.UpdateIssue(ctx, resolvedID, updates, actor); err != nil {
			FatalError("updating advice: %v", err)
		}
	}

	// Apply label operations
	if len(addLabels) > 0 || len(removeLabels) > 0 {
		for _, label := range addLabels {
			if err := store.AddLabel(ctx, resolvedID, label, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to add label %s: %v\n", label, err)
			}
		}
		for _, label := range removeLabels {
			if err := store.RemoveLabel(ctx, resolvedID, label, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to remove label %s: %v\n", label, err)
			}
		}
	}

	markDirtyAndScheduleFlush()

	// Get updated issue for output and hooks
	updatedIssue, err := store.GetIssue(ctx, resolvedID)
	if err != nil {
		FatalError("fetching updated advice: %v", err)
	}

	// Run update hook
	if updatedIssue != nil && hookRunner != nil {
		hookRunner.Run(hooks.EventUpdate, updatedIssue)
	}

	if jsonOutput {
		outputJSON(updatedIssue)
	} else {
		printAdviceEdited(updatedIssue, addLabels, removeLabels)
	}

	SetLastTouchedID(resolvedID)
}

// printAdviceEdited prints a human-readable summary of the edit
func printAdviceEdited(issue *types.Issue, addedLabels, removedLabels []string) {
	fmt.Printf("%s Updated advice: %s\n", ui.RenderPass("✓"), ui.RenderID(issue.ID))
	fmt.Printf("  Title: %s\n", issue.Title)

	if len(addedLabels) > 0 {
		fmt.Printf("  Added labels: %s\n", strings.Join(addedLabels, ", "))
	}
	if len(removedLabels) > 0 {
		fmt.Printf("  Removed labels: %s\n", strings.Join(removedLabels, ", "))
	}

	if issue.AdviceHookCommand != "" {
		fmt.Printf("  Hook: %s @ %s", issue.AdviceHookCommand, issue.AdviceHookTrigger)
		if issue.AdviceHookOnFailure != "" {
			fmt.Printf(" (%s)", issue.AdviceHookOnFailure)
		}
		fmt.Println()
	}

	fmt.Printf("  Priority: %d\n", issue.Priority)
}
