// Package jsonl provides utilities for reading, writing, and cleaning JSONL files.
package jsonl

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// CleanerOptions controls how the cleaner processes issues
type CleanerOptions struct {
	// RemoveDuplicates removes duplicate IDs, keeping the newest version
	RemoveDuplicates bool

	// RemoveTestPollution removes issues with test/baseline prefixes
	RemoveTestPollution bool

	// RepairBrokenReferences removes dependencies to non-existent issues
	RepairBrokenReferences bool

	// Verbose enables detailed output
	Verbose bool
}

// CleanResult contains statistics about the cleaning operation
type CleanResult struct {
	// Original issue count
	OriginalCount int

	// After deduplication
	DeduplicateCount int
	DuplicateIDCount int

	// After test pollution removal
	TestPollutionCount int

	// After reference repair
	BrokenReferencesRemoved int
	BrokenDependencies      []string // Dependencies that were removed

	// Final count
	FinalCount int
}

// DefaultCleanerOptions returns a CleanerOptions with all cleaning enabled
func DefaultCleanerOptions() CleanerOptions {
	return CleanerOptions{
		RemoveDuplicates:       true,
		RemoveTestPollution:    true,
		RepairBrokenReferences: true,
		Verbose:                false,
	}
}

// CleanIssues applies all cleaning steps to a list of issues
func CleanIssues(issues []*types.Issue, opts CleanerOptions) (*CleanResult, []*types.Issue, error) {
	result := &CleanResult{
		OriginalCount:       len(issues),
		BrokenDependencies:  []string{},
	}

	cleaned := issues

	// Phase 1: Deduplication - keep newest version of duplicate IDs
	if opts.RemoveDuplicates {
		dedupResult, newIssues := deduplicateIssues(cleaned)
		result.DeduplicateCount = dedupResult.Count
		result.DuplicateIDCount = dedupResult.DuplicateIDCount
		cleaned = newIssues
	}

	// Phase 2: Remove test pollution
	if opts.RemoveTestPollution {
		count := 0
		cleaned = filterTestPollution(cleaned, &count)
		result.TestPollutionCount = count
	}

	// Phase 3: Repair broken references
	if opts.RepairBrokenReferences {
		repairResult := repairBrokenReferences(cleaned)
		result.BrokenReferencesRemoved = repairResult.Count
		result.BrokenDependencies = repairResult.Dependencies
	}

	result.FinalCount = len(cleaned)

	return result, cleaned, nil
}

// dedupResult holds statistics from deduplication
type dedupResult struct {
	Count            int
	DuplicateIDCount int
}

// deduplicateIssues removes duplicate IDs, keeping the newest version (by UpdatedAt)
func deduplicateIssues(issues []*types.Issue) (dedupResult, []*types.Issue) {
	if len(issues) == 0 {
		return dedupResult{Count: 0}, issues
	}

	// Group issues by ID
	byID := make(map[string][]*types.Issue)
	for _, issue := range issues {
		byID[issue.ID] = append(byID[issue.ID], issue)
	}

	// Keep only the newest version of each ID
	result := make([]*types.Issue, 0, len(byID))
	duplicateCount := 0

	for _, group := range byID {
		if len(group) > 1 {
			duplicateCount += len(group) - 1
			// Sort by UpdatedAt descending, keeping newest first
			sort.Slice(group, func(i, j int) bool {
				return group[i].UpdatedAt.After(group[j].UpdatedAt)
			})
		}
		// Keep the newest (first after sort)
		result = append(result, group[0])
	}

	return dedupResult{Count: len(result), DuplicateIDCount: duplicateCount}, result
}

// filterTestPollution removes issues with test/baseline prefixes that aren't tracked in git
func filterTestPollution(issues []*types.Issue, count *int) []*types.Issue {
	// Patterns that indicate test pollution
	testPrefixes := []string{
		"-baseline-",
		"-test-",
		"-tmp-",
		"-temp-",
		"-scratch-",
		"-demo-",
	}

	// Specific known pollution IDs from failed quality gate checks
	knownPollutionPrefixes := []string{
		"bd-9f86-baseline-",
		"bd-da96-baseline-",
	}

	*count = 0
	filtered := make([]*types.Issue, 0, len(issues))

	for _, issue := range issues {
		isTestPollution := false

		// Check against known pollution prefixes first
		for _, prefix := range knownPollutionPrefixes {
			if strings.HasPrefix(issue.ID, prefix) {
				isTestPollution = true
				break
			}
		}

		// Check against general test patterns
		if !isTestPollution {
			for _, prefix := range testPrefixes {
				if strings.Contains(issue.ID, prefix) {
					isTestPollution = true
					break
				}
			}
		}

		if !isTestPollution {
			filtered = append(filtered, issue)
		} else {
			*count++
		}
	}

	return filtered
}

// repairResult holds statistics from reference repair
type repairResult struct {
	Count        int
	Dependencies []string
}

// repairBrokenReferences removes dependencies to non-existent issues
func repairBrokenReferences(issues []*types.Issue) repairResult {
	// Build a set of all existing issue IDs
	idSet := make(map[string]bool)
	for _, issue := range issues {
		idSet[issue.ID] = true
	}

	result := repairResult{
		Count:        0,
		Dependencies: []string{},
	}

	// For each issue, check and repair its dependencies
	for _, issue := range issues {
		if issue.Dependencies == nil {
			continue
		}

		// Filter out broken dependencies
		validDeps := make([]*types.Dependency, 0, len(issue.Dependencies))
		for _, dep := range issue.Dependencies {
			// Skip dependencies to deleted issues (marked with "deleted:" prefix)
			if strings.HasPrefix(dep.DependsOnID, "deleted:") {
				result.Count++
				result.Dependencies = append(result.Dependencies, fmt.Sprintf(
					"%s -> %s (deleted parent)",
					issue.ID, dep.DependsOnID,
				))
				continue
			}

			// Skip dependencies to non-existent issues
			if !idSet[dep.DependsOnID] {
				result.Count++
				result.Dependencies = append(result.Dependencies, fmt.Sprintf(
					"%s -> %s (non-existent)",
					issue.ID, dep.DependsOnID,
				))
				continue
			}

			// Keep valid dependency
			validDeps = append(validDeps, dep)
		}

		// Update issue's dependencies
		issue.Dependencies = validDeps
	}

	return result
}

