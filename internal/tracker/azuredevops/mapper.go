package azuredevops

import (
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// AzureDevOpsMapper implements tracker.FieldMapper for Azure DevOps.
type AzureDevOpsMapper struct {
	// PriorityMap maps Azure DevOps priority (1-4) to Beads priority (0-4).
	PriorityMap map[int]int

	// StatusMap maps Azure DevOps state names (lowercase) to Beads status.
	StatusMap map[string]string

	// TypeMap maps Azure DevOps work item types (lowercase) to Beads issue types.
	TypeMap map[string]string
}

// NewAzureDevOpsMapper creates a new AzureDevOpsMapper with default configuration.
func NewAzureDevOpsMapper() *AzureDevOpsMapper {
	return &AzureDevOpsMapper{
		// Azure DevOps priority: 1=High, 2=Medium, 3=Low, 4=Backlog (default)
		// Beads priority: 0=Critical, 1=High, 2=Medium, 3=Low, 4=Backlog
		PriorityMap: map[int]int{
			1: 1, // High -> High
			2: 2, // Medium -> Medium
			3: 3, // Low -> Low
			4: 4, // Backlog -> Backlog
		},
		StatusMap: map[string]string{
			"new":       "open",
			"active":    "in_progress",
			"resolved":  "in_progress",
			"closed":    "closed",
			"done":      "closed",
			"removed":   "closed",
			"to do":     "open",
			"doing":     "in_progress",
			"approved":  "open",
			"committed": "in_progress",
		},
		TypeMap: map[string]string{
			"bug":              "bug",
			"user story":       "feature",
			"feature":          "feature",
			"product backlog item": "feature",
			"task":             "task",
			"epic":             "epic",
			"issue":            "task",
			"impediment":       "bug",
		},
	}
}

// LoadConfig loads mapping configuration from a config loader.
func (m *AzureDevOpsMapper) LoadConfig(cfg tracker.ConfigLoader) {
	if cfg == nil {
		return
	}

	allConfig, err := cfg.GetAllConfig()
	if err != nil {
		return
	}

	for key, value := range allConfig {
		// Parse priority mappings: azuredevops.priority_map.<priority>
		if strings.HasPrefix(key, "azuredevops.priority_map.") {
			priorityStr := strings.TrimPrefix(key, "azuredevops.priority_map.")
			var adoPriority int
			if _, err := fmt.Sscanf(priorityStr, "%d", &adoPriority); err == nil {
				if beadsPriority, ok := parseIntValue(value); ok {
					m.PriorityMap[adoPriority] = beadsPriority
				}
			}
		}

		// Parse status mappings: azuredevops.status_map.<state_name>
		if strings.HasPrefix(key, "azuredevops.status_map.") {
			stateName := strings.ToLower(strings.TrimPrefix(key, "azuredevops.status_map."))
			m.StatusMap[stateName] = value
		}

		// Parse type mappings: azuredevops.type_map.<type_name>
		if strings.HasPrefix(key, "azuredevops.type_map.") {
			typeName := strings.ToLower(strings.TrimPrefix(key, "azuredevops.type_map."))
			m.TypeMap[typeName] = value
		}
	}
}

// PriorityToBeads maps Azure DevOps priority to Beads priority (0-4).
func (m *AzureDevOpsMapper) PriorityToBeads(trackerPriority interface{}) int {
	priority, ok := trackerPriority.(int)
	if !ok {
		return 2 // Default to Medium
	}

	if beadsPriority, ok := m.PriorityMap[priority]; ok {
		return beadsPriority
	}
	return 2 // Default to Medium
}

// PriorityToTracker maps Beads priority to Azure DevOps priority.
func (m *AzureDevOpsMapper) PriorityToTracker(beadsPriority int) interface{} {
	// Reverse mapping
	for adoPriority, beads := range m.PriorityMap {
		if beads == beadsPriority {
			return adoPriority
		}
	}
	// Default mapping
	switch beadsPriority {
	case 0:
		return 1 // Critical -> High (Azure DevOps doesn't have Critical)
	case 1:
		return 1 // High
	case 2:
		return 2 // Medium
	case 3:
		return 3 // Low
	case 4:
		return 4 // Backlog
	default:
		return 2 // Default Medium
	}
}

// StatusToBeads maps Azure DevOps state to Beads status.
func (m *AzureDevOpsMapper) StatusToBeads(trackerState interface{}) types.Status {
	state, ok := trackerState.(string)
	if !ok {
		return types.StatusOpen
	}

	stateLower := strings.ToLower(state)
	if beadsStatus, ok := m.StatusMap[stateLower]; ok {
		return tracker.ParseBeadsStatus(beadsStatus)
	}

	return types.StatusOpen
}

// StatusToTracker maps Beads status to Azure DevOps state.
func (m *AzureDevOpsMapper) StatusToTracker(beadsStatus types.Status) interface{} {
	switch beadsStatus {
	case types.StatusOpen:
		return "New"
	case types.StatusInProgress:
		return "Active"
	case types.StatusBlocked:
		return "Active" // Azure DevOps doesn't have a blocked state
	case types.StatusClosed:
		return "Closed"
	default:
		return "New"
	}
}

// TypeToBeads maps Azure DevOps work item type to Beads issue type.
func (m *AzureDevOpsMapper) TypeToBeads(trackerType interface{}) types.IssueType {
	workItemType, ok := trackerType.(string)
	if !ok {
		return types.TypeTask
	}

	typeLower := strings.ToLower(workItemType)
	if beadsType, ok := m.TypeMap[typeLower]; ok {
		return tracker.ParseIssueType(beadsType)
	}
	return types.TypeTask
}

// TypeToTracker maps Beads issue type to Azure DevOps work item type.
func (m *AzureDevOpsMapper) TypeToTracker(beadsType types.IssueType) interface{} {
	typeNames := map[types.IssueType]string{
		types.TypeBug:     "Bug",
		types.TypeFeature: "User Story",
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
func (m *AzureDevOpsMapper) IssueToBeads(ti *tracker.TrackerIssue) *tracker.IssueConversion {
	// Get the raw Azure DevOps work item for full access
	var wi *WorkItem
	if ti.Raw != nil {
		wi, _ = ti.Raw.(*WorkItem)
	}

	issue := &types.Issue{
		Title:       ti.Title,
		Description: ti.Description,
		Priority:    m.PriorityToBeads(ti.Priority),
		CreatedAt:   ti.CreatedAt,
		UpdatedAt:   ti.UpdatedAt,
	}

	// Map status and type
	if wi != nil {
		issue.Status = m.StatusToBeads(wi.Fields.State)
		issue.IssueType = m.TypeToBeads(wi.Fields.WorkItemType)
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

	// Parse tags from semicolon-separated string
	if wi != nil && wi.Fields.Tags != "" {
		tags := strings.Split(wi.Fields.Tags, ";")
		for _, tag := range tags {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				issue.Labels = append(issue.Labels, tag)
			}
		}
	}

	// Set external ref
	issue.ExternalRef = &ti.URL

	// Collect dependencies
	var deps []tracker.DependencyInfo

	// Map parent relationship
	if ti.ParentID != "" {
		deps = append(deps, tracker.DependencyInfo{
			FromExternalID: ti.Identifier,
			ToExternalID:   ti.ParentID,
			Type:           "parent-child",
		})
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

// Ensure AzureDevOpsMapper implements FieldMapper.
var _ tracker.FieldMapper = (*AzureDevOpsMapper)(nil)
