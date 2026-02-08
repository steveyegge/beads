// Package labelmutex provides shared logic for label mutex validation.
// It is imported by both cmd/bd/doctor and cmd/bd/doctor/fix to avoid
// code duplication without creating import cycles.
package labelmutex

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/viper"
	"github.com/steveyegge/beads/internal/types"
)

// MutexGroup defines a mutually exclusive label set parsed from config.
type MutexGroup struct {
	Name     string
	Labels   []string
	Required bool
	Query    string // optional scope query using internal/query syntax
}

// Violation records a single label mutex violation on an issue.
type Violation struct {
	IssueID   string
	GroupName string
	Kind      string   // "conflict", "missing", or "config"
	Present   []string // conflicting labels (for conflict) or error info (for config)
	Expected  []string // the mutex group labels
}

// ParseMutexGroups reads validation.labels.mutex from a config.yaml file.
// Returns nil, nil if the key is absent or the file doesn't exist.
func ParseMutexGroups(configPath string) ([]MutexGroup, error) {
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

// ShouldExcludeIssue returns true if the issue should be excluded from mutex
// checks by default (when no scope query is provided).
func ShouldExcludeIssue(issue *types.Issue) bool {
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

// FindViolations checks a set of issues+labels against mutex groups.
func FindViolations(issues []*types.Issue, labelsMap map[string][]string, groups []MutexGroup) []Violation {
	var violations []Violation

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
				violations = append(violations, Violation{
					IssueID:   issue.ID,
					GroupName: group.Name,
					Kind:      "conflict",
					Present:   present,
					Expected:  group.Labels,
				})
			}
			if group.Required && len(present) == 0 {
				violations = append(violations, Violation{
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
