package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

var createFormCmd = &cobra.Command{
	Use:   "create-form",
	Short: "Create a new issue using an interactive form",
	Long: `Create a new issue using an interactive terminal form.

This command provides a user-friendly form interface for creating issues,
with fields for title, description, type, priority, labels, and more.

The form uses keyboard navigation:
  - Tab/Shift+Tab: Move between fields
  - Enter: Submit the form (on the last field or submit button)
  - Ctrl+C: Cancel and exit
  - Arrow keys: Navigate within select fields`,
	Run: func(cmd *cobra.Command, args []string) {
		CheckReadonly("create-form")
		runCreateForm(cmd)
	},
}

func runCreateForm(cmd *cobra.Command) {
	// Form field values
	var (
		title       string
		description string
		issueType   string
		priorityStr string
		assignee    string
		labelsInput string
		design      string
		acceptance  string
		externalRef string
		depsInput   string
	)

	// Issue type options
	typeOptions := []huh.Option[string]{
		huh.NewOption("Task", "task"),
		huh.NewOption("Bug", "bug"),
		huh.NewOption("Feature", "feature"),
		huh.NewOption("Epic", "epic"),
		huh.NewOption("Chore", "chore"),
	}

	// Priority options
	priorityOptions := []huh.Option[string]{
		huh.NewOption("P0 - Critical", "0"),
		huh.NewOption("P1 - High", "1"),
		huh.NewOption("P2 - Medium (default)", "2"),
		huh.NewOption("P3 - Low", "3"),
		huh.NewOption("P4 - Backlog", "4"),
	}

	// Build the form
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Title").
				Description("Brief summary of the issue (required)").
				Placeholder("e.g., Fix authentication bug in login handler").
				Value(&title).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("title is required")
					}
					if len(s) > 500 {
						return fmt.Errorf("title must be 500 characters or less")
					}
					return nil
				}),

			huh.NewText().
				Title("Description").
				Description("Detailed context about the issue").
				Placeholder("Explain why this issue exists and what needs to be done...").
				CharLimit(5000).
				Value(&description),

			huh.NewSelect[string]().
				Title("Type").
				Description("Categorize the kind of work").
				Options(typeOptions...).
				Value(&issueType),

			huh.NewSelect[string]().
				Title("Priority").
				Description("Set urgency level").
				Options(priorityOptions...).
				Value(&priorityStr),
		),

		huh.NewGroup(
			huh.NewInput().
				Title("Assignee").
				Description("Who should work on this? (optional)").
				Placeholder("username or email").
				Value(&assignee),

			huh.NewInput().
				Title("Labels").
				Description("Comma-separated tags (optional)").
				Placeholder("e.g., urgent, backend, needs-review").
				Value(&labelsInput),

			huh.NewInput().
				Title("External Reference").
				Description("Link to external tracker (optional)").
				Placeholder("e.g., gh-123, jira-ABC-456").
				Value(&externalRef),
		),

		huh.NewGroup(
			huh.NewText().
				Title("Design Notes").
				Description("Technical approach or design details (optional)").
				Placeholder("Describe the implementation approach...").
				CharLimit(5000).
				Value(&design),

			huh.NewText().
				Title("Acceptance Criteria").
				Description("How do we know this is done? (optional)").
				Placeholder("List the criteria for completion...").
				CharLimit(5000).
				Value(&acceptance),
		),

		huh.NewGroup(
			huh.NewInput().
				Title("Dependencies").
				Description("Format: type:id or just id (optional)").
				Placeholder("e.g., discovered-from:bd-20, blocks:bd-15").
				Value(&depsInput),

			huh.NewConfirm().
				Title("Create this issue?").
				Affirmative("Create").
				Negative("Cancel"),
		),
	).WithTheme(huh.ThemeDracula())

	err := form.Run()
	if err != nil {
		if err == huh.ErrUserAborted {
			fmt.Fprintln(os.Stderr, "Issue creation cancelled.")
			os.Exit(0)
		}
		FatalError("form error: %v", err)
	}

	// Parse priority
	priority, err := strconv.Atoi(priorityStr)
	if err != nil {
		priority = 2 // Default to medium if parsing fails
	}

	// Parse labels
	var labels []string
	if labelsInput != "" {
		for _, l := range strings.Split(labelsInput, ",") {
			l = strings.TrimSpace(l)
			if l != "" {
				labels = append(labels, l)
			}
		}
	}

	// Parse dependencies
	var deps []string
	if depsInput != "" {
		for _, d := range strings.Split(depsInput, ",") {
			d = strings.TrimSpace(d)
			if d != "" {
				deps = append(deps, d)
			}
		}
	}

	// Create the issue
	var externalRefPtr *string
	if externalRef != "" {
		externalRefPtr = &externalRef
	}

	// If daemon is running, use RPC
	if daemonClient != nil {
		createArgs := &rpc.CreateArgs{
			Title:              title,
			Description:        description,
			IssueType:          issueType,
			Priority:           priority,
			Design:             design,
			AcceptanceCriteria: acceptance,
			Assignee:           assignee,
			ExternalRef:        externalRef,
			Labels:             labels,
			Dependencies:       deps,
		}

		resp, err := daemonClient.Create(createArgs)
		if err != nil {
			FatalError("%v", err)
		}

		if jsonOutput {
			fmt.Println(string(resp.Data))
		} else {
			var issue types.Issue
			if err := json.Unmarshal(resp.Data, &issue); err != nil {
				FatalError("parsing response: %v", err)
			}
			printCreatedIssue(&issue)
		}
		return
	}

	// Direct mode
	issue := &types.Issue{
		Title:              title,
		Description:        description,
		Design:             design,
		AcceptanceCriteria: acceptance,
		Status:             types.StatusOpen,
		Priority:           priority,
		IssueType:          types.IssueType(issueType),
		Assignee:           assignee,
		ExternalRef:        externalRefPtr,
	}

	ctx := rootCtx

	// Check if any dependencies are discovered-from type
	// If so, inherit source_repo from the parent issue
	var discoveredFromParentID string
	for _, depSpec := range deps {
		depSpec = strings.TrimSpace(depSpec)
		if depSpec == "" {
			continue
		}

		if strings.Contains(depSpec, ":") {
			parts := strings.SplitN(depSpec, ":", 2)
			if len(parts) == 2 {
				depType := types.DependencyType(strings.TrimSpace(parts[0]))
				dependsOnID := strings.TrimSpace(parts[1])

				if depType == types.DepDiscoveredFrom && dependsOnID != "" {
					discoveredFromParentID = dependsOnID
					break
				}
			}
		}
	}

	// If we found a discovered-from dependency, inherit source_repo from parent
	if discoveredFromParentID != "" {
		parentIssue, err := store.GetIssue(ctx, discoveredFromParentID)
		if err == nil && parentIssue.SourceRepo != "" {
			issue.SourceRepo = parentIssue.SourceRepo
		}
	}

	if err := store.CreateIssue(ctx, issue, actor); err != nil {
		FatalError("%v", err)
	}

	// Add labels if specified
	for _, label := range labels {
		if err := store.AddLabel(ctx, issue.ID, label, actor); err != nil {
			WarnError("failed to add label %s: %v", label, err)
		}
	}

	// Add dependencies if specified
	for _, depSpec := range deps {
		depSpec = strings.TrimSpace(depSpec)
		if depSpec == "" {
			continue
		}

		var depType types.DependencyType
		var dependsOnID string

		if strings.Contains(depSpec, ":") {
			parts := strings.SplitN(depSpec, ":", 2)
			if len(parts) != 2 {
				WarnError("invalid dependency format '%s', expected 'type:id' or 'id'", depSpec)
				continue
			}
			depType = types.DependencyType(strings.TrimSpace(parts[0]))
			dependsOnID = strings.TrimSpace(parts[1])
		} else {
			depType = types.DepBlocks
			dependsOnID = depSpec
		}

		if !depType.IsValid() {
			WarnError("invalid dependency type '%s' (valid: blocks, related, parent-child, discovered-from)", depType)
			continue
		}

		dep := &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: dependsOnID,
			Type:        depType,
		}
		if err := store.AddDependency(ctx, dep, actor); err != nil {
			WarnError("failed to add dependency %s -> %s: %v", issue.ID, dependsOnID, err)
		}
	}

	// Schedule auto-flush
	markDirtyAndScheduleFlush()

	if jsonOutput {
		outputJSON(issue)
	} else {
		printCreatedIssue(issue)
	}
}

func printCreatedIssue(issue *types.Issue) {
	green := color.New(color.FgGreen).SprintFunc()
	fmt.Printf("\n%s Created issue: %s\n", green("✓"), issue.ID)
	fmt.Printf("  Title:    %s\n", issue.Title)
	fmt.Printf("  Type:     %s\n", issue.IssueType)
	fmt.Printf("  Priority: P%d\n", issue.Priority)
	fmt.Printf("  Status:   %s\n", issue.Status)
	if issue.Assignee != "" {
		fmt.Printf("  Assignee: %s\n", issue.Assignee)
	}
	if issue.Description != "" {
		desc := issue.Description
		if len(desc) > 100 {
			desc = desc[:97] + "..."
		}
		fmt.Printf("  Description: %s\n", desc)
	}
}

func init() {
	// Note: --json flag is defined as a persistent flag in main.go
	rootCmd.AddCommand(createFormCmd)
}
