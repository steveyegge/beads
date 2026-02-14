package gitlab

import (
	"fmt"
	"os"

	gitlablib "github.com/steveyegge/beads/internal/gitlab"
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// fieldMapper implements tracker.FieldMapper for GitLab.
type fieldMapper struct {
	config *gitlablib.MappingConfig
}

func (m *fieldMapper) PriorityToBeads(trackerPriority interface{}) int {
	// GitLab uses label-based priority (string), not numeric
	if label, ok := trackerPriority.(string); ok {
		if p, exists := m.config.PriorityMap[label]; exists {
			return p
		}
	}
	return 2
}

func (m *fieldMapper) PriorityToTracker(beadsPriority int) interface{} {
	// Inverse lookup: find the label for this priority
	for label, p := range m.config.PriorityMap {
		if p == beadsPriority {
			return label
		}
	}
	return "medium"
}

func (m *fieldMapper) StatusToBeads(trackerState interface{}) types.Status {
	if state, ok := trackerState.(string); ok {
		if status, exists := m.config.StateMap[state]; exists {
			return types.Status(status)
		}
		// GitLab-specific defaults
		switch state {
		case "opened", "reopened":
			return types.StatusOpen
		case "closed":
			return types.StatusClosed
		}
	}
	return types.StatusOpen
}

func (m *fieldMapper) StatusToTracker(beadsStatus types.Status) interface{} {
	switch beadsStatus {
	case types.StatusClosed:
		return "closed"
	default:
		return "opened"
	}
}

func (m *fieldMapper) TypeToBeads(trackerType interface{}) types.IssueType {
	if t, ok := trackerType.(string); ok {
		if issueType, exists := m.config.LabelTypeMap[t]; exists {
			return types.IssueType(issueType)
		}
	}
	return types.TypeTask
}

func (m *fieldMapper) TypeToTracker(beadsType types.IssueType) interface{} {
	return string(beadsType)
}

func (m *fieldMapper) IssueToBeads(ti *tracker.TrackerIssue) *tracker.IssueConversion {
	gl, ok := ti.Raw.(*gitlablib.Issue)
	if !ok {
		return nil
	}

	conv := gitlablib.GitLabIssueToBeads(gl, m.config)
	if conv == nil {
		return nil
	}

	// Convert gitlab.DependencyInfo to tracker.DependencyInfo
	var deps []tracker.DependencyInfo
	for _, d := range conv.Dependencies {
		deps = append(deps, tracker.DependencyInfo{
			FromExternalID: fmt.Sprintf("%d", d.FromGitLabIID),
			ToExternalID:   fmt.Sprintf("%d", d.ToGitLabIID),
			Type:           d.Type,
		})
	}

	return &tracker.IssueConversion{
		Issue:        conv.Issue,
		Dependencies: deps,
	}
}

func (m *fieldMapper) IssueToTracker(issue *types.Issue) map[string]interface{} {
	return gitlablib.BeadsIssueToGitLabFields(issue, m.config)
}

// envLookup reads an environment variable.
func envLookup(key string) string {
	return os.Getenv(key)
}
