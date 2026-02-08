package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/steveyegge/beads/internal/query"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
)

// MutexGroup defines a mutually exclusive label set parsed from config.
type MutexGroup struct {
	Name     string
	Labels   []string
	Required bool
	Query    string // optional scope query using internal/query syntax
}

// mutexViolation records a single label mutex violation on an issue.
type mutexViolation struct {
	IssueID   string
	GroupName string
	Kind      string   // "conflict" or "missing"
	Present   []string // conflicting labels (for conflict) or empty (for missing)
	Expected  []string // the mutex group labels
}

// parseMutexGroups reads validation.labels.mutex from a config.yaml file.
// Returns nil, nil if the key is absent or the file doesn't exist.
func parseMutexGroups(configPath string) ([]MutexGroup, error) {
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, nil
	}

	v := viper.New()
	v.SetConfigFile(configPath)
	v.SetConfigType("yaml")
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	raw := v.Get("validation.labels.mutex")
	if raw == nil {
		return nil, nil
	}

	rawSlice, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("validation.labels.mutex must be a list, got %T", raw)
	}

	var groups []MutexGroup
	for i, item := range rawSlice {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("validation.labels.mutex[%d]: expected map, got %T", i, item)
		}

		group := MutexGroup{}

		// Parse name (optional)
		if name, ok := m["name"].(string); ok {
			group.Name = strings.TrimSpace(name)
		}

		// Parse labels (required)
		labelsRaw, ok := m["labels"]
		if !ok {
			return nil, fmt.Errorf("validation.labels.mutex[%d]: missing 'labels' field", i)
		}
		labelsSlice, ok := labelsRaw.([]any)
		if !ok {
			return nil, fmt.Errorf("validation.labels.mutex[%d]: 'labels' must be a list", i)
		}
		for j, l := range labelsSlice {
			s, ok := l.(string)
			if !ok {
				return nil, fmt.Errorf("validation.labels.mutex[%d].labels[%d]: expected string, got %T", i, j, l)
			}
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			group.Labels = append(group.Labels, s)
		}
		if len(group.Labels) < 2 {
			return nil, fmt.Errorf("validation.labels.mutex[%d]: need at least 2 labels, got %d", i, len(group.Labels))
		}

		// Dedupe labels within group
		seen := make(map[string]bool, len(group.Labels))
		deduped := group.Labels[:0]
		for _, l := range group.Labels {
			if !seen[l] {
				seen[l] = true
				deduped = append(deduped, l)
			}
		}
		group.Labels = deduped

		// Parse required (optional, defaults to false)
		if req, ok := m["required"].(bool); ok {
			group.Required = req
		}

		// Parse scope.query (optional)
		if scope, ok := m["scope"].(map[string]any); ok {
			if q, ok := scope["query"].(string); ok {
				group.Query = strings.TrimSpace(q)
			}
		}

		// Synthesize name if not provided
		if group.Name == "" {
			group.Name = "labels: " + strings.Join(group.Labels, ",")
		}

		groups = append(groups, group)
	}

	return groups, nil
}

// shouldExcludeIssue returns true if the issue should be excluded from mutex checks
// by default (when no scope query is provided).
func shouldExcludeIssue(issue *types.Issue) bool {
	if issue.Status == types.StatusTombstone {
		return true
	}
	if issue.Ephemeral {
		return true
	}
	if issue.IsTemplate {
		return true
	}
	if issue.Pinned {
		return true
	}
	if issue.Status == types.StatusPinned {
		return true
	}
	return false
}

// findViolations checks a set of issues+labels against mutex groups.
func findViolations(issues []*types.Issue, labelsMap map[string][]string, groups []MutexGroup) []mutexViolation {
	var violations []mutexViolation

	for _, issue := range issues {
		issueLabels := labelsMap[issue.ID]
		labelSet := make(map[string]bool, len(issueLabels))
		for _, l := range issueLabels {
			labelSet[l] = true
		}

		for _, group := range groups {
			var present []string
			for _, gl := range group.Labels {
				if labelSet[gl] {
					present = append(present, gl)
				}
			}

			if len(present) > 1 {
				violations = append(violations, mutexViolation{
					IssueID:   issue.ID,
					GroupName: group.Name,
					Kind:      "conflict",
					Present:   present,
					Expected:  group.Labels,
				})
			}
			if group.Required && len(present) == 0 {
				violations = append(violations, mutexViolation{
					IssueID:   issue.ID,
					GroupName: group.Name,
					Kind:      "missing",
					Expected:  group.Labels,
				})
			}
		}
	}

	return violations
}

// CheckLabelMutexInvariants checks that issues conform to label mutex rules
// defined in validation.labels.mutex in .beads/config.yaml.
func CheckLabelMutexInvariants(path string) DoctorCheck {
	beadsDir := resolveBeadsDir(filepath.Join(path, ".beads"))

	configPath := filepath.Join(beadsDir, "config.yaml")
	groups, err := parseMutexGroups(configPath)
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
	var allViolations []mutexViolation

	// Partition groups by scope query to batch queries where possible.
	// Groups with no scope query share a default fetch.
	var defaultGroups []MutexGroup
	scopedGroups := make(map[string][]MutexGroup) // query string -> groups
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
			if !shouldExcludeIssue(issue) {
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
			allViolations = append(allViolations, findViolations(filtered, labelsMap, defaultGroups)...)
		}
	}

	// Check scoped groups using the query evaluator.
	for scopeQuery, scopeGroups := range scopedGroups {
		qr, err := query.EvaluateAt(scopeQuery, time.Now())
		if err != nil {
			allViolations = append(allViolations, mutexViolation{
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
			allViolations = append(allViolations, findViolations(issues, labelsMap, scopeGroups)...)
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
