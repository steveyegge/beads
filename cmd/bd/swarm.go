// Package main implements the bd CLI swarm management commands.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

var swarmCmd = &cobra.Command{
	Use:     "swarm",
	GroupID: "deps",
	Short:   "Swarm management for structured epics",
	Long: `Swarm management commands for coordinating parallel work on epics.

A swarm is a structured body of work defined by an epic and its children,
with dependencies forming a DAG (directed acyclic graph) of work.`,
}

// SwarmAnalysis holds the results of analyzing an epic's structure for swarming.
type SwarmAnalysis struct {
	EpicID          string                  `json:"epic_id"`
	EpicTitle       string                  `json:"epic_title"`
	TotalIssues     int                     `json:"total_issues"`
	ClosedIssues    int                     `json:"closed_issues"`
	ReadyFronts     []ReadyFront            `json:"ready_fronts"`
	MaxParallelism  int                     `json:"max_parallelism"`
	EstimatedSessions int                   `json:"estimated_sessions"`
	Warnings        []string                `json:"warnings"`
	Errors          []string                `json:"errors"`
	Swarmable       bool                    `json:"swarmable"`
	Issues          map[string]*IssueNode   `json:"issues,omitempty"` // Only included with --verbose
}

// ReadyFront represents a group of issues that can be worked on in parallel.
type ReadyFront struct {
	Wave    int      `json:"wave"`
	Issues  []string `json:"issues"`
	Titles  []string `json:"titles,omitempty"` // Only for human output
}

// IssueNode represents an issue in the dependency graph.
type IssueNode struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Priority     int      `json:"priority"`
	DependsOn    []string `json:"depends_on"`     // What this issue depends on
	DependedOnBy []string `json:"depended_on_by"` // What depends on this issue
	Wave         int      `json:"wave"`           // Which ready front this belongs to (-1 if blocked by cycle)
}

var swarmValidateCmd = &cobra.Command{
	Use:   "validate [epic-id]",
	Short: "Validate epic structure for swarming",
	Long: `Validate an epic's structure to ensure it's ready for swarm execution.

Checks for:
- Correct dependency direction (requirement-based, not temporal)
- Orphaned issues (roots with no dependents)
- Missing dependencies (leaves that should depend on something)
- Cycles (impossible to resolve)
- Disconnected subgraphs

Reports:
- Ready fronts (waves of parallel work)
- Estimated worker-sessions
- Maximum parallelism
- Warnings for potential issues

Examples:
  bd swarm validate gt-epic-123           # Validate epic structure
  bd swarm validate gt-epic-123 --verbose # Include detailed issue graph`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := rootCtx
		verbose, _ := cmd.Flags().GetBool("verbose")

		// Swarm commands require direct store access
		if store == nil {
			if daemonClient != nil {
				var err error
				store, err = sqlite.New(ctx, dbPath)
				if err != nil {
					FatalErrorRespectJSON("failed to open database: %v", err)
				}
				defer func() { _ = store.Close() }()
			} else {
				FatalErrorRespectJSON("no database connection")
			}
		}

		// Resolve epic ID
		epicID, err := utils.ResolvePartialID(ctx, store, args[0])
		if err != nil {
			FatalErrorRespectJSON("epic '%s' not found: %v", args[0], err)
		}

		// Get the epic
		epic, err := store.GetIssue(ctx, epicID)
		if err != nil {
			FatalErrorRespectJSON("failed to get epic: %v", err)
		}
		if epic == nil {
			FatalErrorRespectJSON("epic '%s' not found", epicID)
		}

		// Verify it's an epic
		if epic.IssueType != types.TypeEpic && epic.IssueType != types.TypeMolecule {
			FatalErrorRespectJSON("'%s' is not an epic or molecule (type: %s)", epicID, epic.IssueType)
		}

		// Analyze the epic structure
		analysis, err := analyzeEpicForSwarm(ctx, store, epic)
		if err != nil {
			FatalErrorRespectJSON("failed to analyze epic: %v", err)
		}

		// Include detailed graph only in verbose mode
		if !verbose {
			analysis.Issues = nil
		}

		if jsonOutput {
			outputJSON(analysis)
			if !analysis.Swarmable {
				os.Exit(1)
			}
			return
		}

		// Human-readable output
		renderSwarmAnalysis(analysis)

		if !analysis.Swarmable {
			os.Exit(1)
		}
	},
}

