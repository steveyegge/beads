package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/ui"
)

var branchStrategy string

var branchCmd = &cobra.Command{
	Use:     "branch [name]",
	GroupID: "sync",
	Short:   "List or create branches (requires Dolt backend)",
	Long: `List all branches or create a new branch with a merge strategy.

This command requires the Dolt storage backend. Without arguments,
it lists all branches showing their merge strategies. With an argument,
it creates a new branch and registers it with a merge strategy.

Merge strategies:
  stay-on-main       — DB writes go directly to Dolt main (default)
  merge-with-branch  — DB stays on Dolt branch, merges when code merges
  merge-on-close     — DB stays on Dolt branch, merges on bd close

The default strategy can be configured with:
  bd config set branch_strategy.default_strategy <strategy>

Examples:
  bd branch                                          # List all branches with strategies
  bd branch feature-xyz                              # Create branch with default strategy
  bd branch feature-xyz --strategy merge-with-branch # Create with isolated strategy`,
	Args: cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx

		// If no args, list branches
		if len(args) == 0 {
			branches, err := store.ListBranches(ctx)
			if err != nil {
				FatalErrorRespectJSON("failed to list branches: %v", err)
			}

			currentBranch, err := store.CurrentBranch(ctx)
			if err != nil {
				currentBranch = ""
			}

			// Get registry info for strategy display
			registered, _ := store.ListRegisteredBranches(ctx, "")
			regMap := make(map[string]*dolt.BranchInfo, len(registered))
			for i := range registered {
				regMap[registered[i].Name] = &registered[i]
			}

			if jsonOutput {
				type branchJSON struct {
					Name     string `json:"name"`
					Current  bool   `json:"current"`
					Strategy string `json:"strategy,omitempty"`
					Status   string `json:"status,omitempty"`
				}
				var out []branchJSON
				for _, b := range branches {
					bj := branchJSON{Name: b, Current: b == currentBranch}
					if info, ok := regMap[b]; ok {
						bj.Strategy = info.MergeStrategy
						bj.Status = info.Status
					}
					out = append(out, bj)
				}
				outputJSON(map[string]interface{}{
					"current":  currentBranch,
					"branches": out,
				})
				return
			}

			fmt.Printf("\n%s Branches:\n\n", ui.RenderAccent("🌿"))
			for _, branch := range branches {
				marker := "   "
				nameStyle := branch
				if branch == currentBranch {
					marker = " * "
					nameStyle = ui.StatusInProgressStyle.Render(branch)
				}
				if info, ok := regMap[branch]; ok {
					fmt.Printf("%s%s  %s  [%s]\n", marker, nameStyle,
						ui.RenderMuted(info.MergeStrategy), ui.RenderMuted(info.Status))
				} else {
					fmt.Printf("%s%s\n", marker, nameStyle)
				}
			}
			fmt.Println()
			return
		}

		// Create new branch with strategy
		branchName := args[0]
		strategy := resolveStrategy(branchStrategy)

		if err := store.RegisterBranch(ctx, branchName, strategy); err != nil {
			FatalErrorRespectJSON("failed to create branch: %v", err)
		}

		commandDidWrite.Store(true)

		if jsonOutput {
			outputJSON(map[string]interface{}{
				"created":  branchName,
				"strategy": strategy,
			})
			return
		}

		fmt.Printf("Created branch %s with strategy %s\n",
			ui.RenderAccent(branchName), strategy)
	},
}

// validStrategies maps accepted input forms to canonical strategy slugs.
var validStrategies = map[string]string{
	"stay-on-main":      "stay-on-main",
	"merge-with-branch": "merge-with-branch",
	"merge-on-close":    "merge-on-close",
	// A/B/C shortcuts for interactive prompts
	"a": "stay-on-main",
	"b": "merge-with-branch",
	"c": "merge-on-close",
}

// resolveStrategy determines the merge strategy from flag, config, or default.
func resolveStrategy(flagValue string) string {
	if flagValue != "" {
		if slug, ok := validStrategies[strings.ToLower(flagValue)]; ok {
			return slug
		}
	}

	// Check branch_strategy.default_strategy config
	if store != nil {
		if cfgVal, err := store.GetConfig(rootCtx, "branch_strategy.default_strategy"); err == nil && cfgVal != "" {
			if slug, ok := validStrategies[strings.ToLower(cfgVal)]; ok {
				return slug
			}
		}
	}

	return "stay-on-main"
}

// strategyShortcut returns the A/B/C shortcut letter for a strategy slug.
func strategyShortcut(strategy string) string {
	switch strategy {
	case "stay-on-main":
		return "A"
	case "merge-with-branch":
		return "B"
	case "merge-on-close":
		return "C"
	default:
		return "?"
	}
}

func init() {
	branchCmd.Flags().StringVarP(&branchStrategy, "strategy", "s", "", "Merge strategy: stay-on-main, merge-with-branch, or merge-on-close (also accepts A/B/C)")
	rootCmd.AddCommand(branchCmd)
}
