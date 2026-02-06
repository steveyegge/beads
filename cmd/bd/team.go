package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

var teamRigFilter string

var teamCmd = &cobra.Command{
	Use:   "team",
	Short: "Team coordination and orchestration",
	Long:  `Coordinate multi-agent work using dependency waves, live dashboards, and drift detection.`,
}

var teamInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Set up team workflow (sync branch, auto-sync)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx := rootCtx
		return runTeamWizard(ctx, store)
	},
}

var teamPlanCmd = &cobra.Command{
	Use:   "plan <epic-id>",
	Short: "Dependency wave analysis for an epic",
	Long: `Analyze an epic's dependents and sort them into parallel execution waves
using topological ordering (Kahn's algorithm).

Wave 1 contains all unblocked tasks. Wave 2 contains tasks blocked only by
Wave 1 tasks, and so on. Tasks within the same wave can run in parallel.

Examples:
  bd team plan bd-42              # Plan waves for epic bd-42
  bd team plan bd-42 --json       # Output as JSON
  bd team plan bd-42 --rig myrig  # Filter to agents in rig "myrig"`,
	Args: cobra.ExactArgs(1),
	RunE: runTeamPlan,
}

var teamWatchCmd = &cobra.Command{
	Use:   "watch",
	Short: "Live agent dashboard",
	Long: `Display a dashboard of all agent beads showing their current state,
active slot, and time since last activity.

Use --rig to filter agents by rig membership.

Examples:
  bd team watch              # Show all agents
  bd team watch --rig myrig  # Show agents in rig "myrig"
  bd team watch --json       # Output as JSON`,
	RunE: runTeamWatch,
}

var teamScoreCmd = &cobra.Command{
	Use:   "score",
	Short: "Pacman leaderboard for agents",
	Long: `Display a leaderboard of all agents ranked by closed issues.

Shows closed, in-progress, and open issue counts per agent.
Use --rig to filter agents by rig membership.

Examples:
  bd team score              # Show all agents
  bd team score --rig myrig  # Show agents in rig "myrig"
  bd team score --json       # Output as JSON`,
	RunE: runTeamScore,
}

func init() {
	teamPlanCmd.Flags().StringVar(&teamRigFilter, "rig", "", "Filter agents by rig name")
	teamWatchCmd.Flags().StringVar(&teamRigFilter, "rig", "", "Filter agents by rig name")
	teamScoreCmd.Flags().StringVar(&teamRigFilter, "rig", "", "Filter agents by rig name")

	teamCmd.AddCommand(teamInitCmd)
	teamCmd.AddCommand(teamPlanCmd)
	teamCmd.AddCommand(teamWatchCmd)
	teamCmd.AddCommand(teamScoreCmd)
	rootCmd.AddCommand(teamCmd)
}

// relativeTime formats a time as a human-readable relative duration.
func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	default:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
}