// analyzeEpicForSwarm performs structural analysis of an epic for swarm execution.
func analyzeEpicForSwarm(ctx context.Context, s interface{
	GetIssue(context.Context, string) (*types.Issue, error)
	GetDependents(context.Context, string) ([]*types.Issue, error)
	GetDependencyRecords(context.Context, string) ([]*types.Dependency, error)
}, epic *types.Issue) (*SwarmAnalysis, error) {
	analysis := &SwarmAnalysis{
		EpicID:    epic.ID,
		EpicTitle: epic.Title,
		Swarmable: true,
		Issues:    make(map[string]*IssueNode),
	}

	// Get all issues that depend on the epic
	allDependents, err := s.GetDependents(ctx, epic.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to get epic dependents: %w", err)
	}

	// Filter to only parent-child relationships by checking each dependent's dependency records
	var childIssues []*types.Issue
	for _, dependent := range allDependents {
		deps, err := s.GetDependencyRecords(ctx, dependent.ID)
		if err != nil {
			continue // Skip issues we can't query
		}
		for _, dep := range deps {
			if dep.DependsOnID == epic.ID && dep.Type == types.DepParentChild {
				childIssues = append(childIssues, dependent)
				break
			}
		}
	}

	if len(childIssues) == 0 {
		analysis.Warnings = append(analysis.Warnings, "Epic has no children")
		return analysis, nil
	}

	analysis.TotalIssues = len(childIssues)

	// Build the issue graph
	for _, issue := range childIssues {
		node := &IssueNode{
			ID:           issue.ID,
			Title:        issue.Title,
			Status:       string(issue.Status),
			Priority:     issue.Priority,
			DependsOn:    []string{},
			DependedOnBy: []string{},
			Wave:         -1, // Will be set later
		}
		analysis.Issues[issue.ID] = node

		if issue.Status == types.StatusClosed {
			analysis.ClosedIssues++
		}
	}

	// Build dependency relationships (only within the epic's children)
	childIDSet := make(map[string]bool)
	for _, issue := range childIssues {
		childIDSet[issue.ID] = true
	}

	for _, issue := range childIssues {
		deps, err := s.GetDependencyRecords(ctx, issue.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to get dependencies for %s: %w", issue.ID, err)
		}

		node := analysis.Issues[issue.ID]
		for _, dep := range deps {
			// Only consider dependencies within the epic (not parent-child to epic itself)
			if dep.DependsOnID == epic.ID && dep.Type == types.DepParentChild {
				continue // Skip the parent relationship to the epic
			}
			// Only track blocking dependencies
			if !dep.Type.AffectsReadyWork() {
				continue
			}
			// Only track dependencies within the epic's children
			if childIDSet[dep.DependsOnID] {
				node.DependsOn = append(node.DependsOn, dep.DependsOnID)
				if targetNode, ok := analysis.Issues[dep.DependsOnID]; ok {
					targetNode.DependedOnBy = append(targetNode.DependedOnBy, issue.ID)
				}
			}
			// External dependencies to issues outside the epic
			if !childIDSet[dep.DependsOnID] && dep.DependsOnID != epic.ID {
				// Check if it's an external ref
				if strings.HasPrefix(dep.DependsOnID, "external:") {
					analysis.Warnings = append(analysis.Warnings,
						fmt.Sprintf("%s has external dependency: %s", issue.ID, dep.DependsOnID))
				} else {
					analysis.Warnings = append(analysis.Warnings,
						fmt.Sprintf("%s depends on %s (outside epic)", issue.ID, dep.DependsOnID))
				}
			}
		}
	}

	// Detect structural issues
	detectStructuralIssues(analysis, childIssues)

	// Compute ready fronts (waves of parallel work)
	computeReadyFronts(analysis)

	// Set swarmable based on errors
	analysis.Swarmable = len(analysis.Errors) == 0

	return analysis, nil
}

