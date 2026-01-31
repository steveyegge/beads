package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
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

func init() {
	skillsCmd.AddCommand(skillsAuditCmd)
	skillsCmd.AddCommand(skillsSyncCmd)
	rootCmd.AddCommand(skillsCmd)
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
