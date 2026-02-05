package formula

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// TemplateSubgraph holds a template epic and all its descendants.
type TemplateSubgraph struct {
	Root           *types.Issue              // The template epic
	Issues         []*types.Issue            // All issues in the subgraph (including root)
	Dependencies   []*types.Dependency       // All dependencies within the subgraph
	IssueMap       map[string]*types.Issue   // ID -> Issue for quick lookup
	VarDefs        map[string]VarDef         // Variable definitions from formula (for defaults)
	Phase          string                    // Recommended phase: "liquid" (pour) or "vapor" (wisp)
	RequiredSkills []string                  // Skill IDs required by the formula (creates requires-skill deps on instantiation)
	DecisionPoints []*types.DecisionPoint    // Decision points from steps with Decision configs
}

// InstantiateResult holds the result of template instantiation.
type InstantiateResult struct {
	NewEpicID string            `json:"new_epic_id"`
	IDMapping map[string]string `json:"id_mapping"` // old ID -> new ID
	Created   int               `json:"created"`    // number of issues created
}

// CloneOptions controls how the subgraph is cloned during spawn/bond.
type CloneOptions struct {
	Vars      map[string]string // Variable substitutions for {{key}} placeholders
	Assignee  string            // Assign the root epic to this agent/user
	Actor     string            // Actor performing the operation
	Ephemeral bool              // If true, spawned issues are marked for bulk deletion
	Prefix    string            // Override prefix for ID generation (bd-hobo: distinct prefixes)

	// Dynamic bonding fields (for Christmas Ornament pattern)
	ParentID string // Parent molecule ID to bond under (e.g., "patrol-x7k")
	ChildRef string // Child reference with variables (e.g., "arm-{{polecat_name}}")
}

// stepTypeToIssueType converts a formula step type string to a types.IssueType.
// Returns types.TypeTask for empty or unrecognized types.
func stepTypeToIssueType(stepType string) types.IssueType {
	switch stepType {
	case "task":
		return types.TypeTask
	case "bug":
		return types.TypeBug
	case "feature":
		return types.TypeFeature
	case "epic":
		return types.TypeEpic
	case "chore":
		return types.TypeChore
	default:
		return types.TypeTask
	}
}