// getAgentBeads retrieves all agent beads, optionally filtered by rig.
func getAgentBeads() ([]*types.Issue, error) {
	ctx := rootCtx

	var agents []*types.Issue
	if daemonClient != nil {
		resp, err := daemonClient.List(&rpc.ListArgs{
			Labels: []string{"gt:agent"},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list agents: %w", err)
		}
		if err := json.Unmarshal(resp.Data, &agents); err != nil {
			return nil, fmt.Errorf("parsing response: %w", err)
		}
	} else {
		filter := types.IssueFilter{
			Labels: []string{"gt:agent"},
		}
		var err error
		agents, err = store.SearchIssues(ctx, "", filter)
		if err != nil {
			return nil, fmt.Errorf("failed to list agents: %w", err)
		}
	}

	// Filter out tombstoned agents
	var filtered []*types.Issue
	for _, a := range agents {
		if a.Status == types.StatusTombstone {
			continue
		}
		filtered = append(filtered, a)
	}

	// Apply rig filter if specified
	if teamRigFilter != "" {
		var rigFiltered []*types.Issue
		for _, a := range filtered {
			if a.Rig == teamRigFilter {
				rigFiltered = append(rigFiltered, a)
				continue
			}
			// Also check labels for rig:<name> in case Rig field is empty
			var labels []string
			if daemonClient != nil {
				resp, err := daemonClient.Show(&rpc.ShowArgs{ID: a.ID})
				if err == nil {
					var full types.Issue
					if err := json.Unmarshal(resp.Data, &full); err == nil {
						labels = full.Labels
					}
				}
			} else {
				labels, _ = store.GetLabels(ctx, a.ID)
			}
			rigLabel := "rig:" + teamRigFilter
			for _, l := range labels {
				if l == rigLabel {
					rigFiltered = append(rigFiltered, a)
					break
				}
			}
		}
		filtered = rigFiltered
	}

	return filtered, nil
}

// planWaves performs topological sorting of an epic's dependents into parallel execution waves.
// Uses direct store access for dependency queries (no RPC equivalent exists).
func planWaves(epicID string) ([][]types.Issue, error) {
	ctx := rootCtx

	// 1. Get the epic bead to verify it exists
	var epic *types.Issue
	if daemonClient != nil {
		resp, err := daemonClient.Show(&rpc.ShowArgs{ID: epicID})
		if err != nil {
			return nil, fmt.Errorf("epic not found: %s", epicID)
		}
		if err := json.Unmarshal(resp.Data, &epic); err != nil {
			return nil, fmt.Errorf("parsing epic: %w", err)
		}
	} else {
		var err error
		epic, err = store.GetIssue(ctx, epicID)
		if err != nil || epic == nil {
			return nil, fmt.Errorf("epic not found: %s", epicID)
		}
	}
	_ = epic // verified existence

	// 2. Get all dependents (issues that depend on this epic).
	// No RPC method for GetDependents -- use store directly (same pattern as dep.go).
	dependents, err := store.GetDependents(ctx, epicID)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependents: %w", err)
	}

	if len(dependents) == 0 {
		return nil, nil
	}

	// Filter to open/in-progress only (skip closed/tombstoned)
	var openDeps []*types.Issue
	for _, d := range dependents {
		if d.Status == types.StatusClosed || d.Status == types.StatusTombstone {
			continue
		}
		openDeps = append(openDeps, d)
	}

	if len(openDeps) == 0 {
		return nil, nil
	}

	// Build a set of dependent IDs for fast lookup
	depSet := make(map[string]bool, len(openDeps))
	depMap := make(map[string]*types.Issue, len(openDeps))
	for _, d := range openDeps {
		depSet[d.ID] = true
		depMap[d.ID] = d
	}

	// 3. Build adjacency: for each issue, find its dependencies within the dependent set.
	// inDegree tracks how many within-set blockers each issue has.
	inDegree := make(map[string]int, len(openDeps))
	// blockedBy maps each issue to its within-set blockers (for display)
	blockedBy := make(map[string][]string, len(openDeps))

	for _, d := range openDeps {
		inDegree[d.ID] = 0 // Initialize
	}

	for _, d := range openDeps {
		// Get this issue's dependencies (things it depends on) -- direct store access
		deps, _ := store.GetDependencies(ctx, d.ID)

		for _, dep := range deps {
			// Only count dependencies within the dependent set
			if depSet[dep.ID] {
				inDegree[d.ID]++
				blockedBy[d.ID] = append(blockedBy[d.ID], dep.ID)
			}
		}
	}

	// 4. Kahn's algorithm: assign waves
	var waves [][]types.Issue
	remaining := make(map[string]bool)
	for id := range depMap {
		remaining[id] = true
	}

	for len(remaining) > 0 {
		// Find all issues with in-degree 0 (unblocked in this round)
		var wave []types.Issue
		var waveIDs []string
		for id := range remaining {
			if inDegree[id] == 0 {
				wave = append(wave, *depMap[id])
				waveIDs = append(waveIDs, id)
			}
		}

		if len(wave) == 0 {
			// Remaining issues form a cycle -- put them all in the last wave
			for id := range remaining {
				wave = append(wave, *depMap[id])
			}
			waves = append(waves, wave)
			break
		}

		waves = append(waves, wave)

		// Remove wave items and decrement in-degrees
		for _, id := range waveIDs {
			delete(remaining, id)
			// Decrement in-degree for all issues that depended on this one
			for depID := range remaining {
				for _, blocker := range blockedBy[depID] {
					if blocker == id {
						inDegree[depID]--
					}
				}
			}
		}
	}

	return waves, nil
}

