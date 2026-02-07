package runbook

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// RunbookContent represents a runbook stored as a bead.
// The raw file content is preserved for lossless roundtrip.
type RunbookContent struct {
	Name     string   `json:"name"`               // Runbook name (derived from filename)
	Format   string   `json:"format"`             // File format: "hcl", "toml", "json"
	Content  string   `json:"content"`            // Raw file content
	Jobs     []string `json:"jobs,omitempty"`     // Extracted job names
	Commands []string `json:"commands,omitempty"` // Extracted command names
	Workers  []string `json:"workers,omitempty"`  // Extracted worker names
	Crons    []string `json:"crons,omitempty"`    // Extracted cron names
	Queues   []string `json:"queues,omitempty"`   // Extracted queue names
	Source   string   `json:"source,omitempty"`   // Where loaded from (file path or bead:ID)
	Scope    string   `json:"scope,omitempty"`    // Scope level: "global", "town:X", "rig:X", "role:X", "agent:X"
}

// Scope hierarchy levels for specificity scoring.
// Higher values win when multiple runbooks share the same name.
const (
	ScopeGlobal = 0 // scope:global - available everywhere
	ScopeTown   = 1 // scope:town:X - available within a town
	ScopeRig    = 2 // scope:rig:X - available within a rig
	ScopeRole   = 3 // scope:role:X - available to a specific role
	ScopeAgent  = 4 // scope:agent:X - available to a specific agent
)

// ScopeSpecificity returns the specificity score (0-4) for a scope string.
// Higher values are more specific and win in most-specific-wins resolution.
func ScopeSpecificity(scope string) int {
	switch {
	case scope == "" || scope == "global":
		return ScopeGlobal
	case strings.HasPrefix(scope, "town:"):
		return ScopeTown
	case strings.HasPrefix(scope, "rig:"):
		return ScopeRig
	case strings.HasPrefix(scope, "role:"):
		return ScopeRole
	case strings.HasPrefix(scope, "agent:"):
		return ScopeAgent
	default:
		return ScopeGlobal
	}
}

// ScopeLabel returns the label string for a scope (e.g., "scope:rig:beads").
// Empty or "global" scope returns "scope:global".
func ScopeLabel(scope string) string {
	if scope == "" || scope == "global" {
		return "scope:global"
	}
	return "scope:" + scope
}

// ParseScopeFromLabels extracts the scope from a set of labels.
// Returns the most specific scope label found (highest specificity).
func ParseScopeFromLabels(labels []string) string {
	bestScope := ""
	bestSpec := -1
	for _, l := range labels {
		if !strings.HasPrefix(l, "scope:") {
			continue
		}
		scopeVal := strings.TrimPrefix(l, "scope:")
		spec := ScopeSpecificity(scopeVal)
		if spec > bestSpec {
			bestSpec = spec
			bestScope = scopeVal
		}
	}
	if bestScope == "" {
		return "global"
	}
	return bestScope
}

// RunbookToIssue converts a RunbookContent to an Issue for database storage.
// The full runbook content is stored in Issue.Metadata as JSON.
func RunbookToIssue(rb *RunbookContent, idPrefix string) (*types.Issue, []string, error) {
	if rb == nil {
		return nil, nil, fmt.Errorf("runbook is nil")
	}
	if rb.Name == "" {
		return nil, nil, fmt.Errorf("runbook name is empty")
	}

	metadataBytes, err := json.Marshal(rb)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal runbook metadata: %w", err)
	}

	slug := nameToSlug(rb.Name)
	id := idPrefix + "runbook-" + slug

	// Build a human-readable description from the extracted names
	var descParts []string
	if len(rb.Jobs) > 0 {
		descParts = append(descParts, fmt.Sprintf("Jobs: %s", strings.Join(rb.Jobs, ", ")))
	}
	if len(rb.Commands) > 0 {
		descParts = append(descParts, fmt.Sprintf("Commands: %s", strings.Join(rb.Commands, ", ")))
	}
	if len(rb.Workers) > 0 {
		descParts = append(descParts, fmt.Sprintf("Workers: %s", strings.Join(rb.Workers, ", ")))
	}
	if len(rb.Crons) > 0 {
		descParts = append(descParts, fmt.Sprintf("Crons: %s", strings.Join(rb.Crons, ", ")))
	}
	desc := strings.Join(descParts, ". ")
	if desc == "" {
		desc = fmt.Sprintf("OJ runbook (%s format)", rb.Format)
	}

	issue := &types.Issue{
		ID:          id,
		Title:       rb.Name,
		Description: desc,
		IssueType:   types.TypeRunbook,
		Metadata:    json.RawMessage(metadataBytes),
		IsTemplate:  true,
	}

	// Build labels for queryable facets
	var labels []string
	labels = append(labels, "format:"+rb.Format)
	labels = append(labels, ScopeLabel(rb.Scope))
	for _, j := range rb.Jobs {
		labels = append(labels, "job:"+j)
	}
	for _, c := range rb.Commands {
		labels = append(labels, "cmd:"+c)
	}
	for _, w := range rb.Workers {
		labels = append(labels, "worker:"+w)
	}

	return issue, labels, nil
}

