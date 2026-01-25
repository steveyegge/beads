package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/ui"
)

// tasksCmd is the parent command for task-related subcommands
var tasksCmd = &cobra.Command{
	Use:     "tasks",
	GroupID: "issues",
	Short:   "Manage Claude Code task tracking",
	Long: `View and manage Claude Code tasks synced from ~/.claude/tasks/.

The beads daemon monitors Claude Code's task files and syncs them to beads.
Tasks can be linked to beads by prefixing task content with the bead ID:
  "bd-f7k2: Implement JWT validation"

Examples:
  bd tasks list                    # List all task lists
  bd tasks list --task-list abc123 # Show tasks for a specific task list
  bd tasks list --bead bd-f7k2     # Show tasks linked to a bead
  bd tasks list --active           # Show all in-progress tasks`,
}

var tasksListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Claude Code tasks",
	Long: `List Claude Code tasks synced from ~/.claude/tasks/.

By default, shows a summary of all tracked task lists.
Use --task-list to show tasks for a specific Claude Code session/task list.
Use --bead to show tasks linked to a specific bead.
Use --active to show all tasks currently in_progress.

Examples:
  bd tasks list                         # Summary of all task lists
  bd tasks list --task-list abc123      # Tasks for task list abc123
  bd tasks list --bead bd-f7k2          # Tasks linked to bd-f7k2
  bd tasks list --active                # All in-progress tasks
  bd tasks list --task-list abc123 --by-bead  # Group by bead`,
	Run: runTasksList,
}

func runTasksList(cmd *cobra.Command, args []string) {
	taskListID, _ := cmd.Flags().GetString("task-list")
	beadID, _ := cmd.Flags().GetString("bead")
	active, _ := cmd.Flags().GetBool("active")
	byBead, _ := cmd.Flags().GetBool("by-bead")

	ctx := context.Background()

	// Get database connection
	db, err := getTasksDB(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	switch {
	case taskListID != "":
		// Show tasks for a specific task list
		showTasksForTaskList(ctx, db, taskListID, byBead)
	case beadID != "":
		// Show tasks linked to a bead
		showTasksForBead(ctx, db, beadID)
	case active:
		// Show all active tasks
		showActiveTasks(ctx, db)
	default:
		// Show summary of all task lists
		showTaskListsSummary(ctx, db)
	}
}

// getTasksDB gets a database connection for task queries
func getTasksDB(ctx context.Context) (*sql.DB, error) {
	// Try daemon first
	if daemonClient != nil {
		// For now, we need direct DB access for task queries
		// TODO: Add RPC methods for task queries
	}

	// Direct database access
	if store != nil {
		if sqlStore, ok := store.(interface{ UnderlyingDB() *sql.DB }); ok {
			return sqlStore.UnderlyingDB(), nil
		}
	}

	return nil, fmt.Errorf("no database connection available (daemon may not be running)")
}

// showTaskListsSummary displays a summary of all tracked task lists
func showTaskListsSummary(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `
		SELECT
			tfs.task_list_id,
			tfs.file_path,
			tfs.last_modified_at,
			COUNT(ct.id) as task_count,
			SUM(CASE WHEN ct.status = 'completed' THEN 1 ELSE 0 END) as completed_count,
			SUM(CASE WHEN ct.status = 'in_progress' THEN 1 ELSE 0 END) as in_progress_count,
			SUM(CASE WHEN ct.status = 'pending' THEN 1 ELSE 0 END) as pending_count
		FROM task_file_state tfs
		LEFT JOIN cc_tasks ct ON tfs.task_list_id = ct.task_list_id
		GROUP BY tfs.task_list_id
		ORDER BY tfs.last_modified_at DESC
	`)
	if err != nil {
		// Tables might not exist yet
		if strings.Contains(err.Error(), "no such table") {
			fmt.Printf("\n%s No Claude Code tasks tracked yet.\n", ui.RenderWarn("📋"))
			fmt.Println("   The daemon will sync tasks from ~/.claude/tasks/ automatically.")
			fmt.Println()
			return
		}
		fmt.Fprintf(os.Stderr, "Error querying task lists: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	type taskListInfo struct {
		TaskListID      string
		FilePath        string
		LastModified    time.Time
		TaskCount       int
		CompletedCount  int
		InProgressCount int
		PendingCount    int
	}

	var lists []taskListInfo
	for rows.Next() {
		var info taskListInfo
		var lastModified sql.NullTime
		err := rows.Scan(&info.TaskListID, &info.FilePath, &lastModified,
			&info.TaskCount, &info.CompletedCount, &info.InProgressCount, &info.PendingCount)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning row: %v\n", err)
			continue
		}
		if lastModified.Valid {
			info.LastModified = lastModified.Time
		}
		lists = append(lists, info)
	}

	if jsonOutput {
		outputJSON(lists)
		return
	}

	if len(lists) == 0 {
		fmt.Printf("\n%s No Claude Code tasks tracked yet.\n", ui.RenderWarn("📋"))
		fmt.Println("   The daemon will sync tasks from ~/.claude/tasks/ automatically.")
		fmt.Println()
		return
	}

	fmt.Printf("\n%s Claude Code Task Lists (%d tracked):\n\n", ui.RenderAccent("📋"), len(lists))
	for _, info := range lists {
		// Format progress
		progress := fmt.Sprintf("%d/%d completed", info.CompletedCount, info.TaskCount)
		if info.InProgressCount > 0 {
			progress += fmt.Sprintf(", %d in progress", info.InProgressCount)
		}

		// Format time
		timeAgo := formatTimeAgo(info.LastModified)

		fmt.Printf("  %s  %s\n", ui.RenderID(info.TaskListID), progress)
		fmt.Printf("    Last updated: %s\n", timeAgo)
	}
	fmt.Println()
	fmt.Println("Use 'bd tasks list --task-list <id>' to see tasks for a specific list.")
	fmt.Println()
}