// ValidationReport contains the results of JSONL validation
type ValidationReport struct {
	TotalIssues       int
	DuplicateIDs      map[string]int    // ID -> count of occurrences
	BrokenReferences  map[string][]string // Issue ID -> list of broken deps
	TestPollutionIDs  []string
	InvalidIssues     []InvalidIssueReport
	Timestamp         time.Time
}

// InvalidIssueReport describes an issue that failed validation
type InvalidIssueReport struct {
	ID     string
	Reason string
}

// ValidateIssues checks for common issues in a JSONL dataset
func ValidateIssues(issues []*types.Issue) *ValidationReport {
	report := &ValidationReport{
		TotalIssues:      len(issues),
		DuplicateIDs:     make(map[string]int),
		BrokenReferences: make(map[string][]string),
		TestPollutionIDs: []string{},
		InvalidIssues:    []InvalidIssueReport{},
		Timestamp:        time.Now(),
	}

	// Build ID set for reference validation
	idSet := make(map[string]bool)
	for _, issue := range issues {
		idSet[issue.ID] = true
		// Count duplicate IDs
		report.DuplicateIDs[issue.ID]++
	}

	// Filter to only duplicates
	for id := range report.DuplicateIDs {
		if report.DuplicateIDs[id] == 1 {
			delete(report.DuplicateIDs, id)
		}
	}

	// Check for broken references
	testPrefixes := []string{"-baseline-", "-test-", "-tmp-", "-temp-", "-scratch-", "-demo-"}
	knownPollutionPrefixes := []string{"bd-9f86-baseline-", "bd-da96-baseline-"}

	for _, issue := range issues {
		// Check for test pollution
		isTestPollution := false
		for _, prefix := range knownPollutionPrefixes {
			if strings.HasPrefix(issue.ID, prefix) {
				isTestPollution = true
				break
			}
		}
		if !isTestPollution {
			for _, prefix := range testPrefixes {
				if strings.Contains(issue.ID, prefix) {
					isTestPollution = true
					break
				}
			}
		}
		if isTestPollution {
			report.TestPollutionIDs = append(report.TestPollutionIDs, issue.ID)
		}

		// Check dependencies
		if issue.Dependencies != nil {
			for _, dep := range issue.Dependencies {
				if strings.HasPrefix(dep.DependsOnID, "deleted:") ||
					!idSet[dep.DependsOnID] {
					report.BrokenReferences[issue.ID] = append(
						report.BrokenReferences[issue.ID],
						dep.DependsOnID,
					)
				}
			}
		}

		// Validate issue structure
		if err := issue.Validate(); err != nil {
			report.InvalidIssues = append(report.InvalidIssues, InvalidIssueReport{
				ID:     issue.ID,
				Reason: err.Error(),
			})
		}
	}

	return report
}

// HasIssues returns true if the validation report found any problems
func (r *ValidationReport) HasIssues() bool {
	return len(r.DuplicateIDs) > 0 ||
		len(r.BrokenReferences) > 0 ||
		len(r.TestPollutionIDs) > 0 ||
		len(r.InvalidIssues) > 0
}

// Summary returns a human-readable summary of the validation
func (r *ValidationReport) Summary() string {
	lines := []string{
		fmt.Sprintf("JSONL Validation Report (%d total issues)", r.TotalIssues),
		fmt.Sprintf("Generated: %s", r.Timestamp.Format(time.RFC3339)),
		"",
	}

	if len(r.DuplicateIDs) > 0 {
		lines = append(lines,
			fmt.Sprintf("❌ Duplicate IDs (%d):", len(r.DuplicateIDs)),
		)
		for id, count := range r.DuplicateIDs {
			lines = append(lines, fmt.Sprintf("   %s appears %d times", id, count))
		}
		lines = append(lines, "")
	}

	if len(r.BrokenReferences) > 0 {
		lines = append(lines,
			fmt.Sprintf("❌ Broken References (%d issues):", len(r.BrokenReferences)),
		)
		for id, refs := range r.BrokenReferences {
			for _, ref := range refs {
				lines = append(lines, fmt.Sprintf("   %s -> %s", id, ref))
			}
		}
		lines = append(lines, "")
	}

	if len(r.TestPollutionIDs) > 0 {
		lines = append(lines,
			fmt.Sprintf("⚠️  Test Pollution (%d issues):", len(r.TestPollutionIDs)),
		)
		for _, id := range r.TestPollutionIDs {
			lines = append(lines, fmt.Sprintf("   %s", id))
		}
		lines = append(lines, "")
	}

	if len(r.InvalidIssues) > 0 {
		lines = append(lines,
			fmt.Sprintf("❌ Invalid Issues (%d):", len(r.InvalidIssues)),
		)
		for _, inv := range r.InvalidIssues {
			lines = append(lines, fmt.Sprintf("   %s: %s", inv.ID, inv.Reason))
		}
		lines = append(lines, "")
	}

	if !r.HasIssues() {
		lines = append(lines, "✓ No issues found")
	}

	return strings.Join(lines, "\n")
}
