// Package swarm provides business logic for swarm (structured epic) coordination.
// It handles epic analysis, ready front computation, and swarm status tracking.
package swarm

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// Analysis holds the results of analyzing an epic's structure for swarming.
type Analysis struct {
	EpicID            string                `json:"epic_id"`
	EpicTitle         string                `json:"epic_title"`
	TotalIssues       int                   `json:"total_issues"`
	ClosedIssues      int                   `json:"closed_issues"`
	ReadyFronts       []ReadyFront          `json:"ready_fronts"`
	MaxParallelism    int                   `json:"max_parallelism"`
	EstimatedSessions int                   `json:"estimated_sessions"`
	Warnings          []string              `json:"warnings"`
	Errors            []string              `json:"errors"`
	Swarmable         bool                  `json:"swarmable"`
	Issues            map[string]*IssueNode `json:"issues,omitempty"`
}

// ReadyFront represents a group of issues that can be worked on in parallel.
type ReadyFront struct {
	Wave   int      `json:"wave"`
	Issues []string `json:"issues"`
	Titles []string `json:"titles,omitempty"`
}

// IssueNode represents an issue in the dependency graph.
type IssueNode struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Priority     int      `json:"priority"`
	DependsOn    []string `json:"depends_on"`
	DependedOnBy []string `json:"depended_on_by"`
	Wave         int      `json:"wave"`
}

// Storage defines the storage interface needed by swarm operations.
type Storage interface {
	GetIssue(context.Context, string) (*types.Issue, error)
	GetDependents(context.Context, string) ([]*types.Issue, error)
	GetDependencyRecords(context.Context, string) ([]*types.Dependency, error)
}

// Status holds the current status of a swarm (computed from beads).
type Status struct {
	EpicID       string        `json:"epic_id"`
	EpicTitle    string        `json:"epic_title"`
	TotalIssues  int           `json:"total_issues"`
	Completed    []StatusIssue `json:"completed"`
	Active       []StatusIssue `json:"active"`
	Ready        []StatusIssue `json:"ready"`
	Blocked      []StatusIssue `json:"blocked"`
	Progress     float64       `json:"progress_percent"`
	ActiveCount  int           `json:"active_count"`
	ReadyCount   int           `json:"ready_count"`
	BlockedCount int           `json:"blocked_count"`
}

// StatusIssue represents an issue in swarm status output.
type StatusIssue struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Assignee  string   `json:"assignee,omitempty"`
	BlockedBy []string `json:"blocked_by,omitempty"`
	ClosedAt  string   `json:"closed_at,omitempty"`
}

// FindExistingSwarm returns the swarm molecule for an epic, if one exists.
// Returns nil if no swarm molecule is linked to the epic.
func FindExistingSwarm(ctx context.Context, s Storage, epicID string) (*types.Issue, error) {
	dependents, err := s.GetDependents(ctx, epicID)
	if err != nil {
		return nil, fmt.Errorf("failed to get epic dependents: %w", err)
	}

	for _, dep := range dependents {
		if dep.IssueType != "molecule" {
			continue
		}

		fullIssue, err := s.GetIssue(ctx, dep.ID)
		if err != nil || fullIssue == nil {
			continue
		}
		if fullIssue.MolType != types.MolTypeSwarm {
			continue
		}

		deps, err := s.GetDependencyRecords(ctx, dep.ID)
		if err != nil {
			continue
		}
		for _, d := range deps {
			if d.DependsOnID == epicID && d.Type == types.DepRelatesTo {
				return fullIssue, nil
			}
		}
	}

	return nil, nil
}

// GetEpicChildren returns all child issues of an epic (via parent-child dependencies).
func GetEpicChildren(ctx context.Context, s Storage, epicID string) ([]*types.Issue, error) {
	allDependents, err := s.GetDependents(ctx, epicID)
	if err != nil {
		return nil, fmt.Errorf("failed to get epic dependents: %w", err)
	}

	var children []*types.Issue
	for _, dependent := range allDependents {
		deps, err := s.GetDependencyRecords(ctx, dependent.ID)
		if err != nil {
			continue
		}
		for _, dep := range deps {
			if dep.DependsOnID == epicID && dep.Type == types.DepParentChild {
				children = append(children, dependent)
				break
			}
		}
	}

	return children, nil
}

