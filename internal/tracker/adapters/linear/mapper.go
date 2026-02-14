package linear

import (
	"os"

	linearlib "github.com/steveyegge/beads/internal/linear"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// fieldMapper implements tracker.FieldMapper for Linear.
type fieldMapper struct {
	config *linearlib.MappingConfig
}

func (m *fieldMapper) PriorityToBeads(trackerPriority interface{}) int {
	if p, ok := trackerPriority.(int); ok {
		return linearlib.PriorityToBeads(p, m.config)
	}
	return 2
}

func (m *fieldMapper) PriorityToTracker(beadsPriority int) interface{} {
	return linearlib.PriorityToLinear(beadsPriority, m.config)
}

func (m *fieldMapper) StatusToBeads(trackerState interface{}) types.Status {
	if state, ok := trackerState.(*linearlib.State); ok {
		return linearlib.StateToBeadsStatus(state, m.config)
	}
	return types.StatusOpen
}

func (m *fieldMapper) StatusToTracker(beadsStatus types.Status) interface{} {
	return linearlib.StatusToLinearStateType(beadsStatus)
}

func (m *fieldMapper) TypeToBeads(trackerType interface{}) types.IssueType {
	if labels, ok := trackerType.(*linearlib.Labels); ok {
		return linearlib.LabelToIssueType(labels, m.config)
	}
	return types.TypeTask
}

func (m *fieldMapper) TypeToTracker(beadsType types.IssueType) interface{} {
	return string(beadsType)
}

func (m *fieldMapper) IssueToBeads(ti *tracker.TrackerIssue) *tracker.IssueConversion {
	li, ok := ti.Raw.(*linearlib.Issue)
	if !ok {
		return nil
	}

	conv := linearlib.IssueToBeads(li, m.config)
	if conv == nil {
		return nil
	}

	issue, ok := conv.Issue.(*types.Issue)
	if !ok {
		return nil
	}

	// Convert linear.DependencyInfo to tracker.DependencyInfo
	var deps []tracker.DependencyInfo
	for _, d := range conv.Dependencies {
		deps = append(deps, tracker.DependencyInfo{
			FromExternalID: d.FromLinearID,
			ToExternalID:   d.ToLinearID,
			Type:           d.Type,
		})
	}

	return &tracker.IssueConversion{
		Issue:        issue,
		Dependencies: deps,
	}
}

func (m *fieldMapper) IssueToTracker(issue *types.Issue) map[string]interface{} {
	updates := map[string]interface{}{
		"title":       issue.Title,
		"description": issue.Description,
		"priority":    linearlib.PriorityToLinear(issue.Priority, m.config),
	}
	return updates
}

// envLookup reads an environment variable.
func envLookup(key string) string {
	return os.Getenv(key)
}
