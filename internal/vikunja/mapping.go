// internal/vikunja/mapping.go
package vikunja

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// MappingConfig holds configurable mappings between Vikunja and Beads.
type MappingConfig struct {
	// PriorityMap maps Vikunja priority (0-4) to Beads priority (0-4).
	// Vikunja: 0=unset, 1=low, 2=medium, 3=high, 4=urgent
	// Beads: 0=critical, 1=high, 2=medium, 3=low, 4=backlog
	PriorityMap map[string]int

	// LabelTypeMap maps Vikunja label names to Beads issue types.
	LabelTypeMap map[string]string

	// RelationMap maps Vikunja relation kinds to Beads dependency types.
	RelationMap map[string]string
}

// DefaultMappingConfig returns sensible default mappings.
func DefaultMappingConfig() *MappingConfig {
	return &MappingConfig{
		// Vikunja: 0=unset, 1=low, 2=medium, 3=high, 4=urgent
		// Beads: 0=critical, 1=high, 2=medium, 3=low, 4=backlog
		PriorityMap: map[string]int{
			"0": 4, // Unset -> Backlog
			"1": 3, // Low -> Low
			"2": 2, // Medium -> Medium
			"3": 1, // High -> High
			"4": 0, // Urgent -> Critical
		},
		LabelTypeMap: map[string]string{
			"bug":         "bug",
			"defect":      "bug",
			"feature":     "feature",
			"enhancement": "feature",
			"epic":        "epic",
			"chore":       "chore",
			"task":        "task",
		},
		RelationMap: map[string]string{
			"blocking":    "blocks",
			"blocked":     "blocks",
			"subtask":     "parent-child",
			"parenttask":  "parent-child",
			"related":     "related",
			"duplicateof": "duplicates",
			"duplicates":  "duplicates",
			"precedes":    "blocks",
			"follows":     "blocks",
			"copiedfrom":  "related",
			"copiedto":    "related",
		},
	}
}

// ConfigLoader interface for decoupling from storage layer.
type ConfigLoader interface {
	GetAllConfig() (map[string]string, error)
}

// LoadMappingConfig loads mapping configuration from a config loader.
func LoadMappingConfig(loader ConfigLoader) *MappingConfig {
	config := DefaultMappingConfig()

	if loader == nil {
		return config
	}

	allConfig, err := loader.GetAllConfig()
	if err != nil {
		return config
	}

	for key, value := range allConfig {
		// Parse priority mappings: vikunja.priority_map.<vikunja_priority>
		if strings.HasPrefix(key, "vikunja.priority_map.") {
			vikunjaPriority := strings.TrimPrefix(key, "vikunja.priority_map.")
			if beadsPriority, err := strconv.Atoi(value); err == nil {
				config.PriorityMap[vikunjaPriority] = beadsPriority
			}
		}

		// Parse label type mappings: vikunja.label_type_map.<label_name>
		if strings.HasPrefix(key, "vikunja.label_type_map.") {
			labelName := strings.ToLower(strings.TrimPrefix(key, "vikunja.label_type_map."))
			config.LabelTypeMap[labelName] = value
		}

		// Parse relation mappings: vikunja.relation_map.<relation_kind>
		if strings.HasPrefix(key, "vikunja.relation_map.") {
			relationKind := strings.ToLower(strings.TrimPrefix(key, "vikunja.relation_map."))
			config.RelationMap[relationKind] = value
		}
	}

	return config
}

// PriorityToBeads maps Vikunja priority to Beads priority.
func PriorityToBeads(vikunjaPriority int, config *MappingConfig) int {
	key := strconv.Itoa(vikunjaPriority)
	if beadsPriority, ok := config.PriorityMap[key]; ok {
		return beadsPriority
	}
	return 2 // Default to Medium
}

// PriorityToVikunja maps Beads priority to Vikunja priority.
func PriorityToVikunja(beadsPriority int, config *MappingConfig) int {
	// Build inverse map
	inverseMap := make(map[int]int)
	for vikunjaKey, beadsVal := range config.PriorityMap {
		if vikunjaVal, err := strconv.Atoi(vikunjaKey); err == nil {
			inverseMap[beadsVal] = vikunjaVal
		}
	}

	if vikunjaPriority, ok := inverseMap[beadsPriority]; ok {
		return vikunjaPriority
	}
	return 2 // Default to Medium
}

// StatusToBeads maps Vikunja done boolean to Beads status.
func StatusToBeads(done bool) types.Status {
	if done {
		return types.StatusClosed
	}
	return types.StatusOpen
}

// StatusToVikunja maps Beads status to Vikunja done boolean.
func StatusToVikunja(status types.Status) bool {
	return status == types.StatusClosed
}

// RelationToBeadsDep maps Vikunja relation kind to Beads dependency type.
func RelationToBeadsDep(relationKind string, config *MappingConfig) string {
	kind := strings.ToLower(relationKind)
	if depType, ok := config.RelationMap[kind]; ok {
		return depType
	}
	return "related" // Default fallback
}

// LabelToIssueType infers Beads issue type from Vikunja labels.
func LabelToIssueType(labels []Label, config *MappingConfig) types.IssueType {
	for _, label := range labels {
		labelName := strings.ToLower(label.Title)
		if issueType, ok := config.LabelTypeMap[labelName]; ok {
			return parseIssueType(issueType)
		}
	}
	return types.TypeTask // Default
}

