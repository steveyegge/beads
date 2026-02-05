package formula

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// variablePattern matches {{variable}} placeholders
var variablePattern = regexp.MustCompile(`\{\{([a-zA-Z_][a-zA-Z0-9_]*)\}\}`)

// SubstituteVariables replaces {{variable}} with values from the vars map.
// Variables not found in the map are left unchanged.
func SubstituteVariables(text string, vars map[string]string) string {
	return variablePattern.ReplaceAllStringFunc(text, func(match string) string {
		// Extract variable name from {{name}}
		name := match[2 : len(match)-2]
		if val, ok := vars[name]; ok {
			return val
		}
		return match // Leave unchanged if not found
	})
}

// ExtractVariablesFromText finds all {{variable}} patterns in text.
// Returns a deduplicated list of variable names in order of first occurrence.
func ExtractVariablesFromText(text string) []string {
	matches := variablePattern.FindAllStringSubmatch(text, -1)
	seen := make(map[string]bool)
	var vars []string
	for _, match := range matches {
		if len(match) >= 2 && !seen[match[1]] {
			vars = append(vars, match[1])
			seen[match[1]] = true
		}
	}
	return vars
}

// ExtractAllSubgraphVariables finds all variables across all issues in a subgraph.
func ExtractAllSubgraphVariables(issues []*types.Issue) []string {
	allText := ""
	for _, issue := range issues {
		allText += issue.Title + " " + issue.Description + " "
		allText += issue.Design + " " + issue.AcceptanceCriteria + " " + issue.Notes + " "
	}
	return ExtractVariablesFromText(allText)
}

// ExtractRequiredSubgraphVariables returns only variables that are defined in varDefs and don't have defaults.
// If varDefs is nil, all variables found in text are considered required (legacy behavior).
// Variables used as {{placeholder}} but NOT defined in varDefs are output placeholders
// (e.g., {{timestamp}}, {{total_count}}) and should not require input values.
func ExtractRequiredSubgraphVariables(issues []*types.Issue, varDefs map[string]VarDef) []string {
	allVars := ExtractAllSubgraphVariables(issues)

	// If no VarDefs, assume all variables are required (legacy template behavior)
	if varDefs == nil {
		return allVars
	}

	// VarDefs exists (from a cooked formula) - only declared variables matter.
	// Variables in text but NOT in VarDefs are ignored - they're documentation
	// handlebars meant for LLM agents, not formula input variables.
	var required []string
	for _, v := range allVars {
		def, exists := varDefs[v]
		if !exists {
			// Not a declared formula variable - skip (documentation handlebars)
			continue
		}
		// A declared variable is required if it has no default
		if def.Default == "" {
			required = append(required, v)
		}
	}
	return required
}

// ApplyVariableDefaults merges formula default values with provided variables.
// Returns a new map with defaults applied for any missing variables.
func ApplyVariableDefaults(vars map[string]string, varDefs map[string]VarDef) map[string]string {
	if varDefs == nil {
		return vars
	}

	result := make(map[string]string)
	for k, v := range vars {
		result[k] = v
	}

	// Apply defaults for missing variables
	for name, def := range varDefs {
		if _, exists := result[name]; !exists && def.Default != "" {
			result[name] = def.Default
		}
	}

	return result
}

// SubstituteFormulaVars substitutes {{variable}} placeholders in a formula.
// This is used in runtime mode to fully resolve the formula before output.
func SubstituteFormulaVars(f *Formula, vars map[string]string) {
	// Substitute in top-level fields
	f.Description = SubstituteVariables(f.Description, vars)

	// Substitute in all steps recursively
	SubstituteStepVars(f.Steps, vars)
}

// SubstituteStepVars recursively substitutes variables in step titles and descriptions.
func SubstituteStepVars(steps []*Step, vars map[string]string) {
	for _, step := range steps {
		step.Title = SubstituteVariables(step.Title, vars)
		step.Description = SubstituteVariables(step.Description, vars)
		if len(step.Children) > 0 {
			SubstituteStepVars(step.Children, vars)
		}
	}
}

// bondedIDPattern validates bonded IDs (alphanumeric, dash, underscore, dot)
var bondedIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// GenerateBondedID creates a custom ID for dynamically bonded molecules.
// When bonding a proto to a parent molecule, this generates IDs like:
//   - Root: parent.childref (e.g., "patrol-x7k.arm-ace")
//   - Children: parent.childref.step (e.g., "patrol-x7k.arm-ace.capture")
//
// The childRef is variable-substituted before use.
// Returns empty string if not a bonded operation (opts.ParentID empty).
func GenerateBondedID(oldID string, rootID string, opts CloneOptions) (string, error) {
	if opts.ParentID == "" {
		return "", nil // Not a bonded operation
	}

	// Substitute variables in childRef
	childRef := SubstituteVariables(opts.ChildRef, opts.Vars)

	// Validate childRef after substitution
	if childRef == "" {
		return "", fmt.Errorf("childRef is empty after variable substitution")
	}
	if !bondedIDPattern.MatchString(childRef) {
		return "", fmt.Errorf("invalid childRef '%s': must be alphanumeric, dash, underscore, or dot only", childRef)
	}

	if oldID == rootID {
		// Root issue: parent.childref
		newID := fmt.Sprintf("%s.%s", opts.ParentID, childRef)
		return newID, nil
	}

	// Child issue: parent.childref.relative
	// Extract the relative portion of the old ID (part after root)
	relativeID := GetRelativeID(oldID, rootID)
	if relativeID == "" {
		// No hierarchical relationship - use a suffix from the old ID to ensure uniqueness.
		suffix := ExtractIDSuffix(oldID)
		newID := fmt.Sprintf("%s.%s.%s", opts.ParentID, childRef, suffix)
		return newID, nil
	}

	newID := fmt.Sprintf("%s.%s.%s", opts.ParentID, childRef, relativeID)
	return newID, nil
}

// ExtractIDSuffix extracts a suffix from an ID for use when IDs aren't hierarchical.
// For "patrol-abc123", returns "abc123".
// For "bd-xyz.1", returns "1".
func ExtractIDSuffix(id string) string {
	// First try to get the part after the last dot (for hierarchical IDs)
	if lastDot := strings.LastIndex(id, "."); lastDot >= 0 {
		return id[lastDot+1:]
	}
	// Otherwise, get the part after the last dash (for prefix-hash IDs)
	if lastDash := strings.LastIndex(id, "-"); lastDash >= 0 {
		return id[lastDash+1:]
	}
	// Fallback: use the whole ID
	return id
}

// GetRelativeID extracts the relative portion of a child ID from its parent.
// For example: GetRelativeID("bd-abc.step1.sub", "bd-abc") returns "step1.sub"
// Returns empty string if oldID equals rootID or doesn't start with rootID.
func GetRelativeID(oldID, rootID string) string {
	if oldID == rootID {
		return ""
	}
	// Check if oldID starts with rootID followed by a dot
	prefix := rootID + "."
	if strings.HasPrefix(oldID, prefix) {
		return oldID[len(prefix):]
	}
	return ""
}
