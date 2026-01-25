package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/hooks"
	"github.com/steveyegge/beads/internal/idgen"
	"github.com/steveyegge/beads/internal/notification"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// decisionCreateCmd creates a new decision point
var decisionCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new decision point",
	Long: `Create a decision point gate that blocks until a human responds.

The decision point is a gate issue (type=gate, await_type=decision) with associated
decision data stored in the decision_points table.

Options are specified as a JSON array of objects with id, short, label, and optional description:
  [{"id":"a","short":"Redis","label":"Use Redis for caching","description":"Full markdown..."}]

Examples:
  # Simple yes/no decision
  bd decision create --prompt="Proceed with migration?" \
    --options='[{"id":"yes","short":"Yes","label":"Yes, proceed"},{"id":"no","short":"No","label":"No, abort"}]'

  # Decision with default and timeout
  bd decision create --prompt="Which approach?" \
    --options='[{"id":"a","label":"Option A"},{"id":"b","label":"Option B"}]' \
    --default=a --timeout=24h

  # Decision that blocks another issue
  bd decision create --prompt="Approve design?" \
    --options='[{"id":"approve","label":"Approve"},{"id":"reject","label":"Reject"}]' \
    --blocks=gt-abc123.4

  # Decision with parent molecule
  bd decision create --prompt="Select strategy" --parent=gt-abc123 \
    --options='[{"id":"a","label":"Strategy A"}]'`,
	Run: runDecisionCreate,
}

func init() {
	decisionCreateCmd.Flags().StringP("prompt", "p", "", "The question to ask (required)")
	decisionCreateCmd.Flags().StringP("options", "o", "", "JSON array of options")
	decisionCreateCmd.Flags().StringP("default", "d", "", "Default option ID if timeout")
	decisionCreateCmd.Flags().Duration("timeout", 24*time.Hour, "Timeout duration (default 24h)")
	decisionCreateCmd.Flags().String("parent", "", "Parent issue (molecule)")
	decisionCreateCmd.Flags().String("blocks", "", "Issue ID this decision blocks")
	decisionCreateCmd.Flags().Int("max-iterations", 3, "Maximum refinement iterations")
	decisionCreateCmd.Flags().Bool("no-notify", false, "Don't send notifications (for testing)")
	decisionCreateCmd.Flags().String("requested-by", "", "Agent/session that requested this decision (for wake notifications)")

	_ = decisionCreateCmd.MarkFlagRequired("prompt")

	decisionCmd.AddCommand(decisionCreateCmd)
}

