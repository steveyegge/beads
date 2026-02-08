package doctor

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/labelmutex"
	"github.com/steveyegge/beads/internal/query"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
)

// CheckLabelMutexInvariants checks that issues conform to label mutex rules
// defined in validation.labels.mutex in .beads/config.yaml.
func CheckLabelMutexInvariants(path string) DoctorCheck {
	beadsDir := resolveBeadsDir(filepath.Join(path, ".beads"))

	configPath := filepath.Join(beadsDir, "config.yaml")
	groups, err := labelmutex.ParseMutexGroups(configPath)
	if err != nil {
		return DoctorCheck{
			Name:     "Label Mutex Invariants",
			Status:   StatusWarning,
			Message:  "Invalid label mutex config",
			Detail:   err.Error(),
			Fix:      "Fix the validation.labels.mutex section in .beads/config.yaml",
			Category: CategoryData,
		}
	}
	if len(groups) == 0 {
		return DoctorCheck{
			Name:     "Label Mutex Invariants",
			Status:   StatusOK,
			Message:  "No label mutex rules configured",
			Category: CategoryData,
		}
	}

	// Open store read-only using the backend-agnostic factory.
	ctx := context.Background()
	store, err := factory.NewFromConfigWithOptions(ctx, beadsDir, factory.Options{ReadOnly: true})
	if err != nil {
		return DoctorCheck{
			Name:     "Label Mutex Invariants",
			Status:   StatusOK,
			Message:  "N/A (no database)",
			Category: CategoryData,
		}
	}
	defer func() { _ = store.Close() }()

	// Process each group — groups may have different scopes.
	var allViolations []labelmutex.Violation

	// Partition groups by scope query to batch queries where possible.
	// Groups with no scope query share a default fetch.
	var defaultGroups []labelmutex.MutexGroup
	scopedGroups := make(map[string][]labelmutex.MutexGroup) // query string -> groups
	for _, g := range groups {
		if g.Query == "" {
			defaultGroups = append(defaultGroups, g)
		} else {
			scopedGroups[g.Query] = append(scopedGroups[g.Query], g)
		}
	}

	// Check default-scoped groups (all non-excluded issues).
	if len(defaultGroups) > 0 {
		issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
		if err != nil {
			return DoctorCheck{
				Name:     "Label Mutex Invariants",
				Status:   StatusWarning,
				Message:  "Unable to query issues",
				Detail:   err.Error(),
				Category: CategoryData,
			}
		}

		// Apply default exclusions.
		var filtered []*types.Issue
		for _, issue := range issues {
			if !labelmutex.ShouldExcludeIssue(issue) {
				filtered = append(filtered, issue)
			}
		}

		if len(filtered) > 0 {
			issueIDs := make([]string, len(filtered))
			for i, issue := range filtered {
				issueIDs[i] = issue.ID
			}
			labelsMap, err := store.GetLabelsForIssues(ctx, issueIDs)
			if err != nil {
				return DoctorCheck{
					Name:     "Label Mutex Invariants",
					Status:   StatusWarning,
					Message:  "Unable to query labels",
					Detail:   err.Error(),
					Category: CategoryData,
				}
			}
			allViolations = append(allViolations, labelmutex.FindViolations(filtered, labelsMap, defaultGroups)...)
		}
	}

	// Check scoped groups using the query evaluator.
	for scopeQuery, scopeGroups := range scopedGroups {
		qr, err := query.EvaluateAt(scopeQuery, time.Now())
		if err != nil {
			allViolations = append(allViolations, labelmutex.Violation{
				IssueID:   "",
				GroupName: scopeGroups[0].Name,
				Kind:      "config",
				Present:   []string{fmt.Sprintf("invalid scope query: %v", err)},
			})
			continue
		}

		issues, err := store.SearchIssues(ctx, "", qr.Filter)
		if err != nil {
			continue
		}

		// Apply predicate if needed (complex queries with OR/NOT).
		if qr.RequiresPredicate && qr.Predicate != nil {
			var filtered []*types.Issue
			for _, issue := range issues {
				if qr.Predicate(issue) {
					filtered = append(filtered, issue)
				}
			}
			issues = filtered
		}

		if len(issues) > 0 {
			issueIDs := make([]string, len(issues))
			for i, issue := range issues {
				issueIDs[i] = issue.ID
			}
			labelsMap, err := store.GetLabelsForIssues(ctx, issueIDs)
			if err != nil {
				continue
			}
			allViolations = append(allViolations, labelmutex.FindViolations(issues, labelsMap, scopeGroups)...)
		}
	}

	if len(allViolations) == 0 {
		return DoctorCheck{
			Name:     "Label Mutex Invariants",
			Status:   StatusOK,
			Message:  fmt.Sprintf("All issues conform to %d mutex rule(s)", len(groups)),
			Category: CategoryData,
		}
	}

	// Build detail output, sorted for deterministic output.
	sort.Slice(allViolations, func(i, j int) bool {
		if allViolations[i].IssueID != allViolations[j].IssueID {
			return allViolations[i].IssueID < allViolations[j].IssueID
		}
		return allViolations[i].GroupName < allViolations[j].GroupName
	})

	var detail strings.Builder
	const maxDetail = 20
	for i, v := range allViolations {
		if i >= maxDetail {
			fmt.Fprintf(&detail, "... and %d more\n", len(allViolations)-maxDetail)
			break
		}
		switch v.Kind {
		case "conflict":
			fmt.Fprintf(&detail, "%s: %s conflict (%s)\n", v.IssueID, v.GroupName, strings.Join(v.Present, ", "))
		case "missing":
			fmt.Fprintf(&detail, "%s: %s missing (expected one of: %s)\n", v.IssueID, v.GroupName, strings.Join(v.Expected, ", "))
		case "config":
			fmt.Fprintf(&detail, "scope error for %s: %s\n", v.GroupName, strings.Join(v.Present, "; "))
		}
	}

	return DoctorCheck{
		Name:     "Label Mutex Invariants",
		Status:   StatusWarning,
		Message:  fmt.Sprintf("%d label mutex violation(s)", len(allViolations)),
		Detail:   strings.TrimSpace(detail.String()),
		Fix:      "Remove conflicting labels (bd update <id> --remove-label <label>) or add missing required labels (bd update <id> --add-label <label>)",
		Category: CategoryData,
	}
}
