package jira

import (
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// JiraMapper implements tracker.FieldMapper for Jira.
type JiraMapper struct {
	// PriorityMap maps Jira priority names (lowercase) to Beads priority (0-4).
	PriorityMap map[string]int

	// StatusMap maps Jira status names (lowercase) to Beads status.
	StatusMap map[string]string

	// TypeMap maps Jira issue type names (lowercase) to Beads issue types.
	TypeMap map[string]string
}

// NewJiraMapper creates a new JiraMapper with default configuration.
func NewJiraMapper() *JiraMapper {
	return &JiraMapper{
		PriorityMap: map[string]int{
			"highest":  0,
			"critical": 0,
			"blocker":  0,
			"high":     1,
			"major":    1,
			"medium":   2,
			"normal":   2,
			"low":      3,
			"minor":    3,
			"lowest":   4,
			"trivial":  4,
		},
		StatusMap: map[string]string{
			"to do":           "open",
			"todo":            "open",
			"open":            "open",
			"backlog":         "open",
			"new":             "open",
			"in progress":     "in_progress",
			"in development":  "in_progress",
			"in review":       "in_progress",
			"review":          "in_progress",
			"blocked":         "blocked",
			"on hold":         "blocked",
			"done":            "closed",
			"closed":          "closed",
			"resolved":        "closed",
			"complete":        "closed",
			"completed":       "closed",
			"won't do":        "closed",
			"won't fix":       "closed",
			"duplicate":       "closed",
			"cannot reproduce": "closed",
		},
		TypeMap: map[string]string{
			"bug":            "bug",
			"defect":         "bug",
			"story":          "feature",
			"feature":        "feature",
			"new feature":    "feature",
			"improvement":    "feature",
			"enhancement":    "feature",
			"task":           "task",
			"sub-task":       "task",
			"subtask":        "task",
			"epic":           "epic",
			"initiative":     "epic",
			"technical task": "chore",
			"technical debt": "chore",
			"maintenance":    "chore",
			"chore":          "chore",
		},
	}
}

// LoadConfig loads mapping configuration from a config loader.
func (m *JiraMapper) LoadConfig(cfg tracker.ConfigLoader) {
	if cfg == nil {
		return
	}

	allConfig, err := cfg.GetAllConfig()
	if err != nil {
		return
	}

	for key, value := range allConfig {
		// Parse priority mappings: jira.priority_map.<priority_name>
		if strings.HasPrefix(key, "jira.priority_map.") {
			priorityName := strings.ToLower(strings.TrimPrefix(key, "jira.priority_map."))
			if priority, ok := parseIntValue(value); ok {
				m.PriorityMap[priorityName] = priority
			}
		}

		// Parse status mappings: jira.status_map.<status_name>
		if strings.HasPrefix(key, "jira.status_map.") {
			statusName := strings.ToLower(strings.TrimPrefix(key, "jira.status_map."))
			m.StatusMap[statusName] = value
		}

		// Parse type mappings: jira.type_map.<type_name>
		if strings.HasPrefix(key, "jira.type_map.") {
			typeName := strings.ToLower(strings.TrimPrefix(key, "jira.type_map."))
			m.TypeMap[typeName] = value
		}
	}
}

// PriorityToBeads maps Jira priority to Beads priority (0-4).
func (m *JiraMapper) PriorityToBeads(trackerPriority interface{}) int {
	priority, ok := trackerPriority.(*Priority)
	if !ok || priority == nil {
		return 2 // Default to Medium
	}

	name := strings.ToLower(priority.Name)
	if beadsPriority, ok := m.PriorityMap[name]; ok {
		return beadsPriority
	}
	return 2 // Default to Medium
}

// PriorityToTracker maps Beads priority to Jira priority name.
func (m *JiraMapper) PriorityToTracker(beadsPriority int) interface{} {
	// Reverse mapping - find the first Jira priority that maps to this Beads priority
	priorityNames := map[int]string{
		0: "Highest",
		1: "High",
		2: "Medium",
		3: "Low",
		4: "Lowest",
	}
	if name, ok := priorityNames[beadsPriority]; ok {
		return name
	}
	return "Medium"
}

// StatusToBeads maps Jira status to Beads status.
func (m *JiraMapper) StatusToBeads(trackerState interface{}) types.Status {
	status, ok := trackerState.(*Status)
	if !ok || status == nil {
		return types.StatusOpen
	}

	name := strings.ToLower(status.Name)
	if beadsStatus, ok := m.StatusMap[name]; ok {
		return tracker.ParseBeadsStatus(beadsStatus)
	}

	// Try status category as fallback
	if status.StatusCategory != nil {
		switch strings.ToLower(status.StatusCategory.Key) {
		case "new", "todo":
			return types.StatusOpen
		case "indeterminate", "inprogress":
			return types.StatusInProgress
		case "done":
			return types.StatusClosed
		}
	}

	return types.StatusOpen
}

// StatusToTracker maps Beads status to Jira status name.
func (m *JiraMapper) StatusToTracker(beadsStatus types.Status) interface{} {
	switch beadsStatus {
	case types.StatusOpen:
		return "To Do"
	case types.StatusInProgress:
		return "In Progress"
	case types.StatusBlocked:
		return "Blocked"
	case types.StatusClosed:
		return "Done"
	default:
		return "To Do"
	}
}

// TypeToBeads maps Jira issue type to Beads issue type.
func (m *JiraMapper) TypeToBeads(trackerType interface{}) types.IssueType {
	issueType, ok := trackerType.(*IssueType)
	if !ok || issueType == nil {
		return types.TypeTask
	}

	name := strings.ToLower(issueType.Name)
	if beadsType, ok := m.TypeMap[name]; ok {
		return tracker.ParseIssueType(beadsType)
	}
	return types.TypeTask
}

// TypeToTracker maps Beads issue type to Jira issue type name.
func (m *JiraMapper) TypeToTracker(beadsType types.IssueType) interface{} {
	typeNames := map[types.IssueType]string{
		types.TypeBug:     "Bug",
		types.TypeFeature: "Story",
		types.TypeTask:    "Task",
		types.TypeEpic:    "Epic",
		types.TypeChore:   "Task",
	}
	if name, ok := typeNames[beadsType]; ok {
		return name
	}
	return "Task"
}

// IssueToBeads converts a TrackerIssue to a Beads issue.
func (m *JiraMapper) IssueToBeads(ti *tracker.TrackerIssue) *tracker.IssueConversion {
	// Get the raw Jira issue for full access
	var ji *Issue
	if ti.Raw != nil {
		ji, _ = ti.Raw.(*Issue)
	}

	issue := &types.Issue{
		Title:       ti.Title,
		Description: ADFToText(ti.Raw.(*Issue).Fields.Description),
		Priority:    m.PriorityToBeads(ji.Fields.Priority),
		CreatedAt:   ti.CreatedAt,
		UpdatedAt:   ti.UpdatedAt,
	}

	// Map status
	if ji != nil {
		issue.Status = m.StatusToBeads(ji.Fields.Status)
		issue.IssueType = m.TypeToBeads(ji.Fields.IssueType)
	} else {
		issue.Status = types.StatusOpen
		issue.IssueType = types.TypeTask
	}

	// Handle completed timestamp
	if ti.CompletedAt != nil {
		issue.ClosedAt = ti.CompletedAt
	}

	// Set assignee
	if ti.AssigneeEmail != "" {
		issue.Assignee = ti.AssigneeEmail
	} else if ti.Assignee != "" {
		issue.Assignee = ti.Assignee
	}

	// Copy labels
	issue.Labels = append([]string{}, ti.Labels...)

	// Set external ref
	issue.ExternalRef = &ti.URL

	// Collect dependencies from issue links
	var deps []tracker.DependencyInfo

	// Map parent-child relationship
	if ti.ParentID != "" {
		deps = append(deps, tracker.DependencyInfo{
			FromExternalID: ti.Identifier,
			ToExternalID:   ti.ParentID,
			Type:           "parent-child",
		})
	}

	// Map issue links from raw Jira issue
	if ji != nil {
		for _, link := range ji.Fields.IssueLinks {
			linkType := strings.ToLower(link.Type.Name)

			if link.InwardIssue != nil {
				// Inward: the other issue has this relationship TO us
				if strings.Contains(linkType, "block") {
					deps = append(deps, tracker.DependencyInfo{
						FromExternalID: ti.Identifier,
						ToExternalID:   link.InwardIssue.Key,
						Type:           "blocks",
					})
				} else {
					deps = append(deps, tracker.DependencyInfo{
						FromExternalID: ti.Identifier,
						ToExternalID:   link.InwardIssue.Key,
						Type:           "related",
					})
				}
			} else if link.OutwardIssue != nil {
				// Outward: we have this relationship TO the other issue
				if strings.Contains(linkType, "block") {
					deps = append(deps, tracker.DependencyInfo{
						FromExternalID: link.OutwardIssue.Key,
						ToExternalID:   ti.Identifier,
						Type:           "blocks",
					})
				} else {
					deps = append(deps, tracker.DependencyInfo{
						FromExternalID: ti.Identifier,
						ToExternalID:   link.OutwardIssue.Key,
						Type:           "related",
					})
				}
			}
		}
	}

	return &tracker.IssueConversion{
		Issue:        issue,
		Dependencies: deps,
	}
}

// parseIntValue safely parses an integer from a string config value.
func parseIntValue(s string) (int, bool) {
	var v int
	n, err := fmt.Sscanf(s, "%d", &v)
	return v, n == 1 && err == nil
}

// Ensure JiraMapper implements FieldMapper.
var _ tracker.FieldMapper = (*JiraMapper)(nil)
