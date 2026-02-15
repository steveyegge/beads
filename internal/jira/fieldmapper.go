package jira

import (
	"github.com/steveyegge/beads/internal/tracker"
	"github.com/steveyegge/beads/internal/types"
)

// jiraFieldMapper implements tracker.FieldMapper for Jira.
// This is a minimal implementation since Jira sync currently uses the Python script.
type jiraFieldMapper struct{}

func (m *jiraFieldMapper) PriorityToBeads(trackerPriority interface{}) int {
	// Jira priority names: Highest(1), High(2), Medium(3), Low(4), Lowest(5)
	if name, ok := trackerPriority.(string); ok {
		switch name {
		case "Highest":
			return 0
		case "High":
			return 1
		case "Medium":
			return 2
		case "Low":
			return 3
		case "Lowest":
			return 4
		}
	}
	return 2
}

func (m *jiraFieldMapper) PriorityToTracker(beadsPriority int) interface{} {
	switch beadsPriority {
	case 0:
		return "Highest"
	case 1:
		return "High"
	case 2:
		return "Medium"
	case 3:
		return "Low"
	case 4:
		return "Lowest"
	default:
		return "Medium"
	}
}

func (m *jiraFieldMapper) StatusToBeads(trackerState interface{}) types.Status {
	if state, ok := trackerState.(string); ok {
		switch state {
		case "To Do", "Open", "Backlog", "New":
			return types.StatusOpen
		case "In Progress", "In Review":
			return types.StatusInProgress
		case "Blocked":
			return types.StatusBlocked
		case "Done", "Closed", "Resolved":
			return types.StatusClosed
		}
	}
	return types.StatusOpen
}

func (m *jiraFieldMapper) StatusToTracker(beadsStatus types.Status) interface{} {
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

func (m *jiraFieldMapper) TypeToBeads(trackerType interface{}) types.IssueType {
	if t, ok := trackerType.(string); ok {
		switch t {
		case "Bug":
			return types.TypeBug
		case "Story", "Feature":
			return types.TypeFeature
		case "Epic":
			return types.TypeEpic
		case "Task", "Sub-task":
			return types.TypeTask
		}
	}
	return types.TypeTask
}

func (m *jiraFieldMapper) TypeToTracker(beadsType types.IssueType) interface{} {
	switch beadsType {
	case types.TypeBug:
		return "Bug"
	case types.TypeFeature:
		return "Story"
	case types.TypeEpic:
		return "Epic"
	default:
		return "Task"
	}
}

func (m *jiraFieldMapper) IssueToBeads(_ *tracker.TrackerIssue) *tracker.IssueConversion {
	return nil
}

func (m *jiraFieldMapper) IssueToTracker(_ *types.Issue) map[string]interface{} {
	return nil
}