// CookToSubgraph creates an in-memory TemplateSubgraph from a resolved formula.
// This is the ephemeral proto implementation - no database storage.
// The returned subgraph can be passed directly to CloneSubgraph for instantiation.
//
//nolint:unparam // error return kept for API consistency with future error handling
func CookToSubgraph(f *Formula, protoID string) (*TemplateSubgraph, error) {
	// Map step ID -> created issue
	issueMap := make(map[string]*types.Issue)

	// Collect all issues and dependencies
	var issues []*types.Issue
	var deps []*types.Dependency

	// Determine root title: use {{title}} placeholder if the variable is defined,
	// otherwise fall back to formula name (GH#852)
	rootTitle := f.Formula
	if _, hasTitle := f.Vars["title"]; hasTitle {
		rootTitle = "{{title}}"
	}

	// Determine root description: use {{desc}} placeholder if the variable is defined,
	// otherwise fall back to formula description (GH#852)
	rootDesc := f.Description
	if _, hasDesc := f.Vars["desc"]; hasDesc {
		rootDesc = "{{desc}}"
	}

	// Create root proto epic
	rootIssue := &types.Issue{
		ID:          protoID,
		Title:       rootTitle,
		Description: rootDesc,
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   types.TypeEpic,
		IsTemplate:  true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	issues = append(issues, rootIssue)
	issueMap[protoID] = rootIssue

	// Collect issues for each step (use protoID as parent for step IDs)
	// The unified CollectSteps builds both issueMap and idMapping
	idMapping := make(map[string]string)
	var decisionPoints []*types.DecisionPoint
	CollectSteps(f.Steps, protoID, idMapping, issueMap, &issues, &deps, nil, &decisionPoints) // nil labelHandler = keep labels on issues

	// Collect dependencies from depends_on using the idMapping built above
	for _, step := range f.Steps {
		CollectDependencies(step, idMapping, &deps)
	}

	return &TemplateSubgraph{
		Root:           rootIssue,
		Issues:         issues,
		Dependencies:   deps,
		IssueMap:       issueMap,
		RequiredSkills: f.RequiresSkills, // Propagate formula-level skill requirements
		DecisionPoints: decisionPoints,
	}, nil
}

// CookToSubgraphWithVars creates an in-memory subgraph with variable info attached.
func CookToSubgraphWithVars(f *Formula, protoID string, vars map[string]*VarDef) (*TemplateSubgraph, error) {
	subgraph, err := CookToSubgraph(f, protoID)
	if err != nil {
		return nil, err
	}
	// Attach variable definitions to the subgraph for default handling during pour
	// Convert from *VarDef to VarDef for simpler handling
	if vars != nil {
		subgraph.VarDefs = make(map[string]VarDef)
		for k, v := range vars {
			if v != nil {
				subgraph.VarDefs[k] = *v
			}
		}
	}
	// Attach recommended phase from formula (warn on pour of vapor formulas)
	subgraph.Phase = f.Phase
	return subgraph, nil
}

// CreateGateIssue creates a gate issue for a step with a Gate field.
// Gate issues have type=gate and block the step they guard.
// Returns the gate issue and its ID.
func CreateGateIssue(step *Step, parentID string) *types.Issue {
	if step.Gate == nil {
		return nil
	}

	// Generate gate issue ID: {parentID}.gate-{step.ID}
	gateID := fmt.Sprintf("%s.gate-%s", parentID, step.ID)

	// Build title from gate type and ID
	title := fmt.Sprintf("Gate: %s", step.Gate.Type)
	if step.Gate.ID != "" {
		title = fmt.Sprintf("Gate: %s %s", step.Gate.Type, step.Gate.ID)
	}

	// Parse timeout if specified
	var timeout time.Duration
	if step.Gate.Timeout != "" {
		if parsed, err := time.ParseDuration(step.Gate.Timeout); err == nil {
			timeout = parsed
		}
	}

	return &types.Issue{
		ID:          gateID,
		Title:       title,
		Description: fmt.Sprintf("Async gate for step %s", step.ID),
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   "gate",
		AwaitType:   step.Gate.Type,
		AwaitID:     step.Gate.ID,
		Timeout:     timeout,
		IsTemplate:  true,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

// CreateDecisionIssue creates a decision issue for a step with a Decision field.
// Decision issues have type=gate, await_type=decision, and block the step they guard.
// Returns the decision issue and the associated DecisionPoint, or (nil, nil) if no decision.
func CreateDecisionIssue(step *Step, parentID string) (*types.Issue, *types.DecisionPoint) {
	if step.Decision == nil {
		return nil, nil
	}

	// Generate decision issue ID: {parentID}.decision-{step.ID}
	decisionID := fmt.Sprintf("%s.decision-%s", parentID, step.ID)

	// Build title from prompt (truncate if too long)
	title := step.Decision.Prompt
	if len(title) > 100 {
		title = title[:97] + "..."
	}

	// Parse timeout if specified
	var timeout time.Duration
	if step.Decision.Timeout != "" {
		if parsed, err := time.ParseDuration(step.Decision.Timeout); err == nil {
			timeout = parsed
		}
	}

	optionsJSON, _ := json.Marshal(step.Decision.Options)

	now := time.Now()
	issue := &types.Issue{
		ID:          decisionID,
		Title:       title,
		Description: fmt.Sprintf("Decision point for step %s", step.ID),
		Status:      types.StatusOpen,
		Priority:    2,
		IssueType:   "gate",
		AwaitType:   "decision",
		Timeout:     timeout,
		IsTemplate:  true,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	decisionPoint := &types.DecisionPoint{
		IssueID:       decisionID,
		Prompt:        step.Decision.Prompt,
		Options:       string(optionsJSON),
		DefaultOption: step.Decision.Default,
		Iteration:     1,
		MaxIterations: 3,
		CreatedAt:     now,
	}

	return issue, decisionPoint
}

// ProcessStepToIssue converts a formula.Step to a types.Issue.
// The issue includes all fields including Labels populated from step.Labels and waits_for.
// This is the shared core logic used by both DB-persisted and in-memory cooking.
func ProcessStepToIssue(step *Step, parentID string) *types.Issue {
	// Generate issue ID (formula-name.step-id)
	issueID := fmt.Sprintf("%s.%s", parentID, step.ID)

	// Determine issue type (children override to epic)
	issueType := stepTypeToIssueType(step.Type)
	if len(step.Children) > 0 {
		issueType = types.TypeEpic
	}

	// Determine priority
	priority := 2
	if step.Priority != nil {
		priority = *step.Priority
	}

	issue := &types.Issue{
		ID:             issueID,
		Title:          step.Title, // Keep {{variables}} for substitution at pour time
		Description:    step.Description,
		Status:         types.StatusOpen,
		Priority:       priority,
		IssueType:      issueType,
		Assignee:       step.Assignee,
		IsTemplate:     true,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		SourceFormula:  step.SourceFormula,  // Source tracing
		SourceLocation: step.SourceLocation, // Source tracing
	}

	// Populate labels from step
	issue.Labels = append(issue.Labels, step.Labels...)

	// Add gate label for waits_for field
	if step.WaitsFor != "" {
		gateLabel := fmt.Sprintf("gate:%s", step.WaitsFor)
		issue.Labels = append(issue.Labels, gateLabel)
	}

	return issue
}

// CollectSteps collects issues and dependencies for steps and their children.
// This is the unified implementation used by both DB-persisted and in-memory cooking.
//
// Parameters:
//   - idMapping: step.ID -> issue.ID (always populated, used for dependency resolution)
//   - issueMap: issue.ID -> issue (optional, nil for DB path, populated for in-memory path)
//   - labelHandler: callback for each label (if nil, labels stay on issue; if set, labels are
//     extracted and issue.Labels is cleared - use for DB path)
//   - decisionPoints: optional collector for decision points (nil to skip)
func CollectSteps(steps []*Step, parentID string,
	idMapping map[string]string,
	issueMap map[string]*types.Issue,
	issues *[]*types.Issue,
	deps *[]*types.Dependency,
	labelHandler func(issueID, label string),
	decisionPoints *[]*types.DecisionPoint) {

	for _, step := range steps {
		issue := ProcessStepToIssue(step, parentID)
		*issues = append(*issues, issue)

		// Build mappings
		idMapping[step.ID] = issue.ID
		if issueMap != nil {
			issueMap[issue.ID] = issue
		}

		// Handle labels: extract via callback (DB path) or keep on issue (in-memory path)
		if labelHandler != nil {
			for _, label := range issue.Labels {
				labelHandler(issue.ID, label)
			}
			issue.Labels = nil // DB stores labels separately
		}

		// Add parent-child dependency
		*deps = append(*deps, &types.Dependency{
			IssueID:     issue.ID,
			DependsOnID: parentID,
			Type:        types.DepParentChild,
		})

		// Create gate issue if step has a Gate
		if step.Gate != nil {
			gateIssue := CreateGateIssue(step, parentID)
			*issues = append(*issues, gateIssue)

			// Add gate to mapping (use gate-{step.ID} as key)
			gateKey := fmt.Sprintf("gate-%s", step.ID)
			idMapping[gateKey] = gateIssue.ID
			if issueMap != nil {
				issueMap[gateIssue.ID] = gateIssue
			}

			// Handle gate labels if needed
			if labelHandler != nil && len(gateIssue.Labels) > 0 {
				for _, label := range gateIssue.Labels {
					labelHandler(gateIssue.ID, label)
				}
				gateIssue.Labels = nil
			}

			// Gate is a child of the parent (same level as the step)
			*deps = append(*deps, &types.Dependency{
				IssueID:     gateIssue.ID,
				DependsOnID: parentID,
				Type:        types.DepParentChild,
			})

			// Step depends on gate (gate blocks the step)
			*deps = append(*deps, &types.Dependency{
				IssueID:     issue.ID,
				DependsOnID: gateIssue.ID,
				Type:        types.DepBlocks,
			})
		}

		// Create decision issue if step has a Decision
		if step.Decision != nil {
			decisionIssue, dp := CreateDecisionIssue(step, parentID)
			*issues = append(*issues, decisionIssue)

			// Add decision to mapping (use decision-{step.ID} as key)
			decisionKey := fmt.Sprintf("decision-%s", step.ID)
			idMapping[decisionKey] = decisionIssue.ID
			if issueMap != nil {
				issueMap[decisionIssue.ID] = decisionIssue
			}

			// Handle decision labels if needed
			if labelHandler != nil && len(decisionIssue.Labels) > 0 {
				for _, label := range decisionIssue.Labels {
					labelHandler(decisionIssue.ID, label)
				}
				decisionIssue.Labels = nil
			}

			// Decision is a child of the parent (same level as the step)
			*deps = append(*deps, &types.Dependency{
				IssueID:     decisionIssue.ID,
				DependsOnID: parentID,
				Type:        types.DepParentChild,
			})

			// Step depends on decision (decision blocks the step)
			*deps = append(*deps, &types.Dependency{
				IssueID:     issue.ID,
				DependsOnID: decisionIssue.ID,
				Type:        types.DepBlocks,
			})

			// Collect decision point if collector provided
			if decisionPoints != nil {
				*decisionPoints = append(*decisionPoints, dp)
			}
		}

		// Recursively collect children
		if len(step.Children) > 0 {
			CollectSteps(step.Children, issue.ID, idMapping, issueMap, issues, deps, labelHandler, decisionPoints)
		}
	}
}

// CollectDependencies collects blocking dependencies from depends_on, needs, and waits_for fields.
// This is the shared implementation used by both DB-persisted and in-memory subgraph cooking.
func CollectDependencies(step *Step, idMapping map[string]string, deps *[]*types.Dependency) {
	issueID := idMapping[step.ID]

	// Process depends_on field
	for _, depID := range step.DependsOn {
		depIssueID, ok := idMapping[depID]
		if !ok {
			continue // Will be caught during validation
		}

		*deps = append(*deps, &types.Dependency{
			IssueID:     issueID,
			DependsOnID: depIssueID,
			Type:        types.DepBlocks,
		})
	}

	// Process needs field - simpler alias for sibling dependencies
	for _, needID := range step.Needs {
		needIssueID, ok := idMapping[needID]
		if !ok {
			continue // Will be caught during validation
		}

		*deps = append(*deps, &types.Dependency{
			IssueID:     issueID,
			DependsOnID: needIssueID,
			Type:        types.DepBlocks,
		})
	}

	// Process waits_for field - fanout gate dependency
	if step.WaitsFor != "" {
		waitsForSpec := ParseWaitsFor(step.WaitsFor)
		if waitsForSpec != nil {
			// Determine spawner ID
			spawnerStepID := waitsForSpec.SpawnerID
			if spawnerStepID == "" && len(step.Needs) > 0 {
				// Infer spawner from first need
				spawnerStepID = step.Needs[0]
			}

			if spawnerStepID != "" {
				if spawnerIssueID, ok := idMapping[spawnerStepID]; ok {
					// Create WaitsFor dependency with metadata
					meta := types.WaitsForMeta{
						Gate: waitsForSpec.Gate,
					}
					metaJSON, _ := json.Marshal(meta)

					*deps = append(*deps, &types.Dependency{
						IssueID:     issueID,
						DependsOnID: spawnerIssueID,
						Type:        types.DepWaitsFor,
						Metadata:    string(metaJSON),
					})
				}
			}
		}
	}

	// Recursively handle children
	for _, child := range step.Children {
		CollectDependencies(child, idMapping, deps)
	}
}