// showTasksForTaskList displays tasks for a specific task list
func showTasksForTaskList(ctx context.Context, db *sql.DB, taskListID string, byBead bool) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, task_list_id, bead_id, ordinal, content, active_form, status,
		       created_at, updated_at, completed_at
		FROM cc_tasks
		WHERE task_list_id = ?
		ORDER BY ordinal
	`, taskListID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying tasks: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	tasks := scanTasks(rows)

	if jsonOutput {
		outputJSON(tasks)
		return
	}

	if len(tasks) == 0 {
		fmt.Printf("\n%s No tasks found for task list: %s\n\n", ui.RenderWarn("📋"), taskListID)
		return
	}

	if byBead {
		// Group by bead
		showTasksGroupedByBead(tasks, taskListID)
		return
	}

	fmt.Printf("\n%s Tasks for task list: %s\n\n", ui.RenderAccent("📋"), taskListID)
	displayTasks(tasks)
}

// showTasksForBead displays tasks linked to a specific bead
func showTasksForBead(ctx context.Context, db *sql.DB, beadID string) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, task_list_id, bead_id, ordinal, content, active_form, status,
		       created_at, updated_at, completed_at
		FROM cc_tasks
		WHERE bead_id = ?
		ORDER BY task_list_id, ordinal
	`, beadID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying tasks: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	tasks := scanTasks(rows)

	if jsonOutput {
		outputJSON(tasks)
		return
	}

	if len(tasks) == 0 {
		fmt.Printf("\n%s No tasks linked to bead: %s\n\n", ui.RenderWarn("📋"), beadID)
		fmt.Println("Tip: Prefix tasks with the bead ID when creating them in Claude Code:")
		fmt.Printf("     \"%s: Task description\"\n\n", beadID)
		return
	}

	fmt.Printf("\n%s Tasks linked to bead: %s\n\n", ui.RenderAccent("📋"), ui.RenderID(beadID))
	displayTasks(tasks)
}