// AnalyzeEpicForSwarm performs structural analysis of an epic for swarm execution.
func AnalyzeEpicForSwarm(ctx context.Context, s Storage, epic *types.Issue) (*Analysis, error) {
	analysis := &Analysis{
		EpicID:    epic.ID,
		EpicTitle: epic.Title,
		Swarmable: true,
		Issues:    make(map[string]*IssueNode),
	}

	childIssues, err := GetEpicChildren(ctx, s, epic.ID)
	if err != nil {
		return nil, err
	}

	if len(childIssues) == 0 {
		analysis.Warnings = append(analysis.Warnings, "Epic has no children")
		return analysis, nil
	}

	analysis.TotalIssues = len(childIssues)

	for _, issue := range childIssues {
		node := &IssueNode{
			ID:           issue.ID,
			Title:        issue.Title,
			Status:       string(issue.Status),
			Priority:     issue.Priority,
			DependsOn:    []string{},
			DependedOnBy: []string{},
			Wave:         -1,
		}
		analysis.Issues[issue.ID] = node

		if issue.Status == types.StatusClosed {
			analysis.ClosedIssues++
		}
	}

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
			if dep.DependsOnID == epic.ID && dep.Type == types.DepParentChild {
				continue
			}
			if !dep.Type.AffectsReadyWork() {
				continue
			}
			if childIDSet[dep.DependsOnID] {
				node.DependsOn = append(node.DependsOn, dep.DependsOnID)
				if targetNode, ok := analysis.Issues[dep.DependsOnID]; ok {
					targetNode.DependedOnBy = append(targetNode.DependedOnBy, issue.ID)
				}
			}
			if !childIDSet[dep.DependsOnID] && dep.DependsOnID != epic.ID {
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

	DetectStructuralIssues(analysis, childIssues)

	ComputeReadyFronts(analysis)

	analysis.Swarmable = len(analysis.Errors) == 0

	return analysis, nil
}

// DetectStructuralIssues looks for common problems in the dependency graph.
//
//nolint:unparam // issues reserved for future use
func DetectStructuralIssues(analysis *Analysis, _ []*types.Issue) {
	var roots []string
	for id, node := range analysis.Issues {
		if len(node.DependsOn) == 0 {
			roots = append(roots, id)
		}
	}

	for id, node := range analysis.Issues {
		lowerTitle := strings.ToLower(node.Title)

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

	for _, root := range roots {
		dfs(root)
	}

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

// ComputeReadyFronts calculates the waves of parallel work.
func ComputeReadyFronts(analysis *Analysis) {
	if len(analysis.Errors) > 0 {
		return
	}

	inDegree := make(map[string]int)
	for id, node := range analysis.Issues {
		inDegree[id] = len(node.DependsOn)
	}

	var currentWave []string
	for id, degree := range inDegree {
		if degree == 0 {
			currentWave = append(currentWave, id)
			analysis.Issues[id].Wave = 0
		}
	}

	wave := 0
	for len(currentWave) > 0 {
		sort.Strings(currentWave)

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

		if len(currentWave) > analysis.MaxParallelism {
			analysis.MaxParallelism = len(currentWave)
		}

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

	analysis.EstimatedSessions = analysis.TotalIssues
}

// GetSwarmStatus computes current swarm status from beads.
func GetSwarmStatus(ctx context.Context, s Storage, epic *types.Issue) (*Status, error) {
	status := &Status{
		EpicID:    epic.ID,
		EpicTitle: epic.Title,
		Completed: []StatusIssue{},
		Active:    []StatusIssue{},
		Ready:     []StatusIssue{},
		Blocked:   []StatusIssue{},
	}

	childIssues, err := GetEpicChildren(ctx, s, epic.ID)
	if err != nil {
		return nil, err
	}

	status.TotalIssues = len(childIssues)
	if len(childIssues) == 0 {
		return status, nil
	}

	childIDSet := make(map[string]bool)
	for _, issue := range childIssues {
		childIDSet[issue.ID] = true
	}

	dependsOn := make(map[string][]string)
	for _, issue := range childIssues {
		deps, err := s.GetDependencyRecords(ctx, issue.ID)
		if err != nil {
			continue
		}
		for _, dep := range deps {
			if dep.DependsOnID == epic.ID && dep.Type == types.DepParentChild {
				continue
			}
			if !dep.Type.AffectsReadyWork() {
				continue
			}
			if childIDSet[dep.DependsOnID] {
				dependsOn[issue.ID] = append(dependsOn[issue.ID], dep.DependsOnID)
			}
		}
	}

	for _, issue := range childIssues {
		si := StatusIssue{
			ID:       issue.ID,
			Title:    issue.Title,
			Assignee: issue.Assignee,
		}

		switch issue.Status {
		case types.StatusClosed:
			if issue.ClosedAt != nil {
				si.ClosedAt = issue.ClosedAt.Format("2006-01-02 15:04")
			}
			status.Completed = append(status.Completed, si)

		case types.StatusInProgress:
			status.Active = append(status.Active, si)

		default:
			deps := dependsOn[issue.ID]
			var blockers []string
			for _, depID := range deps {
				depIssue, _ := s.GetIssue(ctx, depID)
				if depIssue != nil && depIssue.Status != types.StatusClosed {
					blockers = append(blockers, depID)
				}
			}

			if len(blockers) > 0 {
				si.BlockedBy = blockers
				status.Blocked = append(status.Blocked, si)
			} else {
				status.Ready = append(status.Ready, si)
			}
		}
	}

	sort.Slice(status.Completed, func(i, j int) bool {
		return status.Completed[i].ID < status.Completed[j].ID
	})
	sort.Slice(status.Active, func(i, j int) bool {
		return status.Active[i].ID < status.Active[j].ID
	})
	sort.Slice(status.Ready, func(i, j int) bool {
		return status.Ready[i].ID < status.Ready[j].ID
	})
	sort.Slice(status.Blocked, func(i, j int) bool {
		return status.Blocked[i].ID < status.Blocked[j].ID
	})

	status.ActiveCount = len(status.Active)
	status.ReadyCount = len(status.Ready)
	status.BlockedCount = len(status.Blocked)
	if status.TotalIssues > 0 {
		status.Progress = float64(len(status.Completed)) / float64(status.TotalIssues) * 100
	}

	return status, nil
}
