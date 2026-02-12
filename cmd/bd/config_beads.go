package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
)

// configListBeadsCmd lists config beads, optionally filtered by category and scope.
var configListBeadsCmd = &cobra.Command{
	Use:   "list-beads [--category <cat>] [--scope <scope>]",
	Short: "List config beads",
	Long: `List config beads, optionally filtered by category and scope.

Config beads are issues with type=config that store configuration data in their
metadata field. They use labels to indicate category (config:<category>) and
scope (scope:global, town:<name>, rig:<name>, role:<name>, agent:<name>).

Examples:
  bd config list-beads
  bd config list-beads --category claude-hooks
  bd config list-beads --scope global
  bd config list-beads --category mcp --scope global --json`,
	Run: runConfigListBeads,
}

// configShowBeadCmd shows a single config bead or merged config for a category+scope.
var configShowBeadCmd = &cobra.Command{
	Use:   "show-bead <id-or-category> [--scope <scope>] [--merged]",
	Short: "Show a config bead or merged config",
	Long: `Show a single config bead, or show merged config for a category+scope.

When called with an issue ID, shows that specific config bead.
When called with a category name and --merged, queries all config beads for
that category, filters to applicable scopes, scores by specificity, and
performs a deep merge.

Deep merge strategy:
  - Hook arrays (keys under "hooks"): APPEND (more specific adds to less specific)
  - Top-level keys: OVERRIDE (more specific wins)
  - Explicit null: SUPPRESSES inherited value

Specificity scoring:
  0: scope:global (no role/agent)
  1: scope:global + role:<role>
  2: town:<town> + rig:<rig>
  3: town:<town> + rig:<rig> + role:<role>
  4: town:<town> + rig:<rig> + agent:<agent>

Examples:
  bd config show-bead hq-cfg-hooks-base
  bd config show-bead claude-hooks --merged
  bd config show-bead claude-hooks --scope "town:gt11,rig:gastown,role:crew" --merged`,
	Args: cobra.ExactArgs(1),
	Run:  runConfigShowBead,
}

// configSetBeadCmd creates or updates a config bead.
var configSetBeadCmd = &cobra.Command{
	Use:   "set-bead --category <cat> --scope <scope> --title <title> --data <json>",
	Short: "Create or update a config bead",
	Long: `Create or update a config bead.

Creates a new config bead or updates an existing one. The bead ID is generated
from category+scope as hq-cfg-<slug>. The rig field is set based on scope.

Scope values:
  global                          - scope:global, rig="*"
  town:<name>                     - town-scoped, rig="<name>"
  town:<name>,rig:<name>          - rig-scoped, rig="<town>/<rig>"
  town:<t>,rig:<r>,role:<role>    - role-scoped
  town:<t>,rig:<r>,agent:<agent>  - agent-scoped

Examples:
  bd config set-bead --category claude-hooks --scope global \
    --title "Claude Hooks: base" --data '{"editorMode":"normal"}'

  bd config set-bead --category mcp --scope "town:gt11,rig:gastown" \
    --title "MCP: gastown" --data '{"mcpServers":{}}'`,
	Run: runConfigSetBead,
}

func init() {
	// list-beads flags
	configListBeadsCmd.Flags().String("category", "", "Filter by config category (e.g., claude-hooks, mcp)")
	configListBeadsCmd.Flags().String("scope", "", "Filter by scope label (e.g., global, town:gt11)")

	// show-bead flags
	configShowBeadCmd.Flags().String("scope", "", "Scope labels for merged output (comma-separated)")
	configShowBeadCmd.Flags().Bool("merged", false, "Show merged config across all matching scopes")

	// set-bead flags
	configSetBeadCmd.Flags().String("category", "", "Config category (e.g., claude-hooks, mcp)")
	configSetBeadCmd.Flags().String("scope", "", "Scope (e.g., global, town:gt11,rig:gastown)")
	configSetBeadCmd.Flags().String("title", "", "Human-readable title for the config bead")
	configSetBeadCmd.Flags().String("data", "", "Config payload as JSON")
	_ = configSetBeadCmd.MarkFlagRequired("category")
	_ = configSetBeadCmd.MarkFlagRequired("scope")
	_ = configSetBeadCmd.MarkFlagRequired("title")
	_ = configSetBeadCmd.MarkFlagRequired("data")

	configCmd.AddCommand(configListBeadsCmd)
	configCmd.AddCommand(configShowBeadCmd)
	configCmd.AddCommand(configSetBeadCmd)
}

