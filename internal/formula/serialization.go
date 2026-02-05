package formula

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// FormulaToIssue converts a Formula to an Issue for database storage.
// The full formula content is stored in Issue.Metadata as JSON.
// Issue fields provide queryable facets (title, description, type, labels).
func FormulaToIssue(f *Formula, idPrefix string) (*types.Issue, []string, error) {
	if f == nil {
		return nil, nil, fmt.Errorf("formula is nil")
	}
	if f.Formula == "" {
		return nil, nil, fmt.Errorf("formula name is empty")
	}

	// Serialize entire formula to JSON for metadata
	metadataBytes, err := json.Marshal(f)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal formula metadata: %w", err)
	}

	// Build deterministic ID from formula name
	slug := formulaNameToSlug(f.Formula)
	id := idPrefix + "formula-" + slug

	issue := &types.Issue{
		ID:            id,
		Title:         f.Formula,
		Description:   f.Description,
		IssueType:     types.TypeFormula,
		Metadata:      json.RawMessage(metadataBytes),
		IsTemplate:    true,
		SourceFormula: f.Formula,
	}

	// Build labels for queryable facets
	var labels []string
	if f.Type != "" {
		labels = append(labels, "formula-type:"+string(f.Type))
	}
	if f.Phase != "" {
		labels = append(labels, "phase:"+f.Phase)
	}
	for _, skill := range f.RequiresSkills {
		labels = append(labels, "skill:"+skill)
	}

	return issue, labels, nil
}

// IssueToFormula converts an Issue back to a Formula by deserializing Metadata.
// Returns an error if the issue is not a formula type or metadata is malformed.
func IssueToFormula(issue *types.Issue) (*Formula, error) {
	if issue == nil {
		return nil, fmt.Errorf("issue is nil")
	}
	if issue.IssueType != types.TypeFormula {
		return nil, fmt.Errorf("issue type is %q, expected %q", issue.IssueType, types.TypeFormula)
	}
	if len(issue.Metadata) == 0 {
		return nil, fmt.Errorf("issue %s has no metadata", issue.ID)
	}

	var f Formula
	if err := json.Unmarshal(issue.Metadata, &f); err != nil {
		return nil, fmt.Errorf("unmarshal formula from issue %s: %w", issue.ID, err)
	}

	// Set source to indicate DB origin
	f.Source = "bead:" + issue.ID

	return &f, nil
}

// formulaNameToSlug converts a formula name to an ID-safe slug.
// e.g., "mol-polecat-work" -> "mol-polecat-work"
// e.g., "My Formula Name" -> "my-formula-name"
func formulaNameToSlug(name string) string {
	slug := strings.ToLower(name)
	slug = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		if r == '_' || r == ' ' || r == '.' {
			return '-'
		}
		return -1
	}, slug)
	// Collapse multiple hyphens
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	return slug
}
