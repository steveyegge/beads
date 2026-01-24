package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage skill beads",
	Long: `Manage skill beads for capability tracking and work routing.

Skills are first-class beads that represent capabilities. They can be:
- Attached to agents (agent provides skill)
- Required by issues (issue requires skill)
- Required by formulas (workflow requires skill)

Skills enable intelligent work routing by matching required skills to agents
that provide them.

Examples:
  bd skill create go-testing --description "Write and run Go tests"
  bd skill list --category testing
  bd skill show skill-go-testing`,
}

var skillCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new skill bead",
	Long: `Create a new skill bead with the given name and metadata.

Skills are stored as beads with issue_type=skill and prefix "skill-".
The skill name becomes both the ID suffix and the skill_name field.

Examples:
  bd skill create go-testing --description "Write and run Go tests" --category testing
  bd skill create sql-migrations --version 1.0.0 --category devops
  bd skill create pr-review --inputs "PR URL,Code changes" --outputs "Review comments"`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillCreate,
}

var skillShowCmd = &cobra.Command{
	Use:   "show <skill-id>",
	Short: "Show skill bead details",
	Long: `Show detailed information about a skill bead.

Accepts either the full skill ID (skill-go-testing) or just the name (go-testing).

Examples:
  bd skill show go-testing
  bd skill show skill-go-testing`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillShow,
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all skill beads",
	Long: `List all skill beads with optional filtering by category.

Examples:
  bd skill list
  bd skill list --category testing
  bd skill list --json`,
	RunE: runSkillList,
}

var skillAddCmd = &cobra.Command{
	Use:   "add <agent-id> <skill-id>",
	Short: "Add a skill to an agent",
	Long: `Declare that an agent provides/has a skill.

Creates a "provides-skill" dependency edge from the agent to the skill.
This enables skill-based work routing.

Examples:
  bd skill add beads/crew/skills go-testing
  bd skill add agent-alpha skill-sql-migrations`,
	Args: cobra.ExactArgs(2),
	RunE: runSkillAdd,
}

var skillRequireCmd = &cobra.Command{
	Use:   "require <issue-id> <skill-id>",
	Short: "Mark that an issue requires a skill",
	Long: `Declare that an issue requires a specific skill.

Creates a "requires-skill" dependency edge from the issue to the skill.
This enables skill-based work filtering (bd ready --with-skills).

Examples:
  bd skill require bd-abc123 go-testing
  bd skill require gt-xyz skill-pr-review`,
	Args: cobra.ExactArgs(2),
	RunE: runSkillRequire,
}

var skillProvidersCmd = &cobra.Command{
	Use:   "providers <skill-id>",
	Short: "List agents that provide a skill",
	Long: `Show all agents that have declared they provide a skill.

Examples:
  bd skill providers go-testing
  bd skill providers skill-sql-migrations`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillProviders,
}

var skillRequiredCmd = &cobra.Command{
	Use:   "required <issue-id>",
	Short: "List skills required by an issue",
	Long: `Show all skills that an issue requires.

Examples:
  bd skill required bd-abc123
  bd skill required gt-xyz`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillRequired,
}

var skillLoadCmd = &cobra.Command{
	Use:   "load <skill-id>",
	Short: "Load and display a skill's SKILL.md content",
	Long: `Load a skill's documentation from its claude_skill_path.

This outputs the SKILL.md content that teaches Claude how to use the skill.
If the skill has no claude_skill_path set, shows the skill's metadata instead.

Examples:
  bd skill load go-testing
  bd skill load skill-beads-usage`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillLoad,
}

var skillPrimeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Output skill content for current agent (for gt prime integration)",
	Long: `Output the SKILL.md content for all skills the current agent provides.

This is designed to be called by gt prime to inject skill documentation
into Claude's context at session start. Only loads skills that have
claude_skill_path set.

The agent is determined by BD_ACTOR or --actor flag.

Examples:
  bd skill prime              # Output skills for current agent
  bd skill prime --actor foo  # Output skills for specific agent`,
	RunE: runSkillPrime,
}