// detectStructuralIssues looks for common problems in the dependency graph.
func detectStructuralIssues(analysis *SwarmAnalysis, issues []*types.Issue) {
	// 1. Find roots (issues with no dependencies within the epic)
	//    These are the starting points. Having multiple roots is normal.
	var roots []string
	for id, node := range analysis.Issues {
		if len(node.DependsOn) == 0 {
			roots = append(roots, id)
		}
	}

	// 2. Find leaves (issues that nothing depends on within the epic)
	//    Multiple leaves might indicate missing dependencies or just multiple end points.
	var leaves []string
	for id, node := range analysis.Issues {
		if len(node.DependedOnBy) == 0 {
			leaves = append(leaves, id)
		}
	}

	// 3. Detect potential dependency inversions
	//    Heuristic: If a "foundation" or "setup" issue has no dependents, it might be inverted.
	//    Heuristic: If an "integration" or "final" issue depends on nothing, it might be inverted.
	for id, node := range analysis.Issues {
		lowerTitle := strings.ToLower(node.Title)

		// Foundation-like issues should have dependents
		if len(node.DependedOnBy) == 0 {
			if strings.Contains(lowerTitle, "foundation") ||
				strings.Contains(lowerTitle, "setup") ||
				strings.Contains(lowerTitle, "base") ||
				strings.Contains(lowerTitle, "core") {
				analysis.Warnings = append(analysis.Warnings,
					fmt.Sprintf("%s (%s) has no dependents - should other issues depend on it?",
						id, node.Title))
			}
		}

		// Integration-like issues should have dependencies
		if len(node.DependsOn) == 0 {
			if strings.Contains(lowerTitle, "integration") ||
				strings.Contains(lowerTitle, "final") ||
				strings.Contains(lowerTitle, "test") {
				analysis.Warnings = append(analysis.Warnings,
					fmt.Sprintf("%s (%s) has no dependencies - should it depend on implementation?",
						id, node.Title))
			}
		}
	}

	// 4. Check for disconnected subgraphs
	// Start from roots and see if we can reach all nodes
	visited := make(map[string]bool)
	var dfs func(id string)
	dfs = func(id string) {
		if visited[id] {
			return
		}
		visited[id] = true
		if node, ok := analysis.Issues[id]; ok {
			for _, depID := range node.DependedOnBy {
				dfs(depID)
			}
		}
	}

	// Visit from all roots
	for _, root := range roots {
		dfs(root)
	}

	// Check for unvisited nodes (disconnected from roots)
	var disconnected []string
	for id := range analysis.Issues {
		if !visited[id] {
			disconnected = append(disconnected, id)
		}
	}

	if len(disconnected) > 0 {
		analysis.Warnings = append(analysis.Warnings,
			fmt.Sprintf("Disconnected issues (not reachable from roots): %v", disconnected))
	}

	// 5. Detect cycles using simple DFS
	// (The main DetectCycles in storage is more sophisticated, but we do a simple check here)
	inProgress := make(map[string]bool)
	completed := make(map[string]bool)
	var cyclePath []string
	hasCycle := false

	var detectCycle func(id string) bool
	detectCycle = func(id string) bool {
		if completed[id] {
			return false
		}
		if inProgress[id] {
			hasCycle = true
			return true
		}
		inProgress[id] = true
		cyclePath = append(cyclePath, id)

		if node, ok := analysis.Issues[id]; ok {
			for _, depID := range node.DependsOn {
				if detectCycle(depID) {
					return true
				}
			}
		}

		cyclePath = cyclePath[:len(cyclePath)-1]
		inProgress[id] = false
		completed[id] = true
		return false
	}

	for id := range analysis.Issues {
		if !completed[id] {
			if detectCycle(id) {
				break
			}
		}
	}

	if hasCycle {
		analysis.Errors = append(analysis.Errors,
			fmt.Sprintf("Dependency cycle detected involving: %v", cyclePath))
	}
}