// runConfigListBeads implements bd config list-beads.
func runConfigListBeads(cmd *cobra.Command, _ []string) {
	category, _ := cmd.Flags().GetString("category")
	scope, _ := cmd.Flags().GetString("scope")

	// Build labels filter
	var labels []string
	if category != "" {
		labels = append(labels, "config:"+category)
	}
	if scope != "" {
		// scope can be "global" (shorthand for scope:global) or a label like "town:gt11"
		if scope == "global" {
			labels = append(labels, "scope:global")
		} else {
			labels = append(labels, scope)
		}
	}

	configType := "config"

	// Use daemon RPC
	requireDaemon("config list-beads")
	listArgs := &rpc.ListArgs{
		IssueType: configType,
		Labels:    labels,
		Limit:     200,
	}
	listResp, err := daemonClient.List(listArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var issues []*types.Issue
	if err := json.Unmarshal(listResp.Data, &issues); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		// Labels are already populated from the daemon List RPC
		outputJSON(issues)
		return
	}

	// Pretty print
	if len(issues) == 0 {
		fmt.Println("No config beads found")
		if category != "" || scope != "" {
			fmt.Println("  Try: bd config list-beads (without filters)")
		}
		return
	}

	fmt.Printf("\nConfig Beads (%d):\n", len(issues))
	for _, issue := range issues {
		metaPreview := ""
		if issue.Metadata != nil {
			metaPreview = truncateConfigMeta(string(issue.Metadata), 60)
		}

		scopeStr := extractScopeFromLabels(issue.Labels)
		categoryStr := extractCategoryFromLabels(issue.Labels)

		fmt.Printf("  %s  %s\n", issue.ID, issue.Title)
		if categoryStr != "" {
			fmt.Printf("    category: %s", categoryStr)
		}
		if scopeStr != "" {
			if categoryStr != "" {
				fmt.Printf("  scope: %s", scopeStr)
			} else {
				fmt.Printf("    scope: %s", scopeStr)
			}
		}
		if categoryStr != "" || scopeStr != "" {
			fmt.Println()
		}
		if metaPreview != "" {
			fmt.Printf("    metadata: %s\n", metaPreview)
		}
	}
}

// printConfigBeadsList pretty-prints config beads from daemon response.
func printConfigBeadsList(issues []*types.IssueWithCounts) {
	if len(issues) == 0 {
		fmt.Println("No config beads found")
		return
	}

	fmt.Printf("\nConfig Beads (%d):\n", len(issues))
	for _, iwc := range issues {
		issue := iwc.Issue
		metaPreview := ""
		if issue.Metadata != nil {
			metaPreview = truncateConfigMeta(string(issue.Metadata), 60)
		}

		scopeStr := extractScopeFromLabels(issue.Labels)
		categoryStr := extractCategoryFromLabels(issue.Labels)

		fmt.Printf("  %s  %s\n", issue.ID, issue.Title)
		if categoryStr != "" {
			fmt.Printf("    category: %s", categoryStr)
		}
		if scopeStr != "" {
			if categoryStr != "" {
				fmt.Printf("  scope: %s", scopeStr)
			} else {
				fmt.Printf("    scope: %s", scopeStr)
			}
		}
		if categoryStr != "" || scopeStr != "" {
			fmt.Println()
		}
		if metaPreview != "" {
			fmt.Printf("    metadata: %s\n", metaPreview)
		}
	}
}

// runConfigShowBead implements bd config show-bead.
func runConfigShowBead(cmd *cobra.Command, args []string) {
	idOrCategory := args[0]
	merged, _ := cmd.Flags().GetBool("merged")
	scope, _ := cmd.Flags().GetString("scope")

	if merged {
		runConfigShowMerged(idOrCategory, scope)
		return
	}

	// Show a single config bead by ID
	requireDaemon("config show-bead")
	showArgs := &rpc.ShowArgs{ID: idOrCategory}
	resp, err := daemonClient.Show(showArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var issue types.Issue
	if err := json.Unmarshal(resp.Data, &issue); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		outputJSON(issue)
		return
	}

	printConfigBead(&issue)
}