// Flag variables for skill commands
var (
	skillDescription    string
	skillVersion        string
	skillCategory       string
	skillInputs         []string
	skillOutputs        []string
	skillExamples       []string
	skillClaudePath     string
	skillFilterCategory string
)

func init() {
	// skill create flags
	skillCreateCmd.Flags().StringVarP(&skillDescription, "description", "d", "", "Skill description")
	skillCreateCmd.Flags().StringVar(&skillVersion, "version", "1.0.0", "Skill version (semver)")
	skillCreateCmd.Flags().StringVar(&skillCategory, "category", "", "Skill category (e.g., testing, devops, docs)")
	skillCreateCmd.Flags().StringSliceVar(&skillInputs, "inputs", nil, "What the skill needs (comma-separated)")
	skillCreateCmd.Flags().StringSliceVar(&skillOutputs, "outputs", nil, "What the skill produces (comma-separated)")
	skillCreateCmd.Flags().StringSliceVar(&skillExamples, "examples", nil, "Usage examples (comma-separated)")
	skillCreateCmd.Flags().StringVar(&skillClaudePath, "claude-skill-path", "", "Path to SKILL.md for Claude integration")

	// skill list flags
	skillListCmd.Flags().StringVar(&skillFilterCategory, "category", "", "Filter by category")

	// Add subcommands
	skillCmd.AddCommand(skillCreateCmd)
	skillCmd.AddCommand(skillShowCmd)
	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillAddCmd)
	skillCmd.AddCommand(skillRequireCmd)
	skillCmd.AddCommand(skillProvidersCmd)
	skillCmd.AddCommand(skillRequiredCmd)
	skillCmd.AddCommand(skillLoadCmd)
	skillCmd.AddCommand(skillPrimeCmd)

	// Add to root
	rootCmd.AddCommand(skillCmd)
}

func runSkillCreate(cmd *cobra.Command, args []string) error {
	CheckReadonly("skill create")

	skillName := args[0]
	ctx := rootCtx

	// Normalize skill name (lowercase, hyphens for spaces)
	skillName = strings.ToLower(strings.ReplaceAll(skillName, " ", "-"))

	// Generate skill ID
	skillID := "skill-" + skillName

	// Build title from name
	title := strings.Title(strings.ReplaceAll(skillName, "-", " "))

	// Create the skill issue
	issue := &types.Issue{
		ID:          skillID,
		Title:       title,
		Description: skillDescription,
		IssueType:   types.TypeSkill,
		Status:      types.StatusPinned, // Skills are pinned by default
		Priority:    2,                  // Default priority

		// Skill-specific fields
		SkillName:       skillName,
		SkillVersion:    skillVersion,
		SkillCategory:   skillCategory,
		SkillInputs:     skillInputs,
		SkillOutputs:    skillOutputs,
		SkillExamples:   skillExamples,
		ClaudeSkillPath: skillClaudePath,
	}

	// Use direct storage mode for skill creation
	// TODO: Add skill fields to RPC CreateArgs for daemon support
	if store == nil {
		return fmt.Errorf("database not initialized - run 'bd init' first")
	}
	actor := getActor()
	if err := store.CreateIssue(ctx, issue, actor); err != nil {
		return fmt.Errorf("failed to create skill: %w", err)
	}

	// Output based on format
	if jsonOutput {
		output := map[string]interface{}{
			"id":             skillID,
			"skill_name":     skillName,
			"skill_version":  skillVersion,
			"skill_category": skillCategory,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(output)
	}

	fmt.Printf("Created skill: %s\n", ui.RenderID(skillID))
	return nil
}

func runSkillShow(cmd *cobra.Command, args []string) error {
	skillArg := args[0]
	ctx := rootCtx

	// Normalize skill ID
	skillID := skillArg
	if !strings.HasPrefix(skillID, "skill-") {
		skillID = "skill-" + skillID
	}

	// Use direct storage mode for skill show
	if store == nil {
		return fmt.Errorf("database not initialized - run 'bd init' first")
	}
	issue, err := store.GetIssue(ctx, skillID)
	if err != nil {
		return fmt.Errorf("skill not found: %s", skillID)
	}

	// Verify it's a skill
	if issue.IssueType != types.TypeSkill {
		return fmt.Errorf("%s is not a skill (type: %s)", skillID, issue.IssueType)
	}

	// Output based on format
	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(issue)
	}

	// Human-readable output
	fmt.Printf("%s %s\n", ui.RenderType("SKILL"), ui.RenderID(issue.ID))
	fmt.Printf("  Name:     %s\n", issue.SkillName)
	fmt.Printf("  Version:  %s\n", issue.SkillVersion)
	if issue.SkillCategory != "" {
		fmt.Printf("  Category: %s\n", issue.SkillCategory)
	}
	if issue.Description != "" {
		fmt.Printf("  Desc:     %s\n", issue.Description)
	}
	if len(issue.SkillInputs) > 0 {
		fmt.Printf("  Inputs:   %s\n", strings.Join(issue.SkillInputs, ", "))
	}
	if len(issue.SkillOutputs) > 0 {
		fmt.Printf("  Outputs:  %s\n", strings.Join(issue.SkillOutputs, ", "))
	}
	if len(issue.SkillExamples) > 0 {
		fmt.Printf("  Examples: %s\n", strings.Join(issue.SkillExamples, ", "))
	}
	if issue.ClaudeSkillPath != "" {
		fmt.Printf("  Claude:   %s\n", issue.ClaudeSkillPath)
	}

	return nil
}