func runDecisionCreate(cmd *cobra.Command, args []string) {
	CheckReadonly("decision create")

	// Ensure store is initialized (handles daemon disconnect, direct mode, etc.)
	if err := ensureStoreActive(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	prompt, _ := cmd.Flags().GetString("prompt")
	optionsJSON, _ := cmd.Flags().GetString("options")
	defaultOption, _ := cmd.Flags().GetString("default")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	parent, _ := cmd.Flags().GetString("parent")
	blocks, _ := cmd.Flags().GetString("blocks")
	maxIterations, _ := cmd.Flags().GetInt("max-iterations")
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	requestedBy, _ := cmd.Flags().GetString("requested-by")

	ctx := rootCtx

	// Validate options JSON if provided
	var options []types.DecisionOption
	if optionsJSON != "" {
		if err := json.Unmarshal([]byte(optionsJSON), &options); err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid options JSON: %v\n", err)
			os.Exit(1)
		}

		// Validate each option has required fields
		for i, opt := range options {
			if opt.ID == "" {
				fmt.Fprintf(os.Stderr, "Error: option %d missing 'id' field\n", i)
				os.Exit(1)
			}
			if opt.Label == "" {
				fmt.Fprintf(os.Stderr, "Error: option %d missing 'label' field\n", i)
				os.Exit(1)
			}
			// Auto-fill short from ID if not provided
			if opt.Short == "" {
				options[i].Short = opt.ID
			}
		}

		// Validate default option exists
		if defaultOption != "" {
			found := false
			for _, opt := range options {
				if opt.ID == defaultOption {
					found = true
					break
				}
			}
			if !found {
				fmt.Fprintf(os.Stderr, "Error: default option '%s' not found in options\n", defaultOption)
				os.Exit(1)
			}
		}

		// Re-marshal with any fixes
		optionsBytes, _ := json.Marshal(options)
		optionsJSON = string(optionsBytes)
	}

	// Generate decision point ID
	decisionID, err := generateDecisionID(ctx, parent, prompt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating ID: %v\n", err)
		os.Exit(1)
	}

	// Create the gate issue
	now := time.Now()
	gateIssue := &types.Issue{
		ID:        decisionID, // May be empty - CreateIssue will generate
		Title:     truncateTitle(prompt, 100),
		IssueType: types.TypeGate,
		Status:    types.StatusOpen,
		Priority:  2,
		AwaitType: "decision",
		Timeout:   timeout,
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Create the decision point record (IssueID set after CreateIssue)
	decisionPoint := &types.DecisionPoint{
		Prompt:        prompt,
		Options:       optionsJSON,
		DefaultOption: defaultOption,
		Iteration:     1,
		MaxIterations: maxIterations,
		CreatedAt:     now,
		RequestedBy:   requestedBy,
	}

	// Use transaction to create both atomically
	err = store.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// Create the gate issue (generates ID if empty)
		if err := tx.CreateIssue(ctx, gateIssue, actor); err != nil {
			return fmt.Errorf("creating gate issue: %w", err)
		}

		// Now gateIssue.ID is populated (either provided or generated)
		decisionID = gateIssue.ID
		decisionPoint.IssueID = decisionID

		// Create the decision point record
		if err := tx.CreateDecisionPoint(ctx, decisionPoint); err != nil {
			return fmt.Errorf("creating decision point: %w", err)
		}

		// Add parent-child dependency if parent specified
		if parent != "" {
			dep := &types.Dependency{
				IssueID:     decisionID,
				DependsOnID: parent,
				Type:        types.DepParentChild,
				CreatedAt:   now,
			}
			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("adding parent dependency: %w", err)
			}
		}

		// Add blocks dependency if specified
		if blocks != "" {
			dep := &types.Dependency{
				IssueID:     blocks,
				DependsOnID: decisionID,
				Type:        types.DepBlocks,
				CreatedAt:   now,
			}
			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("adding blocks dependency: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	markDirtyAndScheduleFlush()

	// Trigger decision create hook (hq-e0adf6.4)
	// Use RunDecisionSync to ensure hook completes before program exits
	if hookRunner != nil {
		_ = hookRunner.RunDecisionSync(hooks.EventDecisionCreate, decisionPoint, nil, requestedBy)
	}

	// Output
	if jsonOutput {
		result := map[string]interface{}{
			"id":             decisionID,
			"prompt":         prompt,
			"options":        options,
			"default_option": defaultOption,
			"timeout":        timeout.String(),
			"parent":         parent,
			"blocks":         blocks,
		}
		outputJSON(result)
		return
	}

	// Human-readable output
	fmt.Printf("%s Created decision point: %s\n\n", ui.RenderPass("✓"), ui.RenderID(decisionID))
	fmt.Printf("  %s\n\n", prompt)

	if len(options) > 0 {
		for _, opt := range options {
			defaultMarker := ""
			if opt.ID == defaultOption {
				defaultMarker = " (default)"
			}
			fmt.Printf("  [%s] %s - %s%s\n", opt.ID, opt.Short, opt.Label, defaultMarker)
		}
		fmt.Println()
	}

	fmt.Println("  Or provide custom text response.")
	fmt.Println()

	fmt.Printf("  Timeout: %s\n", formatTimeout(timeout, now))
	if blocks != "" {
		fmt.Printf("  Blocks: %s\n", blocks)
	}
	if parent != "" {
		fmt.Printf("  Parent: %s\n", parent)
	}

	if noNotify {
		fmt.Println("\n  (Notifications skipped)")
	} else {
		// Dispatch notifications (hq-5d43fc)
		beadsDir := filepath.Dir(dbPath)
		results, err := notification.DispatchDecisionNotification(beadsDir, decisionPoint, gateIssue, "default")
		if err != nil {
			fmt.Fprintf(os.Stderr, "\n  Warning: notification dispatch failed: %v\n", err)
		} else if len(results) > 0 {
			fmt.Printf("\n  Notifications sent: %d\n", len(results))
			for _, r := range results {
				if r.Success {
					fmt.Printf("    ✓ %s\n", r.Channel)
				} else {
					fmt.Printf("    ✗ %s: %s\n", r.Channel, r.Error)
				}
			}
		} else {
			fmt.Println("\n  (No notification routes configured)")
		}
	}
}

// generateDecisionID creates an ID for the decision point
func generateDecisionID(ctx context.Context, parent, prompt string) (string, error) {
	if parent != "" {
		// Find next available decision suffix under parent
		// Format: parent.decision-N
		for i := 1; i <= 100; i++ {
			candidateID := fmt.Sprintf("%s.decision-%d", parent, i)
			issue, err := store.GetIssue(ctx, candidateID)
			if err != nil {
				return "", fmt.Errorf("checking issue existence: %w", err)
			}
			if issue == nil {
				// Issue doesn't exist, use this ID
				return candidateID, nil
			}
		}
		return "", fmt.Errorf("too many decisions under parent %s", parent)
	}

	// No parent - generate a root-level decision ID with collision avoidance
	prefix, err := store.GetConfig(ctx, "issue_prefix")
	if err != nil || prefix == "" {
		prefix = "hq-" // fallback default
	}
	now := time.Now()
	for nonce := 0; nonce < 100; nonce++ {
		candidateID := idgen.GenerateHashID(prefix, prompt, "", actor, now, 6, nonce)
		issue, err := store.GetIssue(ctx, candidateID)
		if err != nil {
			return "", fmt.Errorf("checking issue existence: %w", err)
		}
		if issue == nil {
			return candidateID, nil
		}
	}
	return "", fmt.Errorf("failed to generate unique decision ID after 100 attempts")
}


// formatTimeout formats the timeout duration relative to creation time
func formatTimeout(timeout time.Duration, created time.Time) string {
	expires := created.Add(timeout)
	return fmt.Sprintf("%s (%s)", expires.Format("2006-01-02 15:04 MST"), timeout)
}
