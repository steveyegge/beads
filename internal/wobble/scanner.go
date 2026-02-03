package wobble

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultGlobalSkillsDir is the default location for global Claude skills.
var DefaultGlobalSkillsDir = filepath.Join(os.Getenv("HOME"), ".claude", "skills")

// ScanResult contains the full analysis of a single skill.
type ScanResult struct {
	Skill      string              `json:"skill"`
	Expected   string              `json:"expected,omitempty"`
	Structure  *StructuralAnalysis `json:"structure"`
	Behavioral *WobbleMetrics      `json:"behavioral,omitempty"`
	Verdict    string              `json:"verdict"`
	Recommendation string          `json:"recommendation"`
	CombinedRisk   float64         `json:"combined_risk"`
}

// SkillSummary is a brief summary for ranking skills by risk.
type SkillSummary struct {
	Name           string  `json:"name"`
	StructuralRisk float64 `json:"structural_risk"`
	WobbleScore    *float64 `json:"wobble_score,omitempty"`
	CombinedRisk   float64 `json:"combined_risk"`
	HasDefault     bool    `json:"has_default"`
}

// ProjectInspection contains the results of inspecting a project.
type ProjectInspection struct {
	ProjectName string          `json:"project_name"`
	ProjectPath string          `json:"project_path"`
	HasClaude   bool            `json:"has_claude"`
	Inventory   ProjectInventory `json:"inventory"`
	SkillRisks  []SkillRiskEntry `json:"skill_risks,omitempty"`
	Summary     InspectionSummary `json:"summary"`
}

// ProjectInventory counts the different types of Claude configuration items.
type ProjectInventory struct {
	Skills []string `json:"skills"`
	Rules  []string `json:"rules"`
	Agents []string `json:"agents"`
	Hooks  []string `json:"hooks"`
}

// SkillRiskEntry contains risk information for a single skill.
type SkillRiskEntry struct {
	Name           string  `json:"name"`
	StructuralRisk float64 `json:"structural_risk"`
	Verdict        string  `json:"verdict"`
}

// InspectionSummary summarizes the wobble analysis results.
type InspectionSummary struct {
	TotalSkills    int `json:"total_skills"`
	StableCount    int `json:"stable_count"`
	WobblyCount    int `json:"wobbly_count"`
	UnstableCount  int `json:"unstable_count"`
}

// DetectSkillsDir finds the skills directory for a given project.
// It checks project-local first, then falls back to global.
func DetectSkillsDir(projectPath string) string {
	if projectPath != "" {
		localSkills := filepath.Join(projectPath, ".claude", "skills")
		if info, err := os.Stat(localSkills); err == nil && info.IsDir() {
			return localSkills
		}
	}

	// Check current directory
	cwd, _ := os.Getwd()
	cwdSkills := filepath.Join(cwd, ".claude", "skills")
	if info, err := os.Stat(cwdSkills); err == nil && info.IsDir() {
		return cwdSkills
	}

	return DefaultGlobalSkillsDir
}

// ScanSkill performs a full analysis on a single skill.
func ScanSkill(skillsDir, skillName string, runs int) (*ScanResult, error) {
	skillPath := filepath.Join(skillsDir, skillName)

	// Check if skill exists as directory or .md file
	info, err := os.Stat(skillPath)
	if os.IsNotExist(err) {
		// Try with .md extension
		skillPath = skillPath + ".md"
		info, err = os.Stat(skillPath)
	}
	if err != nil {
		return nil, err
	}

	// For single-file skills, use the file path directly
	if !info.IsDir() {
		skillPath = skillPath
	}

	// Structural analysis
	structure, err := AnalyzeSkillStructure(skillPath)
	if err != nil {
		return nil, err
	}

	// Extract default command
	expected, err := ExtractDefaultCommand(skillPath)
	if err != nil {
		return nil, err
	}

	result := &ScanResult{
		Skill:     skillName,
		Expected:  expected,
		Structure: structure,
	}

	// Behavioral analysis (only if we have a default command)
	if expected != "" && runs > 0 {
		commands := SimulateExecutions(skillName, expected, runs)
		metrics := CalculateWobble(commands, expected)
		result.Behavioral = metrics
		result.Verdict, result.Recommendation = GetVerdict(metrics.WobbleScore, structure.RiskScore)
		result.CombinedRisk = GetCombinedRisk(metrics.WobbleScore, structure.RiskScore)
	} else {
		// Structural-only verdict
		result.Verdict, result.Recommendation = GetVerdict(0, structure.RiskScore)
		result.CombinedRisk = structure.RiskScore
	}

	return result, nil
}