// showActiveTasks displays all in-progress tasks
func showActiveTasks(ctx context.Context, db *sql.DB) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, task_list_id, bead_id, ordinal, content, active_form, status,
		       created_at, updated_at, completed_at
		FROM cc_tasks
		WHERE status = 'in_progress'
		ORDER BY updated_at DESC
	`)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying active tasks: %v\n", err)
		os.Exit(1)
	}
	defer rows.Close()

	tasks := scanTasks(rows)

	if jsonOutput {
		outputJSON(tasks)
		return
	}

	if len(tasks) == 0 {
		fmt.Printf("\n%s No active (in_progress) tasks\n\n", ui.RenderPass("✨"))
		return
	}

	fmt.Printf("\n%s Active tasks (%d in progress):\n\n", ui.RenderAccent("→"), len(tasks))
	for _, t := range tasks {
		beadInfo := ""
		if t.BeadID != nil {
			beadInfo = fmt.Sprintf(" [%s]", ui.RenderID(*t.BeadID))
		}
		fmt.Printf("  → %s%s\n", t.Content, beadInfo)
		fmt.Printf("    Task list: %s | Updated: %s\n", t.TaskListID, formatTimeAgo(t.UpdatedAt))
	}
	fmt.Println()
}

// showTasksGroupedByBead shows tasks grouped by their bead association
func showTasksGroupedByBead(tasks []rpc.CCTask, taskListID string) {
	// Group tasks by bead
	byBead := make(map[string][]rpc.CCTask)
	var unlinked []rpc.CCTask

	for _, t := range tasks {
		if t.BeadID != nil {
			byBead[*t.BeadID] = append(byBead[*t.BeadID], t)
		} else {
			unlinked = append(unlinked, t)
		}
	}

	fmt.Printf("\n%s Tasks for task list: %s (grouped by bead)\n\n", ui.RenderAccent("📋"), taskListID)

	// Show linked tasks by bead
	for beadID, beadTasks := range byBead {
		fmt.Printf("%s:\n", ui.RenderID(beadID))
		for _, t := range beadTasks {
			statusIcon := getStatusIcon(t.Status)
			fmt.Printf("  %s %s\n", statusIcon, t.Content)
		}
		fmt.Println()
	}

	// Show unlinked tasks
	if len(unlinked) > 0 {
		fmt.Println("Unlinked:")
		for _, t := range unlinked {
			statusIcon := getStatusIcon(t.Status)
			fmt.Printf("  %s %s\n", statusIcon, t.Content)
		}
		fmt.Println()
	}
}

// displayTasks renders a list of tasks
func displayTasks(tasks []rpc.CCTask) {
	for _, t := range tasks {
		statusIcon := getStatusIcon(t.Status)
		beadInfo := ""
		if t.BeadID != nil {
			beadInfo = fmt.Sprintf(" [%s]", ui.RenderID(*t.BeadID))
		}
		fmt.Printf("  %s %s%s\n", statusIcon, t.Content, beadInfo)
	}
	fmt.Println()

	// Show legend
	fmt.Println("Legend: ✓ completed  → in_progress  ○ pending")
	fmt.Println()
}

// scanTasks scans rows into CCTask slice
func scanTasks(rows *sql.Rows) []rpc.CCTask {
	var tasks []rpc.CCTask
	for rows.Next() {
		var t rpc.CCTask
		var beadID sql.NullString
		var completedAt sql.NullTime
		err := rows.Scan(&t.ID, &t.TaskListID, &beadID, &t.Ordinal, &t.Content,
			&t.ActiveForm, &t.Status, &t.CreatedAt, &t.UpdatedAt, &completedAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning task: %v\n", err)
			continue
		}
		if beadID.Valid {
			t.BeadID = &beadID.String
		}
		if completedAt.Valid {
			t.CompletedAt = &completedAt.Time
		}
		tasks = append(tasks, t)
	}
	return tasks
}

// getStatusIcon and formatTimeAgo are defined in mol_current.go and wisp.go respectively

func init() {
	tasksListCmd.Flags().String("task-list", "", "Show tasks for a specific task list (directory name under ~/.claude/tasks/)")
	tasksListCmd.Flags().String("bead", "", "Show tasks linked to a specific bead")
	tasksListCmd.Flags().Bool("active", false, "Show all in-progress tasks")
	tasksListCmd.Flags().Bool("by-bead", false, "Group tasks by bead (use with --task-list)")

	tasksVerifyCmd.Flags().String("task-list", "", "Task list ID to verify (defaults to CLAUDE_CODE_TASK_LIST_ID env var)")

	tasksCmd.AddCommand(tasksListCmd)
	tasksCmd.AddCommand(tasksVerifyCmd)
	rootCmd.AddCommand(tasksCmd)
}

// tasksVerifyCmd verifies task completion status before session end
var tasksVerifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify task completion status (for session end hooks)",
	Long: `Verify that tasks are properly completed before ending a Claude Code session.

This command is designed to be called from a Claude Code Stop hook.
It checks for incomplete tasks and warns if work may not be finished.

The task list ID is read from CLAUDE_CODE_TASK_LIST_ID environment variable,
or can be specified with --task-list flag.

Checks performed:
  - Tasks still in_progress (should be completed or explicitly deferred)
  - Tasks still pending (may indicate incomplete work)
  - Completed tasks linked to beads that are still open

This command always exits 0 (warnings only, does not block session end).

Examples:
  bd tasks verify                           # Uses CLAUDE_CODE_TASK_LIST_ID
  bd tasks verify --task-list abc123        # Explicit task list`,
	Run: runTasksVerify,
}

// VerifyResult holds the result of task verification
type VerifyResult struct {
	TaskListID       string          `json:"task_list_id"`
	TotalTasks       int             `json:"total_tasks"`
	CompletedTasks   int             `json:"completed_tasks"`
	InProgressTasks  int             `json:"in_progress_tasks"`
	PendingTasks     int             `json:"pending_tasks"`
	Warnings         []VerifyWarning `json:"warnings,omitempty"`
	HasWarnings      bool            `json:"has_warnings"`
}

