package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
)

var exportCmd = &cobra.Command{
	Use:   "export",
	Short: "Export issues to JSONL format",
	Long: `Export all issues to JSON Lines format (one JSON object per line).
Issues are sorted by ID for consistent diffs.

Output to stdout by default, or use -o flag for file output.`,
	Run: func(cmd *cobra.Command, args []string) {
		format, _ := cmd.Flags().GetString("format")
		output, _ := cmd.Flags().GetString("output")
		statusFilter, _ := cmd.Flags().GetString("status")

		if format != "jsonl" {
			fmt.Fprintf(os.Stderr, "Error: only 'jsonl' format is currently supported\n")
			os.Exit(1)
		}

		// Build filter
		filter := types.IssueFilter{}
		if statusFilter != "" {
			status := types.Status(statusFilter)
			filter.Status = &status
		}

		// Get all issues
		ctx := context.Background()
		issues, err := store.SearchIssues(ctx, "", filter)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Sort by ID for consistent output
		sort.Slice(issues, func(i, j int) bool {
			return issues[i].ID < issues[j].ID
		})

		// Populate dependencies for all issues in one query (avoids N+1 problem)
		allDeps, err := store.GetAllDependencyRecords(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting dependencies: %v\n", err)
			os.Exit(1)
		}
		for _, issue := range issues {
			issue.Dependencies = allDeps[issue.ID]
		}

		// Open output
		out := os.Stdout
		if output != "" {
			f, err := os.Create(output)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
				os.Exit(1)
			}
			defer func() {
				if err := f.Close(); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to close output file: %v\n", err)
				}
			}()
			out = f
		}

		// Write JSONL
		encoder := json.NewEncoder(out)
		exportedIDs := make([]string, 0, len(issues))
		for _, issue := range issues {
			if err := encoder.Encode(issue); err != nil {
				fmt.Fprintf(os.Stderr, "Error encoding issue %s: %v\n", issue.ID, err)
				os.Exit(1)
			}
			exportedIDs = append(exportedIDs, issue.ID)
		}

		// Only clear dirty issues and auto-flush state if exporting to the default JSONL path
		// This prevents clearing dirty flags when exporting to custom paths (e.g., bd export -o backup.jsonl)
		if output == "" || output == findJSONLPath() {
			// Clear only the issues that were actually exported (fixes bd-52 race condition)
			if err := store.ClearDirtyIssuesByID(ctx, exportedIDs); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to clear dirty issues: %v\n", err)
			}

			// Clear auto-flush state since we just manually exported
			// This cancels any pending auto-flush timer and marks DB as clean
			clearAutoFlushState()
		}
	},
}

func init() {
	exportCmd.Flags().StringP("format", "f", "jsonl", "Export format (jsonl)")
	exportCmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	exportCmd.Flags().StringP("status", "s", "", "Filter by status")
	rootCmd.AddCommand(exportCmd)
}
