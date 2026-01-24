package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/rpc"
	"github.com/steveyegge/beads/internal/storage"
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

var skillSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync beads skills to .claude/skills/ for Claude Code discovery",
	Long: `Sync beads skills to .claude/skills/ directory.

This makes beads skills discoverable by Claude Code's native Skill tool.
For each skill bead with a claude_skill_path set, creates a symlink or
copies the SKILL.md file to .claude/skills/<skill-name>/.

Run this:
- After creating new skills: bd skill create ... && bd skill sync
- At session start via hook
- Manually to update skill availability

Examples:
  bd skill sync              # Sync all skills with claude_skill_path
  bd skill sync --clean      # Remove .claude/skills/ first, then sync`,
	RunE: runSkillSync,
}

var skillSyncClean bool

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
	skillTown           bool // Create/list skills at town level (hq- prefix)
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
	skillCreateCmd.Flags().BoolVar(&skillTown, "town", false, "Create skill at town level (accessible from all rigs)")

	// skill list flags
	skillListCmd.Flags().StringVar(&skillFilterCategory, "category", "", "Filter by category")
	skillListCmd.Flags().BoolVar(&skillTown, "town", false, "List town-level skills only")

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
	skillCmd.AddCommand(skillSyncCmd)
	skillCmd.AddCommand(skillSpyCmd)
	skillCmd.AddCommand(skillTestCmd)

	// skill sync flags
	skillSyncCmd.Flags().BoolVar(&skillSyncClean, "clean", false, "Remove existing .claude/skills/ before syncing")

	// skill spy flags
	skillSpyCmd.Flags().IntVar(&spyLines, "lines", 200, "Number of lines to capture from session")

	// skill test flags
	skillTestCmd.Flags().BoolVar(&testSetupOnly, "setup-only", false, "Only perform setup, don't monitor")
	skillTestCmd.Flags().IntVar(&testTimeout, "timeout", 60, "Timeout in seconds for monitoring")
	skillTestCmd.Flags().IntVar(&testInterval, "interval", 5, "Poll interval in seconds")

	// Add to root
	rootCmd.AddCommand(skillCmd)
}