// VerifyWarning represents a single verification warning
type VerifyWarning struct {
	Type    string `json:"type"`    // "incomplete_task", "open_bead", etc.
	Message string `json:"message"`
	TaskID  string `json:"task_id,omitempty"`
	BeadID  string `json:"bead_id,omitempty"`
}

func runTasksVerify(cmd *cobra.Command, args []string) {
	// Get task list ID from flag or environment variable
	taskListID, _ := cmd.Flags().GetString("task-list")
	if taskListID == "" {
		taskListID = os.Getenv("CLAUDE_CODE_TASK_LIST_ID")
	}

	// If no task list ID, silently exit (not in a tracked session)
	if taskListID == "" {
		if jsonOutput {
			outputJSON(VerifyResult{HasWarnings: false})
		}
		return
	}

	ctx := context.Background()

	// Get database connection
	db, err := getTasksDB(ctx)
	if err != nil {
		// No database - silently exit (beads may not be set up)
		if jsonOutput {
			outputJSON(VerifyResult{TaskListID: taskListID, HasWarnings: false})
		}
		return
	}

	result := verifyTasks(ctx, db, taskListID)

	if jsonOutput {
		outputJSON(result)
		return
	}

	// Human-readable output
	if !result.HasWarnings {
		fmt.Fprintf(os.Stderr, "✓ All tasks verified for session %s\n", taskListID)
		return
	}

	fmt.Fprintf(os.Stderr, "\n⚠️  Task verification warnings for session %s:\n\n", taskListID)
	for _, w := range result.Warnings {
		fmt.Fprintf(os.Stderr, "  • %s\n", w.Message)
	}
	fmt.Fprintf(os.Stderr, "\nConsider completing these tasks or updating their status before ending the session.\n\n")
}

func verifyTasks(ctx context.Context, db *sql.DB, taskListID string) VerifyResult {
	result := VerifyResult{
		TaskListID: taskListID,
		Warnings:   []VerifyWarning{},
	}

	// Query tasks for this task list
	rows, err := db.QueryContext(ctx, `
		SELECT ct.id, ct.bead_id, ct.content, ct.status,
		       i.id as issue_id, i.status as issue_status, i.title as issue_title
		FROM cc_tasks ct
		LEFT JOIN issues i ON ct.bead_id = i.id
		WHERE ct.task_list_id = ?
		ORDER BY ct.ordinal
	`, taskListID)
	if err != nil {
		// Tables might not exist - return empty result
		return result
	}
	defer rows.Close()

	for rows.Next() {
		var taskID, content, taskStatus string
		var beadID, issueID, issueStatus, issueTitle sql.NullString

		err := rows.Scan(&taskID, &beadID, &content, &taskStatus,
			&issueID, &issueStatus, &issueTitle)
		if err != nil {
			continue
		}

		result.TotalTasks++

		switch taskStatus {
		case "completed":
			result.CompletedTasks++
			// Check if linked bead is still open
			if beadID.Valid && issueID.Valid && issueStatus.Valid {
				if issueStatus.String == "open" || issueStatus.String == "in_progress" {
					result.Warnings = append(result.Warnings, VerifyWarning{
						Type:    "open_bead",
						Message: fmt.Sprintf("Task '%s' completed but bead %s (%s) is still %s",
							truncateString(content, 40), beadID.String,
							truncateString(issueTitle.String, 30), issueStatus.String),
						TaskID: taskID,
						BeadID: beadID.String,
					})
				}
			}

		case "in_progress":
			result.InProgressTasks++
			result.Warnings = append(result.Warnings, VerifyWarning{
				Type:    "incomplete_task",
				Message: fmt.Sprintf("Task still in progress: '%s'", truncateString(content, 50)),
				TaskID:  taskID,
			})

		case "pending":
			result.PendingTasks++
			// Only warn about pending tasks if there are also completed tasks
			// (indicates partial completion)
		}
	}

	// If there are pending tasks AND completed tasks, warn about incomplete work
	if result.PendingTasks > 0 && result.CompletedTasks > 0 {
		result.Warnings = append(result.Warnings, VerifyWarning{
			Type:    "pending_tasks",
			Message: fmt.Sprintf("%d task(s) still pending - work may be incomplete", result.PendingTasks),
		})
	}

	result.HasWarnings = len(result.Warnings) > 0
	return result
}

// truncateString is defined in activity.go
