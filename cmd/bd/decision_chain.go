package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

// decisionChainCmd shows the chain of decisions leading to this point
var decisionChainCmd = &cobra.Command{
	Use:   "chain <decision-id>",
	Short: "Show the chain of predecessor decisions",
	Long: `Walk the predecessor links of a decision and display the full chain.

This command traces back through the predecessor chain to show how a series
of decisions led to the current state. Useful for:
  - Understanding decision history
  - Auditing how a conclusion was reached
  - Debugging decision workflows

Examples:
  bd decision chain gt-abc.decision-3
  bd decision chain gt-abc.decision-3 --json`,
	Args: cobra.ExactArgs(1),
	Run:  runDecisionChain,
}

func init() {
	decisionCmd.AddCommand(decisionChainCmd)
}

// chainNode represents a decision in the chain
type chainNode struct {
	ID         string `json:"id"`
	Prompt     string `json:"prompt"`
	Selected   string `json:"selected_option,omitempty"`
	Response   string `json:"response_text,omitempty"`
	RespondedBy string `json:"responded_by,omitempty"`
	Depth      int    `json:"depth"`
}

func runDecisionChain(cmd *cobra.Command, args []string) {
	// Ensure store is initialized
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	decisionID := args[0]
	ctx := rootCtx

	// Resolve partial ID
	resolvedID, err := utils.ResolvePartialID(ctx, store, decisionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Build chain by walking predecessor links
	var chain []chainNode
	currentID := resolvedID
	depth := 0
	visited := make(map[string]bool)

	for currentID != "" {
		// Detect cycles
		if visited[currentID] {
			fmt.Fprintf(os.Stderr, "Warning: cycle detected at %s\n", currentID)
			break
		}
		visited[currentID] = true

		// Get the decision point
		dp, err := store.GetDecisionPoint(ctx, currentID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error getting decision point %s: %v\n", currentID, err)
			os.Exit(1)
		}
		if dp == nil {
			// Not a decision point - might be referenced but not exist
			if depth == 0 {
				fmt.Fprintf(os.Stderr, "Error: %s is not a decision point\n", currentID)
				os.Exit(1)
			}
			break
		}

		node := chainNode{
			ID:          currentID,
			Prompt:      dp.Prompt,
			Selected:    dp.SelectedOption,
			Response:    dp.ResponseText,
			RespondedBy: dp.RespondedBy,
			Depth:       depth,
		}
		chain = append(chain, node)

		// Move to predecessor
		currentID = dp.PriorID
		depth++

		// Safety limit
		if depth > 100 {
			fmt.Fprintf(os.Stderr, "Warning: chain exceeds 100 nodes, stopping\n")
			break
		}
	}

	// Reverse chain so root is first
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
		chain[i].Depth = i
		chain[j].Depth = j
	}

	// JSON output
	if jsonOutput {
		outputJSON(chain)
		return
	}

	// Human-readable output
	if len(chain) == 0 {
		fmt.Println("No decision chain found")
		return
	}

	if len(chain) == 1 {
		fmt.Printf("Decision %s has no predecessors (root decision)\n", ui.RenderID(resolvedID))
		return
	}

	fmt.Printf("Decision chain (%d decisions):\n\n", len(chain))

	for i, node := range chain {
		// Determine status
		status := "pending"
		if node.Selected != "" || node.Response != "" {
			status = "responded"
		}

		// Tree-like visualization
		prefix := strings.Repeat("  ", i)
		connector := ""
		if i > 0 {
			connector = "└─ "
		}

		fmt.Printf("%s%s%s [%s]\n", prefix, connector, ui.RenderID(node.ID), status)

		// Show prompt (truncated)
		prompt := node.Prompt
		if len(prompt) > 60 {
			prompt = prompt[:57] + "..."
		}
		fmt.Printf("%s   %s\n", prefix, prompt)

		// Show response if any
		if node.Selected != "" {
			fmt.Printf("%s   → Selected: %s\n", prefix, node.Selected)
		} else if node.Response != "" {
			response := node.Response
			if len(response) > 50 {
				response = response[:47] + "..."
			}
			fmt.Printf("%s   → Response: %s\n", prefix, response)
		}

		if i < len(chain)-1 {
			fmt.Println()
		}
	}
}
