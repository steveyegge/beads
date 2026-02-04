package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
)

// SkillInfo represents a discovered skill
type SkillInfo struct {
	Name   string `json:"name"`
	Source string `json:"source"` // claude | codex | superpowers
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

var skillsCmd = &cobra.Command{
	Use:     "skills",
	GroupID: "views",
	Short:   "Manage skills across agents",
	Long: `Discover and manage skills across Claude Code and Codex CLI.

Skills are reusable workflows that can be shared between agents.
This command helps detect drift (skills that exist in one agent but not another).

Subcommands:
  audit  - List all skills across agents, highlight drift
  sync   - Sync missing skills between agents`,
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var skillsAuditCmd = &cobra.Command{
	Use:   "audit",
	Short: "List all skills across agents",
	Long: `Discover skills in Claude Code and Codex CLI directories.

Shows which skills exist in each agent and highlights drift
(skills that exist in one but not the other).

Skill directories searched:
  Claude Code: .claude/skills/ (project-local)
  Codex CLI:   ~/.codex/skills/ (global)
  Superpowers: $HOME/workspace/my-superpowers/ (if exists)`,
	Run: runSkillsAudit,
}

var skillsSyncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync skills between agents",
	Long: `Copy missing skills from Claude Code to Codex CLI.

This is a one-way sync: Claude Code is the source of truth.
Skills in .claude/skills/ are copied to ~/.codex/skills/.`,
	Run: runSkillsSync,
}

// UnusedSkill represents a skill in the manifest with no linked beads
type UnusedSkill struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Source     string     `json:"source"`
	Tier       string     `json:"tier"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

var skillsCleanupCandidatesCmd = &cobra.Command{
	Use:   "cleanup-candidates",
	Short: "Find skills with no linked beads",
	Long: `Find active skills that are not linked to any beads.

These skills may be candidates for archiving or removal.`,
	Run: runSkillsCleanupCandidates,
}

type SkillCollision struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	Path         string `json:"path"`
	Reason       string `json:"reason"`
	SupersededBy string `json:"superseded_by,omitempty"`
}

var skillsCollisionCmd = &cobra.Command{
	Use:   "collisions",
	Short: "Detect global vs project skill collisions",
	Long: `Detect collisions between project-local and global skills.

This surfaces:
- name collisions (same skill name in multiple directories)
- supersedes collisions (project skill declares supersedes:)

Use --fix to archive superseded skills.`,
	Run: runSkillsCollisions,
}

func init() {
	skillsCmd.AddCommand(skillsAuditCmd)
	skillsCmd.AddCommand(skillsSyncCmd)
	skillsCmd.AddCommand(skillsCleanupCandidatesCmd)
	skillsCmd.AddCommand(skillsCollisionCmd)
	rootCmd.AddCommand(skillsCmd)

	skillsCollisionCmd.Flags().Bool("fix", false, "Archive superseded skills")
	skillsCollisionCmd.Flags().Bool("dry-run", false, "Preview changes without modifying files")
	skillsCollisionCmd.Flags().Bool("quiet", false, "Print summary only")
	skillsCollisionCmd.Flags().Bool("prefix-match", false, "Detect prefix-based collisions")
}

func runSkillsAudit(cmd *cobra.Command, args []string) {
	// Discover skill directories
	claudeDir := ".claude/skills"
	codexDir := os.ExpandEnv("$HOME/.codex/skills")
	superpowersDir := os.ExpandEnv("$HOME/.codex/superpowers/skills")

	claudeSkills := scanSkillDir(claudeDir, "claude")
	codexSkills := scanSkillDir(codexDir, "codex")
	superpowersSkills := scanSkillDir(superpowersDir, "superpowers")

	// Build skill index by name
	allSkills := make(map[string]map[string]*SkillInfo) // name -> source -> info
	for _, skill := range claudeSkills {
		if allSkills[skill.Name] == nil {
			allSkills[skill.Name] = make(map[string]*SkillInfo)
		}
		allSkills[skill.Name][skill.Source] = &skill
	}
	for _, skill := range codexSkills {
		if allSkills[skill.Name] == nil {
			allSkills[skill.Name] = make(map[string]*SkillInfo)
		}
		allSkills[skill.Name][skill.Source] = &skill
	}
	for _, skill := range superpowersSkills {
		if allSkills[skill.Name] == nil {
			allSkills[skill.Name] = make(map[string]*SkillInfo)
		}
		allSkills[skill.Name][skill.Source] = &skill
	}

	if jsonOutput {
		outputJSON(allSkills)
		return
	}

	// Sort skill names
	names := make([]string, 0, len(allSkills))
	for name := range allSkills {
		names = append(names, name)
	}
	sort.Strings(names)

	// Print header
	fmt.Println("Skills Audit")
	fmt.Println()
	fmt.Printf("%-30s  %-8s  %-8s  %-12s\n", "Skill", "Claude", "Codex", "Superpowers")
	fmt.Println(strings.Repeat("-", 70))

	// Track drift
	driftCount := 0
	syncedCount := 0
	totalCount := len(names)

	for _, name := range names {
		sources := allSkills[name]

		claudeStatus := "   "
		codexStatus := "   "
		superpowersStatus := "   "

		hasAll := true

		// Using approved symbols: ○ ◐ ● ✓ ❄
		if _, ok := sources["claude"]; ok {
			claudeStatus = ui.RenderPass(" ✓ ")
		} else {
			claudeStatus = ui.RenderFail(" ○ ")
			hasAll = false
		}

		if _, ok := sources["codex"]; ok {
			codexStatus = ui.RenderPass(" ✓ ")
		} else {
			codexStatus = ui.RenderMuted(" - ")
			// Missing in codex is drift only if present in claude
			if sources["claude"] != nil {
				hasAll = false
			}
		}

		if _, ok := sources["superpowers"]; ok {
			superpowersStatus = ui.RenderPass(" ✓ ")
		} else {
			superpowersStatus = ui.RenderMuted(" - ")
		}

		// Check for hash mismatch between claude and codex
		hashMatch := true
		if sources["claude"] != nil && sources["codex"] != nil {
			if sources["claude"].SHA256 != sources["codex"].SHA256 {
				hashMatch = false
				hasAll = false
			}
		}

		// Format name with drift indicator (using approved symbols)
		nameDisplay := name
		if !hasAll {
			nameDisplay = ui.RenderWarn(name + " ◐")
			driftCount++
		} else {
			syncedCount++
		}

		if !hashMatch {
			nameDisplay = ui.RenderFail(name + " ●")
		}

		fmt.Printf("%-30s  %-8s  %-8s  %-12s\n", nameDisplay, claudeStatus, codexStatus, superpowersStatus)
	}

	// Summary
	fmt.Println()
	fmt.Printf("Total: %d skills, %d synced, %d with drift\n", totalCount, syncedCount, driftCount)

	if driftCount > 0 {
		fmt.Println()
		fmt.Println("Run 'bd skills sync' to sync Claude skills to Codex")
	}

	appendSkillsVolatilityRisk(rootCtx)
}

func runSkillsSync(cmd *cobra.Command, args []string) {
	claudeDir := ".claude/skills"
	codexDir := os.ExpandEnv("$HOME/.codex/skills")

	claudeSkills := scanSkillDir(claudeDir, "claude")
	codexSkills := scanSkillDir(codexDir, "codex")

	// Build codex skill index
	codexIndex := make(map[string]*SkillInfo)
	for i := range codexSkills {
		codexIndex[codexSkills[i].Name] = &codexSkills[i]
	}

	// Find skills to sync
	toSync := []SkillInfo{}
	for _, skill := range claudeSkills {
		existing := codexIndex[skill.Name]
		if existing == nil {
			toSync = append(toSync, skill)
		} else if existing.SHA256 != skill.SHA256 {
			toSync = append(toSync, skill)
		}
	}

	if len(toSync) == 0 {
		fmt.Println("✓ All skills are synced")
		return
	}

	// Create codex skills dir if needed
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating Codex skills dir: %v\n", err)
		os.Exit(1)
	}

	// Sync skills
	for _, skill := range toSync {
		srcDir := filepath.Join(claudeDir, skill.Name)
		dstDir := filepath.Join(codexDir, skill.Name)

		// Copy directory
		if err := copyDir(srcDir, dstDir); err != nil {
			fmt.Fprintf(os.Stderr, "Error syncing %s: %v\n", skill.Name, err)
			continue
		}

		action := "Added"
		if codexIndex[skill.Name] != nil {
			action = "Updated"
		}
		fmt.Printf("%s %s %s\n", ui.RenderPass("✓"), action, skill.Name)
	}

	fmt.Println()
	fmt.Printf("Synced %d skills to Codex\n", len(toSync))
}

func runSkillsCleanupCandidates(cmd *cobra.Command, args []string) {
	ctx := context.Background()

	// Skills command requires direct DB access (not daemon) since it queries
	// extension tables (skills_manifest, skill_bead_links) that aren't part
	// of the core RPC interface.
	if dbPath == "" {
		fmt.Fprintf(os.Stderr, "Error: no database path available\n")
		fmt.Fprintf(os.Stderr, "Hint: run from a directory with a beads database\n")
		os.Exit(1)
	}

	// Open read-only connection to query skills tables
	roStore, err := sqlite.NewReadOnlyWithTimeout(ctx, dbPath, lockTimeout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening database: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = roStore.Close() }()

	// Query for unused skills: active skills with no entries in skill_bead_links
	db := roStore.UnderlyingDB()
	query := `
		SELECT
			sm.id,
			sm.name,
			sm.source,
			sm.tier,
			sm.last_used_at,
			sm.created_at
		FROM skills_manifest sm
		LEFT JOIN skill_bead_links sbl ON sm.id = sbl.skill_id
		WHERE sm.status = 'active'
		  AND sbl.skill_id IS NULL
		ORDER BY sm.name
	`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error querying unused skills: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = rows.Close() }()

	var unusedSkills []UnusedSkill
	for rows.Next() {
		var skill UnusedSkill
		var lastUsedAt, createdAt *string

		if err := rows.Scan(&skill.ID, &skill.Name, &skill.Source, &skill.Tier, &lastUsedAt, &createdAt); err != nil {
			fmt.Fprintf(os.Stderr, "Error scanning row: %v\n", err)
			os.Exit(1)
		}

		// Parse timestamps
		if lastUsedAt != nil && *lastUsedAt != "" {
			if t, parseErr := time.Parse(time.RFC3339, *lastUsedAt); parseErr == nil {
				skill.LastUsedAt = &t
			} else if t, parseErr := time.Parse("2006-01-02 15:04:05", *lastUsedAt); parseErr == nil {
				skill.LastUsedAt = &t
			}
		}
		if createdAt != nil && *createdAt != "" {
			if t, parseErr := time.Parse(time.RFC3339, *createdAt); parseErr == nil {
				skill.CreatedAt = t
			} else if t, parseErr := time.Parse("2006-01-02 15:04:05", *createdAt); parseErr == nil {
				skill.CreatedAt = t
			}
		}

		unusedSkills = append(unusedSkills, skill)
	}

	if err := rows.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Error iterating rows: %v\n", err)
		os.Exit(1)
	}

	// JSON output
	if jsonOutput {
		outputJSON(unusedSkills)
		return
	}

	// Human-readable output
	if len(unusedSkills) == 0 {
		fmt.Println("No cleanup candidates found - all active skills are linked to beads")
		return
	}

	fmt.Println("Skills Cleanup Candidates")
	fmt.Println("Active skills with no linked beads:")
	fmt.Println()
	fmt.Printf("%-25s  %-12s  %-10s  %s\n", "Name", "Source", "Tier", "Last Used")
	fmt.Println(strings.Repeat("-", 70))

	for _, skill := range unusedSkills {
		lastUsedStr := "never"
		if skill.LastUsedAt != nil {
			lastUsedStr = skill.LastUsedAt.Format("2006-01-02")
		}

		fmt.Printf("%-25s  %-12s  %-10s  %s\n",
			skill.Name,
			skill.Source,
			skill.Tier,
			ui.RenderMuted(lastUsedStr))
	}

	fmt.Println()
	fmt.Printf("Found %d cleanup candidates\n", len(unusedSkills))
	fmt.Println()
	fmt.Println("To archive a skill, update its status to 'archived' in the database.")
}

func scanSkillDir(dir, source string) []SkillInfo {
	skills := []SkillInfo{}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return skills // Dir doesn't exist, return empty
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue // Skip hidden dirs
		}

		skillDir := filepath.Join(dir, name)

		// Try multiple filename conventions: skill.md, SKILL.md, README.md
		mainFile := ""
		candidates := []string{"skill.md", "SKILL.md", "README.md", "index.md"}
		for _, candidate := range candidates {
			path := filepath.Join(skillDir, candidate)
			if _, err := os.Stat(path); err == nil {
				mainFile = path
				break
			}
		}
		if mainFile == "" {
			continue // No recognized skill file
		}

		info, err := os.Stat(mainFile)
		if err != nil {
			continue
		}

		// Calculate SHA256
		hash, err := hashFile(mainFile)
		if err != nil {
			continue
		}

		skills = append(skills, SkillInfo{
			Name:   name,
			Source: source,
			Path:   mainFile,
			SHA256: hash,
			Bytes:  info.Size(),
		})
	}

	return skills
}

func runSkillsCollisions(cmd *cobra.Command, _ []string) {
	fix, _ := cmd.Flags().GetBool("fix")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	quiet, _ := cmd.Flags().GetBool("quiet")
	prefixMatch, _ := cmd.Flags().GetBool("prefix-match")

	projectDir := ".claude/skills"
	globalClaudeDir := os.ExpandEnv("$HOME/.claude/skills")
	globalCodexDir := os.ExpandEnv("$HOME/.codex/skills")

	projectSkills := scanSkillDir(projectDir, "project")
	globalClaudeSkills := scanSkillDir(globalClaudeDir, "global-claude")
	globalCodexSkills := scanSkillDir(globalCodexDir, "global-codex")

	projectByName := skillIndexByName(projectSkills)
	globalByName := skillIndexByName(globalClaudeSkills)
	codexByName := skillIndexByName(globalCodexSkills)

	supersedes := collectSupersedes(projectDir, projectSkills)

	collisions := []SkillCollision{}
	archived := []SkillCollision{}

	for name := range projectByName {
		if global, ok := globalByName[name]; ok {
			collisions = append(collisions, SkillCollision{
				Name:   name,
				Source: global.Source,
				Path:   global.Path,
				Reason: "name collision with project",
			})
		}
		if codex, ok := codexByName[name]; ok {
			collisions = append(collisions, SkillCollision{
				Name:   name,
				Source: codex.Source,
				Path:   codex.Path,
				Reason: "name collision with project",
			})
		}
	}

	for superseded, by := range supersedes {
		if global, ok := globalByName[superseded]; ok {
			collisions = append(collisions, SkillCollision{
				Name:         superseded,
				Source:       global.Source,
				Path:         global.Path,
				Reason:       "superseded",
				SupersededBy: by,
			})
		}
		if codex, ok := codexByName[superseded]; ok {
			collisions = append(collisions, SkillCollision{
				Name:         superseded,
				Source:       codex.Source,
				Path:         codex.Path,
				Reason:       "superseded",
				SupersededBy: by,
			})
		}
		if project, ok := projectByName[superseded]; ok && project.Name != by {
			collisions = append(collisions, SkillCollision{
				Name:         superseded,
				Source:       project.Source,
				Path:         project.Path,
				Reason:       "superseded",
				SupersededBy: by,
			})
		}
	}

	if prefixMatch {
		collisions = append(collisions, findPrefixCollisions(projectByName, globalByName, "global-claude")...)
		collisions = append(collisions, findPrefixCollisions(projectByName, codexByName, "global-codex")...)
	}

	if fix && len(collisions) > 0 {
		for _, collision := range collisions {
			if collision.Reason != "superseded" {
				continue
			}
			targetDir := archiveRootForSource(collision.Source, globalClaudeDir, globalCodexDir, projectDir)
			if targetDir == "" {
				continue
			}
			if dryRun {
				archived = append(archived, collision)
				continue
			}
			if err := archiveSkill(targetDir, collision.Name); err == nil {
				archived = append(archived, collision)
			}
		}
	}

	if jsonOutput {
		outputJSON(map[string]interface{}{
			"collisions": collisions,
			"archived":   archived,
		})
		return
	}

	if quiet {
		fmt.Printf("Collisions: %d", len(collisions))
		if fix {
			fmt.Printf(" | Archived: %d", len(archived))
		}
		fmt.Println()
		return
	}

	if len(collisions) == 0 {
		fmt.Println("No skill collisions detected.")
		return
	}

	fmt.Println("Skill Collision Report")
	fmt.Println(strings.Repeat("-", 60))
	for _, collision := range collisions {
		if collision.Reason == "superseded" {
			fmt.Printf("%s %s (%s) superseded by %s\n", ui.RenderWarn("◐"), collision.Name, collision.Source, collision.SupersededBy)
		} else {
			fmt.Printf("%s %s (%s) %s\n", ui.RenderWarn("◐"), collision.Name, collision.Source, collision.Reason)
		}
		fmt.Printf("  %s\n", collision.Path)
	}

	if fix {
		fmt.Println()
		fmt.Printf("Archived %d superseded skills\n", len(archived))
	}
}

func skillIndexByName(skills []SkillInfo) map[string]SkillInfo {
	index := make(map[string]SkillInfo, len(skills))
	for _, skill := range skills {
		index[skill.Name] = skill
	}
	return index
}

func collectSupersedes(projectDir string, skills []SkillInfo) map[string]string {
	supersedes := make(map[string]string)
	for _, skill := range skills {
		list, err := parseSkillFrontmatterList(projectDir, skill.Name, "supersedes")
		if err != nil {
			continue
		}
		for _, target := range list {
			if target == "" {
				continue
			}
			supersedes[target] = skill.Name
		}
	}
	return supersedes
}

func parseSkillFrontmatterList(skillsDir, skillName, key string) ([]string, error) {
	path := resolveSkillPath(skillsDir, skillName)
	if path == "" {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return nil, scanner.Err()
	}
	if strings.TrimSpace(scanner.Text()) != "---" {
		return nil, nil
	}

	var items []string
	listMode := false
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r\n")
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			break
		}
		if strings.HasPrefix(trimmed, key+":") {
			listMode = false
			value := strings.TrimSpace(strings.TrimPrefix(trimmed, key+":"))
			if strings.HasPrefix(value, "[") && strings.HasSuffix(value, "]") {
				entries := strings.Split(strings.Trim(value, "[]"), ",")
				for _, entry := range entries {
					if item := normalizeFrontmatterItem(entry); item != "" {
						items = append(items, item)
					}
				}
			} else if value != "" {
				if item := normalizeFrontmatterItem(value); item != "" {
					items = append(items, item)
				}
			} else {
				listMode = true
			}
			continue
		}
		if listMode {
			if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "-\t") || trimmed == "-" {
				item := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
				if item := normalizeFrontmatterItem(item); item != "" {
					items = append(items, item)
				}
			} else if trimmed != "" {
				listMode = false
			}
		}
	}

	return dedupeStrings(items), nil
}

func normalizeFrontmatterItem(item string) string {
	item = strings.TrimSpace(item)
	item = strings.Trim(item, "\"'")
	return strings.TrimSpace(item)
}

func resolveSkillPath(skillsDir, skillName string) string {
	if skillsDir == "" || skillName == "" {
		return ""
	}
	basePath := filepath.Join(skillsDir, skillName)
	info, err := os.Stat(basePath)
	if err == nil && info.IsDir() {
		return resolveSkillFile(basePath)
	}
	if err == nil && !info.IsDir() {
		return basePath
	}
	if _, err := os.Stat(basePath + ".md"); err == nil {
		return basePath + ".md"
	}
	return ""
}

func resolveSkillFile(skillDir string) string {
	candidates := []string{"SKILL.md", "skill.md", "README.md", "index.md"}
	for _, candidate := range candidates {
		path := filepath.Join(skillDir, candidate)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

func archiveRootForSource(source, globalClaudeDir, globalCodexDir, projectDir string) string {
	switch source {
	case "global-claude":
		return globalClaudeDir
	case "global-codex":
		return globalCodexDir
	case "project":
		return projectDir
	default:
		return ""
	}
}

func archiveSkill(rootDir, skillName string) error {
	path := filepath.Join(rootDir, skillName)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if _, err := os.Stat(path + ".md"); err == nil {
			path = path + ".md"
		} else {
			return nil
		}
	}

	archiveDir := filepath.Join(rootDir, "_archived", time.Now().Format("2006-01"))
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return err
	}

	target := filepath.Join(archiveDir, filepath.Base(path))
	for i := 1; ; i++ {
		if _, err := os.Stat(target); os.IsNotExist(err) {
			break
		}
		target = filepath.Join(archiveDir, fmt.Sprintf("%s.%d", filepath.Base(path), i))
	}
	return os.Rename(path, target)
}

func findPrefixCollisions(project map[string]SkillInfo, globals map[string]SkillInfo, source string) []SkillCollision {
	results := []SkillCollision{}
	for name, info := range project {
		for globalName, global := range globals {
			if name == globalName {
				continue
			}
			if strings.HasPrefix(name, globalName) || strings.HasPrefix(globalName, name) {
				results = append(results, SkillCollision{
					Name:   globalName,
					Source: source,
					Path:   global.Path,
					Reason: fmt.Sprintf("prefix collision with %s", info.Name),
				})
			}
		}
	}
	return results
}

func dedupeStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func appendSkillsVolatilityRisk(ctx context.Context) {
	if dbPath == "" {
		return
	}

	roStore, err := sqlite.NewReadOnlyWithTimeout(ctx, dbPath, lockTimeout)
	if err != nil {
		return
	}
	defer func() { _ = roStore.Close() }()

	db := roStore.UnderlyingDB()
	rows, err := db.QueryContext(ctx, `
		SELECT skill_id, bead_id FROM skill_bead_links ORDER BY linked_at DESC
	`)
	if err != nil {
		return
	}
	defer func() { _ = rows.Close() }()

	skillToBead := make(map[string]string)
	for rows.Next() {
		var skillID, beadID string
		if scanErr := rows.Scan(&skillID, &beadID); scanErr == nil {
			if _, exists := skillToBead[skillID]; !exists {
				skillToBead[skillID] = beadID
			}
		}
	}

	specIDs := make([]string, 0)
	skillToSpec := make(map[string]string)
	for skillID, beadID := range skillToBead {
		issue, err := roStore.GetIssue(ctx, beadID)
		if err != nil || issue == nil || issue.SpecID == "" {
			continue
		}
		skillToSpec[skillID] = issue.SpecID
		specIDs = append(specIDs, issue.SpecID)
	}
	if len(specIDs) == 0 {
		return
	}

	since := time.Now().UTC().Add(-volatilityWindow()).Truncate(time.Second)

	summaries := make(map[string]specVolatilitySummary)
	for _, specID := range specIDs {
		if _, ok := summaries[specID]; ok {
			continue
		}
		events, err := roStore.ListSpecScanEvents(ctx, specID, since)
		if err != nil {
			continue
		}
		changeCount, _ := spec.SummarizeScanEvents(events, time.Time{})
		filter := types.IssueFilter{
			SpecID:        &specID,
			ExcludeStatus: []types.Status{types.StatusClosed, types.StatusTombstone},
		}
		issues, err := roStore.SearchIssues(ctx, "", filter)
		if err != nil {
			continue
		}
		summaries[specID] = specVolatilitySummary{
			ChangeCount: changeCount,
			OpenIssues:  len(issues),
		}
	}

	risky := make([]string, 0)
	for skillID, specID := range skillToSpec {
		summary, ok := summaries[specID]
		if !ok {
			continue
		}
		level := classifySpecVolatility(effectiveVolatilityChanges(summary), summary.OpenIssues)
		if level == specVolatilityHigh || level == specVolatilityMedium {
			risky = append(risky, fmt.Sprintf("%s → %s (%s)", skillID, specID, formatVolatilityLevel(level)))
		}
	}
	if len(risky) == 0 {
		return
	}

	sort.Strings(risky)
	fmt.Println()
	fmt.Println("Skills at risk (linked to volatile specs):")
	for _, line := range risky {
		fmt.Printf("  %s\n", line)
	}
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func copyDir(src, dst string) error {
	// Remove destination if exists
	if err := os.RemoveAll(dst); err != nil {
		return err
	}

	// Create destination
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	// Walk and copy
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		dstPath := filepath.Join(dst, relPath)

		if info.IsDir() {
			return os.MkdirAll(dstPath, info.Mode())
		}

		return copySkillFile(path, dstPath)
	})
}

func copySkillFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = dstFile.Close() }()

	_, err = io.Copy(dstFile, srcFile)
	return err
}
