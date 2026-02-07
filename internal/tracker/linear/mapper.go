package linear

import (
	"fmt"

	"github.com/steveyegge/beads/internal/linear"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// LinearMapper implements tracker.FieldMapper for Linear.
type LinearMapper struct {
	config *linear.MappingConfig
}

// NewLinearMapper creates a new LinearMapper with default configuration.
func NewLinearMapper() *LinearMapper {
	return &LinearMapper{
		config: linear.DefaultMappingConfig(),
	}
}

// LoadConfig loads mapping configuration from a config loader.
func (m *LinearMapper) LoadConfig(cfg tracker.ConfigLoader) {
	m.config = linear.LoadMappingConfig(cfg)
}

// PriorityToBeads maps Linear priority (0-4) to Beads priority (0-4).
// Linear: 0=no priority, 1=urgent, 2=high, 3=medium, 4=low
// Beads:  0=critical, 1=high, 2=medium, 3=low, 4=backlog
func (m *LinearMapper) PriorityToBeads(trackerPriority interface{}) int {
	priority, ok := trackerPriority.(int)
	if !ok {
		return 2 // Default to Medium
	}
	return linear.PriorityToBeads(priority, m.config)
}

// PriorityToTracker maps Beads priority (0-4) to Linear priority (0-4).
func (m *LinearMapper) PriorityToTracker(beadsPriority int) interface{} {
	return linear.PriorityToLinear(beadsPriority, m.config)
}

// StatusToBeads maps Linear state to Beads status.
func (m *LinearMapper) StatusToBeads(trackerState interface{}) types.Status {
	state, ok := trackerState.(*linear.State)
	if !ok {
		return types.StatusOpen
	}
	return linear.StateToBeadsStatus(state, m.config)
}

// StatusToTracker maps Beads status to Linear state type.
func (m *LinearMapper) StatusToTracker(beadsStatus types.Status) interface{} {
	return linear.StatusToLinearStateType(beadsStatus)
}

// TypeToBeads infers issue type from Linear labels.
func (m *LinearMapper) TypeToBeads(trackerType interface{}) types.IssueType {
	labels, ok := trackerType.(*linear.Labels)
	if !ok {
		return types.TypeTask
	}
	return linear.LabelToIssueType(labels, m.config)
}

// TypeToTracker maps Beads issue type to Linear label (for display).
func (m *LinearMapper) TypeToTracker(beadsType types.IssueType) interface{} {
	// Linear doesn't have a native "type" field, we'd use labels
	// For now, just return the type string
	return string(beadsType)
}

// IssueToBeads converts a TrackerIssue to a Beads issue.
func (m *LinearMapper) IssueToBeads(ti *tracker.TrackerIssue) *tracker.IssueConversion {
	// Get the raw Linear issue for full access
	var li *linear.Issue
	if ti.Raw != nil {
		li, _ = ti.Raw.(*linear.Issue)
	}

	issue := &types.Issue{
		Title:       ti.Title,
		Description: ti.Description,
		Priority:    m.PriorityToBeads(ti.Priority),
		CreatedAt:   ti.CreatedAt,
		UpdatedAt:   ti.UpdatedAt,
	}

	// Map status using state
	if state, ok := ti.State.(*linear.State); ok {
		issue.Status = linear.StateToBeadsStatus(state, m.config)
	} else {
		issue.Status = types.StatusOpen
	}

	// Map issue type from labels
	if li != nil && li.Labels != nil {
		issue.IssueType = linear.LabelToIssueType(li.Labels, m.config)
	} else {
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
	externalRef := ti.URL
	if canonical, ok := linear.CanonicalizeLinearExternalRef(externalRef); ok {
		externalRef = canonical
	}
	issue.ExternalRef = &externalRef

	// Collect dependencies
	var deps []tracker.DependencyInfo

	// Map parent-child relationship
	if ti.ParentID != "" {
		deps = append(deps, tracker.DependencyInfo{
			FromExternalID: ti.Identifier,
			ToExternalID:   ti.ParentID,
			Type:           "parent-child",
		})
	}

	// Map relations from raw Linear issue
	if li != nil && li.Relations != nil {
		for _, rel := range li.Relations.Nodes {
			depType := linear.RelationToBeadsDep(rel.Type, m.config)

			// Handle inverse relations
			if rel.Type == "blockedBy" {
				deps = append(deps, tracker.DependencyInfo{
					FromExternalID: ti.Identifier,
					ToExternalID:   rel.RelatedIssue.Identifier,
					Type:           depType,
				})
				continue
			}

			if rel.Type == "blocks" {
				deps = append(deps, tracker.DependencyInfo{
					FromExternalID: rel.RelatedIssue.Identifier,
					ToExternalID:   ti.Identifier,
					Type:           depType,
				})
				continue
			}

			// For other relations, this issue is the source
			deps = append(deps, tracker.DependencyInfo{
				FromExternalID: ti.Identifier,
				ToExternalID:   rel.RelatedIssue.Identifier,
				Type:           depType,
			})
		}
	}

	return &tracker.IssueConversion{
		Issue:        issue,
		Dependencies: deps,
	}
}

// Config returns the underlying mapping configuration.
func (m *LinearMapper) Config() *linear.MappingConfig {
	return m.config
}

// BuildLinearDescription builds a Linear-formatted description from a Beads issue.
// This is delegated to the linear package to ensure consistency.
func BuildLinearDescription(issue *types.Issue) string {
	return linear.BuildLinearDescription(issue)
}

// ParseBeadsStatus converts a status string to types.Status.
func ParseBeadsStatus(s string) types.Status {
	return tracker.ParseBeadsStatus(s)
}

// parseIntValue safely parses an integer from a string config value.
func parseIntValue(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// Ensure LinearMapper implements FieldMapper.
var _ tracker.FieldMapper = (*LinearMapper)(nil)
