package shortcut

import (
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/idgen"
	"github.com/steveyegge/beads/internal/types"
)

// IDGenerationOptions configures Shortcut hash ID generation.
type IDGenerationOptions struct {
	BaseLength int             // Starting hash length (3-8)
	MaxLength  int             // Maximum hash length (3-8)
	UsedIDs    map[string]bool // Pre-populated set to avoid collisions (e.g., DB IDs)
}

// MappingConfig holds configurable mappings between Shortcut and Beads.
type MappingConfig struct {
	// PriorityMap maps Shortcut priority strings to Beads priority (0-4).
	PriorityMap map[string]int

	// StateMap maps Shortcut state names to Beads statuses.
	StateMap map[string]string

	// TypeMap maps Shortcut story types to Beads issue types.
	TypeMap map[string]string

	// RelationMap maps Shortcut story link verbs to Beads dependency types.
	RelationMap map[string]string

	// Organization name for building URLs
	Organization string
}

// DefaultMappingConfig returns sensible default mappings.
func DefaultMappingConfig() *MappingConfig {
	return &MappingConfig{
		// Shortcut priority: none, low, medium, high, urgent (can be missing)
		// Beads priority: 0=critical, 1=high, 2=medium, 3=low, 4=backlog
		PriorityMap: map[string]int{
			"":       4, // No priority -> Backlog
			"none":   4, // No priority -> Backlog
			"low":    3, // Low -> Low
			"medium": 2, // Medium -> Medium
			"high":   1, // High -> High
			"urgent": 0, // Urgent -> Critical
		},
		// Default state mapping (users should configure their workflow states)
		StateMap: map[string]string{
			"unstarted": "open",
			"started":   "in_progress",
			"done":      "closed",
		},
		// Shortcut story types to Beads issue types
		TypeMap: map[string]string{
			"feature": "feature",
			"bug":     "bug",
			"chore":   "task",
		},
		// Shortcut story link verbs to Beads dependency types
		RelationMap: map[string]string{
			"blocks":        "blocks",
			"is blocked by": "blocks", // Inverse: we're blocked by the related story
			"duplicates":    "duplicates",
			"relates to":    "related",
		},
	}
}

// ConfigLoader is an interface for loading configuration values.
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
		// Parse priority mappings: shortcut.priority_map.<priority>
		if strings.HasPrefix(key, "shortcut.priority_map.") {
			priorityKey := strings.ToLower(strings.TrimPrefix(key, "shortcut.priority_map."))
			if beadsPriority, err := parseIntValue(value); err == nil {
				config.PriorityMap[priorityKey] = beadsPriority
			}
		}

		// Parse state mappings: shortcut.state_map.<state_name>
		if strings.HasPrefix(key, "shortcut.state_map.") {
			stateKey := strings.ToLower(strings.TrimPrefix(key, "shortcut.state_map."))
			config.StateMap[stateKey] = value
		}

		// Parse type mappings: shortcut.type_map.<story_type>
		if strings.HasPrefix(key, "shortcut.type_map.") {
			typeKey := strings.ToLower(strings.TrimPrefix(key, "shortcut.type_map."))
			config.TypeMap[typeKey] = value
		}

		// Parse relation mappings: shortcut.relation_map.<verb>
		if strings.HasPrefix(key, "shortcut.relation_map.") {
			relKey := strings.ToLower(strings.TrimPrefix(key, "shortcut.relation_map."))
			config.RelationMap[relKey] = value
		}

		// Organization name
		if key == "shortcut.organization" {
			config.Organization = value
		}
	}

	return config
}