// computeReadyFronts calculates the waves of parallel work.
func computeReadyFronts(analysis *SwarmAnalysis) {
	if len(analysis.Errors) > 0 {
		// Can't compute ready fronts if there are cycles
		return
	}

	// Use Kahn's algorithm for topological sort with level tracking
	inDegree := make(map[string]int)
	for id, node := range analysis.Issues {
		inDegree[id] = len(node.DependsOn)
	}

	// Start with all nodes that have no dependencies (wave 0)
	var currentWave []string
	for id, degree := range inDegree {
		if degree == 0 {
			currentWave = append(currentWave, id)
			analysis.Issues[id].Wave = 0
		}
	}

	wave := 0
	for len(currentWave) > 0 {
		// Sort for deterministic output
		sort.Strings(currentWave)

		// Build titles for this wave
		var titles []string
		for _, id := range currentWave {
			if node, ok := analysis.Issues[id]; ok {
				titles = append(titles, node.Title)
			}
		}

		front := ReadyFront{
			Wave:   wave,
			Issues: currentWave,
			Titles: titles,
		}
		analysis.ReadyFronts = append(analysis.ReadyFronts, front)

		// Track max parallelism
		if len(currentWave) > analysis.MaxParallelism {
			analysis.MaxParallelism = len(currentWave)
		}

		// Find next wave
		var nextWave []string
		for _, id := range currentWave {
			if node, ok := analysis.Issues[id]; ok {
				for _, dependentID := range node.DependedOnBy {
					inDegree[dependentID]--
					if inDegree[dependentID] == 0 {
						nextWave = append(nextWave, dependentID)
						analysis.Issues[dependentID].Wave = wave + 1
					}
				}
			}
		}

		currentWave = nextWave
		wave++
	}

	// Estimated sessions = total issues (each issue is roughly one session)
	analysis.EstimatedSessions = analysis.TotalIssues
}

// renderSwarmAnalysis outputs human-readable analysis.
func renderSwarmAnalysis(analysis *SwarmAnalysis) {
	fmt.Printf("\n%s Swarm Analysis: %s\n", ui.RenderAccent("🐝"), analysis.EpicTitle)
	fmt.Printf("   Epic ID: %s\n", analysis.EpicID)
	fmt.Printf("   Total issues: %d (%d closed)\n", analysis.TotalIssues, analysis.ClosedIssues)

	if analysis.TotalIssues == 0 {
		fmt.Printf("\n%s Epic has no children to swarm\n\n", ui.RenderWarn("⚠"))
		return
	}

	// Ready fronts
	if len(analysis.ReadyFronts) > 0 {
		fmt.Printf("\n%s Ready Fronts (waves of parallel work):\n", ui.RenderPass("📊"))
		for _, front := range analysis.ReadyFronts {
			fmt.Printf("   Wave %d: %d issues\n", front.Wave+1, len(front.Issues))
			for i, id := range front.Issues {
				title := ""
				if i < len(front.Titles) {
					title = front.Titles[i]
				}
				fmt.Printf("      • %s: %s\n", ui.RenderID(id), title)
			}
		}
	}

	// Summary stats
	fmt.Printf("\n%s Summary:\n", ui.RenderAccent("📈"))
	fmt.Printf("   Estimated worker-sessions: %d\n", analysis.EstimatedSessions)
	fmt.Printf("   Max parallelism: %d\n", analysis.MaxParallelism)
	fmt.Printf("   Total waves: %d\n", len(analysis.ReadyFronts))

	// Warnings
	if len(analysis.Warnings) > 0 {
		fmt.Printf("\n%s Warnings:\n", ui.RenderWarn("⚠"))
		for _, warning := range analysis.Warnings {
			fmt.Printf("   • %s\n", warning)
		}
	}

	// Errors
	if len(analysis.Errors) > 0 {
		fmt.Printf("\n%s Errors:\n", ui.RenderFail("❌"))
		for _, err := range analysis.Errors {
			fmt.Printf("   • %s\n", err)
		}
	}

	// Final verdict
	fmt.Println()
	if analysis.Swarmable {
		fmt.Printf("%s Swarmable: YES\n\n", ui.RenderPass("✓"))
	} else {
		fmt.Printf("%s Swarmable: NO (fix errors first)\n\n", ui.RenderFail("✗"))
	}
}

func init() {
	swarmValidateCmd.Flags().Bool("verbose", false, "Include detailed issue graph in output")

	swarmCmd.AddCommand(swarmValidateCmd)
	rootCmd.AddCommand(swarmCmd)
}