func runSkillList(cmd *cobra.Command, args []string) error {
	ctx := rootCtx

	// Get all skills using SearchIssues with skill type filter
	if store == nil {
		return fmt.Errorf("database not initialized - run 'bd init' first")
	}
	skillType := types.TypeSkill
	filter := types.IssueFilter{
		IssueType: &skillType,
	}
	issues, err := store.SearchIssues(ctx, "", filter)
	if err != nil {
		return fmt.Errorf("failed to list skills: %w", err)
	}

	// Filter by category if specified
	var skills []*types.Issue
	for _, issue := range issues {
		// Apply category filter if specified
		if skillFilterCategory != "" && issue.SkillCategory != skillFilterCategory {
			continue
		}
		skills = append(skills, issue)
	}

	// Output based on format
	if jsonOutput {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(skills)
	}

	if len(skills) == 0 {
		if skillFilterCategory != "" {
			fmt.Printf("No skills found in category: %s\n", skillFilterCategory)
		} else {
			fmt.Println("No skills found")
		}
		return nil
	}

	// Human-readable table
	fmt.Printf("Skills (%d):\n", len(skills))
	for _, s := range skills {
		category := s.SkillCategory
		if category == "" {
			category = "-"
		}
		version := s.SkillVersion
		if version == "" {
			version = "-"
		}
		fmt.Printf("  %-25s  v%-8s  %-12s  %s\n",
			s.SkillName, version, category, truncateSkillText(s.Description, 40))
	}

	return nil
}

