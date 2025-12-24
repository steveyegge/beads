// Package formula provides parsing and validation for .formula.yaml files.
//
// Formulas are high-level workflow templates that compile down to proto beads.
// They support:
//   - Variable definitions with defaults and validation
//   - Step definitions that become issue hierarchies
//   - Composition rules for bonding formulas together
//   - Inheritance via extends
//
// Example .formula.yaml:
//
//	formula: mol-feature
//	description: Standard feature workflow
//	version: 1
//	type: workflow
//	vars:
//	  component:
//	    description: "Component name"
//	    required: true
//	steps:
//	  - id: design
//	    title: "Design {{component}}"
//	    type: task
//	  - id: implement
//	    title: "Implement {{component}}"
//	    depends_on: [design]
package formula

import (
	"fmt"
	"strings"
)

// FormulaType categorizes formulas by their purpose.
type FormulaType string

const (
	// TypeWorkflow is a standard workflow template (sequence of steps).
	TypeWorkflow FormulaType = "workflow"

	// TypeExpansion is a macro that expands into multiple steps.
	// Used for common patterns like "test + lint + build".
	TypeExpansion FormulaType = "expansion"

	// TypeAspect is a cross-cutting concern that can be applied to other formulas.
	// Examples: add logging steps, add approval gates.
	TypeAspect FormulaType = "aspect"
)

// IsValid checks if the formula type is recognized.
func (t FormulaType) IsValid() bool {
	switch t {
	case TypeWorkflow, TypeExpansion, TypeAspect:
		return true
	}
	return false
}

// Formula is the root structure for .formula.yaml files.
type Formula struct {
	// Formula is the unique identifier/name for this formula.
	// Convention: mol-<name> for molecules, exp-<name> for expansions.
	Formula string `yaml:"formula" json:"formula"`

	// Description explains what this formula does.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Version is the schema version (currently 1).
	Version int `yaml:"version" json:"version"`

	// Type categorizes the formula: workflow, expansion, or aspect.
	Type FormulaType `yaml:"type" json:"type"`

	// Extends is a list of parent formulas to inherit from.
	// The child formula inherits all vars, steps, and compose rules.
	// Child definitions override parent definitions with the same ID.
	Extends []string `yaml:"extends,omitempty" json:"extends,omitempty"`

	// Vars defines template variables with defaults and validation.
	Vars map[string]*VarDef `yaml:"vars,omitempty" json:"vars,omitempty"`

	// Steps defines the work items to create.
	Steps []*Step `yaml:"steps,omitempty" json:"steps,omitempty"`

	// Compose defines composition/bonding rules.
	Compose *ComposeRules `yaml:"compose,omitempty" json:"compose,omitempty"`

	// Source tracks where this formula was loaded from (set by parser).
	Source string `yaml:"-" json:"source,omitempty"`
}

// VarDef defines a template variable with optional validation.
type VarDef struct {
	// Description explains what this variable is for.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Default is the value to use if not provided.
	Default string `yaml:"default,omitempty" json:"default,omitempty"`

	// Required indicates the variable must be provided (no default).
	Required bool `yaml:"required,omitempty" json:"required,omitempty"`

	// Enum lists the allowed values (if non-empty).
	Enum []string `yaml:"enum,omitempty" json:"enum,omitempty"`

	// Pattern is a regex pattern the value must match.
	Pattern string `yaml:"pattern,omitempty" json:"pattern,omitempty"`

	// Type is the expected value type: string (default), int, bool.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
}