func runSkillCreate(cmd *cobra.Command, args []string) error {
	CheckReadonly("skill create")

	skillName := args[0]
	ctx := rootCtx

	// Normalize skill name (lowercase, hyphens for spaces)
	skillName = strings.ToLower(strings.ReplaceAll(skillName, " ", "-"))

	// Generate skill ID - use hq-skill- prefix for town-level skills
	var skillID string
	if skillTown {
		skillID = "hq-skill-" + skillName
	} else {
		skillID = "skill-" + skillName
	}

	// Build title from name
	title := strings.Title(strings.ReplaceAll(skillName, "-", " "))
	if skillTown {
		title = "[Town] " + title
	}

	// Use daemon if available
	if daemonClient != nil {
		createArgs := &rpc.CreateArgs{
			ID:              skillID,
			Title:           title,
			Description:     skillDescription,
			IssueType:       string(types.TypeSkill),
			Priority:        2,
			Pinned:          true, // Skills are pinned by default
			SkillName:       skillName,
			SkillVersion:    skillVersion,
			SkillCategory:   skillCategory,
			SkillInputs:     skillInputs,
			SkillOutputs:    skillOutputs,
			SkillExamples:   skillExamples,
			ClaudeSkillPath: skillClaudePath,
		}
		resp, err := daemonClient.Create(createArgs)
		if err != nil {
			return fmt.Errorf("failed to create skill via daemon: %w", err)
		}
		if !resp.Success {
			return fmt.Errorf("failed to create skill: %s", resp.Error)
		}

		// Parse response for output
		var issue types.Issue
		if err := json.Unmarshal(resp.Data, &issue); err != nil {
			return fmt.Errorf("parsing create response: %w", err)
		}

		if jsonOutput {
			output := map[string]interface{}{
				"id":             issue.ID,
				"skill_name":     skillName,
				"skill_version":  skillVersion,
				"skill_category": skillCategory,
			}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			return enc.Encode(output)
		}

		fmt.Printf("Created skill: %s\n", ui.RenderID(issue.ID))
		return nil
	}

	// Fall back to direct storage
	if store == nil {
		return fmt.Errorf("database not initialized - run 'bd init' first or start daemon")
	}

	issue := &types.Issue{
		ID:          skillID,
		Title:       title,
		Description: skillDescription,
		IssueType:   types.TypeSkill,
		Status:      types.StatusPinned,
		Priority:    2,
		SkillName:       skillName,
		SkillVersion:    skillVersion,
		SkillCategory:   skillCategory,
		SkillInputs:     skillInputs,
		SkillOutputs:    skillOutputs,
		SkillExamples:   skillExamples,
		ClaudeSkillPath: skillClaudePath,
	}

	actor := getActor()
	if err := store.CreateIssue(ctx, issue, actor); err != nil {
		return fmt.Errorf("failed to create skill: %w", err)
	}

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

	// Normalize skill ID - accept full ID, skill name, or hq-skill- prefix
	skillID := skillArg
	if !strings.HasPrefix(skillID, "skill-") && !strings.HasPrefix(skillID, "hq-skill-") {
		skillID = "skill-" + skillID
	}

	// Get skill via daemon or direct storage
	var issue *types.Issue
	if daemonClient != nil {
		showArgs := &rpc.ShowArgs{ID: skillID}
		resp, err := daemonClient.Show(showArgs)
		if err != nil {
			return fmt.Errorf("skill not found: %s", skillID)
		}
		if !resp.Success {
			return fmt.Errorf("skill not found: %s", skillID)
		}
		if err := json.Unmarshal(resp.Data, &issue); err != nil {
			return fmt.Errorf("parsing show response: %w", err)
		}
	} else {
		err := withStorage(ctx, store, dbPath, lockTimeout, func(s storage.Storage) error {
			var err error
			issue, err = s.GetIssue(ctx, skillID)
			return err
		})
		if err != nil {
			return fmt.Errorf("skill not found: %s", skillID)
		}
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

	// Get all skills using List with skill type filter
	var issues []*types.Issue
	if daemonClient != nil {
		listArgs := &rpc.ListArgs{
			IssueType: string(types.TypeSkill),
		}
		resp, err := daemonClient.List(listArgs)
		if err != nil {
			return fmt.Errorf("failed to list skills: %w", err)
		}
		if err := json.Unmarshal(resp.Data, &issues); err != nil {
			return fmt.Errorf("failed to decode skills: %w", err)
		}
	} else {
		skillType := types.TypeSkill
		filter := types.IssueFilter{
			IssueType: &skillType,
		}
		err := withStorage(ctx, store, dbPath, lockTimeout, func(s storage.Storage) error {
			var err error
			issues, err = s.SearchIssues(ctx, "", filter)
			return err
		})
		if err != nil {
			return fmt.Errorf("failed to list skills: %w", err)
		}
	}

	// Filter by category and town flag
	var skills []*types.Issue
	for _, issue := range issues {
		// Apply category filter if specified
		if skillFilterCategory != "" && issue.SkillCategory != skillFilterCategory {
			continue
		}
		// Apply town filter if specified
		if skillTown {
			// Only include town-level skills (hq-skill- prefix)
			if !strings.HasPrefix(issue.ID, "hq-skill-") {
				continue
			}
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
	if !strings.HasPrefix(skillID, "skill-") && !strings.HasPrefix(skillID, "hq-skill-") {
		skillID = "skill-" + skillID
	}

	// Resolve IDs
	var agentID, resolvedSkillID string

	// Check if agent ID looks like a Gas Town path (contains /)
	// These are canonical identifiers that don't need resolution
	isGasTownPath := strings.Contains(agentArg, "/")

	if isGasTownPath {
		// Use the agent path directly without resolution
		agentID = agentArg
	} else if daemonClient != nil {
		// Resolve agent ID
		resolveArgs := &rpc.ResolveIDArgs{ID: agentArg}
		resp, err := daemonClient.ResolveID(resolveArgs)
		if err != nil {
			return fmt.Errorf("resolving agent ID %s: %w", agentArg, err)
		}
		if err := json.Unmarshal(resp.Data, &agentID); err != nil {
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
	}

	// Resolve skill ID
	if daemonClient != nil {
		resolveArgs := &rpc.ResolveIDArgs{ID: skillID}
		resp, err := daemonClient.ResolveID(resolveArgs)
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
	if !strings.HasPrefix(skillID, "skill-") && !strings.HasPrefix(skillID, "hq-skill-") {
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
	if !strings.HasPrefix(skillID, "skill-") && !strings.HasPrefix(skillID, "hq-skill-") {
		skillID = "skill-" + skillID
	}

	var resolvedSkillID string
	var skill *types.Issue
	var dependents []*types.IssueWithDependencyMetadata
	err := withStorage(ctx, store, dbPath, lockTimeout, func(s storage.Storage) error {
		// Resolve skill ID
		var err error
		resolvedSkillID, err = utils.ResolvePartialID(ctx, s, skillID)
		if err != nil {
			return fmt.Errorf("resolving skill ID %s: %w", skillID, err)
		}

		// Verify it's a skill
		skill, err = s.GetIssue(ctx, resolvedSkillID)
		if err != nil {
			return fmt.Errorf("skill not found: %s", resolvedSkillID)
		}
		if skill.IssueType != types.TypeSkill {
			return fmt.Errorf("%s is not a skill (type: %s)", resolvedSkillID, skill.IssueType)
		}

		// Get dependents with provides-skill type
		dependents, err = s.GetDependentsWithMetadata(ctx, resolvedSkillID)
		return err
	})
	if err != nil {
		return err
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

	var issueID string
	var issue *types.Issue
	var deps []*types.IssueWithDependencyMetadata
	err := withStorage(ctx, store, dbPath, lockTimeout, func(s storage.Storage) error {
		// Resolve issue ID
		var err error
		issueID, err = utils.ResolvePartialID(ctx, s, issueArg)
		if err != nil {
			return fmt.Errorf("resolving issue ID %s: %w", issueArg, err)
		}

		// Get the issue to display its title
		issue, err = s.GetIssue(ctx, issueID)
		if err != nil {
			return fmt.Errorf("issue not found: %s", issueID)
		}

		// Get dependencies with requires-skill type
		deps, err = s.GetDependenciesWithMetadata(ctx, issueID)
		return err
	})
	if err != nil {
		return err
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
	if !strings.HasPrefix(skillID, "skill-") && !strings.HasPrefix(skillID, "hq-skill-") {
		skillID = "skill-" + skillID
	}

	var resolvedSkillID string
	var skill *types.Issue
	err := withStorage(ctx, store, dbPath, lockTimeout, func(s storage.Storage) error {
		// Resolve skill ID
		var err error
		resolvedSkillID, err = utils.ResolvePartialID(ctx, s, skillID)
		if err != nil {
			return fmt.Errorf("resolving skill ID %s: %w", skillID, err)
		}

		// Get the skill
		skill, err = s.GetIssue(ctx, resolvedSkillID)
		if err != nil {
			return fmt.Errorf("skill not found: %s", resolvedSkillID)
		}
		if skill.IssueType != types.TypeSkill {
			return fmt.Errorf("%s is not a skill (type: %s)", resolvedSkillID, skill.IssueType)
		}
		return nil
	})
	if err != nil {
		return err
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
	err := withStorage(ctx, store, dbPath, lockTimeout, func(s storage.Storage) error {
		for _, pattern := range agentPatterns {
			deps, err := s.GetDependenciesWithMetadata(ctx, pattern)
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
		return nil
	})
	if err != nil {
		return nil // Silently fail for prime
	}

	if len(agentSkillIDs) == 0 {
		return nil // No skills to output
	}

	// Load each skill's content
	var loadedSkills []struct {
		Name    string
		Content string
	}

	err = withStorage(ctx, store, dbPath, lockTimeout, func(s storage.Storage) error {
		for _, skillID := range agentSkillIDs {
			skill, err := s.GetIssue(ctx, skillID)
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
		return nil
	})
	if err != nil {
		return nil // Silently fail for prime
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

// runSkillSync syncs beads skills to .claude/skills/ for Claude Code discovery
func runSkillSync(cmd *cobra.Command, args []string) error {
	ctx := rootCtx

	// Find repo root (where .claude should be)
	repoRoot := ""
	if cwd, err := os.Getwd(); err == nil {
		dir := cwd
		for dir != "/" {
			if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
				repoRoot = dir
				break
			}
			dir = filepath.Dir(dir)
		}
	}
	if repoRoot == "" {
		return fmt.Errorf("not in a git repository")
	}

	claudeSkillsDir := filepath.Join(repoRoot, ".claude", "skills")

	// Clean if requested
	if skillSyncClean {
		if err := os.RemoveAll(claudeSkillsDir); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to clean %s: %w", claudeSkillsDir, err)
		}
		if !quietFlag {
			fmt.Printf("Cleaned %s\n", claudeSkillsDir)
		}
	}

	// Get all skills
	var skills []*types.Issue
	if daemonClient != nil {
		listArgs := &rpc.ListArgs{
			IssueType: string(types.TypeSkill),
		}
		resp, err := daemonClient.List(listArgs)
		if err != nil {
			return fmt.Errorf("failed to list skills: %w", err)
		}
		if err := json.Unmarshal(resp.Data, &skills); err != nil {
			return fmt.Errorf("failed to decode skills: %w", err)
		}
	} else {
		skillType := types.TypeSkill
		filter := types.IssueFilter{
			IssueType: &skillType,
		}
		err := withStorage(ctx, store, dbPath, lockTimeout, func(s storage.Storage) error {
			var err error
			skills, err = s.SearchIssues(ctx, "", filter)
			return err
		})
		if err != nil {
			return fmt.Errorf("failed to list skills: %w", err)
		}
	}

	// Filter to skills with claude_skill_path
	var syncedCount int
	for _, skill := range skills {
		if skill.ClaudeSkillPath == "" {
			continue
		}

		// Find the source SKILL.md
		sourcePath := findSkillFile(skill.ClaudeSkillPath, repoRoot)
		if sourcePath == "" {
			fmt.Fprintf(os.Stderr, "Warning: skill %s: claude_skill_path '%s' not found, skipping\n",
				skill.SkillName, skill.ClaudeSkillPath)
			continue
		}

		// Create target directory
		targetDir := filepath.Join(claudeSkillsDir, skill.SkillName)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return fmt.Errorf("failed to create %s: %w", targetDir, err)
		}

		// Create symlink to SKILL.md
		targetPath := filepath.Join(targetDir, "SKILL.md")

		// Remove existing file/symlink
		_ = os.Remove(targetPath)

		// Try to create relative symlink for portability
		// If .claude is a symlink, resolve it first to get correct relative path
		resolvedTargetDir := targetDir
		if resolved, err := filepath.EvalSymlinks(targetDir); err == nil {
			resolvedTargetDir = resolved
		}
		relPath, err := filepath.Rel(resolvedTargetDir, sourcePath)
		if err != nil {
			relPath = sourcePath // Fall back to absolute
		}

		if err := os.Symlink(relPath, targetPath); err != nil {
			// If symlink fails (e.g., on some Windows configs), copy instead
			content, readErr := os.ReadFile(sourcePath)
			if readErr != nil {
				return fmt.Errorf("failed to read %s: %w", sourcePath, readErr)
			}
			if writeErr := os.WriteFile(targetPath, content, 0644); writeErr != nil {
				return fmt.Errorf("failed to write %s: %w", targetPath, writeErr)
			}
		}

		syncedCount++
		if !quietFlag {
			fmt.Printf("Synced: %s -> %s\n", skill.SkillName, targetPath)
		}
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"synced":      syncedCount,
			"target_dir":  claudeSkillsDir,
			"total_skills": len(skills),
		})
		return nil
	}

	if syncedCount == 0 {
		fmt.Println("No skills with claude_skill_path to sync")
	} else {
		fmt.Printf("\nSynced %d skill(s) to %s\n", syncedCount, claudeSkillsDir)
		fmt.Println("Skills are now discoverable by Claude Code's Skill tool")
	}

	return nil
}

// findSkillFile finds a skill file from various locations
func findSkillFile(skillPath, repoRoot string) string {
	candidates := []string{
		skillPath,
		filepath.Join(".", skillPath),
		filepath.Join(repoRoot, skillPath),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			absPath, _ := filepath.Abs(candidate)
			return absPath
		}
	}

	return ""
}

// Spy command for monitoring polecat sessions
var skillSpyCmd = &cobra.Command{
	Use:   "spy <session-name> [marker]",
	Short: "Check a polecat session for skill activation markers",
	Long: `Capture output from a polecat tmux session and check for skill markers.

This is used for E2E testing to verify that skills are being loaded and used.
If no marker is specified, checks for the standard E2E test marker: [E2E-SKILL-ACTIVE]

Examples:
  bd skill spy gt-beads-polecat-alpha              # Check for default marker
  bd skill spy gt-beads-polecat-alpha "[CUSTOM]"   # Check for custom marker
  bd skill spy gt-beads-polecat-alpha --lines 500  # Capture more lines`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSkillSpy,
}

var spyLines int

// runSkillSpy captures session output and checks for skill markers
func runSkillSpy(cmd *cobra.Command, args []string) error {
	sessionName := args[0]
	marker := "[E2E-SKILL-ACTIVE]"
	if len(args) > 1 {
		marker = args[1]
	}

	// Capture tmux pane output
	captureCmd := exec.Command("tmux", "capture-pane", "-t", sessionName, "-p", "-S", fmt.Sprintf("-%d", spyLines))
	output, err := captureCmd.Output()
	if err != nil {
		return fmt.Errorf("failed to capture session %s: %w (is tmux session running?)", sessionName, err)
	}

	outputStr := string(output)
	found := strings.Contains(outputStr, marker)

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"session":      sessionName,
			"marker":       marker,
			"found":        found,
			"lines":        spyLines,
			"output_bytes": len(output),
		})
		return nil
	}

	if found {
		fmt.Printf("%s Marker found in session %s\n", ui.RenderPass("✓"), sessionName)
		fmt.Printf("  Marker: %s\n", marker)

		// Show context around the marker
		lines := strings.Split(outputStr, "\n")
		for i, line := range lines {
			if strings.Contains(line, marker) {
				fmt.Printf("\n  Context (line %d):\n", i+1)
				start := i - 2
				if start < 0 {
					start = 0
				}
				end := i + 3
				if end > len(lines) {
					end = len(lines)
				}
				for j := start; j < end; j++ {
					prefix := "    "
					if j == i {
						prefix = "  > "
					}
					fmt.Printf("%s%s\n", prefix, lines[j])
				}
				break
			}
		}
		return nil
	}

	fmt.Printf("%s Marker NOT found in session %s\n", ui.RenderFail("✗"), sessionName)
	fmt.Printf("  Marker: %s\n", marker)
	fmt.Printf("  Captured %d lines (%d bytes)\n", len(strings.Split(outputStr, "\n")), len(output))
	return fmt.Errorf("skill marker not found")
}

// Test command for running E2E skill integration tests
var skillTestCmd = &cobra.Command{
	Use:   "test [session-name]",
	Short: "Run E2E skill integration test",
	Long: `Run end-to-end test to verify skill integration works.

This command orchestrates:
1. Ensures e2e-test skill exists
2. Syncs skills to .claude/skills/
3. If session-name provided: monitors it for skill activation
4. Reports PASS/FAIL

The e2e-test skill instructs Claude to output [E2E-SKILL-ACTIVE] when activated.

Examples:
  bd skill test                          # Setup only, shows how to test manually
  bd skill test gt-beads-polecat-alpha   # Test existing session
  bd skill test --setup-only             # Only setup, don't monitor`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSkillTest,
}

var (
	testSetupOnly bool
	testTimeout   int
	testInterval  int
)

func runSkillTest(cmd *cobra.Command, args []string) error {
	ctx := rootCtx
	_ = ctx // ctx available for future use

	fmt.Println("=== Skill E2E Integration Test ===\n")

	// Step 1: Check if e2e-test skill exists
	fmt.Println("Step 1: Checking e2e-test skill...")
	skillID := "skill-e2e-test"

	var skillExists bool
	if store != nil {
		_, err := store.GetIssue(ctx, skillID)
		skillExists = err == nil
	}

	if !skillExists {
		fmt.Println("  Creating e2e-test skill...")
		// Check if we have the skill file
		skillPath := "claude-plugin/skills/e2e-test/SKILL.md"
		if _, err := os.Stat(skillPath); err != nil {
			return fmt.Errorf("e2e-test skill file not found at %s", skillPath)
		}

		issue := &types.Issue{
			ID:              skillID,
			Title:           "E2e Test",
			Description:     "Test skill for validating skill integration end-to-end",
			IssueType:       types.TypeSkill,
			Status:          types.StatusPinned,
			Priority:        2,
			SkillName:       "e2e-test",
			SkillVersion:    "1.0.0",
			SkillCategory:   "testing",
			ClaudeSkillPath: skillPath,
		}
		if store != nil {
			actor := getActor()
			if err := store.CreateIssue(ctx, issue, actor); err != nil {
				fmt.Printf("  Warning: Could not create skill bead: %v\n", err)
			} else {
				fmt.Printf("  %s Created skill bead: %s\n", ui.RenderPass("✓"), skillID)
			}
		} else {
			fmt.Println("  Note: No database, skill bead not persisted")
		}
	} else {
		fmt.Printf("  %s e2e-test skill exists\n", ui.RenderPass("✓"))
	}

	// Step 2: Sync skills
	fmt.Println("\nStep 2: Syncing skills to .claude/skills/...")
	syncCmd := exec.Command("bd", "skill", "sync")
	syncCmd.Stdout = os.Stdout
	syncCmd.Stderr = os.Stderr
	if err := syncCmd.Run(); err != nil {
		return fmt.Errorf("skill sync failed: %w", err)
	}

	// Step 3: Verify sync
	fmt.Println("\nStep 3: Verifying skill symlink...")
	claudeSkillPath := ".claude/skills/e2e-test/SKILL.md"
	if info, err := os.Lstat(claudeSkillPath); err != nil {
		return fmt.Errorf("skill symlink not found: %s", claudeSkillPath)
	} else if info.Mode()&os.ModeSymlink != 0 {
		target, _ := os.Readlink(claudeSkillPath)
		fmt.Printf("  %s Symlink exists: %s -> %s\n", ui.RenderPass("✓"), claudeSkillPath, target)
	} else {
		fmt.Printf("  %s File exists (not symlink): %s\n", ui.RenderPass("✓"), claudeSkillPath)
	}

	// If setup-only, stop here
	if testSetupOnly || len(args) == 0 {
		fmt.Println("\n=== Setup Complete ===")
		fmt.Println("\nTo complete E2E testing manually:")
		fmt.Println("  1. Spawn a test polecat: gt polecat spawn test-skill")
		fmt.Println("  2. Give it a simple task that uses the e2e-test skill")
		fmt.Println("  3. Monitor for activation: bd skill spy <session-name>")
		fmt.Println("\nOr provide a session name to monitor:")
		fmt.Println("  bd skill test gt-beads-polecat-alpha")
		return nil
	}

	// Step 4: Monitor session
	sessionName := args[0]
	fmt.Printf("\nStep 4: Monitoring session %s for skill activation...\n", sessionName)

	marker := "[E2E-SKILL-ACTIVE]"
	attempts := testTimeout / testInterval
	if attempts < 1 {
		attempts = 1
	}

	for i := 0; i < attempts; i++ {
		captureCmd := exec.Command("tmux", "capture-pane", "-t", sessionName, "-p", "-S", fmt.Sprintf("-%d", spyLines))
		output, err := captureCmd.Output()
		if err != nil {
			fmt.Printf("  Attempt %d/%d: Session not ready (%v)\n", i+1, attempts, err)
		} else if strings.Contains(string(output), marker) {
			fmt.Printf("\n%s TEST PASSED: Skill marker found!\n", ui.RenderPass("✓"))

			// Show context
			lines := strings.Split(string(output), "\n")
			for j, line := range lines {
				if strings.Contains(line, marker) {
					fmt.Printf("\n  Context (line %d):\n", j+1)
					start := j - 1
					if start < 0 {
						start = 0
					}
					end := j + 3
					if end > len(lines) {
						end = len(lines)
					}
					for k := start; k < end; k++ {
						prefix := "    "
						if k == j {
							prefix = "  > "
						}
						fmt.Printf("%s%s\n", prefix, lines[k])
					}
					break
				}
			}
			return nil
		} else {
			fmt.Printf("  Attempt %d/%d: Marker not found yet\n", i+1, attempts)
		}

		if i < attempts-1 {
			time.Sleep(time.Duration(testInterval) * time.Second)
		}
	}

	fmt.Printf("\n%s TEST FAILED: Skill marker not found after %d seconds\n", ui.RenderFail("✗"), testTimeout)
	return fmt.Errorf("skill activation not detected")
}