// runConfigShowMerged shows merged config for a category across scopes.
func runConfigShowMerged(category, scopeStr string) {
	// Parse scope labels
	scopeLabels := parseScopeLabels(scopeStr)

	// Query all config beads for this category
	requireDaemon("config show-bead --merged")
	configType := "config"
	labels := []string{"config:" + category}

	var allIssues []*types.Issue

	listArgs := &rpc.ListArgs{
		IssueType: configType,
		Labels:    labels,
		Limit:     200,
	}
	listResp, err := daemonClient.List(listArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	var issues []*types.IssueWithCounts
	if listResp.Data != nil {
		if err := json.Unmarshal(listResp.Data, &issues); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
			os.Exit(1)
		}
	}
	for _, iwc := range issues {
		allIssues = append(allIssues, iwc.Issue)
	}

	// Filter to applicable scopes and score
	type scoredIssue struct {
		issue *types.Issue
		score int
	}

	var scored []scoredIssue
	for _, issue := range allIssues {
		score, applicable := scoreConfigBead(issue.Labels, scopeLabels)
		if applicable {
			scored = append(scored, scoredIssue{issue: issue, score: score})
		}
	}

	// Sort by specificity (lowest first for merge order)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score < scored[j].score
	})

	// Deep merge from lowest specificity to highest
	merged := make(map[string]interface{})
	for _, si := range scored {
		if si.issue.Metadata == nil {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal(si.issue.Metadata, &data); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: skipping %s: invalid metadata JSON: %v\n", si.issue.ID, err)
			continue
		}

		deepMergeConfig(merged, data)
	}

	if jsonOutput {
		outputJSON(merged)
		return
	}

	if len(merged) == 0 {
		fmt.Println("No config beads found for merge")
		return
	}

	formatted, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error formatting merged config: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Merged config for category=%s", category)
	if scopeStr != "" {
		fmt.Printf(" scope=%s", scopeStr)
	}
	fmt.Println()
	fmt.Printf("\nSources (%d beads, lowest specificity first):\n", len(scored))
	for _, si := range scored {
		fmt.Printf("  [%d] %s - %s\n", si.score, si.issue.ID, si.issue.Title)
	}
	fmt.Printf("\nMerged result:\n%s\n", string(formatted))
}

// runConfigSetBead implements bd config set-bead.
func runConfigSetBead(cmd *cobra.Command, _ []string) {
	category, _ := cmd.Flags().GetString("category")
	scope, _ := cmd.Flags().GetString("scope")
	title, _ := cmd.Flags().GetString("title")
	data, _ := cmd.Flags().GetString("data")

	// Validate JSON data
	var metadataJSON json.RawMessage
	if err := json.Unmarshal([]byte(data), &metadataJSON); err != nil {
		fmt.Fprintf(os.Stderr, "Error: --data must be valid JSON: %v\n", err)
		os.Exit(1)
	}

	requireDaemon("config set-bead")

	// Generate ID from category+scope
	beadID := generateConfigBeadID(category, scope)

	// Determine rig field and labels from scope
	rigField, scopeLabelsSlice := scopeToRigAndLabels(scope)

	// Build complete label set
	allLabels := append([]string{"config:" + category}, scopeLabelsSlice...)

	// Try to show existing bead first
	showArgs := &rpc.ShowArgs{ID: beadID}
	showResp, err := daemonClient.Show(showArgs)

	if err == nil && showResp.Success {
		// Update existing bead
		titleStr := title
		rigStr := rigField
		updateArgs := &rpc.UpdateArgs{
			ID:        beadID,
			Title:     &titleStr,
			Rig:       &rigStr,
			AddLabels: allLabels,
			Metadata:  &metadataJSON,
		}
		_, err := daemonClient.Update(updateArgs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error updating config bead: %v\n", err)
			os.Exit(1)
		}

		if jsonOutput {
			outputJSON(map[string]string{
				"id":     beadID,
				"action": "updated",
				"title":  title,
			})
		} else {
			fmt.Printf("Updated config bead: %s\n", beadID)
		}
		return
	}

	// Create new bead
	// Config beads use "hq-cfg-" prefix regardless of daemon's configured prefix.
	// Set Prefix="hq" so PrefixOverride skips prefix validation on the server.
	createArgs := &rpc.CreateArgs{
		ID:        beadID,
		Title:     title,
		IssueType: "config",
		Labels:    allLabels,
		Rig:       rigField,
		Metadata:  metadataJSON,
		Prefix:    "hq",
	}
	_, err = daemonClient.Create(createArgs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating config bead: %v\n", err)
		os.Exit(1)
	}

	if jsonOutput {
		outputJSON(map[string]string{
			"id":     beadID,
			"action": "created",
			"title":  title,
		})
	} else {
		fmt.Printf("Created config bead: %s\n", beadID)
	}
}