func truncateSkillText(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

// runSkillAdd adds a skill to an agent (creates provides-skill edge)
func runSkillAdd(cmd *cobra.Command, args []string) error {
	CheckReadonly("skill add")

	agentArg := args[0]
	skillArg := args[1]
	ctx := rootCtx

	// Normalize skill ID
	skillID := skillArg
	if !strings.HasPrefix(skillID, "skill-") {
		skillID = "skill-" + skillID
	}

	// Resolve IDs
	var agentID, resolvedSkillID string

	if daemonClient != nil {
		// Resolve agent ID
		resolveArgs := &rpc.ResolveIDArgs{ID: agentArg}
		resp, err := daemonClient.ResolveID(resolveArgs)
		if err != nil {
			return fmt.Errorf("resolving agent ID %s: %w", agentArg, err)
		}
		if err := json.Unmarshal(resp.Data, &agentID); err != nil {
			return fmt.Errorf("unmarshaling resolved ID: %w", err)
		}

		// Resolve skill ID
		resolveArgs = &rpc.ResolveIDArgs{ID: skillID}
		resp, err = daemonClient.ResolveID(resolveArgs)
		if err != nil {
			return fmt.Errorf("resolving skill ID %s: %w", skillID, err)
		}
		if err := json.Unmarshal(resp.Data, &resolvedSkillID); err != nil {
			return fmt.Errorf("unmarshaling resolved ID: %w", err)
		}
	} else {
		if store == nil {
			return fmt.Errorf("database not initialized - run 'bd init' first")
		}
		var err error
		agentID, err = utils.ResolvePartialID(ctx, store, agentArg)
		if err != nil {
			return fmt.Errorf("resolving agent ID %s: %w", agentArg, err)
		}

		resolvedSkillID, err = utils.ResolvePartialID(ctx, store, skillID)
		if err != nil {
			return fmt.Errorf("resolving skill ID %s: %w", skillID, err)
		}
	}

	// Verify skill exists and is a skill type
	if store != nil {
		skill, err := store.GetIssue(ctx, resolvedSkillID)
		if err != nil {
			return fmt.Errorf("skill not found: %s", resolvedSkillID)
		}
		if skill.IssueType != types.TypeSkill {
			return fmt.Errorf("%s is not a skill (type: %s)", resolvedSkillID, skill.IssueType)
		}
	}

	// Create provides-skill dependency edge (agent -> skill)
	if daemonClient != nil {
		depArgs := &rpc.DepAddArgs{
			FromID:  agentID,
			ToID:    resolvedSkillID,
			DepType: string(types.DepProvidesSkill),
		}
		_, err := daemonClient.AddDependency(depArgs)
		if err != nil {
			return fmt.Errorf("failed to add skill: %w", err)
		}
	} else {
		dep := &types.Dependency{
			IssueID:     agentID,
			DependsOnID: resolvedSkillID,
			Type:        types.DepProvidesSkill,
		}
		if err := store.AddDependency(ctx, dep, actor); err != nil {
			return fmt.Errorf("failed to add skill: %w", err)
		}
		markDirtyAndScheduleFlush()
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"status":   "added",
			"agent_id": agentID,
			"skill_id": resolvedSkillID,
			"type":     string(types.DepProvidesSkill),
		})
		return nil
	}

	fmt.Printf("%s Agent %s now provides skill %s\n",
		ui.RenderPass("✓"), agentID, resolvedSkillID)
	return nil
}

// runSkillRequire marks that an issue requires a skill (creates requires-skill edge)
func runSkillRequire(cmd *cobra.Command, args []string) error {
	CheckReadonly("skill require")

	issueArg := args[0]
	skillArg := args[1]
	ctx := rootCtx

	// Normalize skill ID
	skillID := skillArg
	if !strings.HasPrefix(skillID, "skill-") {
		skillID = "skill-" + skillID
	}

	// Resolve IDs
	var issueID, resolvedSkillID string

	if daemonClient != nil {
		// Resolve issue ID
		resolveArgs := &rpc.ResolveIDArgs{ID: issueArg}
		resp, err := daemonClient.ResolveID(resolveArgs)
		if err != nil {
			return fmt.Errorf("resolving issue ID %s: %w", issueArg, err)
		}
		if err := json.Unmarshal(resp.Data, &issueID); err != nil {
			return fmt.Errorf("unmarshaling resolved ID: %w", err)
		}

		// Resolve skill ID
		resolveArgs = &rpc.ResolveIDArgs{ID: skillID}
		resp, err = daemonClient.ResolveID(resolveArgs)
		if err != nil {
			return fmt.Errorf("resolving skill ID %s: %w", skillID, err)
		}
		if err := json.Unmarshal(resp.Data, &resolvedSkillID); err != nil {
			return fmt.Errorf("unmarshaling resolved ID: %w", err)
		}
	} else {
		if store == nil {
			return fmt.Errorf("database not initialized - run 'bd init' first")
		}
		var err error
		issueID, err = utils.ResolvePartialID(ctx, store, issueArg)
		if err != nil {
			return fmt.Errorf("resolving issue ID %s: %w", issueArg, err)
		}

		resolvedSkillID, err = utils.ResolvePartialID(ctx, store, skillID)
		if err != nil {
			return fmt.Errorf("resolving skill ID %s: %w", skillID, err)
		}
	}

	// Verify skill exists and is a skill type
	if store != nil {
		skill, err := store.GetIssue(ctx, resolvedSkillID)
		if err != nil {
			return fmt.Errorf("skill not found: %s", resolvedSkillID)
		}
		if skill.IssueType != types.TypeSkill {
			return fmt.Errorf("%s is not a skill (type: %s)", resolvedSkillID, skill.IssueType)
		}
	}

	// Create requires-skill dependency edge (issue -> skill)
	if daemonClient != nil {
		depArgs := &rpc.DepAddArgs{
			FromID:  issueID,
			ToID:    resolvedSkillID,
			DepType: string(types.DepRequiresSkill),
		}
		_, err := daemonClient.AddDependency(depArgs)
		if err != nil {
			return fmt.Errorf("failed to add skill requirement: %w", err)
		}
	} else {
		dep := &types.Dependency{
			IssueID:     issueID,
			DependsOnID: resolvedSkillID,
			Type:        types.DepRequiresSkill,
		}
		if err := store.AddDependency(ctx, dep, actor); err != nil {
			return fmt.Errorf("failed to add skill requirement: %w", err)
		}
		markDirtyAndScheduleFlush()
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"status":   "added",
			"issue_id": issueID,
			"skill_id": resolvedSkillID,
			"type":     string(types.DepRequiresSkill),
		})
		return nil
	}

	fmt.Printf("%s Issue %s now requires skill %s\n",
		ui.RenderPass("✓"), issueID, resolvedSkillID)
	return nil
}

