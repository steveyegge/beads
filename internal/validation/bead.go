package validation

import (
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// ParsePriority extracts and validates a priority value from content.
// Supports both numeric (0-4) and P-prefix format (P0-P4).
// Returns the parsed priority (0-4) or -1 if invalid.
func ParsePriority(content string) int {
	content = strings.TrimSpace(content)
	
	// Handle "P1", "P0", etc. format
	if strings.HasPrefix(strings.ToUpper(content), "P") {
		content = content[1:] // Strip the "P" prefix
	}
	
	var p int
	if _, err := fmt.Sscanf(content, "%d", &p); err == nil && p >= 0 && p <= 4 {
		return p
	}
	return -1 // Invalid
}

// ParseIssueType extracts and validates an issue type from content.
// Returns the validated type or error if invalid.
func ParseIssueType(content string) (types.IssueType, error) {
	issueType := types.IssueType(strings.TrimSpace(content))

	// Use the canonical IsValid() from types package
	if !issueType.IsValid() {
		return types.TypeTask, fmt.Errorf("invalid issue type: %s", content)
	}

	return issueType, nil
}

// ValidatePriority parses and validates a priority string.
// Returns the parsed priority (0-4) or an error if invalid.
// Supports both numeric (0-4) and P-prefix format (P0-P4).
func ValidatePriority(priorityStr string) (int, error) {
	priority := ParsePriority(priorityStr)
	if priority == -1 {
		return -1, fmt.Errorf("invalid priority %q (expected 0-4 or P0-P4, not words like high/medium/low)", priorityStr)
	}
	return priority, nil
}

// ValidateIDFormat validates that an ID has the correct format.
// Supports: prefix-number (bd-42), prefix-hash (bd-a3f8e9), or hierarchical (bd-a3f8e9.1)
// Returns the prefix part or an error if invalid.
func ValidateIDFormat(id string) (string, error) {
	if id == "" {
		return "", nil
	}

	// Must contain hyphen
	if !strings.Contains(id, "-") {
		return "", fmt.Errorf("invalid ID format '%s' (expected format: prefix-hash or prefix-hash.number, e.g., 'bd-a3f8e9' or 'bd-a3f8e9.1')", id)
	}

	// Extract prefix (before the first hyphen)
	hyphenIdx := strings.Index(id, "-")
	prefix := id[:hyphenIdx]

	return prefix, nil
}

// ValidatePrefix checks that the requested prefix matches the database prefix.
// Returns an error if they don't match (unless force is true).
func ValidatePrefix(requestedPrefix, dbPrefix string, force bool) error {
	if force || dbPrefix == "" || dbPrefix == requestedPrefix {
		return nil
	}

	return fmt.Errorf("prefix mismatch: database uses '%s' but you specified '%s' (use --force to override)", dbPrefix, requestedPrefix)
}

// ValidAgentRoles are the known agent role types for ID pattern validation
var ValidAgentRoles = []string{
	"mayor",    // Town-level: gt-mayor
	"deacon",   // Town-level: gt-deacon
	"witness",  // Per-rig: gt-<rig>-witness
	"refinery", // Per-rig: gt-<rig>-refinery
	"crew",     // Per-rig with name: gt-<rig>-crew-<name>
	"polecat",  // Per-rig with name: gt-<rig>-polecat-<name>
}

// TownLevelRoles are agent roles that don't have a rig
var TownLevelRoles = []string{"mayor", "deacon"}

// RigLevelRoles are agent roles that have a rig but no name
var RigLevelRoles = []string{"witness", "refinery"}

// NamedRoles are agent roles that include a worker name
var NamedRoles = []string{"crew", "polecat"}

// isValidRole checks if a string is a valid agent role
func isValidRole(s string) bool {
	for _, r := range ValidAgentRoles {
		if s == r {
			return true
		}
	}
	return false
}

// isTownLevelRole checks if a role is a town-level role (no rig)
func isTownLevelRole(s string) bool {
	for _, r := range TownLevelRoles {
		if s == r {
			return true
		}
	}
	return false
}

// isRigLevelRole checks if a role is a rig-level singleton role
func isRigLevelRole(s string) bool {
	for _, r := range RigLevelRoles {
		if s == r {
			return true
		}
	}
	return false
}

// isNamedRole checks if a role requires a worker name
func isNamedRole(s string) bool {
	for _, r := range NamedRoles {
		if s == r {
			return true
		}
	}
	return false
}

// ValidateAgentID validates that an agent ID follows the expected pattern.
// Canonical format: prefix-rig-role-name
// Patterns:
//   - Town-level: <prefix>-<role> (e.g., gt-mayor, bd-deacon)
//   - Per-rig singleton: <prefix>-<rig>-<role> (e.g., gt-gastown-witness)
//   - Per-rig named: <prefix>-<rig>-<role>-<name> (e.g., gt-gastown-polecat-nux)
//
// The prefix can be any rig's configured prefix (gt-, bd-, etc.).
// Returns nil if the ID is valid, or an error describing the issue.
func ValidateAgentID(id string) error {
	if id == "" {
		return fmt.Errorf("agent ID is required")
	}

	// Must contain a hyphen to have a prefix
	hyphenIdx := strings.Index(id, "-")
	if hyphenIdx <= 0 {
		return fmt.Errorf("agent ID must have a prefix followed by '-' (got %q)", id)
	}

	// Split into parts after the prefix
	rest := id[hyphenIdx+1:] // Skip "<prefix>-"
	parts := strings.Split(rest, "-")
	if len(parts) < 1 || parts[0] == "" {
		return fmt.Errorf("agent ID must include content after prefix (got %q)", id)
	}

	// Case 1: Town-level roles (gt-mayor, gt-deacon)
	if len(parts) == 1 {
		role := parts[0]
		if isTownLevelRole(role) {
			return nil // Valid town-level agent
		}
		if isValidRole(role) {
			return fmt.Errorf("agent role %q requires rig: <prefix>-<rig>-%s (got %q)", role, role, id)
		}
		return fmt.Errorf("invalid agent role %q (valid: %s)", role, strings.Join(ValidAgentRoles, ", "))
	}

	// Case 2: Rig-level roles (<prefix>-<rig>-witness, <prefix>-<rig>-refinery)
	if len(parts) == 2 {
		rig, role := parts[0], parts[1]

		if isRigLevelRole(role) {
			if rig == "" {
				return fmt.Errorf("rig name cannot be empty in %q", id)
			}
			return nil // Valid rig-level singleton agent
		}

		if isNamedRole(role) {
			return fmt.Errorf("agent role %q requires name: <prefix>-<rig>-%s-<name> (got %q)", role, role, id)
		}

		if isTownLevelRole(role) {
			return fmt.Errorf("town-level agent %q cannot have rig suffix (expected <prefix>-%s, got %q)", role, role, id)
		}

		// First part might be a rig name with second part being something invalid
		return fmt.Errorf("invalid agent format: expected <prefix>-<rig>-<role>[-<name>] where role is one of: %s (got %q)", strings.Join(ValidAgentRoles, ", "), id)
	}

	// Case 3: Named roles (<prefix>-<rig>-crew-<name>, <prefix>-<rig>-polecat-<name>)
	if len(parts) >= 3 {
		rig, role := parts[0], parts[1]
		name := strings.Join(parts[2:], "-") // Allow hyphens in names

		if isNamedRole(role) {
			if rig == "" {
				return fmt.Errorf("rig name cannot be empty in %q", id)
			}
			if name == "" {
				return fmt.Errorf("agent name cannot be empty in %q", id)
			}
			return nil // Valid named agent
		}

		if isRigLevelRole(role) {
			return fmt.Errorf("agent role %q cannot have name suffix (expected <prefix>-<rig>-%s, got %q)", role, role, id)
		}

		if isTownLevelRole(role) {
			return fmt.Errorf("town-level agent %q cannot have rig/name suffixes (expected <prefix>-%s, got %q)", role, role, id)
		}

		return fmt.Errorf("invalid agent format: expected <prefix>-<rig>-<role>-<name> where role is one of: %s (got %q)", strings.Join(NamedRoles, ", "), id)
	}

	return fmt.Errorf("invalid agent ID format: %q", id)
}
