package main

import (
	"context"
	"database/sql"
	"encoding/json"
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

// getStatusIcon returns the icon for a task status
func getStatusIcon(status string) string {
	switch status {
	case "completed":
		return ui.RenderPass("✓")
	case "in_progress":
		return ui.RenderAccent("→")
	default:
		return "○"
	}
}

// formatTimeAgo formats a time as a human-readable relative time
func formatTimeAgo(t time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case d < 24*time.Hour:
		hours := int(d.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	default:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

func init() {
	tasksListCmd.Flags().String("task-list", "", "Show tasks for a specific task list (directory name under ~/.claude/tasks/)")
	tasksListCmd.Flags().String("bead", "", "Show tasks linked to a specific bead")
	tasksListCmd.Flags().Bool("active", false, "Show all in-progress tasks")
	tasksListCmd.Flags().Bool("by-bead", false, "Group tasks by bead (use with --task-list)")

	tasksCmd.AddCommand(tasksListCmd)
	rootCmd.AddCommand(tasksCmd)
}