func parseIntValue(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// GenerateIssueIDs generates unique hash-based IDs for issues that don't have one.
func GenerateIssueIDs(issues []*types.Issue, prefix, creator string, opts IDGenerationOptions) error {
	usedIDs := opts.UsedIDs
	if usedIDs == nil {
		usedIDs = make(map[string]bool)
	}

	baseLength := opts.BaseLength
	if baseLength == 0 {
		baseLength = 6
	}
	maxLength := opts.MaxLength
	if maxLength == 0 {
		maxLength = 8
	}
	if baseLength < 3 {
		baseLength = 3
	}
	if maxLength > 8 {
		maxLength = 8
	}
	if baseLength > maxLength {
		baseLength = maxLength
	}

	// First pass: record existing IDs
	for _, issue := range issues {
		if issue.ID != "" {
			usedIDs[issue.ID] = true
		}
	}

	// Second pass: generate IDs for issues without one
	for _, issue := range issues {
		if issue.ID != "" {
			continue
		}

		var generated bool
		for length := baseLength; length <= maxLength && !generated; length++ {
			for nonce := 0; nonce < 10; nonce++ {
				candidate := idgen.GenerateHashID(
					prefix,
					issue.Title,
					issue.Description,
					creator,
					issue.CreatedAt,
					length,
					nonce,
				)

				if !usedIDs[candidate] {
					issue.ID = candidate
					usedIDs[candidate] = true
					generated = true
					break
				}
			}
		}

		if !generated {
			return fmt.Errorf("failed to generate unique ID for issue '%s'", issue.Title)
		}
	}

	return nil
}

// PriorityToBeads maps Shortcut priority string to Beads priority (0-4).
func PriorityToBeads(shortcutPriority string, config *MappingConfig) int {
	key := strings.ToLower(shortcutPriority)
	if beadsPriority, ok := config.PriorityMap[key]; ok {
		return beadsPriority
	}
	return 2 // Default to Medium
}

// PriorityToShortcut maps Beads priority (0-4) to Shortcut priority string.
func PriorityToShortcut(beadsPriority int, config *MappingConfig) string {
	// Build inverse map
	inverseMap := make(map[int]string)
	for scKey, beadsVal := range config.PriorityMap {
		if scKey != "" && scKey != "none" {
			inverseMap[beadsVal] = scKey
		}
	}

	if scPriority, ok := inverseMap[beadsPriority]; ok {
		return scPriority
	}
	return "medium" // Default
}

// StateToBeadsStatus maps Shortcut workflow state to Beads status.
func StateToBeadsStatus(state *WorkflowState, config *MappingConfig) types.Status {
	if state == nil {
		return types.StatusOpen
	}

	// First try state type (unstarted, started, done)
	stateType := strings.ToLower(state.Type)
	if statusStr, ok := config.StateMap[stateType]; ok {
		return ParseBeadsStatus(statusStr)
	}

	// Then try state name
	stateName := strings.ToLower(state.Name)
	if statusStr, ok := config.StateMap[stateName]; ok {
		return ParseBeadsStatus(statusStr)
	}

	// Default based on type
	switch stateType {
	case "unstarted":
		return types.StatusOpen
	case "started":
		return types.StatusInProgress
	case "done":
		return types.StatusClosed
	default:
		return types.StatusOpen
	}
}

// ParseBeadsStatus converts a status string to types.Status.
func ParseBeadsStatus(s string) types.Status {
	switch strings.ToLower(s) {
	case "open":
		return types.StatusOpen
	case "in_progress", "in-progress", "inprogress":
		return types.StatusInProgress
	case "blocked":
		return types.StatusBlocked
	case "closed":
		return types.StatusClosed
	case "deferred":
		return types.StatusDeferred
	default:
		return types.StatusOpen
	}
}

// TypeToBeads maps Shortcut story type to Beads issue type.
func TypeToBeads(storyType string, config *MappingConfig) types.IssueType {
	key := strings.ToLower(storyType)
	if issueType, ok := config.TypeMap[key]; ok {
		return ParseIssueType(issueType)
	}
	return types.TypeTask
}

// TypeToShortcut maps Beads issue type to Shortcut story type.
func TypeToShortcut(issueType types.IssueType) string {
	switch issueType {
	case types.TypeBug:
		return "bug"
	case types.TypeFeature:
		return "feature"
	case types.TypeTask, types.TypeChore:
		return "chore"
	default:
		return "feature"
	}
}

// ParseIssueType converts an issue type string to types.IssueType.
func ParseIssueType(s string) types.IssueType {
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

// RelationToBeadsDep converts a Shortcut story link verb to Beads dependency type.
func RelationToBeadsDep(verb string, config *MappingConfig) string {
	key := strings.ToLower(verb)
	if depType, ok := config.RelationMap[key]; ok {
		return depType
	}
	return "related"
}

// StoryToBeads converts a Shortcut story to a Beads issue.
func StoryToBeads(story *Story, stateCache *StateCache, config *MappingConfig) *StoryConversion {
	createdAt, err := time.Parse(time.RFC3339, story.CreatedAt)
	if err != nil {
		createdAt = time.Now()
	}

	updatedAt, err := time.Parse(time.RFC3339, story.UpdatedAt)
	if err != nil {
		updatedAt = time.Now()
	}

	issue := &types.Issue{
		Title:       story.Name,
		Description: story.Description,
		Priority:    PriorityToBeads(story.Priority, config),
		IssueType:   TypeToBeads(story.StoryType, config),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}

	// Map workflow state to status
	if stateCache != nil {
		if state, ok := stateCache.StatesByID[story.WorkflowStateID]; ok {
			issue.Status = StateToBeadsStatus(&state, config)
		}
	}

	// Handle completed timestamp
	if story.CompletedAt != nil && *story.CompletedAt != "" {
		completedAt, err := time.Parse(time.RFC3339, *story.CompletedAt)
		if err == nil {
			issue.ClosedAt = &completedAt
		}
	}

	// Set assignee (first owner)
	if len(story.OwnerIDs) > 0 {
		issue.Assignee = story.OwnerIDs[0]
	}

	// Copy labels
	for _, label := range story.Labels {
		issue.Labels = append(issue.Labels, label.Name)
	}

	// Set external reference
	externalRef := story.AppURL
	if canonical, ok := CanonicalizeShortcutExternalRef(externalRef); ok {
		externalRef = canonical
	}
	issue.ExternalRef = &externalRef

	// Collect dependencies
	var deps []DependencyInfo

	// Map story links to dependencies
	for _, link := range story.StoryLinks {
		depType := RelationToBeadsDep(link.Verb, config)

		// Handle different link types
		switch strings.ToLower(link.Verb) {
		case "is blocked by":
			// This story is blocked by the object story
			deps = append(deps, DependencyInfo{
				FromStoryID: story.ID,
				ToStoryID:   link.ObjectID,
				Type:        depType,
			})
		case "blocks":
			// This story blocks the object story
			deps = append(deps, DependencyInfo{
				FromStoryID: link.ObjectID,
				ToStoryID:   story.ID,
				Type:        depType,
			})
		default:
			// For relates to, duplicates, etc.
			deps = append(deps, DependencyInfo{
				FromStoryID: story.ID,
				ToStoryID:   link.ObjectID,
				Type:        depType,
			})
		}
	}

	return &StoryConversion{
		Issue:        issue,
		Dependencies: deps,
	}
}

// BeadsToStoryParams converts a Beads issue to Shortcut story creation parameters.
func BeadsToStoryParams(issue *types.Issue, stateCache *StateCache, config *MappingConfig) *CreateStoryParams {
	params := &CreateStoryParams{
		Name:        issue.Title,
		Description: issue.Description,
		StoryType:   TypeToShortcut(issue.IssueType),
	}

	// Set workflow state if we have a state cache
	if stateCache != nil {
		params.WorkflowStateID = stateCache.FindStateForBeadsStatus(string(issue.Status))
	}

	// Set owner
	if issue.Assignee != "" {
		params.OwnerIDs = []string{issue.Assignee}
	}

	// Copy labels
	for _, labelName := range issue.Labels {
		params.Labels = append(params.Labels, Label{Name: labelName})
	}

	return params
}

// BeadsToStoryUpdateParams converts a Beads issue to Shortcut story update parameters.
func BeadsToStoryUpdateParams(issue *types.Issue, stateCache *StateCache, config *MappingConfig) *UpdateStoryParams {
	params := &UpdateStoryParams{
		Name:        &issue.Title,
		Description: &issue.Description,
	}

	storyType := TypeToShortcut(issue.IssueType)
	params.StoryType = &storyType

	// Set workflow state if we have a state cache
	if stateCache != nil {
		stateID := stateCache.FindStateForBeadsStatus(string(issue.Status))
		params.WorkflowStateID = &stateID
	}

	// Set owner
	if issue.Assignee != "" {
		params.OwnerIDs = []string{issue.Assignee}
	}

	return params
}

// BuildShortcutToLocalUpdates creates an updates map from a Shortcut story
// to apply to a local Beads issue. This is used when Shortcut wins a conflict.
func BuildShortcutToLocalUpdates(story *Story, stateCache *StateCache, config *MappingConfig) map[string]interface{} {
	updates := make(map[string]interface{})

	updates["title"] = story.Name
	updates["description"] = story.Description
	updates["priority"] = PriorityToBeads(story.Priority, config)
	updates["issue_type"] = string(TypeToBeads(story.StoryType, config))

	// Map status
	if stateCache != nil {
		if state, ok := stateCache.StatesByID[story.WorkflowStateID]; ok {
			updates["status"] = string(StateToBeadsStatus(&state, config))
		}
	}

	// Set assignee (first owner)
	if len(story.OwnerIDs) > 0 {
		updates["assignee"] = story.OwnerIDs[0]
	} else {
		updates["assignee"] = ""
	}

	// Update labels
	var labels []string
	for _, label := range story.Labels {
		labels = append(labels, label.Name)
	}
	updates["labels"] = labels

	// Update timestamps
	if updatedAt, err := time.Parse(time.RFC3339, story.UpdatedAt); err == nil {
		updates["updated_at"] = updatedAt
	}

	if story.CompletedAt != nil && *story.CompletedAt != "" {
		if closedAt, err := time.Parse(time.RFC3339, *story.CompletedAt); err == nil {
			updates["closed_at"] = closedAt
		}
	}

	return updates
}
