package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/idgen"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/validation"
)

var migrateSemanticIDsCmd = &cobra.Command{
	Use:   "semantic-ids",
	Short: "Generate semantic slugs for existing beads",
	Long: `Generate semantic ID slugs for existing beads.

This command generates human-readable semantic slugs as aliases for existing
random hash-based IDs. The original canonical IDs are preserved.

Format: <prefix>-<type>-<title><random>[.<child>]

Examples:
  gt-epc-semantic_idszfyl8           # Epic with random from gt-zfyl8
  gt-bug-fix_login_timeout3q6a9      # Bug with random from gt-3q6a9
  gt-epc-semantic_idszfyl8.format_spec  # Child task

What this does:
- Lists all issues without semantic slugs
- Generates slug from type + title + canonical random ID
- Shows before/after preview
- Optionally updates database with slugs

Modes:
  --dry-run       Preview changes without modifying database
  --interactive   Confirm each slug before applying
  --filter=TYPE   Only process issues of specific type (bug, task, epic, etc.)

Examples:
  bd migrate semantic-ids --dry-run     # Preview all slugs
  bd migrate semantic-ids --interactive # Confirm each slug
  bd migrate semantic-ids --filter=epic # Only generate for epics`,
	Run: runMigrateSemanticIDs,
}

func init() {
	migrateSemanticIDsCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
	migrateSemanticIDsCmd.Flags().Bool("interactive", false, "Confirm each slug before applying")
	migrateSemanticIDsCmd.Flags().String("filter", "", "Only process issues of specific type (bug, task, epic, etc.)")
	migrateSemanticIDsCmd.Flags().Bool("json", false, "Output in JSON format")
	migrateCmd.AddCommand(migrateSemanticIDsCmd)
}

