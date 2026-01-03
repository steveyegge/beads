package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// GHWorkflowRun represents a GitHub workflow run from `gh run list --json`
type GHWorkflowRun struct {
	DatabaseID     int64     `json:"databaseId"`
	DisplayTitle   string    `json:"displayTitle"`
	HeadBranch     string    `json:"headBranch"`
	HeadSha        string    `json:"headSha"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	Conclusion     string    `json:"conclusion,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	WorkflowName   string    `json:"workflowName"`
	URL            string    `json:"url"`
}

// gateDiscoverCmd discovers GitHub run IDs for gh:run gates
var gateDiscoverCmd = &cobra.Command{
	Use:   "discover",
	Short: "Discover await_id for gh:run gates",
	Long: `Discovers GitHub workflow run IDs for gates awaiting CI/CD completion.

This command finds open gates with await_type="gh:run" that don't have an await_id,
queries recent GitHub workflow runs, and matches them using heuristics:
  - Branch name matching
  - Commit SHA matching
  - Time proximity (runs within 5 minutes of gate creation)

Once matched, the gate's await_id is updated with the GitHub run ID, enabling
subsequent polling to check the run's status.

Examples:
  bd gate discover           # Auto-discover run IDs for all matching gates
  bd gate discover --dry-run # Preview what would be matched (no updates)
  bd gate discover --branch main --limit 10  # Only match runs on 'main' branch`,
	Run: runGateDiscover,
}

func init() {
	gateDiscoverCmd.Flags().BoolP("dry-run", "n", false, "Preview mode: show matches without updating")
	gateDiscoverCmd.Flags().StringP("branch", "b", "", "Filter runs by branch (default: current branch)")
	gateDiscoverCmd.Flags().IntP("limit", "l", 10, "Max runs to query from GitHub")
	gateDiscoverCmd.Flags().DurationP("max-age", "a", 30*time.Minute, "Max age for gate/run matching")

	gateCmd.AddCommand(gateDiscoverCmd)
}

func runGateDiscover(cmd *cobra.Command, args []string) {
	CheckReadonly("gate discover")

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	branchFilter, _ := cmd.Flags().GetString("branch")
	limit, _ := cmd.Flags().GetInt("limit")
	maxAge, _ := cmd.Flags().GetDuration("max-age")

	ctx := rootCtx

	// Step 1: Find open gh:run gates without await_id
	gates, err := findPendingGates()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error finding gates: %v\n", err)
		os.Exit(1)
	}

	if len(gates) == 0 {
		fmt.Println("No pending gh:run gates found (all gates have await_id set)")
		return
	}

	fmt.Printf("%s Found %d gate(s) awaiting run ID discovery\n\n", ui.RenderAccent("🔍"), len(gates))

	// Get current branch if not specified
	if branchFilter == "" {
		branchFilter = getGitBranchForGateDiscovery()
	}

	// Step 2: Query recent GitHub workflow runs
	runs, err := queryGitHubRuns(branchFilter, limit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying GitHub runs: %v\n", err)
		os.Exit(1)
	}

	if len(runs) == 0 {
		fmt.Println("No recent workflow runs found on GitHub")
		return
	}

	fmt.Printf("Found %d recent workflow run(s) on branch '%s'\n\n", len(runs), branchFilter)

	// Step 3: Match runs to gates
	matchCount := 0
	for _, gate := range gates {
		match := matchGateToRun(gate, runs, maxAge)
		if match == nil {
			if jsonOutput {
				continue
			}
			fmt.Printf("  %s %s - no matching run found\n",
				ui.RenderFail("✗"), ui.RenderID(gate.ID))
			continue
		}

		matchCount++
		runIDStr := strconv.FormatInt(match.DatabaseID, 10)

		if dryRun {
			fmt.Printf("  %s %s → run %s (%s) [dry-run]\n",
				ui.RenderPass("✓"), ui.RenderID(gate.ID), runIDStr, match.Status)
			continue
		}

		// Step 4: Update gate with discovered run ID
		if err := updateGateAwaitID(ctx, gate.ID, runIDStr); err != nil {
			fmt.Fprintf(os.Stderr, "  %s %s - update failed: %v\n",
				ui.RenderFail("✗"), ui.RenderID(gate.ID), err)
			continue
		}

		fmt.Printf("  %s %s → run %s (%s)\n",
			ui.RenderPass("✓"), ui.RenderID(gate.ID), runIDStr, match.Status)
	}

	fmt.Println()
	if dryRun {
		fmt.Printf("Would update %d gate(s). Run without --dry-run to apply.\n", matchCount)
	} else {
		fmt.Printf("Updated %d gate(s) with discovered run IDs.\n", matchCount)
	}
}

