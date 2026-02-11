// Package github provides client and data types for the GitHub REST API.
package github

import (
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// MappingConfig configures how GitHub fields map to beads fields.
type MappingConfig struct {
	PriorityMap  map[string]int    // priority label value → beads priority (0-4)
	StateMap     map[string]string // GitHub state → beads status
	LabelTypeMap map[string]string // type label value → beads issue type
}

// DefaultMappingConfig returns the default mapping configuration.
func DefaultMappingConfig() *MappingConfig {
	priorityMap := make(map[string]int, len(PriorityMapping))
	for k, v := range PriorityMapping {
		priorityMap[k] = v
	}

	labelTypeMap := make(map[string]string, len(TypeMapping))
	for k, v := range TypeMapping {
		labelTypeMap[k] = v
	}

	return &MappingConfig{
		PriorityMap: priorityMap,
		// GitHub uses "open"/"closed" (not "opened"/"closed" like GitLab)
		StateMap: map[string]string{
			"open":   "open",
			"closed": "closed",
		},
		LabelTypeMap: labelTypeMap,
	}
}

// PriorityFromLabels extracts priority from GitHub labels.
// Returns default priority (2 = medium) if no priority label found.
// Supports label formats: "priority:high", "priority/high", "P0", "P1", etc.
func PriorityFromLabels(labels []Label, config *MappingConfig) int {
	for _, label := range labels {
		name := label.Name
		// Check scoped format: "priority:high" or "priority/high"
		prefix, value := ParseLabelName(name)
		if prefix == "priority" {
			if p, ok := config.PriorityMap[strings.ToLower(value)]; ok {
				return p
			}
		}
		// Check shorthand format: P0, P1, P2, P3, P4
		upper := strings.ToUpper(name)
		switch upper {
		case "P0":
			return 0
		case "P1":
			return 1
		case "P2":
			return 2
		case "P3":
			return 3
		case "P4":
			return 4
		}
	}
	return 2 // Default to medium
}

// StatusFromLabelsAndState determines beads status from GitHub labels and state.
// GitHub's closed state takes precedence over status labels.
func StatusFromLabelsAndState(labels []Label, state string, config *MappingConfig) string {
	// Closed state always wins
	if state == "closed" {
		return "closed"
	}

	// Check for status label
	for _, label := range labels {
		prefix, value := ParseLabelName(label.Name)
		if prefix == "status" {
			normalized := strings.ToLower(value)
			if normalized == "in_progress" || normalized == "in-progress" {
				return "in_progress"
			}
			if normalized == "blocked" {
				return "blocked"
			}
			if normalized == "deferred" {
				return "deferred"
			}
		}
	}

	// Default: map GitHub state to beads status
	if s, ok := config.StateMap[state]; ok {
		return s
	}
	return "open"
}

// TypeFromLabels extracts issue type from GitHub labels.
// Checks scoped labels (type:bug, type/bug) and bare labels (bug).
// Returns "task" if no type label found.
func TypeFromLabels(labels []Label, config *MappingConfig) string {
	for _, label := range labels {
		prefix, value := ParseLabelName(label.Name)
		if prefix == "type" {
			if t, ok := config.LabelTypeMap[strings.ToLower(value)]; ok {
				return t
			}
		}
		// Also check bare labels (no prefix)
		if prefix == "" {
			if t, ok := config.LabelTypeMap[strings.ToLower(value)]; ok {
				return t
			}
		}
	}
	return "task" // Default to task
}

// GitHubIssueToBeads converts a GitHub Issue to a beads Issue.
func GitHubIssueToBeads(gh *Issue, owner, repo string, config *MappingConfig) *IssueConversion {
	htmlURL := gh.HTMLURL
	sourceSystem := fmt.Sprintf("github:%s/%s:%d", owner, repo, gh.Number)

	labelNames := LabelNames(gh.Labels)

	issue := &types.Issue{
		Title:        gh.Title,
		Description:  gh.Body,
		ExternalRef:  &htmlURL,
		SourceSystem: sourceSystem,
		IssueType:    types.IssueType(TypeFromLabels(gh.Labels, config)),
		Priority:     PriorityFromLabels(gh.Labels, config),
		Status:       types.Status(StatusFromLabelsAndState(gh.Labels, gh.State, config)),
		Labels:       FilterNonScopedLabels(labelNames),
	}

	// Set assignee from GitHub user
	if gh.Assignee != nil {
		issue.Assignee = gh.Assignee.Login
	}

	// Set timestamps
	if gh.CreatedAt != nil {
		issue.CreatedAt = *gh.CreatedAt
	}
	if gh.UpdatedAt != nil {
		issue.UpdatedAt = *gh.UpdatedAt
	}

	return &IssueConversion{
		Issue:        issue,
		Dependencies: []DependencyInfo{},
	}
}

// BeadsIssueToGitHubFields converts a beads Issue to GitHub API update fields.
func BeadsIssueToGitHubFields(issue *types.Issue, config *MappingConfig) map[string]interface{} {
	fields := map[string]interface{}{
		"title": issue.Title,
		"body":  issue.Description,
	}

	// Build labels from type, priority, and status
	var labels []string

	// Add type label
	if issue.IssueType != "" {
		labels = append(labels, "type:"+string(issue.IssueType))
	}

	// Add priority label
	priorityLabel := priorityToLabel(issue.Priority)
	if priorityLabel != "" {
		labels = append(labels, "priority:"+priorityLabel)
	}

	// Add status label (if not open or closed - those are handled by state)
	if issue.Status == types.StatusInProgress {
		labels = append(labels, "status:in_progress")
	} else if issue.Status == types.StatusBlocked {
		labels = append(labels, "status:blocked")
	} else if issue.Status == types.StatusDeferred {
		labels = append(labels, "status:deferred")
	}

	// Add any existing non-scoped labels
	labels = append(labels, issue.Labels...)

	fields["labels"] = labels

	// Set state for closed issues
	if issue.Status == types.StatusClosed {
		fields["state"] = "closed"
	}

	return fields
}

// priorityToLabel converts beads priority (0-4) to GitHub priority label value.
func priorityToLabel(priority int) string {
	switch priority {
	case 0:
		return "critical"
	case 1:
		return "high"
	case 2:
		return "medium"
	case 3:
		return "low"
	case 4:
		return "none"
	default:
		return "medium"
	}
}

// FilterNonScopedLabels returns only labels without scoped prefixes.
// Removes priority:*, status:*, and type:* labels.
func FilterNonScopedLabels(labels []string) []string {
	var filtered []string
	for _, label := range labels {
		prefix, _ := ParseLabelName(label)
		if prefix == "priority" || prefix == "status" || prefix == "type" {
			continue
		}
		filtered = append(filtered, label)
	}
	return filtered
}