func runTeamPlan(cmd *cobra.Command, args []string) error {
	epicID := args[0]

	waves, err := planWaves(epicID)
	if err != nil {
		return err
	}

	if waves == nil {
		if jsonOutput {
			result := map[string]interface{}{
				"epic_id": epicID,
				"waves":   []interface{}{},
				"summary": map[string]interface{}{
					"wave_count": 0,
					"task_count": 0,
				},
			}
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		fmt.Printf("No open dependents found for %s\n", epicID)
		return nil
	}

	// Count total tasks
	totalTasks := 0
	for _, w := range waves {
		totalTasks += len(w)
	}

	if jsonOutput {
		type jsonTask struct {
			ID        string   `json:"id"`
			Title     string   `json:"title"`
			Status    string   `json:"status"`
			Assignee  string   `json:"assignee,omitempty"`
			BlockedBy []string `json:"blocked_by,omitempty"`
		}
		type jsonWave struct {
			Wave  int        `json:"wave"`
			Tasks []jsonTask `json:"tasks"`
		}

		var jWaves []jsonWave
		for i, w := range waves {
			jw := jsonWave{Wave: i + 1}
			for _, t := range w {
				jt := jsonTask{
					ID:     t.ID,
					Title:  t.Title,
					Status: string(t.Status),
				}
				if t.Assignee != "" {
					jt.Assignee = t.Assignee
				}
				jw.Tasks = append(jw.Tasks, jt)
			}
			jWaves = append(jWaves, jw)
		}

		result := map[string]interface{}{
			"epic_id": epicID,
			"waves":   jWaves,
			"summary": map[string]interface{}{
				"wave_count": len(waves),
				"task_count": totalTasks,
			},
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	// Human-readable output
	for i, w := range waves {
		if i == 0 {
			fmt.Printf("Wave %d (parallel):\n", i+1)
		} else {
			fmt.Printf("\nWave %d (after wave %d):\n", i+1, i)
		}
		for _, t := range w {
			status := ""
			if t.Assignee != "" {
				status = fmt.Sprintf(" [%s]", t.Assignee)
			}
			fmt.Printf("  [%s] %s%s\n", t.ID, t.Title, status)
		}
	}

	fmt.Printf("\nSummary: %d waves, %d tasks\n", len(waves), totalTasks)
	return nil
}

func runTeamWatch(cmd *cobra.Command, args []string) error {
	agents, err := getAgentBeads()
	if err != nil {
		return err
	}

	if len(agents) == 0 {
		if jsonOutput {
			result := map[string]interface{}{
				"agents": []interface{}{},
			}
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		msg := "No agent beads found"
		if teamRigFilter != "" {
			msg += fmt.Sprintf(" for rig %q", teamRigFilter)
		}
		fmt.Println(msg)
		fmt.Println("\nHint: create agents with 'bd agent state <name> idle'")
		return nil
	}

	if jsonOutput {
		type jsonAgent struct {
			ID           string  `json:"id"`
			State        string  `json:"state"`
			Slot         string  `json:"slot"`
			LastActivity *string `json:"last_activity"`
			Rig          string  `json:"rig,omitempty"`
		}

		var jAgents []jsonAgent
		for _, a := range agents {
			ja := jsonAgent{
				ID:    a.ID,
				State: string(a.AgentState),
				Slot:  a.HookBead,
				Rig:   a.Rig,
			}
			if a.LastActivity != nil {
				t := a.LastActivity.Format(time.RFC3339)
				ja.LastActivity = &t
			}
			jAgents = append(jAgents, ja)
		}

		result := map[string]interface{}{
			"agents": jAgents,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	// Human-readable table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
		ui.RenderBold("AGENT"),
		ui.RenderBold("STATE"),
		ui.RenderBold("SLOT"),
		ui.RenderBold("LAST ACTIVE"),
	)

	for _, a := range agents {
		state := string(a.AgentState)
		if state == "" {
			state = "(unknown)"
		}

		slot := a.HookBead
		if slot == "" {
			slot = "(none)"
		}

		lastActive := "(never)"
		if a.LastActivity != nil {
			lastActive = relativeTime(*a.LastActivity)
		}

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", a.ID, state, slot, lastActive)
	}
	w.Flush()

	return nil
}

func runTeamScore(cmd *cobra.Command, args []string) error {
	agents, err := getAgentBeads()
	if err != nil {
		return err
	}

	if len(agents) == 0 {
		if jsonOutput {
			result := map[string]interface{}{
				"scores": []interface{}{},
			}
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(result)
		}
		msg := "No agent beads found"
		if teamRigFilter != "" {
			msg += fmt.Sprintf(" for rig %q", teamRigFilter)
		}
		fmt.Println(msg)
		return nil
	}

	ctx := rootCtx

	type agentScore struct {
		ID         string
		Closed     int
		InProgress int
		Open       int
	}

	var scores []agentScore

	for _, a := range agents {
		score := agentScore{ID: a.ID}

		// Search for issues assigned to this agent
		assignee := a.ID
		filter := types.IssueFilter{
			Assignee: &assignee,
		}

		var issues []*types.Issue
		if daemonClient != nil {
			resp, err := daemonClient.List(&rpc.ListArgs{
				Assignee: assignee,
			})
			if err == nil {
				_ = json.Unmarshal(resp.Data, &issues)
			}
		} else {
			issues, _ = store.SearchIssues(ctx, "", filter)
		}

		for _, issue := range issues {
			switch issue.Status {
			case types.StatusClosed:
				score.Closed++
			case types.StatusInProgress:
				score.InProgress++
			case types.StatusOpen:
				score.Open++
			}
		}

		scores = append(scores, score)
	}

	if jsonOutput {
		type jsonScore struct {
			Agent      string `json:"agent"`
			Closed     int    `json:"closed"`
			InProgress int    `json:"in_progress"`
			Open       int    `json:"open"`
		}

		var jScores []jsonScore
		for _, s := range scores {
			jScores = append(jScores, jsonScore{
				Agent:      s.ID,
				Closed:     s.Closed,
				InProgress: s.InProgress,
				Open:       s.Open,
			})
		}

		result := map[string]interface{}{
			"scores": jScores,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		return encoder.Encode(result)
	}

	// Human-readable table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
		ui.RenderBold("AGENT"),
		ui.RenderBold("CLOSED"),
		ui.RenderBold("IN-PROGRESS"),
		ui.RenderBold("OPEN"),
	)

	for _, s := range scores {
		fmt.Fprintf(w, "%s\t%d\t%d\t%d\n", s.ID, s.Closed, s.InProgress, s.Open)
	}
	w.Flush()

	return nil
}

// hasRigLabel checks if labels contain a specific rig label.
func hasRigLabel(labels []string, rig string) bool {
	target := "rig:" + rig
	for _, l := range labels {
		if l == target {
			return true
		}
	}
	return false
}

