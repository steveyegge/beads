package main

import (
	"fmt"
	"regexp"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/ui"
)

var renameCmd = &cobra.Command{
	Use:   "rename <old-id> <new-id>",
	Short: "Rename an issue ID",
	Long: `Rename an issue from one ID to another.

This updates:
- The issue's primary ID
- All references in other issues (descriptions, titles, notes, etc.)
- Dependencies pointing to/from this issue
- Labels, comments, and events

Examples:
  bd rename bd-w382l bd-dolt     # Rename to memorable ID
  bd rename gt-abc123 gt-auth    # Use descriptive ID

Note: The new ID must use a valid prefix for this database.`,
	Args: cobra.ExactArgs(2),
	RunE: runRename,
}

func init() {
	rootCmd.AddCommand(renameCmd)
}

func runRename(cmd *cobra.Command, args []string) error {
	oldID := args[0]
	newID := args[1]

	// Validate IDs
	if oldID == newID {
		return fmt.Errorf("old and new IDs are the same")
	}

	// Basic ID format validation
	idPattern := regexp.MustCompile(`^[a-z]+-[a-zA-Z0-9._-]+$`)
	if !idPattern.MatchString(newID) {
		return fmt.Errorf("invalid new ID format %q: must be prefix-suffix (e.g., bd-dolt)", newID)
	}

	requireDaemon("rename")
	return renameViaDaemon(oldID, newID)
}

// renameViaDaemon renames an issue via the RPC daemon
func renameViaDaemon(oldID, newID string) error {
	renameArgs := &rpc.RenameArgs{
		OldID: oldID,
		NewID: newID,
	}

	result, err := daemonClient.Rename(renameArgs)
	if err != nil {
		return fmt.Errorf("rename failed: %w", err)
	}

	fmt.Printf("Renamed %s -> %s\n", ui.RenderWarn(result.OldID), ui.RenderAccent(result.NewID))
	if result.ReferencesUpdated > 0 {
		fmt.Printf("  Updated references in %d issue(s)\n", result.ReferencesUpdated)
	}

	return nil
}