// parseIssueType converts an issue type string to types.IssueType.
func parseIssueType(s string) types.IssueType {
	switch strings.ToLower(s) {
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

// TaskToBeads converts a Vikunja task to a Beads issue.
func TaskToBeads(task *Task, baseURL string, config *MappingConfig) *IssueConversion {
	issue := &types.Issue{
		Title:       task.Title,
		Description: task.Description,
		Priority:    PriorityToBeads(task.Priority, config),
		IssueType:   LabelToIssueType(task.Labels, config),
		Status:      StatusToBeads(task.Done),
		CreatedAt:   task.Created,
		UpdatedAt:   task.Updated,
	}

	// Set closed timestamp if done
	if task.Done && !task.DoneAt.IsZero() {
		issue.ClosedAt = &task.DoneAt
	}

	// Set assignee from first assignee
	if len(task.Assignees) > 0 {
		if task.Assignees[0].Email != "" {
			issue.Assignee = task.Assignees[0].Email
		} else {
			issue.Assignee = task.Assignees[0].Username
		}
	}

	// Copy labels
	for _, label := range task.Labels {
		issue.Labels = append(issue.Labels, label.Title)
	}

	// Set external ref for re-sync
	externalRef := fmt.Sprintf("%s/tasks/%d", strings.TrimSuffix(baseURL, "/api/v1"), task.ID)
	issue.ExternalRef = &externalRef

	// Extract dependencies from related_tasks
	var deps []DependencyInfo
	for relationKind, relatedTasks := range task.RelatedTasks {
		depType := RelationToBeadsDep(relationKind, config)

		for _, relatedTask := range relatedTasks {
			// Determine direction based on relation type
			switch relationKind {
			case RelationBlocked, RelationFollows:
				// This task is blocked by / follows the related task
				deps = append(deps, DependencyInfo{
					FromVikunjaID: task.ID,
					ToVikunjaID:   relatedTask.ID,
					Type:          depType,
				})
			case RelationBlocking, RelationPrecedes:
				// This task blocks / precedes the related task (inverse)
				deps = append(deps, DependencyInfo{
					FromVikunjaID: relatedTask.ID,
					ToVikunjaID:   task.ID,
					Type:          depType,
				})
			case RelationSubtask:
				// Related task is a subtask of this task
				deps = append(deps, DependencyInfo{
					FromVikunjaID: relatedTask.ID,
					ToVikunjaID:   task.ID,
					Type:          depType,
				})
			case RelationParenttask:
				// This task is a subtask of the related task
				deps = append(deps, DependencyInfo{
					FromVikunjaID: task.ID,
					ToVikunjaID:   relatedTask.ID,
					Type:          depType,
				})
			default:
				// Symmetric relations (related, duplicates, etc.)
				deps = append(deps, DependencyInfo{
					FromVikunjaID: task.ID,
					ToVikunjaID:   relatedTask.ID,
					Type:          depType,
				})
			}
		}
	}

	return &IssueConversion{
		Issue:        issue,
		Dependencies: deps,
	}
}

// BeadsToVikunjaTask converts a Beads issue to Vikunja task fields for create/update.
func BeadsToVikunjaTask(issue *types.Issue, config *MappingConfig) map[string]any {
	task := map[string]any{
		"title":       issue.Title,
		"description": issue.Description,
		"priority":    PriorityToVikunja(issue.Priority, config),
		"done":        StatusToVikunja(issue.Status),
	}

	return task
}

// BuildVikunjaExternalRef constructs the external_ref URL for a Vikunja task.
func BuildVikunjaExternalRef(baseURL string, taskID int64) string {
	// Remove /api/v1 suffix if present to get frontend URL
	frontendURL := strings.TrimSuffix(baseURL, "/api/v1")
	return fmt.Sprintf("%s/tasks/%d", frontendURL, taskID)
}

// ExtractVikunjaTaskID extracts the task ID from an external_ref URL.
func ExtractVikunjaTaskID(externalRef string) (int64, bool) {
	// Expected format: https://vikunja.example.com/tasks/123
	parts := strings.Split(externalRef, "/tasks/")
	if len(parts) != 2 {
		return 0, false
	}

	taskID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return 0, false
	}

	return taskID, true
}

// IsVikunjaExternalRef checks if an external_ref URL points to Vikunja.
func IsVikunjaExternalRef(externalRef, baseURL string) bool {
	frontendURL := strings.TrimSuffix(baseURL, "/api/v1")
	return strings.HasPrefix(externalRef, frontendURL+"/tasks/")
}

// NormalizeIssueForVikunjaHash returns a copy of the issue with fields
// normalized for content comparison with Vikunja.
func NormalizeIssueForVikunjaHash(issue *types.Issue) *types.Issue {
	normalized := *issue

	// Clear fields not synced to/from Vikunja
	normalized.ID = ""
	normalized.ExternalRef = nil
	normalized.CreatedAt = time.Time{}
	normalized.UpdatedAt = time.Time{}
	normalized.ClosedAt = nil

	return &normalized
}