// Step defines a work item to create when the formula is instantiated.
type Step struct {
	// ID is the unique identifier within this formula.
	// Used for dependency references and bond points.
	ID string `yaml:"id" json:"id"`

	// Title is the issue title (supports {{variable}} substitution).
	Title string `yaml:"title" json:"title"`

	// Description is the issue description (supports substitution).
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// Type is the issue type: task, bug, feature, epic, chore.
	Type string `yaml:"type,omitempty" json:"type,omitempty"`

	// Priority is the issue priority (0-4).
	Priority *int `yaml:"priority,omitempty" json:"priority,omitempty"`

	// Labels are applied to the created issue.
	Labels []string `yaml:"labels,omitempty" json:"labels,omitempty"`

	// DependsOn lists step IDs this step blocks on (within the formula).
	DependsOn []string `yaml:"depends_on,omitempty" json:"depends_on,omitempty"`

	// Assignee is the default assignee (supports substitution).
	Assignee string `yaml:"assignee,omitempty" json:"assignee,omitempty"`

	// Expand references an expansion formula to inline here.
	// When set, this step is replaced by the expansion's steps.
	Expand string `yaml:"expand,omitempty" json:"expand,omitempty"`

	// ExpandVars are variable overrides for the expansion.
	ExpandVars map[string]string `yaml:"expand_vars,omitempty" json:"expand_vars,omitempty"`

	// Condition makes this step optional based on a variable.
	// Format: "{{var}}" (truthy) or "{{var}} == value".
	Condition string `yaml:"condition,omitempty" json:"condition,omitempty"`

	// Children are nested steps (for creating epic hierarchies).
	Children []*Step `yaml:"children,omitempty" json:"children,omitempty"`

	// Gate defines an async wait condition for this step.
	Gate *Gate `yaml:"gate,omitempty" json:"gate,omitempty"`
}

