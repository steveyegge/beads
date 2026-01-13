// Package namespace implements branch-based issue namespacing for beads.
// It provides the IssueID type that separates identity (project:branch-hash)
// from source location (sources.yaml configuration).
package namespace

import (
	"fmt"
	"regexp"
	"strings"
)

// IssueID represents a fully-qualified issue identifier.
// Format: {project}:{branch}-{hash} or {project}:{hash} (main branch implied)
// Examples:
//   - beads:a3f2 (main branch)
//   - beads:fix-auth-a3f2 (feature branch)
//   - other-project:main-b7c9 (different project)
type IssueID struct {
	Project string // "beads", "other-project", etc.
	Branch  string // "main", "fix-auth", etc. (defaults to "main")
	Hash    string // "a3f2", "b7c9" (4-8 char base36)
}

// String returns the fully-qualified form: project:branch-hash or project:hash
func (id IssueID) String() string {
	if id.Branch == "" || id.Branch == "main" {
		return fmt.Sprintf("%s:%s", id.Project, id.Hash)
	}
	return fmt.Sprintf("%s:%s-%s", id.Project, id.Branch, id.Hash)
}

// Short returns the short form relative to context: branch-hash or just hash
func (id IssueID) Short() string {
	if id.Branch == "" || id.Branch == "main" {
		return id.Hash
	}
	return fmt.Sprintf("%s-%s", id.Branch, id.Hash)
}

// ShortWithBranch returns the short form with branch always shown: branch-hash
func (id IssueID) ShortWithBranch() string {
	if id.Branch == "" {
		return fmt.Sprintf("main-%s", id.Hash)
	}
	return fmt.Sprintf("%s-%s", id.Branch, id.Hash)
}

// ParseIssueID parses an issue ID string into its components.
// Supports multiple formats:
//   - hash (4-8 chars): resolves with current context (branch, project)
//   - branch-hash: resolves with current context (project)
//   - project:hash: uses specified project, main branch
//   - project:branch-hash: fully qualified
//
// Returns the parsed IssueID with defaults applied based on context.
func ParseIssueID(input string, contextProject, contextBranch string) (IssueID, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return IssueID{}, fmt.Errorf("empty issue ID")
	}

	result := IssueID{
		Project: contextProject,
		Branch:  contextBranch,
	}

	// Check if input contains colon (explicit project)
	hasExplicitProject := strings.Contains(input, ":")
	if hasExplicitProject {
		parts := strings.SplitN(input, ":", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return IssueID{}, fmt.Errorf("invalid project:id format: %s", input)
		}

		result.Project = parts[0]
		result.Branch = "main" // Reset branch to main when explicit project is given
		input = parts[1]       // Process the hash part after the colon
	}

	// Now input is the "hash" or "branch-hash" part
	// Check if it contains a dash
	if strings.Contains(input, "-") {
		// Could be "branch-hash" or just a hash with a dash in it
		// Try to parse as branch-hash by looking for the rightmost dash
		// followed by a valid hash
		lastDash := strings.LastIndex(input, "-")
		potentialBranch := input[:lastDash]
		potentialHash := input[lastDash+1:]

		// A valid hash is 4-8 base36 characters
		if isValidHash(potentialHash) && isValidBranchName(potentialBranch) {
			result.Branch = potentialBranch
			result.Hash = potentialHash
			return result, nil
		}
	}

	// No dash, treat entire input as hash
	if !isValidHash(input) {
		return IssueID{}, fmt.Errorf("invalid hash format: %s (expected 4-8 base36 chars)", input)
	}
	result.Hash = input

	// If no branch was set from parsing, use main (only if not already set by colon)
	if result.Branch == "" {
		result.Branch = "main"
	}

	return result, nil
}

// isValidHash checks if a string is a valid hash (4-8 base36 characters)
func isValidHash(s string) bool {
	if len(s) < 4 || len(s) > 8 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) {
			return false
		}
	}
	return true
}

// isValidBranchName checks if a string is a valid branch name
// Must start with letter/digit, contain only [a-z0-9_-]
func isValidBranchName(s string) bool {
	if s == "" {
		return false
	}

	// First character must be alphanumeric
	if !((s[0] >= 'a' && s[0] <= 'z') ||
		(s[0] >= 'A' && s[0] <= 'Z') ||
		(s[0] >= '0' && s[0] <= '9')) {
		return false
	}

	// Remaining characters can be alphanumeric, dash, underscore
	for _, c := range s[1:] {
		if !((c >= 'a' && c <= 'z') ||
			(c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') ||
			c == '-' || c == '_') {
			return false
		}
	}

	return true
}

// isValidProjectName checks if a string is a valid project name
func isValidProjectName(s string) bool {
	if s == "" {
		return false
	}
	return isValidBranchName(s) // Same rules as branch names
}

// ParseMultiFormats tries to parse issue ID in multiple formats
// Handles backward compatibility with old "bd-a3f2" format
func ParseMultiFormats(input string, contextProject, contextBranch string) (IssueID, error) {
	// Try the new format first
	if result, err := ParseIssueID(input, contextProject, contextBranch); err == nil {
		return result, nil
	}

	// Try backward-compatible format: "prefix-hash" or "prefix-branch/hash"
	// This is for old-style IDs that may not have the namespace syntax
	if strings.Contains(input, "-") {
		// Old format: "bd-a3f2" where "bd" is the prefix (project)
		// Not implemented yet - will handle in compatibility layer
	}

	return IssueID{}, fmt.Errorf("invalid issue ID format: %s", input)
}

// ResolutionRule describes how to resolve a short issue ID to a full ID
type ResolutionRule struct {
	Input    string
	Context  ResolutionContext
	Expected IssueID
}

// ResolutionContext provides context for ID resolution
type ResolutionContext struct {
	CurrentProject string
	CurrentBranch  string
}

// Validate checks that the IssueID has all required fields
func (id IssueID) Validate() error {
	if id.Project == "" {
		return fmt.Errorf("project is required")
	}
	if !isValidProjectName(id.Project) {
		return fmt.Errorf("invalid project name: %s", id.Project)
	}
	if id.Branch == "" {
		return fmt.Errorf("branch is required")
	}
	if !isValidBranchName(id.Branch) {
		return fmt.Errorf("invalid branch name: %s", id.Branch)
	}
	if id.Hash == "" {
		return fmt.Errorf("hash is required")
	}
	if !isValidHash(id.Hash) {
		return fmt.Errorf("invalid hash: %s", id.Hash)
	}
	return nil
}

// Regex patterns for parsing
var (
	// Pattern for fully qualified ID: "project:hash" or "project:branch-hash"
	qualifiedIDPattern = regexp.MustCompile(`^([a-zA-Z0-9_-]+):(.+)$`)
	// Pattern for branch-hash: "branch-hash"
	branchHashPattern = regexp.MustCompile(`^([a-zA-Z0-9_-]+)-([a-z0-9]{4,8})$`)
	// Pattern for just hash: "a3f2"
	hashPattern = regexp.MustCompile(`^([a-z0-9]{4,8})$`)
)