// runSkillProviders lists agents that provide a skill
func runSkillProviders(cmd *cobra.Command, args []string) error {
	skillArg := args[0]
	ctx := rootCtx

	// Normalize skill ID
	skillID := skillArg
	if !strings.HasPrefix(skillID, "skill-") {
		skillID = "skill-" + skillID
	}

	if store == nil {
		return fmt.Errorf("database not initialized - run 'bd init' first")
	}

	// Resolve skill ID
	resolvedSkillID, err := utils.ResolvePartialID(ctx, store, skillID)
	if err != nil {
		return fmt.Errorf("resolving skill ID %s: %w", skillID, err)
	}

	// Verify it's a skill
	skill, err := store.GetIssue(ctx, resolvedSkillID)
	if err != nil {
		return fmt.Errorf("skill not found: %s", resolvedSkillID)
	}
	if skill.IssueType != types.TypeSkill {
		return fmt.Errorf("%s is not a skill (type: %s)", resolvedSkillID, skill.IssueType)
	}

	// Get dependents with provides-skill type
	dependents, err := store.GetDependentsWithMetadata(ctx, resolvedSkillID)
	if err != nil {
		return fmt.Errorf("failed to get skill providers: %w", err)
	}

	// Filter to only provides-skill edges
	var providers []*types.IssueWithDependencyMetadata
	for _, dep := range dependents {
		if dep.DependencyType == types.DepProvidesSkill {
			providers = append(providers, dep)
		}
	}

	if jsonOutput {
		if providers == nil {
			providers = []*types.IssueWithDependencyMetadata{}
		}
		outputJSON(providers)
		return nil
	}

	if len(providers) == 0 {
		fmt.Printf("No agents provide skill %s\n", resolvedSkillID)
		return nil
	}

	fmt.Printf("Agents providing %s (%d):\n", skill.SkillName, len(providers))
	for _, p := range providers {
		fmt.Printf("  %s: %s\n", ui.RenderID(p.ID), p.Title)
	}

	return nil
}

