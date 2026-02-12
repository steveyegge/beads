package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var duplicateCmd = &cobra.Command{
	Use:     "duplicate <id> --of <canonical>",
	GroupID: "deps",
	Short:   "Mark an issue as a duplicate of another",
	Long: `Mark an issue as a duplicate of a canonical issue.

The duplicate issue is automatically closed with a reference to the canonical.
This is essential for large issue databases with many similar reports.

Examples:
  bd duplicate bd-abc --of bd-xyz    # Mark bd-abc as duplicate of bd-xyz`,
	Args: cobra.ExactArgs(1),
	RunE: runDuplicate,
}

var supersedeCmd = &cobra.Command{
	Use:     "supersede <id> --with <new>",
	GroupID: "deps",
	Short:   "Mark an issue as superseded by a newer one",
	Long: `Mark an issue as superseded by a newer version.

The superseded issue is automatically closed with a reference to the replacement.
Useful for design docs, specs, and evolving artifacts.

Examples:
  bd supersede bd-old --with bd-new    # Mark bd-old as superseded by bd-new`,
	Args: cobra.ExactArgs(1),
	RunE: runSupersede,
}

var (
	duplicateOf    string
	supersededWith string
)

func init() {
	duplicateCmd.Flags().StringVar(&duplicateOf, "of", "", "Canonical issue ID (required)")
	_ = duplicateCmd.MarkFlagRequired("of") // Only fails if flag missing (caught in tests)
	duplicateCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(duplicateCmd)

	supersedeCmd.Flags().StringVar(&supersededWith, "with", "", "Replacement issue ID (required)")
	_ = supersedeCmd.MarkFlagRequired("with") // Only fails if flag missing (caught in tests)
	supersedeCmd.ValidArgsFunction = issueIDCompletion
	rootCmd.AddCommand(supersedeCmd)
}

func runDuplicate(cmd *cobra.Command, args []string) error {
	CheckReadonly("duplicate")

	// Resolve partial IDs
	requireDaemon("duplicate")
	var duplicateID, canonicalID string
	{
		resp1, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: args[0]})
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", args[0], err)
		}
		if err := json.Unmarshal(resp1.Data, &duplicateID); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
		resp2, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: duplicateOf})
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", duplicateOf, err)
		}
		if err := json.Unmarshal(resp2.Data, &canonicalID); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}

	if duplicateID == canonicalID {
		return fmt.Errorf("cannot mark an issue as duplicate of itself")
	}

	// Verify canonical issue exists
	var canonical *types.Issue
	{
		resp, err := daemonClient.Show(&rpc.ShowArgs{ID: canonicalID})
		if err != nil {
			return fmt.Errorf("canonical issue not found: %s", canonicalID)
		}
		if err := json.Unmarshal(resp.Data, &canonical); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}

	// Update the duplicate issue with duplicate_of and close it
	closedStatus := string(types.StatusClosed)
	{
		_, err := daemonClient.Update(&rpc.UpdateArgs{
			ID:          duplicateID,
			DuplicateOf: &canonicalID,
			Status:      &closedStatus,
		})
		if err != nil {
			return fmt.Errorf("failed to mark as duplicate: %w", err)
		}
	}

	// Trigger auto-flush
	if flushManager != nil {
		flushManager.MarkDirty(false)
	}

	if jsonOutput {
		result := map[string]interface{}{
			"duplicate":  duplicateID,
			"canonical":  canonicalID,
			"status":     "closed",
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	fmt.Printf("%s Marked %s as duplicate of %s (closed)\n", ui.RenderPass("✓"), duplicateID, canonicalID)
	return nil
}

func runSupersede(cmd *cobra.Command, args []string) error {
	CheckReadonly("supersede")

	// Resolve partial IDs
	requireDaemon("supersede")
	var oldID, newID string
	{
		resp1, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: args[0]})
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", args[0], err)
		}
		if err := json.Unmarshal(resp1.Data, &oldID); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
		resp2, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: supersededWith})
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", supersededWith, err)
		}
		if err := json.Unmarshal(resp2.Data, &newID); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}

	if oldID == newID {
		return fmt.Errorf("cannot mark an issue as superseded by itself")
	}

	// Verify new issue exists
	var newIssue *types.Issue
	{
		resp, err := daemonClient.Show(&rpc.ShowArgs{ID: newID})
		if err != nil {
			return fmt.Errorf("replacement issue not found: %s", newID)
		}
		if err := json.Unmarshal(resp.Data, &newIssue); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}

	// Update the old issue with superseded_by and close it
	closedStatus := string(types.StatusClosed)
	{
		_, err := daemonClient.Update(&rpc.UpdateArgs{
			ID:           oldID,
			SupersededBy: &newID,
			Status:       &closedStatus,
		})
		if err != nil {
			return fmt.Errorf("failed to mark as superseded: %w", err)
		}
	}

	// Trigger auto-flush
	if flushManager != nil {
		flushManager.MarkDirty(false)
	}

	if jsonOutput {
		result := map[string]interface{}{
			"superseded":    oldID,
			"replacement":   newID,
			"status":        "closed",
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	fmt.Printf("%s Marked %s as superseded by %s (closed)\n", ui.RenderPass("✓"), oldID, newID)
	return nil
}