// Gate defines an async wait condition (integrates with bd-udsi).
type Gate struct {
	// Type is the condition type: gh:run, gh:pr, timer, human, mail.
	Type string `yaml:"type" json:"type"`

	// ID is the condition identifier (e.g., workflow name for gh:run).
	ID string `yaml:"id,omitempty" json:"id,omitempty"`

	// Timeout is how long to wait before escalation (e.g., "1h", "24h").
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// ComposeRules define how formulas can be bonded together.
type ComposeRules struct {
	// BondPoints are named locations where other formulas can attach.
	BondPoints []*BondPoint `yaml:"bond_points,omitempty" json:"bond_points,omitempty"`

	// Hooks are automatic attachments triggered by labels or conditions.
	Hooks []*Hook `yaml:"hooks,omitempty" json:"hooks,omitempty"`
}

// BondPoint is a named attachment site for composition.
type BondPoint struct {
	// ID is the unique identifier for this bond point.
	ID string `yaml:"id" json:"id"`

	// Description explains what should be attached here.
	Description string `yaml:"description,omitempty" json:"description,omitempty"`

	// AfterStep is the step ID after which to attach.
	// Mutually exclusive with BeforeStep.
	AfterStep string `yaml:"after_step,omitempty" json:"after_step,omitempty"`

	// BeforeStep is the step ID before which to attach.
	// Mutually exclusive with AfterStep.
	BeforeStep string `yaml:"before_step,omitempty" json:"before_step,omitempty"`

	// Parallel makes attached steps run in parallel with the anchor step.
	Parallel bool `yaml:"parallel,omitempty" json:"parallel,omitempty"`
}

// Hook defines automatic formula attachment based on conditions.
type Hook struct {
	// Trigger is what activates this hook.
	// Formats: "label:security", "type:bug", "priority:0-1".
	Trigger string `yaml:"trigger" json:"trigger"`

	// Attach is the formula to attach when triggered.
	Attach string `yaml:"attach" json:"attach"`

	// At is the bond point to attach at (default: end).
	At string `yaml:"at,omitempty" json:"at,omitempty"`

	// Vars are variable overrides for the attached formula.
	Vars map[string]string `yaml:"vars,omitempty" json:"vars,omitempty"`
}

// Validate checks the formula for structural errors.
func (f *Formula) Validate() error {
	var errs []string

	if f.Formula == "" {
		errs = append(errs, "formula: name is required")
	}

	if f.Version < 1 {
		errs = append(errs, "version: must be >= 1")
	}

	if f.Type != "" && !f.Type.IsValid() {
		errs = append(errs, fmt.Sprintf("type: invalid value %q (must be workflow, expansion, or aspect)", f.Type))
	}

	// Validate variables
	for name, v := range f.Vars {
		if name == "" {
			errs = append(errs, "vars: variable name cannot be empty")
			continue
		}
		if v.Required && v.Default != "" {
			errs = append(errs, fmt.Sprintf("vars.%s: cannot have both required:true and default", name))
		}
	}

	// Validate steps
	stepIDs := make(map[string]bool)
	for i, step := range f.Steps {
		if step.ID == "" {
			errs = append(errs, fmt.Sprintf("steps[%d]: id is required", i))
			continue
		}
		if stepIDs[step.ID] {
			errs = append(errs, fmt.Sprintf("steps[%d]: duplicate id %q", i, step.ID))
		}
		stepIDs[step.ID] = true

		if step.Title == "" && step.Expand == "" {
			errs = append(errs, fmt.Sprintf("steps[%d] (%s): title is required (unless using expand)", i, step.ID))
		}

		// Validate priority range
		if step.Priority != nil && (*step.Priority < 0 || *step.Priority > 4) {
			errs = append(errs, fmt.Sprintf("steps[%d] (%s): priority must be 0-4", i, step.ID))
		}

		// Collect child IDs (for dependency validation)
		collectChildIDs(step.Children, stepIDs, &errs, fmt.Sprintf("steps[%d]", i))
	}

	// Validate step dependencies reference valid IDs
	for i, step := range f.Steps {
		for _, dep := range step.DependsOn {
			if !stepIDs[dep] {
				errs = append(errs, fmt.Sprintf("steps[%d] (%s): depends_on references unknown step %q", i, step.ID, dep))
			}
		}
	}

	// Validate compose rules
	if f.Compose != nil {
		for i, bp := range f.Compose.BondPoints {
			if bp.ID == "" {
				errs = append(errs, fmt.Sprintf("compose.bond_points[%d]: id is required", i))
			}
			if bp.AfterStep != "" && bp.BeforeStep != "" {
				errs = append(errs, fmt.Sprintf("compose.bond_points[%d] (%s): cannot have both after_step and before_step", i, bp.ID))
			}
			if bp.AfterStep != "" && !stepIDs[bp.AfterStep] {
				errs = append(errs, fmt.Sprintf("compose.bond_points[%d] (%s): after_step references unknown step %q", i, bp.ID, bp.AfterStep))
			}
			if bp.BeforeStep != "" && !stepIDs[bp.BeforeStep] {
				errs = append(errs, fmt.Sprintf("compose.bond_points[%d] (%s): before_step references unknown step %q", i, bp.ID, bp.BeforeStep))
			}
		}

		for i, hook := range f.Compose.Hooks {
			if hook.Trigger == "" {
				errs = append(errs, fmt.Sprintf("compose.hooks[%d]: trigger is required", i))
			}
			if hook.Attach == "" {
				errs = append(errs, fmt.Sprintf("compose.hooks[%d]: attach is required", i))
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("formula validation failed:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// collectChildIDs recursively collects step IDs from children.
func collectChildIDs(children []*Step, ids map[string]bool, errs *[]string, prefix string) {
	for i, child := range children {
		childPrefix := fmt.Sprintf("%s.children[%d]", prefix, i)
		if child.ID == "" {
			*errs = append(*errs, fmt.Sprintf("%s: id is required", childPrefix))
			continue
		}
		if ids[child.ID] {
			*errs = append(*errs, fmt.Sprintf("%s: duplicate id %q", childPrefix, child.ID))
		}
		ids[child.ID] = true

		if child.Title == "" && child.Expand == "" {
			*errs = append(*errs, fmt.Sprintf("%s (%s): title is required", childPrefix, child.ID))
		}

		collectChildIDs(child.Children, ids, errs, childPrefix)
	}
}

// GetRequiredVars returns the names of all required variables.
func (f *Formula) GetRequiredVars() []string {
	var required []string
	for name, v := range f.Vars {
		if v.Required {
			required = append(required, name)
		}
	}
	return required
}

// GetStepByID finds a step by its ID (searches recursively).
func (f *Formula) GetStepByID(id string) *Step {
	for _, step := range f.Steps {
		if found := findStepByID(step, id); found != nil {
			return found
		}
	}
	return nil
}

// findStepByID recursively searches for a step by ID.
func findStepByID(step *Step, id string) *Step {
	if step.ID == id {
		return step
	}
	for _, child := range step.Children {
		if found := findStepByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

// GetBondPoint finds a bond point by ID.
func (f *Formula) GetBondPoint(id string) *BondPoint {
	if f.Compose == nil {
		return nil
	}
	for _, bp := range f.Compose.BondPoints {
		if bp.ID == id {
			return bp
		}
	}
	return nil
}