// runSkillRequired lists skills required by an issue
func runSkillRequired(cmd *cobra.Command, args []string) error {
	issueArg := args[0]
	ctx := rootCtx

	if store == nil {
		return fmt.Errorf("database not initialized - run 'bd init' first")
	}

	// Resolve issue ID
	issueID, err := utils.ResolvePartialID(ctx, store, issueArg)
	if err != nil {
		return fmt.Errorf("resolving issue ID %s: %w", issueArg, err)
	}

	// Get the issue to display its title
	issue, err := store.GetIssue(ctx, issueID)
	if err != nil {
		return fmt.Errorf("issue not found: %s", issueID)
	}

	// Get dependencies with requires-skill type
	deps, err := store.GetDependenciesWithMetadata(ctx, issueID)
	if err != nil {
		return fmt.Errorf("failed to get skill requirements: %w", err)
	}

	// Filter to only requires-skill edges
	var requiredSkills []*types.IssueWithDependencyMetadata
	for _, dep := range deps {
		if dep.DependencyType == types.DepRequiresSkill {
			requiredSkills = append(requiredSkills, dep)
		}
	}

	if jsonOutput {
		if requiredSkills == nil {
			requiredSkills = []*types.IssueWithDependencyMetadata{}
		}
		outputJSON(requiredSkills)
		return nil
	}

	if len(requiredSkills) == 0 {
		fmt.Printf("Issue %s has no skill requirements\n", issueID)
		return nil
	}

	fmt.Printf("Skills required by %s (%d):\n", issue.Title, len(requiredSkills))
	for _, s := range requiredSkills {
		fmt.Printf("  %s: %s\n", ui.RenderID(s.ID), s.Title)
	}

	return nil
}

// runSkillLoad loads and displays a skill's SKILL.md content
func runSkillLoad(cmd *cobra.Command, args []string) error {
	skillArg := args[0]
	ctx := rootCtx

	// Normalize skill ID
	skillID := skillArg
	if !strings.HasPrefix(skillID, "skill-") {
		skillID = "skill-" + skillID
	}

	if store == nil {
		return fmt.Errorf("database not initialized - run 'bd init' first")
	}

	// Resolve skill ID
	resolvedSkillID, err := utils.ResolvePartialID(ctx, store, skillID)
	if err != nil {
		return fmt.Errorf("resolving skill ID %s: %w", skillID, err)
	}

	// Get the skill
	skill, err := store.GetIssue(ctx, resolvedSkillID)
	if err != nil {
		return fmt.Errorf("skill not found: %s", resolvedSkillID)
	}
	if skill.IssueType != types.TypeSkill {
		return fmt.Errorf("%s is not a skill (type: %s)", resolvedSkillID, skill.IssueType)
	}

	// If claude_skill_path is set, try to load the file
	if skill.ClaudeSkillPath != "" {
		// Resolve the path relative to the .beads directory
		skillPath := skill.ClaudeSkillPath

		// Try multiple locations: absolute, relative to cwd, relative to repo root
		candidates := []string{
			skillPath,
			filepath.Join(".", skillPath),
		}

		// Find the repo root by looking for .git
		if cwd, err := os.Getwd(); err == nil {
			dir := cwd
			for dir != "/" {
				if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
					candidates = append(candidates, filepath.Join(dir, skillPath))
					break
				}
				dir = filepath.Dir(dir)
			}
		}

		var content []byte
		var loadedFrom string
		for _, candidate := range candidates {
			if data, err := os.ReadFile(candidate); err == nil {
				content = data
				loadedFrom = candidate
				break
			}
		}

		if content != nil {
			if jsonOutput {
				outputJSON(map[string]interface{}{
					"skill_id":          resolvedSkillID,
					"skill_name":        skill.SkillName,
					"claude_skill_path": skill.ClaudeSkillPath,
					"loaded_from":       loadedFrom,
					"content":           string(content),
				})
				return nil
			}

			// Output the skill content directly for Claude to read
			fmt.Printf("# Skill: %s\n\n", skill.SkillName)
			fmt.Printf("Source: %s\n\n", loadedFrom)
			fmt.Printf("---\n\n")
			fmt.Print(string(content))
			return nil
		}

		// Path set but file not found - warn and fall through to metadata
		fmt.Fprintf(os.Stderr, "Warning: claude_skill_path '%s' not found, showing metadata instead\n\n", skill.ClaudeSkillPath)
	}

	// No claude_skill_path or file not found - show metadata
	if jsonOutput {
		outputJSON(skill)
		return nil
	}

	fmt.Printf("# Skill: %s\n\n", skill.SkillName)
	fmt.Printf("**No SKILL.md file associated with this skill.**\n\n")
	fmt.Printf("## Metadata\n\n")
	fmt.Printf("- **ID**: %s\n", skill.ID)
	fmt.Printf("- **Version**: %s\n", skill.SkillVersion)
	if skill.SkillCategory != "" {
		fmt.Printf("- **Category**: %s\n", skill.SkillCategory)
	}
	if skill.Description != "" {
		fmt.Printf("\n## Description\n\n%s\n", skill.Description)
	}
	if len(skill.SkillInputs) > 0 {
		fmt.Printf("\n## Inputs\n\n")
		for _, input := range skill.SkillInputs {
			fmt.Printf("- %s\n", input)
		}
	}
	if len(skill.SkillOutputs) > 0 {
		fmt.Printf("\n## Outputs\n\n")
		for _, output := range skill.SkillOutputs {
			fmt.Printf("- %s\n", output)
		}
	}
	if len(skill.SkillExamples) > 0 {
		fmt.Printf("\n## Examples\n\n")
		for _, example := range skill.SkillExamples {
			fmt.Printf("- %s\n", example)
		}
	}

	return nil
}

