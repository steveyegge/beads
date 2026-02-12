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

var relateCmd = &cobra.Command{
	Use:   "relate <id1> <id2>",
	Short: "Create a bidirectional relates_to link between issues",
	Long: `Create a loose 'see also' relationship between two issues.

The relates_to link is bidirectional - both issues will reference each other.
This enables knowledge graph connections without blocking or hierarchy.

Examples:
  bd relate bd-abc bd-xyz    # Link two related issues
  bd relate bd-123 bd-456    # Create see-also connection`,
	Args: cobra.ExactArgs(2),
	RunE: runRelate,
}

var unrelateCmd = &cobra.Command{
	Use:   "unrelate <id1> <id2>",
	Short: "Remove a relates_to link between issues",
	Long: `Remove a relates_to relationship between two issues.

Removes the link in both directions.

Example:
  bd unrelate bd-abc bd-xyz`,
	Args: cobra.ExactArgs(2),
	RunE: runUnrelate,
}

func init() {
	// Issue ID completions
	relateCmd.ValidArgsFunction = issueIDCompletion
	unrelateCmd.ValidArgsFunction = issueIDCompletion

	// Add as subcommands of dep
	depCmd.AddCommand(relateCmd)
	depCmd.AddCommand(unrelateCmd)

	// Backwards compatibility aliases at root level (hidden)
	relateAliasCmd := *relateCmd
	relateAliasCmd.Hidden = true
	relateAliasCmd.Deprecated = "use 'bd dep relate' instead (will be removed in v1.0.0)"
	rootCmd.AddCommand(&relateAliasCmd)

	unrelateAliasCmd := *unrelateCmd
	unrelateAliasCmd.Hidden = true
	unrelateAliasCmd.Deprecated = "use 'bd dep unrelate' instead (will be removed in v1.0.0)"
	rootCmd.AddCommand(&unrelateAliasCmd)
}

func runRelate(cmd *cobra.Command, args []string) error {
	CheckReadonly("relate")

	// Resolve partial IDs
	requireDaemon("relate")
	var id1, id2 string
	{
		resp1, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: args[0]})
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", args[0], err)
		}
		if err := json.Unmarshal(resp1.Data, &id1); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
		resp2, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: args[1]})
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", args[1], err)
		}
		if err := json.Unmarshal(resp2.Data, &id2); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}

	if id1 == id2 {
		return fmt.Errorf("cannot relate an issue to itself")
	}

	// Get both issues
	var issue1, issue2 *types.Issue
	{
		resp1, err := daemonClient.Show(&rpc.ShowArgs{ID: id1})
		if err != nil {
			return fmt.Errorf("failed to get issue %s: %w", id1, err)
		}
		if err := json.Unmarshal(resp1.Data, &issue1); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
		resp2, err := daemonClient.Show(&rpc.ShowArgs{ID: id2})
		if err != nil {
			return fmt.Errorf("failed to get issue %s: %w", id2, err)
		}
		if err := json.Unmarshal(resp2.Data, &issue2); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}

	if issue1 == nil {
		return fmt.Errorf("issue not found: %s", id1)
	}
	if issue2 == nil {
		return fmt.Errorf("issue not found: %s", id2)
	}

	// Add relates-to dependency: id1 -> id2 (bidirectional, so also id2 -> id1)
	// Per Decision 004, relates-to links are now stored in dependencies table
	{
		_, err := daemonClient.AddBidirectionalRelation(&rpc.DepAddBidirectionalArgs{
			ID1:     id1,
			ID2:     id2,
			DepType: string(types.DepRelatesTo),
		})
		if err != nil {
			return fmt.Errorf("failed to add relates-to %s <-> %s: %w", id1, id2, err)
		}
	}

	// Trigger auto-flush
	if flushManager != nil {
		flushManager.MarkDirty(false)
	}

	if jsonOutput {
		result := map[string]interface{}{
			"id1":     id1,
			"id2":     id2,
			"related": true,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	fmt.Printf("%s Linked %s ↔ %s\n", ui.RenderPass("✓"), id1, id2)
	return nil
}

func runUnrelate(cmd *cobra.Command, args []string) error {
	CheckReadonly("unrelate")

	// Resolve partial IDs
	requireDaemon("unrelate")
	var id1, id2 string
	{
		resp1, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: args[0]})
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", args[0], err)
		}
		if err := json.Unmarshal(resp1.Data, &id1); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
		resp2, err := daemonClient.ResolveID(&rpc.ResolveIDArgs{ID: args[1]})
		if err != nil {
			return fmt.Errorf("failed to resolve %s: %w", args[1], err)
		}
		if err := json.Unmarshal(resp2.Data, &id2); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}

	// Get both issues
	var issue1, issue2 *types.Issue
	{
		resp1, err := daemonClient.Show(&rpc.ShowArgs{ID: id1})
		if err != nil {
			return fmt.Errorf("failed to get issue %s: %w", id1, err)
		}
		if err := json.Unmarshal(resp1.Data, &issue1); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
		resp2, err := daemonClient.Show(&rpc.ShowArgs{ID: id2})
		if err != nil {
			return fmt.Errorf("failed to get issue %s: %w", id2, err)
		}
		if err := json.Unmarshal(resp2.Data, &issue2); err != nil {
			return fmt.Errorf("parsing response: %w", err)
		}
	}

	if issue1 == nil {
		return fmt.Errorf("issue not found: %s", id1)
	}
	if issue2 == nil {
		return fmt.Errorf("issue not found: %s", id2)
	}

	// Remove relates-to dependency in both directions
	// Per Decision 004, relates-to links are now stored in dependencies table
	{
		_, err := daemonClient.RemoveBidirectionalRelation(&rpc.DepRemoveBidirectionalArgs{
			ID1:     id1,
			ID2:     id2,
			DepType: string(types.DepRelatesTo),
		})
		if err != nil {
			return fmt.Errorf("failed to remove relates-to %s <-> %s: %w", id1, id2, err)
		}
	}

	// Trigger auto-flush
	if flushManager != nil {
		flushManager.MarkDirty(false)
	}

	if jsonOutput {
		result := map[string]interface{}{
			"id1":       id1,
			"id2":       id2,
			"unrelated": true,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	fmt.Printf("%s Unlinked %s ↔ %s\n", ui.RenderPass("✓"), id1, id2)
	return nil
}

// Note: contains, remove, formatRelatesTo functions removed per Decision 004
// relates-to links now use dependencies API instead of Issue.RelatesTo field
