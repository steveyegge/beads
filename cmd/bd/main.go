package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/steveyegge/beads"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

var (
	dbPath     string
	actor      string
	store      storage.Storage
	jsonOutput bool

	// Auto-flush state
	autoFlushEnabled = true // Can be disabled with --no-auto-flush
	isDirty          = false
	flushMutex       sync.Mutex
	flushTimer       *time.Timer
	flushDebounce    = 5 * time.Second
	storeMutex       sync.Mutex // Protects store access from background goroutine
	storeActive      = false    // Tracks if store is available

	// Auto-import state
	autoImportEnabled = true // Can be disabled with --no-auto-import
)

var rootCmd = &cobra.Command{
	Use:   "bd",
	Short: "bd - Dependency-aware issue tracker",
	Long:  `Issues chained together like beads. A lightweight issue tracker with first-class dependency support.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// Skip database initialization for init command
		if cmd.Name() == "init" {
			return
		}

		// Set auto-flush based on flag (invert no-auto-flush)
		autoFlushEnabled = !noAutoFlush

		// Set auto-import based on flag (invert no-auto-import)
		autoImportEnabled = !noAutoImport

		// Initialize storage
		if dbPath == "" {
			// Use public API to find database (same logic as extensions)
			if foundDB := beads.FindDatabasePath(); foundDB != "" {
				dbPath = foundDB
			} else {
				// Fallback to default location (will be created by init command)
				home, _ := os.UserHomeDir()
				dbPath = filepath.Join(home, ".beads", "default.db")
			}
		}

		var err error
		store, err = sqlite.New(dbPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: failed to open database: %v\n", err)
			os.Exit(1)
		}

		// Mark store as active for flush goroutine safety
		storeMutex.Lock()
		storeActive = true
		storeMutex.Unlock()

		// Set actor from env or default
		if actor == "" {
			actor = os.Getenv("USER")
			if actor == "" {
				actor = "unknown"
			}
		}

		// Auto-import if JSONL is newer than DB (e.g., after git pull)
		// Skip for import command itself to avoid recursion
		if cmd.Name() != "import" && autoImportEnabled {
			autoImportIfNewer()
		}
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		// Signal that store is closing (prevents background flush from accessing closed store)
		storeMutex.Lock()
		storeActive = false
		storeMutex.Unlock()

		// Flush any pending changes before closing
		flushMutex.Lock()
		needsFlush := isDirty && autoFlushEnabled
		if needsFlush {
			// Cancel timer and flush immediately
			if flushTimer != nil {
				flushTimer.Stop()
				flushTimer = nil
			}
			isDirty = false
		}
		flushMutex.Unlock()

		if needsFlush {
			// Flush without checking isDirty again (we already cleared it)
			jsonlPath := findJSONLPath()
			ctx := context.Background()
			issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
			if err == nil {
				sort.Slice(issues, func(i, j int) bool {
					return issues[i].ID < issues[j].ID
				})
				allDeps, err := store.GetAllDependencyRecords(ctx)
				if err == nil {
					for _, issue := range issues {
						issue.Dependencies = allDeps[issue.ID]
					}
					tempPath := jsonlPath + ".tmp"
					f, err := os.Create(tempPath)
					if err == nil {
						encoder := json.NewEncoder(f)
						hasError := false
						for _, issue := range issues {
							if err := encoder.Encode(issue); err != nil {
								hasError = true
								break
							}
						}
						f.Close()
						if !hasError {
							os.Rename(tempPath, jsonlPath)
						} else {
							os.Remove(tempPath)
						}
					}
				}
			}
		}

		if store != nil {
			_ = store.Close()
		}
	},
}

// outputJSON outputs data as pretty-printed JSON
func outputJSON(v interface{}) {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		fmt.Fprintf(os.Stderr, "Error encoding JSON: %v\n", err)
		os.Exit(1)
	}
}

// findJSONLPath finds the JSONL file path for the current database
func findJSONLPath() string {
	// Use public API for path discovery
	jsonlPath := beads.FindJSONLPath(dbPath)

	// Ensure the directory exists (important for new databases)
	// This is the only difference from the public API - we create the directory
	dbDir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dbDir, 0755); err != nil {
		// If we can't create the directory, return discovered path anyway
		// (the subsequent write will fail with a clearer error)
		return jsonlPath
	}

	return jsonlPath
}

// autoImportIfNewer checks if JSONL is newer than DB and imports if so
func autoImportIfNewer() {
	// Find JSONL path
	jsonlPath := findJSONLPath()

	// Check if JSONL exists
	jsonlInfo, err := os.Stat(jsonlPath)
	if err != nil {
		// JSONL doesn't exist or can't be accessed, skip import
		return
	}

	// Check if DB exists
	dbInfo, err := os.Stat(dbPath)
	if err != nil {
		// DB doesn't exist (new init?), skip import
		return
	}

	// Compare modification times
	if !jsonlInfo.ModTime().After(dbInfo.ModTime()) {
		// JSONL is not newer than DB, skip import
		return
	}

	// JSONL is newer, perform silent import
	ctx := context.Background()

	// Read and parse JSONL
	f, err := os.Open(jsonlPath)
	if err != nil {
		// Can't open JSONL, skip import
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var allIssues []*types.Issue

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var issue types.Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			// Parse error, skip this import
			return
		}

		allIssues = append(allIssues, &issue)
	}

	if err := scanner.Err(); err != nil {
		return
	}

	// Import issues (create new, update existing)
	for _, issue := range allIssues {
		existing, err := store.GetIssue(ctx, issue.ID)
		if err != nil {
			continue
		}

		if existing != nil {
			// Update existing issue
			updates := make(map[string]interface{})
			updates["title"] = issue.Title
			updates["description"] = issue.Description
			updates["design"] = issue.Design
			updates["acceptance_criteria"] = issue.AcceptanceCriteria
			updates["notes"] = issue.Notes
			updates["status"] = issue.Status
			updates["priority"] = issue.Priority
			updates["issue_type"] = issue.IssueType
			updates["assignee"] = issue.Assignee
			if issue.EstimatedMinutes != nil {
				updates["estimated_minutes"] = *issue.EstimatedMinutes
			}

			_ = store.UpdateIssue(ctx, issue.ID, updates, "auto-import")
		} else {
			// Create new issue
			_ = store.CreateIssue(ctx, issue, "auto-import")
		}
	}

	// Import dependencies
	for _, issue := range allIssues {
		if len(issue.Dependencies) == 0 {
			continue
		}

		// Get existing dependencies
		existingDeps, err := store.GetDependencyRecords(ctx, issue.ID)
		if err != nil {
			continue
		}

		// Add missing dependencies
		for _, dep := range issue.Dependencies {
			exists := false
			for _, existing := range existingDeps {
				if existing.DependsOnID == dep.DependsOnID && existing.Type == dep.Type {
					exists = true
					break
				}
			}

			if !exists {
				_ = store.AddDependency(ctx, dep, "auto-import")
			}
		}
	}
}

// markDirtyAndScheduleFlush marks the database as dirty and schedules a flush
func markDirtyAndScheduleFlush() {
	if !autoFlushEnabled {
		return
	}

	flushMutex.Lock()
	defer flushMutex.Unlock()

	isDirty = true

	// Cancel existing timer if any
	if flushTimer != nil {
		flushTimer.Stop()
		flushTimer = nil
	}

	// Schedule new flush
	flushTimer = time.AfterFunc(flushDebounce, func() {
		flushToJSONL()
	})
}

// flushToJSONL exports all issues to JSONL if dirty
func flushToJSONL() {
	// Check if store is still active (not closed)
	storeMutex.Lock()
	if !storeActive {
		storeMutex.Unlock()
		return
	}
	storeMutex.Unlock()

	flushMutex.Lock()
	if !isDirty {
		flushMutex.Unlock()
		return
	}
	isDirty = false
	flushMutex.Unlock()

	jsonlPath := findJSONLPath()

	// Double-check store is still active before accessing
	storeMutex.Lock()
	if !storeActive {
		storeMutex.Unlock()
		return
	}
	storeMutex.Unlock()

	// Get all issues
	ctx := context.Background()
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-flush failed to get issues: %v\n", err)
		return
	}

	// Sort by ID for consistent output
	sort.Slice(issues, func(i, j int) bool {
		return issues[i].ID < issues[j].ID
	})

	// Populate dependencies for all issues
	allDeps, err := store.GetAllDependencyRecords(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-flush failed to get dependencies: %v\n", err)
		return
	}
	for _, issue := range issues {
		issue.Dependencies = allDeps[issue.ID]
	}

	// Write to temp file first, then rename (atomic)
	tempPath := jsonlPath + ".tmp"
	f, err := os.Create(tempPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-flush failed to create temp file: %v\n", err)
		return
	}

	encoder := json.NewEncoder(f)
	for _, issue := range issues {
		if err := encoder.Encode(issue); err != nil {
			f.Close()
			os.Remove(tempPath)
			fmt.Fprintf(os.Stderr, "Warning: auto-flush failed to encode issue %s: %v\n", issue.ID, err)
			return
		}
	}

	if err := f.Close(); err != nil {
		os.Remove(tempPath)
		fmt.Fprintf(os.Stderr, "Warning: auto-flush failed to close temp file: %v\n", err)
		return
	}

	// Atomic rename
	if err := os.Rename(tempPath, jsonlPath); err != nil {
		os.Remove(tempPath)
		fmt.Fprintf(os.Stderr, "Warning: auto-flush failed to rename file: %v\n", err)
		return
	}
}

var (
	noAutoFlush  bool
	noAutoImport bool
)

func init() {
	rootCmd.PersistentFlags().StringVar(&dbPath, "db", "", "Database path (default: auto-discover .beads/*.db or ~/.beads/default.db)")
	rootCmd.PersistentFlags().StringVar(&actor, "actor", "", "Actor name for audit trail (default: $USER)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")
	rootCmd.PersistentFlags().BoolVar(&noAutoFlush, "no-auto-flush", false, "Disable automatic JSONL sync after CRUD operations")
	rootCmd.PersistentFlags().BoolVar(&noAutoImport, "no-auto-import", false, "Disable automatic JSONL import when newer than DB")
}

var createCmd = &cobra.Command{
	Use:   "create [title]",
	Short: "Create a new issue",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		title := args[0]
		description, _ := cmd.Flags().GetString("description")
		design, _ := cmd.Flags().GetString("design")
		acceptance, _ := cmd.Flags().GetString("acceptance")
		priority, _ := cmd.Flags().GetInt("priority")
		issueType, _ := cmd.Flags().GetString("type")
		assignee, _ := cmd.Flags().GetString("assignee")
		labels, _ := cmd.Flags().GetStringSlice("labels")

		issue := &types.Issue{
			Title:              title,
			Description:        description,
			Design:             design,
			AcceptanceCriteria: acceptance,
			Status:             types.StatusOpen,
			Priority:           priority,
			IssueType:          types.IssueType(issueType),
			Assignee:           assignee,
		}

		ctx := context.Background()
		if err := store.CreateIssue(ctx, issue, actor); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Add labels if specified
		for _, label := range labels {
			if err := store.AddLabel(ctx, issue.ID, label, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to add label %s: %v\n", label, err)
			}
		}

		// Schedule auto-flush
		markDirtyAndScheduleFlush()

		if jsonOutput {
			outputJSON(issue)
		} else {
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("%s Created issue: %s\n", green("✓"), issue.ID)
			fmt.Printf("  Title: %s\n", issue.Title)
			fmt.Printf("  Priority: P%d\n", issue.Priority)
			fmt.Printf("  Status: %s\n", issue.Status)
		}
	},
}

func init() {
	createCmd.Flags().StringP("description", "d", "", "Issue description")
	createCmd.Flags().String("design", "", "Design notes")
	createCmd.Flags().String("acceptance", "", "Acceptance criteria")
	createCmd.Flags().IntP("priority", "p", 2, "Priority (0-4, 0=highest)")
	createCmd.Flags().StringP("type", "t", "task", "Issue type (bug|feature|task|epic|chore)")
	createCmd.Flags().StringP("assignee", "a", "", "Assignee")
	createCmd.Flags().StringSliceP("labels", "l", []string{}, "Labels (comma-separated)")
	rootCmd.AddCommand(createCmd)
}

var showCmd = &cobra.Command{
	Use:   "show [id]",
	Short: "Show issue details",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		issue, err := store.GetIssue(ctx, args[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if issue == nil {
			fmt.Fprintf(os.Stderr, "Issue %s not found\n", args[0])
			os.Exit(1)
		}

		if jsonOutput {
			// Include labels and dependencies in JSON output
			type IssueDetails struct {
				*types.Issue
				Labels      []string       `json:"labels,omitempty"`
				Dependencies []*types.Issue `json:"dependencies,omitempty"`
				Dependents   []*types.Issue `json:"dependents,omitempty"`
			}
			details := &IssueDetails{Issue: issue}
			details.Labels, _ = store.GetLabels(ctx, issue.ID)
			details.Dependencies, _ = store.GetDependencies(ctx, issue.ID)
			details.Dependents, _ = store.GetDependents(ctx, issue.ID)
			outputJSON(details)
			return
		}

		cyan := color.New(color.FgCyan).SprintFunc()
		fmt.Printf("\n%s: %s\n", cyan(issue.ID), issue.Title)
		fmt.Printf("Status: %s\n", issue.Status)
		fmt.Printf("Priority: P%d\n", issue.Priority)
		fmt.Printf("Type: %s\n", issue.IssueType)
		if issue.Assignee != "" {
			fmt.Printf("Assignee: %s\n", issue.Assignee)
		}
		if issue.EstimatedMinutes != nil {
			fmt.Printf("Estimated: %d minutes\n", *issue.EstimatedMinutes)
		}
		fmt.Printf("Created: %s\n", issue.CreatedAt.Format("2006-01-02 15:04"))
		fmt.Printf("Updated: %s\n", issue.UpdatedAt.Format("2006-01-02 15:04"))

		if issue.Description != "" {
			fmt.Printf("\nDescription:\n%s\n", issue.Description)
		}
		if issue.Design != "" {
			fmt.Printf("\nDesign:\n%s\n", issue.Design)
		}
		if issue.Notes != "" {
			fmt.Printf("\nNotes:\n%s\n", issue.Notes)
		}
		if issue.AcceptanceCriteria != "" {
			fmt.Printf("\nAcceptance Criteria:\n%s\n", issue.AcceptanceCriteria)
		}

		// Show labels
		labels, _ := store.GetLabels(ctx, issue.ID)
		if len(labels) > 0 {
			fmt.Printf("\nLabels: %v\n", labels)
		}

		// Show dependencies
		deps, _ := store.GetDependencies(ctx, issue.ID)
		if len(deps) > 0 {
			fmt.Printf("\nDepends on (%d):\n", len(deps))
			for _, dep := range deps {
				fmt.Printf("  → %s: %s [P%d]\n", dep.ID, dep.Title, dep.Priority)
			}
		}

		// Show dependents
		dependents, _ := store.GetDependents(ctx, issue.ID)
		if len(dependents) > 0 {
			fmt.Printf("\nBlocks (%d):\n", len(dependents))
			for _, dep := range dependents {
				fmt.Printf("  ← %s: %s [P%d]\n", dep.ID, dep.Title, dep.Priority)
			}
		}

		fmt.Println()
	},
}

func init() {
	rootCmd.AddCommand(showCmd)
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List issues",
	Run: func(cmd *cobra.Command, args []string) {
		status, _ := cmd.Flags().GetString("status")
		assignee, _ := cmd.Flags().GetString("assignee")
		issueType, _ := cmd.Flags().GetString("type")
		limit, _ := cmd.Flags().GetInt("limit")

		filter := types.IssueFilter{
			Limit: limit,
		}
		if status != "" {
			s := types.Status(status)
			filter.Status = &s
		}
		// Use Changed() to properly handle P0 (priority=0)
		if cmd.Flags().Changed("priority") {
			priority, _ := cmd.Flags().GetInt("priority")
			filter.Priority = &priority
		}
		if assignee != "" {
			filter.Assignee = &assignee
		}
		if issueType != "" {
			t := types.IssueType(issueType)
			filter.IssueType = &t
		}

		ctx := context.Background()
		issues, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(issues)
			return
		}

		fmt.Printf("\nFound %d issues:\n\n", len(issues))
		for _, issue := range issues {
			fmt.Printf("%s [P%d] %s\n", issue.ID, issue.Priority, issue.Status)
			fmt.Printf("  %s\n", issue.Title)
			if issue.Assignee != "" {
				fmt.Printf("  Assignee: %s\n", issue.Assignee)
			}
			fmt.Println()
		}
	},
}

func init() {
	listCmd.Flags().StringP("status", "s", "", "Filter by status")
	listCmd.Flags().IntP("priority", "p", 0, "Filter by priority")
	listCmd.Flags().StringP("assignee", "a", "", "Filter by assignee")
	listCmd.Flags().StringP("type", "t", "", "Filter by type")
	listCmd.Flags().IntP("limit", "n", 0, "Limit results")
	rootCmd.AddCommand(listCmd)
}

var updateCmd = &cobra.Command{
	Use:   "update [id]",
	Short: "Update an issue",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		updates := make(map[string]interface{})

		if cmd.Flags().Changed("status") {
			status, _ := cmd.Flags().GetString("status")
			updates["status"] = status
		}
		if cmd.Flags().Changed("priority") {
			priority, _ := cmd.Flags().GetInt("priority")
			updates["priority"] = priority
		}
		if cmd.Flags().Changed("title") {
			title, _ := cmd.Flags().GetString("title")
			updates["title"] = title
		}
		if cmd.Flags().Changed("assignee") {
			assignee, _ := cmd.Flags().GetString("assignee")
			updates["assignee"] = assignee
		}
		if cmd.Flags().Changed("design") {
			design, _ := cmd.Flags().GetString("design")
			updates["design"] = design
		}
		if cmd.Flags().Changed("notes") {
			notes, _ := cmd.Flags().GetString("notes")
			updates["notes"] = notes
		}
		if cmd.Flags().Changed("acceptance-criteria") {
			acceptanceCriteria, _ := cmd.Flags().GetString("acceptance-criteria")
			updates["acceptance_criteria"] = acceptanceCriteria
		}

		if len(updates) == 0 {
			fmt.Println("No updates specified")
			return
		}

		ctx := context.Background()
		if err := store.UpdateIssue(ctx, args[0], updates, actor); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Schedule auto-flush
		markDirtyAndScheduleFlush()

		if jsonOutput {
			// Fetch updated issue and output
			issue, _ := store.GetIssue(ctx, args[0])
			outputJSON(issue)
		} else {
			green := color.New(color.FgGreen).SprintFunc()
			fmt.Printf("%s Updated issue: %s\n", green("✓"), args[0])
		}
	},
}

func init() {
	updateCmd.Flags().StringP("status", "s", "", "New status")
	updateCmd.Flags().IntP("priority", "p", 0, "New priority")
	updateCmd.Flags().String("title", "", "New title")
	updateCmd.Flags().StringP("assignee", "a", "", "New assignee")
	updateCmd.Flags().String("design", "", "Design notes")
	updateCmd.Flags().String("notes", "", "Additional notes")
	updateCmd.Flags().String("acceptance-criteria", "", "Acceptance criteria")
	rootCmd.AddCommand(updateCmd)
}

var closeCmd = &cobra.Command{
	Use:   "close [id...]",
	Short: "Close one or more issues",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		reason, _ := cmd.Flags().GetString("reason")
		if reason == "" {
			reason = "Closed"
		}

		ctx := context.Background()
		closedIssues := []*types.Issue{}
		for _, id := range args {
			if err := store.CloseIssue(ctx, id, reason, actor); err != nil {
				fmt.Fprintf(os.Stderr, "Error closing %s: %v\n", id, err)
				continue
			}
			if jsonOutput {
				issue, _ := store.GetIssue(ctx, id)
				if issue != nil {
					closedIssues = append(closedIssues, issue)
				}
			} else {
				green := color.New(color.FgGreen).SprintFunc()
				fmt.Printf("%s Closed %s: %s\n", green("✓"), id, reason)
			}
		}

		// Schedule auto-flush if any issues were closed
		if len(args) > 0 {
			markDirtyAndScheduleFlush()
		}

		if jsonOutput && len(closedIssues) > 0 {
			outputJSON(closedIssues)
		}
	},
}

func init() {
	closeCmd.Flags().StringP("reason", "r", "", "Reason for closing")
	rootCmd.AddCommand(closeCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
