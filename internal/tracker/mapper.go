package tracker

import (
	"github.com/steveyegge/beads/internal/types"
)

// FieldMapper defines the interface for bidirectional field mapping between
// Beads and external trackers. Each tracker plugin implements this to handle
// its specific field formats and values.
type FieldMapper interface {
	// Priority mapping (bidirectional)
	// Beads uses 0-4: 0=critical, 1=high, 2=medium, 3=low, 4=backlog
	PriorityToBeads(trackerPriority interface{}) int
	PriorityToTracker(beadsPriority int) interface{}

	// Status mapping (bidirectional)
	// Beads uses: open, in_progress, blocked, deferred, closed, etc.
	StatusToBeads(trackerState interface{}) types.Status
	StatusToTracker(beadsStatus types.Status) interface{}

	// Issue type mapping (bidirectional)
	// Beads uses: bug, feature, task, epic, chore, etc.
	TypeToBeads(trackerType interface{}) types.IssueType
	TypeToTracker(beadsType types.IssueType) interface{}

	// Full issue conversion (for import)
	// Returns the converted issue and any dependencies to be created
	IssueToBeads(trackerIssue *TrackerIssue) *IssueConversion

	// LoadConfig loads mapping configuration from a config loader.
	// This allows users to customize the mappings via bd config.
	LoadConfig(cfg ConfigLoader)
}

// ConfigLoader is an interface for loading configuration values.
// This allows the mapper to be decoupled from the storage layer.
type ConfigLoader interface {
	GetAllConfig() (map[string]string, error)
}

// BaseMappingConfig provides default mappings that are commonly used across trackers.
// Tracker-specific mappers can embed this and override as needed.
type BaseMappingConfig struct {
	// PriorityMap maps tracker priority values (as string keys) to Beads priority (0-4).
	PriorityMap map[string]int

	// PriorityReverseMap maps Beads priority (as string keys) to tracker priority values.
	PriorityReverseMap map[string]interface{}

	// StateMap maps tracker state identifiers to Beads statuses.
	StateMap map[string]string

	// StateReverseMap maps Beads statuses to tracker state identifiers.
	StateReverseMap map[string]interface{}

	// LabelTypeMap maps label names to Beads issue types.
	LabelTypeMap map[string]string

	// TypeReverseMap maps Beads issue types to tracker type identifiers.
	TypeReverseMap map[string]interface{}

	// RelationMap maps tracker relation types to Beads dependency types.
	RelationMap map[string]string
}

// DefaultBaseMappingConfig returns sensible defaults for common mappings.
func DefaultBaseMappingConfig() *BaseMappingConfig {
	return &BaseMappingConfig{
		// Default priority mapping (tracker-specific, override as needed)
		PriorityMap: map[string]int{
			"0": 4, // No priority -> Backlog
			"1": 0, // Urgent/Critical
			"2": 1, // High
			"3": 2, // Medium
			"4": 3, // Low
		},

		// Default state mapping (tracker-specific, override as needed)
		StateMap: map[string]string{
			"backlog":   "open",
			"unstarted": "open",
			"todo":      "open",
			"open":      "open",
			"started":   "in_progress",
			"active":    "in_progress",
			"completed": "closed",
			"done":      "closed",
			"canceled":  "closed",
		},

		// Default label-to-type mapping
		LabelTypeMap: map[string]string{
			"bug":         "bug",
			"defect":      "bug",
			"feature":     "feature",
			"enhancement": "feature",
			"epic":        "epic",
			"chore":       "chore",
			"maintenance": "chore",
			"task":        "task",
		},

		// Default relation mapping
		RelationMap: map[string]string{
			"blocks":    "blocks",
			"blockedBy": "blocks",
			"duplicate": "duplicates",
			"related":   "related",
			"parent":    "parent-child",
		},
	}
}

// ParseBeadsStatus converts a status string to types.Status.
func ParseBeadsStatus(s string) types.Status {
	switch s {
	case "open":
		return types.StatusOpen
	case "in_progress", "in-progress", "inprogress":
		return types.StatusInProgress
	case "blocked":
		return types.StatusBlocked
	case "deferred":
		return types.StatusDeferred
	case "closed":
		return types.StatusClosed
	case "pinned":
		return types.StatusPinned
	default:
		return types.StatusOpen
	}
}

// ParseIssueType converts an issue type string to types.IssueType.
func ParseIssueType(s string) types.IssueType {
	switch s {
	case "bug":
		return types.TypeBug
	case "feature":
		return types.TypeFeature
	case "task":
		return types.TypeTask
	case "epic":
		return types.TypeEpic
	case "chore":
		return types.TypeChore
	default:
		return types.TypeTask
	}
}