// printConfigBead pretty-prints a single config bead.
func printConfigBead(issue *types.Issue) {
	fmt.Printf("Config Bead: %s\n", issue.ID)
	fmt.Printf("  Title:  %s\n", issue.Title)
	fmt.Printf("  Type:   %s\n", issue.IssueType)
	fmt.Printf("  Status: %s\n", issue.Status)
	if issue.Rig != "" {
		fmt.Printf("  Rig:    %s\n", issue.Rig)
	}
	if len(issue.Labels) > 0 {
		fmt.Printf("  Labels: %s\n", strings.Join(issue.Labels, ", "))
	}
	if issue.Description != "" {
		fmt.Printf("  Description: %s\n", issue.Description)
	}
	if issue.Metadata != nil {
		// Pretty-print metadata JSON
		var data interface{}
		if err := json.Unmarshal(issue.Metadata, &data); err == nil {
			formatted, err := json.MarshalIndent(data, "  ", "  ")
			if err == nil {
				fmt.Printf("  Metadata:\n  %s\n", string(formatted))
			} else {
				fmt.Printf("  Metadata: %s\n", string(issue.Metadata))
			}
		} else {
			fmt.Printf("  Metadata: %s\n", string(issue.Metadata))
		}
	}
}

// Helper functions

// generateConfigBeadID creates a deterministic ID from category and scope.
// Format: hq-cfg-<category>-<scope-slug>
func generateConfigBeadID(category, scope string) string {
	slug := strings.ToLower(category)

	// Normalize scope to a slug
	scopeSlug := scopeToSlug(scope)
	if scopeSlug != "" {
		slug += "-" + scopeSlug
	}

	return "hq-cfg-" + slug
}

// scopeToSlug converts a scope string to a URL-safe slug.
func scopeToSlug(scope string) string {
	if scope == "global" || scope == "" {
		return "global"
	}

	// Parse comma-separated scope labels
	parts := strings.Split(scope, ",")
	var slugParts []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		// Extract the value after the colon
		if idx := strings.Index(part, ":"); idx >= 0 {
			slugParts = append(slugParts, strings.TrimSpace(part[idx+1:]))
		} else {
			slugParts = append(slugParts, part)
		}
	}

	return strings.Join(slugParts, "-")
}

// scopeToRigAndLabels converts a scope string to a rig field value and label set.
func scopeToRigAndLabels(scope string) (string, []string) {
	if scope == "global" {
		return "*", []string{"scope:global"}
	}

	parts := parseScopeLabels(scope)

	var labels []string
	town := ""
	rig := ""

	for _, part := range parts {
		labels = append(labels, part)
		kv := strings.SplitN(part, ":", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "scope":
			// scope:global already handled above
		case "town":
			town = kv[1]
		case "rig":
			rig = kv[1]
		}
	}

	// Determine rig field based on scope
	rigField := "*"
	if town != "" && rig != "" {
		rigField = town + "/" + rig
	} else if town != "" {
		rigField = town
	}

	return rigField, labels
}

// parseScopeLabels parses a comma-separated scope string into individual labels.
func parseScopeLabels(scope string) []string {
	if scope == "" {
		return nil
	}
	if scope == "global" {
		return []string{"scope:global"}
	}

	parts := strings.Split(scope, ",")
	var labels []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			labels = append(labels, part)
		}
	}
	return labels
}

