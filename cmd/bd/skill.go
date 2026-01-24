package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
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