// IssueToRunbook converts an Issue back to a RunbookContent.
func IssueToRunbook(issue *types.Issue) (*RunbookContent, error) {
	if issue == nil {
		return nil, fmt.Errorf("issue is nil")
	}
	if issue.IssueType != types.TypeRunbook {
		return nil, fmt.Errorf("issue type is %q, expected %q", issue.IssueType, types.TypeRunbook)
	}
	if len(issue.Metadata) == 0 {
		return nil, fmt.Errorf("issue %s has no metadata", issue.ID)
	}

	var rb RunbookContent
	if err := json.Unmarshal(issue.Metadata, &rb); err != nil {
		return nil, fmt.Errorf("unmarshal runbook from issue %s: %w", issue.ID, err)
	}

	rb.Source = "bead:" + issue.ID
	return &rb, nil
}

// HCL block name patterns (e.g., job "name" { or command "name" {)
var (
	jobPattern     = regexp.MustCompile(`(?m)^\s*job\s+"([^"]+)"`)
	commandPattern = regexp.MustCompile(`(?m)^\s*command\s+"([^"]+)"`)
	workerPattern  = regexp.MustCompile(`(?m)^\s*worker\s+"([^"]+)"`)
	cronPattern    = regexp.MustCompile(`(?m)^\s*cron\s+"([^"]+)"`)
	queuePattern   = regexp.MustCompile(`(?m)^\s*queue\s+"([^"]+)"`)
	importPattern  = regexp.MustCompile(`(?m)^\s*import\s+"([^"]+)"`)
	constPattern   = regexp.MustCompile(`(?m)^\s*const\s+"([^"]+)"`)
)

// ParseRunbookFile reads a runbook file and extracts metadata.
// It does not fully parse the HCL/TOML; it extracts top-level block names
// via regex for labeling purposes. The raw content is preserved verbatim.
func ParseRunbookFile(name, content, format string) *RunbookContent {
	rb := &RunbookContent{
		Name:    name,
		Format:  format,
		Content: content,
	}

	// Extract names from HCL content via regex
	if format == "hcl" {
		rb.Jobs = extractNames(jobPattern, content)
		rb.Commands = extractNames(commandPattern, content)
		rb.Workers = extractNames(workerPattern, content)
		rb.Crons = extractNames(cronPattern, content)
		rb.Queues = extractNames(queuePattern, content)
	}

	return rb
}

func extractNames(pattern *regexp.Regexp, content string) []string {
	matches := pattern.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return nil
	}
	var names []string
	seen := make(map[string]bool)
	for _, m := range matches {
		name := m[1]
		if !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// ExtractImports returns import paths from HCL content.
func ExtractImports(content string) []string {
	return extractNames(importPattern, content)
}

// ExtractConsts returns const names from HCL content.
func ExtractConsts(content string) []string {
	return extractNames(constPattern, content)
}

// NameToSlug is the exported version of nameToSlug for use by migrate command.
func NameToSlug(name string) string {
	return nameToSlug(name)
}

// nameToSlug converts a name to an ID-safe slug.
func nameToSlug(name string) string {
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
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	slug = strings.Trim(slug, "-")
	return slug
}