// runSkillPrime outputs skill content for the current agent's skills
func runSkillPrime(cmd *cobra.Command, args []string) error {
	ctx := rootCtx

	if store == nil {
		// No database - nothing to output
		return nil
	}

	agentID := getActor()
	if agentID == "" {
		return nil // No agent, no skills
	}

	// Get skills this agent provides
	// Try multiple ID patterns for the agent
	agentPatterns := []string{
		agentID,
		"agent-" + agentID,
	}

	var agentSkillIDs []string
	for _, pattern := range agentPatterns {
		deps, err := store.GetDependenciesWithMetadata(ctx, pattern)
		if err != nil {
			continue
		}
		for _, dep := range deps {
			if dep.DependencyType == types.DepProvidesSkill {
				agentSkillIDs = append(agentSkillIDs, dep.ID)
			}
		}
		if len(agentSkillIDs) > 0 {
			break
		}
	}

	if len(agentSkillIDs) == 0 {
		return nil // No skills to output
	}

	// Load each skill's content
	var loadedSkills []struct {
		Name    string
		Content string
	}

	for _, skillID := range agentSkillIDs {
		skill, err := store.GetIssue(ctx, skillID)
		if err != nil || skill.IssueType != types.TypeSkill {
			continue
		}

		if skill.ClaudeSkillPath == "" {
			continue // No SKILL.md to load
		}

		// Try to load the file
		content := loadSkillFile(skill.ClaudeSkillPath)
		if content != "" {
			loadedSkills = append(loadedSkills, struct {
				Name    string
				Content string
			}{
				Name:    skill.SkillName,
				Content: content,
			})
		}
	}

	if len(loadedSkills) == 0 {
		return nil
	}

	// Output skills section
	if jsonOutput {
		outputJSON(loadedSkills)
		return nil
	}

	fmt.Println("\n---\n")
	fmt.Printf("## Your Skills (%d loaded)\n\n", len(loadedSkills))
	fmt.Println("The following skill documentation has been loaded for your capabilities:\n")

	for _, skill := range loadedSkills {
		fmt.Printf("### %s\n\n", skill.Name)
		fmt.Println(skill.Content)
		fmt.Println("\n---\n")
	}

	return nil
}

// loadSkillFile tries to load a skill file from various locations
func loadSkillFile(skillPath string) string {
	candidates := []string{
		skillPath,
		filepath.Join(".", skillPath),
	}

	// Find the repo root by looking for .git
	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for dir != "/" {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				candidates = append(candidates, filepath.Join(dir, skillPath))
				break
			}
			dir = filepath.Dir(dir)
		}
	}

	for _, candidate := range candidates {
		if data, err := os.ReadFile(candidate); err == nil {
			return string(data)
		}
	}

	return ""
}