// findPendingGates returns open gh:run gates that have no await_id set
func findPendingGates() ([]*types.Issue, error) {
	var gates []*types.Issue

	if daemonClient != nil {
		listArgs := &rpc.ListArgs{
			IssueType: "gate",
			ExcludeStatus: []string{"closed"},
		}

		resp, err := daemonClient.List(listArgs)
		if err != nil {
			return nil, fmt.Errorf("list gates: %w", err)
		}

		var allGates []*types.Issue
		if err := json.Unmarshal(resp.Data, &allGates); err != nil {
			return nil, fmt.Errorf("parse gates: %w", err)
		}

		// Filter to gh:run gates without await_id
		for _, g := range allGates {
			if g.AwaitType == "gh:run" && g.AwaitID == "" {
				gates = append(gates, g)
			}
		}
	} else {
		// Direct mode
		gateType := types.TypeGate
		filter := types.IssueFilter{
			IssueType: &gateType,
			ExcludeStatus: []types.Status{types.StatusClosed},
		}

		allGates, err := store.SearchIssues(rootCtx, "", filter)
		if err != nil {
			return nil, fmt.Errorf("search gates: %w", err)
		}

		for _, g := range allGates {
			if g.AwaitType == "gh:run" && g.AwaitID == "" {
				gates = append(gates, g)
			}
		}
	}

	return gates, nil
}

// getGitBranchForGateDiscovery returns the current git branch name
func getGitBranchForGateDiscovery() string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "main" // Default fallback
	}
	return strings.TrimSpace(string(output))
}

// getGitCommitForGateDiscovery returns the current git commit SHA
func getGitCommitForGateDiscovery() string {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

// queryGitHubRuns queries recent workflow runs from GitHub using gh CLI
func queryGitHubRuns(branch string, limit int) ([]GHWorkflowRun, error) {
	// Check if gh CLI is available
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("gh CLI not found: install from https://cli.github.com")
	}

	// Build gh run list command with JSON output
	args := []string{
		"run", "list",
		"--json", "databaseId,displayTitle,headBranch,headSha,name,status,conclusion,createdAt,updatedAt,workflowName,url",
		"--limit", strconv.Itoa(limit),
	}

	if branch != "" {
		args = append(args, "--branch", branch)
	}

	cmd := exec.Command("gh", args...)
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gh run list failed: %s", string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gh run list: %w", err)
	}

	var runs []GHWorkflowRun
	if err := json.Unmarshal(output, &runs); err != nil {
		return nil, fmt.Errorf("parse gh output: %w", err)
	}

	return runs, nil
}

// matchGateToRun finds the best matching run for a gate using heuristics
func matchGateToRun(gate *types.Issue, runs []GHWorkflowRun, maxAge time.Duration) *GHWorkflowRun {
	now := time.Now()
	currentCommit := getGitCommitForGateDiscovery()
	currentBranch := getGitBranchForGateDiscovery()

	var bestMatch *GHWorkflowRun
	var bestScore int

	for i := range runs {
		run := &runs[i]
		score := 0

		// Skip runs that are too old
		if now.Sub(run.CreatedAt) > maxAge {
			continue
		}

		// Heuristic 1: Commit SHA match (strongest signal)
		if currentCommit != "" && run.HeadSha == currentCommit {
			score += 100
		}

		// Heuristic 2: Branch match
		if run.HeadBranch == currentBranch {
			score += 50
		}

		// Heuristic 3: Time proximity to gate creation
		// Closer in time = higher score
		timeDiff := run.CreatedAt.Sub(gate.CreatedAt).Abs()
		if timeDiff < 5*time.Minute {
			score += 30
		} else if timeDiff < 10*time.Minute {
			score += 20
		} else if timeDiff < 30*time.Minute {
			score += 10
		}

		// Heuristic 4: Workflow name match (if gate has workflow specified)
		// The gate's AwaitType might include workflow info in the future
		// For now, we prioritize any recent run

		// Heuristic 5: Prefer in_progress or queued runs (more likely to be current)
		if run.Status == "in_progress" || run.Status == "queued" {
			score += 5
		}

		if score > bestScore {
			bestScore = score
			bestMatch = run
		}
	}

	// Require at least some confidence in the match
	if bestScore >= 30 {
		return bestMatch
	}

	return nil
}

// updateGateAwaitID updates a gate's await_id field
func updateGateAwaitID(_ interface{}, gateID, runID string) error {
	if daemonClient != nil {
		updateArgs := &rpc.UpdateArgs{
			ID:      gateID,
			AwaitID: &runID,
		}

		resp, err := daemonClient.Update(updateArgs)
		if err != nil {
			return err
		}
		if !resp.Success {
			return fmt.Errorf("%s", resp.Error)
		}
	} else {
		updates := map[string]interface{}{
			"await_id": runID,
		}
		if err := store.UpdateIssue(rootCtx, gateID, updates, actor); err != nil {
			return err
		}
		markDirtyAndScheduleFlush()
	}

	return nil
}