func runMigrateSemanticIDs(cmd *cobra.Command, _ []string) {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	interactive, _ := cmd.Flags().GetBool("interactive")
	filterType, _ := cmd.Flags().GetString("filter")
	jsonOut, _ := cmd.Flags().GetBool("json")

	// Block writes in readonly mode
	if !dryRun {
		CheckReadonly("migrate semantic-ids")
	}

	ctx := rootCtx

	// Find beads directory
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		if jsonOut {
			outputJSON(map[string]interface{}{
				"error":   "no_beads_directory",
				"message": "No .beads directory found. Run 'bd init' first.",
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: no .beads directory found\n")
			fmt.Fprintf(os.Stderr, "Hint: run 'bd init' to initialize bd\n")
		}
		os.Exit(1)
	}

	// Create backup before migration (unless dry-run)
	dbPath := beads.FindDatabasePath()
	if !dryRun && dbPath != "" {
		backupPath := strings.TrimSuffix(dbPath, ".db") + ".backup-semantic-" + time.Now().Format("20060102-150405") + ".db"
		if err := copyFile(dbPath, backupPath); err != nil {
			if jsonOut {
				outputJSON(map[string]interface{}{
					"error":   "backup_failed",
					"message": err.Error(),
				})
			} else {
				fmt.Fprintf(os.Stderr, "Error: failed to create backup: %v\n", err)
			}
			os.Exit(1)
		}
		if !jsonOut {
			fmt.Printf("%s\n\n", ui.RenderPass(fmt.Sprintf("Created backup: %s", filepath.Base(backupPath))))
		}
	}

	// Open database using factory (supports both SQLite and Dolt backends)
	store, err := factory.NewFromConfig(ctx, beadsDir)
	if err != nil {
		if jsonOut {
			outputJSON(map[string]interface{}{
				"error":   "open_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to open database: %v\n", err)
		}
		os.Exit(1)
	}
	defer func() { _ = store.Close() }()

	// Get all issues
	filter := types.IssueFilter{}
	if filterType != "" {
		issueType := types.IssueType(filterType)
		filter.IssueType = &issueType
	}
	issues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		if jsonOut {
			outputJSON(map[string]interface{}{
				"error":   "list_failed",
				"message": err.Error(),
			})
		} else {
			fmt.Fprintf(os.Stderr, "Error: failed to list issues: %v\n", err)
		}
		os.Exit(1)
	}

	if len(issues) == 0 {
		if jsonOut {
			outputJSON(map[string]interface{}{
				"status":  "no_issues",
				"message": "No issues to process",
			})
		} else {
			fmt.Println("No issues to process")
		}
		return
	}

	// Generate slugs
	gen := idgen.NewSemanticIDGenerator()
	slugMappings := generateSemanticSlugs(issues, gen)

	if len(slugMappings) == 0 {
		if jsonOut {
			outputJSON(map[string]interface{}{
				"status":  "no_changes",
				"message": "All issues already have semantic slugs",
			})
		} else {
			fmt.Println("All issues already have semantic slugs")
		}
		return
	}

	// Output preview
	if jsonOut {
		outputJSON(map[string]interface{}{
			"dry_run":      dryRun,
			"total_issues": len(issues),
			"to_update":    len(slugMappings),
			"mappings":     slugMappings,
		})
		if dryRun {
			return
		}
	} else {
		fmt.Printf("Found %d issues to generate slugs for:\n\n", len(slugMappings))
		for _, m := range slugMappings {
			fmt.Printf("  %s  →  %s\n", ui.RenderWarn(m.CanonicalID), ui.RenderAccent(m.SemanticSlug))
			fmt.Printf("    Title: %s\n", m.Title)
			fmt.Printf("    Type:  %s\n\n", m.IssueType)
		}
	}

	if dryRun {
		fmt.Println("Dry run complete - no changes made")
		return
	}

	// Apply changes
	applied := 0
	skipped := 0
	reader := bufio.NewReader(os.Stdin)

	for _, m := range slugMappings {
		if interactive && !jsonOut {
			fmt.Printf("Apply slug for %s? [y/N/q] ", m.CanonicalID)
			response, _ := reader.ReadString('\n')
			response = strings.TrimSpace(strings.ToLower(response))
			if response == "q" || response == "quit" {
				fmt.Println("Aborted")
				break
			}
			if response != "y" && response != "yes" {
				skipped++
				continue
			}
		}

		// Update the issue with the semantic slug
		// Store slug in a dedicated field or metadata
		updates := map[string]interface{}{
			"semantic_slug": m.SemanticSlug,
		}
		if err := store.UpdateIssue(ctx, m.CanonicalID, updates, "semantic-id-migration"); err != nil {
			if !jsonOut {
				fmt.Printf("%s\n", ui.RenderWarn(fmt.Sprintf("Warning: failed to update %s: %v", m.CanonicalID, err)))
			}
			continue
		}
		applied++
	}

	// Save mapping file for reference
	if applied > 0 {
		mappingPath := filepath.Join(beadsDir, "semantic-slug-mapping.json")
		if err := saveSlugMappingFile(mappingPath, slugMappings); err != nil {
			if !jsonOut {
				fmt.Printf("%s\n", ui.RenderWarn(fmt.Sprintf("Warning: failed to save mapping file: %v", err)))
			}
		} else if !jsonOut {
			fmt.Printf("%s\n", ui.RenderPass(fmt.Sprintf("Saved mapping to: %s", filepath.Base(mappingPath))))
		}
	}

	// Final status
	if jsonOut {
		outputJSON(map[string]interface{}{
			"status":  "success",
			"applied": applied,
			"skipped": skipped,
		})
	} else {
		fmt.Printf("\n%s\n", ui.RenderPass(fmt.Sprintf("Applied %d semantic slugs", applied)))
		if skipped > 0 {
			fmt.Printf("Skipped: %d\n", skipped)
		}
	}

	// Schedule auto-flush
	markDirtyAndScheduleFlush()
}

// SlugMapping represents the mapping from canonical ID to semantic slug
type SlugMapping struct {
	CanonicalID  string `json:"canonical_id"`
	SemanticSlug string `json:"semantic_slug"`
	Title        string `json:"title"`
	IssueType    string `json:"issue_type"`
}

// generateSemanticSlugs generates semantic slugs for issues that don't have one
func generateSemanticSlugs(issues []*types.Issue, gen *idgen.SemanticIDGenerator) []SlugMapping {
	var mappings []SlugMapping

	for _, issue := range issues {
		// Skip if already has a semantic slug
		if issue.SemanticSlug != "" {
			continue
		}

		// Skip if already looks like a semantic ID
		if validation.IsSemanticID(issue.ID) {
			continue
		}

		// Extract prefix and random from canonical ID
		prefix := extractIDPrefix(issue.ID)
		random := idgen.ExtractRandomFromID(issue.ID)

		if prefix == "" || random == "" {
			continue // Can't generate slug without these
		}

		// Get issue type
		issueType := string(issue.IssueType)
		if issueType == "" {
			issueType = "task" // Default
		}

		// Check if this is a child issue (ID contains a dot: gt-abc.1)
		var slug string
		if strings.Contains(issue.ID, ".") {
			// Child issue - for now, generate as standalone
			// Full hierarchical slug support requires parent lookup via dependencies
			slug = gen.GenerateSlugWithRandom(prefix, issueType, issue.Title, random)
		} else {
			// Top-level issue
			slug = gen.GenerateSlugWithRandom(prefix, issueType, issue.Title, random)
		}

		mappings = append(mappings, SlugMapping{
			CanonicalID:  issue.ID,
			SemanticSlug: slug,
			Title:        issue.Title,
			IssueType:    issueType,
		})
	}

	return mappings
}

// extractIDPrefix extracts the prefix from a canonical ID (e.g., "gt" from "gt-zfyl8")
func extractIDPrefix(id string) string {
	parts := strings.SplitN(id, "-", 2)
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}

// saveSlugMappingFile saves the slug mapping to a JSON file
func saveSlugMappingFile(path string, mappings []SlugMapping) error {
	data, err := json.MarshalIndent(map[string]interface{}{
		"generated_at": time.Now().Format(time.RFC3339),
		"count":        len(mappings),
		"mappings":     mappings,
	}, "", "  ")
	if err != nil {
		return err
	}

	// nolint:gosec // G306: JSONL file needs to be readable by other tools
	return os.WriteFile(path, data, 0644)
}