// scoreConfigBead scores a config bead by specificity relative to the target scope.
// Returns (score, applicable). A bead is applicable if its scope is a subset of
// or matches the target scope.
func scoreConfigBead(beadLabels []string, targetScope []string) (int, bool) {
	// Build lookup maps
	beadLabelSet := make(map[string]bool)
	for _, l := range beadLabels {
		beadLabelSet[l] = true
	}

	targetLabelSet := make(map[string]bool)
	for _, l := range targetScope {
		targetLabelSet[l] = true
	}

	// If no target scope, all beads are applicable (score by their own labels)
	if len(targetScope) == 0 {
		return scoreByLabels(beadLabelSet), true
	}

	// Check that the bead's scope labels are compatible with the target
	// Global beads are always applicable
	if beadLabelSet["scope:global"] {
		return scoreByLabels(beadLabelSet), true
	}

	// For non-global beads, check that all scope-relevant labels match
	for _, label := range beadLabels {
		prefix := labelPrefix(label)
		if prefix == "config" {
			continue // Skip category labels
		}
		if prefix == "scope" || prefix == "town" || prefix == "rig" || prefix == "role" || prefix == "agent" {
			if !targetLabelSet[label] {
				return 0, false // Bead requires a scope the target doesn't have
			}
		}
	}

	return scoreByLabels(beadLabelSet), true
}

// scoreByLabels assigns a specificity score based on scope labels.
func scoreByLabels(labels map[string]bool) int {
	hasAgent := false
	hasRole := false
	hasRig := false
	hasTown := false

	for label := range labels {
		prefix := labelPrefix(label)
		switch prefix {
		case "agent":
			hasAgent = true
		case "role":
			hasRole = true
		case "rig":
			hasRig = true
		case "town":
			hasTown = true
		}
	}

	if hasAgent && hasTown {
		return 4
	}
	if hasRole && hasTown {
		return 3
	}
	if hasRig || hasTown {
		return 2
	}
	if hasRole {
		return 1
	}
	return 0
}

// labelPrefix returns the part before the colon in a label.
func labelPrefix(label string) string {
	if idx := strings.Index(label, ":"); idx >= 0 {
		return label[:idx]
	}
	return label
}

// deepMergeConfig merges source into target using the config bead merge strategy:
// - Hook arrays (under "hooks" key): APPEND
// - Top-level keys: OVERRIDE
// - Explicit null: SUPPRESS
func deepMergeConfig(target, source map[string]interface{}) {
	for key, srcVal := range source {
		// Explicit null suppresses
		if srcVal == nil {
			target[key] = nil
			continue
		}

		// Special handling for "hooks" key: deep merge with append for arrays
		if key == "hooks" {
			srcHooks, srcOK := srcVal.(map[string]interface{})
			if !srcOK {
				target[key] = srcVal
				continue
			}

			existingHooks, existOK := target[key].(map[string]interface{})
			if !existOK {
				// No existing hooks, just set
				target[key] = srcVal
				continue
			}

			// Merge each hook type with append
			for hookName, hookVal := range srcHooks {
				if hookVal == nil {
					// Explicit null suppresses this hook
					existingHooks[hookName] = nil
					continue
				}

				srcArr, srcIsArr := hookVal.([]interface{})
				existArr, existIsArr := existingHooks[hookName].([]interface{})

				if srcIsArr && existIsArr {
					// APPEND: more specific adds to less specific
					existingHooks[hookName] = append(existArr, srcArr...)
				} else {
					// Not both arrays, override
					existingHooks[hookName] = hookVal
				}
			}
			continue
		}

		// Default: OVERRIDE
		target[key] = srcVal
	}
}

// truncateConfigMeta truncates a string to maxLen, adding "..." if truncated.
func truncateConfigMeta(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// extractScopeFromLabels extracts scope-related labels.
func extractScopeFromLabels(labels []string) string {
	var scopeParts []string
	for _, l := range labels {
		prefix := labelPrefix(l)
		if prefix == "scope" || prefix == "town" || prefix == "rig" || prefix == "role" || prefix == "agent" {
			scopeParts = append(scopeParts, l)
		}
	}
	return strings.Join(scopeParts, ", ")
}

// extractCategoryFromLabels extracts config category from labels.
func extractCategoryFromLabels(labels []string) string {
	for _, l := range labels {
		if strings.HasPrefix(l, "config:") {
			return strings.TrimPrefix(l, "config:")
		}
	}
	return ""
}