// ScanAllSkills scans all skills in a directory and returns them ranked by risk.
func ScanAllSkills(skillsDir string, runs int) ([]SkillSummary, error) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil, err
	}

	var results []SkillSummary

	for _, entry := range entries {
		name := entry.Name()

		// Skip hidden files/directories
		if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
			continue
		}

		// Handle both directories and .md files
		skillPath := filepath.Join(skillsDir, name)
		isSkill := false
		skillName := name

		if entry.IsDir() {
			isSkill = true
		} else if strings.HasSuffix(name, ".md") {
			isSkill = true
			skillName = strings.TrimSuffix(name, ".md")
		}

		if !isSkill {
			continue
		}

		// Structural analysis
		structure, err := AnalyzeSkillStructure(skillPath)
		if err != nil {
			continue
		}

		// Extract default command
		expected, _ := ExtractDefaultCommand(skillPath)

		summary := SkillSummary{
			Name:           skillName,
			StructuralRisk: structure.RiskScore,
			HasDefault:     expected != "",
		}

		// Behavioral analysis (if default exists and runs > 0)
		if expected != "" && runs > 0 {
			commands := SimulateExecutions(skillName, expected, runs)
			metrics := CalculateWobble(commands, expected)
			wobble := metrics.WobbleScore
			summary.WobbleScore = &wobble
			summary.CombinedRisk = GetCombinedRisk(metrics.WobbleScore, structure.RiskScore)
		} else {
			summary.CombinedRisk = structure.RiskScore
		}

		results = append(results, summary)
	}

	// Sort by combined risk (highest first)
	sort.Slice(results, func(i, j int) bool {
		return results[i].CombinedRisk > results[j].CombinedRisk
	})

	return results, nil
}

// InspectProject analyzes a project's Claude configuration for wobble readiness.
func InspectProject(projectPath string) (*ProjectInspection, error) {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, err
	}

	result := &ProjectInspection{
		ProjectName: filepath.Base(absPath),
		ProjectPath: absPath,
		Inventory:   ProjectInventory{},
	}

	claudeDir := filepath.Join(absPath, ".claude")
	if _, err := os.Stat(claudeDir); os.IsNotExist(err) {
		result.HasClaude = false
		return result, nil
	}
	result.HasClaude = true

	// Inventory skills
	skillsDir := filepath.Join(claudeDir, "skills")
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") {
				continue
			}
			if entry.IsDir() {
				result.Inventory.Skills = append(result.Inventory.Skills, name)
			} else if strings.HasSuffix(name, ".md") {
				result.Inventory.Skills = append(result.Inventory.Skills, strings.TrimSuffix(name, ".md"))
			}
		}
	}

	// Inventory rules
	rulesDir := filepath.Join(claudeDir, "rules")
	if entries, err := os.ReadDir(rulesDir); err == nil {
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".md") {
				result.Inventory.Rules = append(result.Inventory.Rules, strings.TrimSuffix(entry.Name(), ".md"))
			}
		}
	}

	// Inventory agents
	agentsDir := filepath.Join(claudeDir, "agents")
	if entries, err := os.ReadDir(agentsDir); err == nil {
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				result.Inventory.Agents = append(result.Inventory.Agents, name)
			} else if strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".yaml") {
				result.Inventory.Agents = append(result.Inventory.Agents, strings.TrimSuffix(strings.TrimSuffix(name, ".md"), ".yaml"))
			}
		}
	}

	// Inventory hooks
	hooksDir := filepath.Join(claudeDir, "hooks")
	if entries, err := os.ReadDir(hooksDir); err == nil {
		for _, entry := range entries {
			result.Inventory.Hooks = append(result.Inventory.Hooks, entry.Name())
		}
	}

	// Analyze skill risks
	if len(result.Inventory.Skills) > 0 {
		for _, skillName := range result.Inventory.Skills {
			skillPath := filepath.Join(skillsDir, skillName)

			// Check if it's a directory or file
			if _, err := os.Stat(skillPath); os.IsNotExist(err) {
				skillPath = skillPath + ".md"
			}

			structure, err := AnalyzeSkillStructure(skillPath)
			if err != nil {
				continue
			}

			var verdict string
			if structure.RiskScore > 0.5 {
				verdict = "UNSTABLE"
				result.Summary.UnstableCount++
			} else if structure.RiskScore > 0.2 {
				verdict = "WOBBLY"
				result.Summary.WobblyCount++
			} else {
				verdict = "STABLE"
				result.Summary.StableCount++
			}

			result.SkillRisks = append(result.SkillRisks, SkillRiskEntry{
				Name:           skillName,
				StructuralRisk: structure.RiskScore,
				Verdict:        verdict,
			})
		}

		result.Summary.TotalSkills = len(result.Inventory.Skills)

		// Sort by risk (highest first)
		sort.Slice(result.SkillRisks, func(i, j int) bool {
			return result.SkillRisks[i].StructuralRisk > result.SkillRisks[j].StructuralRisk
		})
	}

	return result, nil
}
