package wobble

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// SessionsDir is the default location for Claude session transcripts.
var SessionsDir = filepath.Join(os.Getenv("HOME"), ".claude", "sessions")

// ProjectsDir is where Claude stores project-specific sessions.
var ProjectsDir = filepath.Join(os.Getenv("HOME"), ".claude", "projects")

// SkillExecution represents a single skill invocation with its executed command.
type SkillExecution struct {
	SkillName string    `json:"skill_name"`
	Command   string    `json:"command"`
	Timestamp time.Time `json:"timestamp"`
	SessionID string    `json:"session_id"`
}

// SessionData holds skill->commands mapping from real sessions.
type SessionData struct {
	SkillCommands map[string][]string // skill name -> list of commands executed
	TotalSessions int
	TotalMatches  int
}

var (
	// Pattern 1: "Launching skill: X" followed by bash block
	launchingSkillPattern = regexp.MustCompile(`(?is)Launching skill:\s*(\S+).*?` + "```(?:bash|sh)?\n([^`]+)```")

	// Pattern 2: Skill tool JSON followed by command
	skillToolPattern = regexp.MustCompile(`"skill":\s*"([^"]+)"`)
	commandPattern   = regexp.MustCompile(`"command":\s*"([^"]+)"`)

	// Pattern 3: JSONL format - look for Skill tool calls
	jsonlSkillPattern = regexp.MustCompile(`\{"skill":\s*"([^"]+)"`)

	// Pattern 4: Bash tool calls after skill invocation
	bashToolPattern = regexp.MustCompile(`"name":\s*"Bash"[^}]*"command":\s*"([^"]+)"`)
)

// ParseSessionTranscripts scans Claude session files for real skill->command pairs.
// Returns a map of skill names to lists of commands that were executed.
func ParseSessionTranscripts(skillFilter string, days int) (*SessionData, error) {
	data := &SessionData{
		SkillCommands: make(map[string][]string),
	}

	cutoff := time.Now().AddDate(0, 0, -days)

	// Collect all session file patterns to check
	patterns := []string{
		filepath.Join(SessionsDir, "*.json"),
		filepath.Join(SessionsDir, "*.jsonl"),
		filepath.Join(ProjectsDir, "*", "*.json"),
		filepath.Join(ProjectsDir, "*", "*.jsonl"),
		filepath.Join(ProjectsDir, "**", "*.jsonl"),
	}

	seen := make(map[string]bool)

	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		for _, sessionFile := range matches {
			if seen[sessionFile] {
				continue
			}
			seen[sessionFile] = true

			// Check file modification time
			info, err := os.Stat(sessionFile)
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				continue
			}

			data.TotalSessions++

			// Read and parse the session file
			content, err := os.ReadFile(sessionFile)
			if err != nil {
				continue
			}

			// Extract skill->command pairs
			pairs := extractSkillCommands(string(content), skillFilter)
			for skill, commands := range pairs {
				data.SkillCommands[skill] = append(data.SkillCommands[skill], commands...)
				data.TotalMatches += len(commands)
			}
		}
	}

	return data, nil
}

// extractSkillCommands extracts skill invocations and their executed commands from session content.
func extractSkillCommands(content, skillFilter string) map[string][]string {
	result := make(map[string][]string)

	// Try JSONL format first (line-by-line JSON)
	if strings.Contains(content, "\n{") || strings.HasPrefix(content, "{") {
		lines := strings.Split(content, "\n")
		var currentSkill string

		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			// Look for Skill tool invocation
			if strings.Contains(line, `"Skill"`) || strings.Contains(line, `"skill"`) {
				if match := skillToolPattern.FindStringSubmatch(line); len(match) > 1 {
					currentSkill = match[1]
				}
			}

			// Look for Bash command after skill
			if currentSkill != "" && strings.Contains(line, `"Bash"`) {
				if match := bashToolPattern.FindStringSubmatch(line); len(match) > 1 {
					cmd := unescapeJSON(match[1])
					if cmd != "" && !strings.HasPrefix(cmd, "#") {
						if skillFilter == "" || currentSkill == skillFilter {
							result[currentSkill] = append(result[currentSkill], cmd)
						}
					}
					// Reset after capturing
					currentSkill = ""
				}
			}

			// Also try direct command pattern
			if currentSkill != "" {
				if match := commandPattern.FindStringSubmatch(line); len(match) > 1 {
					cmd := unescapeJSON(match[1])
					if cmd != "" && !strings.HasPrefix(cmd, "#") {
						if skillFilter == "" || currentSkill == skillFilter {
							result[currentSkill] = append(result[currentSkill], cmd)
						}
					}
				}
			}
		}
	}

	// Try markdown-style "Launching skill:" pattern
	matches := launchingSkillPattern.FindAllStringSubmatch(content, -1)
	for _, match := range matches {
		if len(match) >= 3 {
			skillName := strings.TrimSpace(match[1])
			commandBlock := strings.TrimSpace(match[2])

			// Take first non-comment line
			lines := strings.Split(commandBlock, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					if skillFilter == "" || skillName == skillFilter {
						result[skillName] = append(result[skillName], line)
					}
					break
				}
			}
		}
	}

	return result
}

// unescapeJSON handles escaped characters in JSON strings.
func unescapeJSON(s string) string {
	// Try to unmarshal as JSON string to handle escapes
	var unescaped string
	if err := json.Unmarshal([]byte(`"`+s+`"`), &unescaped); err == nil {
		return unescaped
	}
	// Fall back to basic replacements
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\\`, `\`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	return s
}

// ScanFromSessions analyzes skills using real data from Claude session transcripts.
func ScanFromSessions(skillsDir string, skillFilter string, days int) ([]RealScanResult, error) {
	sessionData, err := ParseSessionTranscripts(skillFilter, days)
	if err != nil {
		return nil, err
	}

	if len(sessionData.SkillCommands) == 0 {
		return nil, nil // No data found
	}

	var results []RealScanResult

	for skillName, commands := range sessionData.SkillCommands {
		if len(commands) < 2 {
			// Need at least 2 invocations to calculate variance
			continue
		}

		skillPath, ok := resolveSkillPath(skillsDir, skillName)
		if !ok {
			continue
		}

		// Get structural analysis
		structure, err := AnalyzeSkillStructure(skillPath)
		if err != nil {
			continue
		}

		// Get expected command
		expected, err := ExtractDefaultCommand(skillPath)
		if err != nil || expected == "" {
			continue
		}

		// Calculate wobble on REAL commands
		metrics := CalculateWobble(commands, expected)

		verdict, recommendation := GetVerdict(metrics.WobbleScore, structure.RiskScore)
		combinedRisk := GetCombinedRisk(metrics.WobbleScore, structure.RiskScore)

		results = append(results, RealScanResult{
			Skill:          skillName,
			Expected:       expected,
			Invocations:    len(commands),
			RealData:       true,
			Structure:      structure,
			Behavioral:     metrics,
			Verdict:        verdict,
			Recommendation: recommendation,
			CombinedRisk:   combinedRisk,
		})
	}

	return results, nil
}

func resolveSkillPath(skillsDir, skillName string) (string, bool) {
	candidates := []string{skillName}
	if strings.Contains(skillName, ":") {
		parts := strings.Split(skillName, ":")
		candidates = append(candidates, strings.Join(parts, string(os.PathSeparator)))
		candidates = append(candidates, strings.Join(parts, "-"))
		candidates = append(candidates, parts[len(parts)-1])
	}

	dirs := []string{skillsDir}
	if skillsDir != DefaultGlobalSkillsDir {
		dirs = append(dirs, DefaultGlobalSkillsDir)
	}

	for _, baseDir := range dirs {
		for _, candidate := range candidates {
			path := filepath.Join(baseDir, candidate)
			if _, err := os.Stat(path); err == nil {
				return path, true
			}
			if _, err := os.Stat(path + ".md"); err == nil {
				return path + ".md", true
			}
		}
	}

	return "", false
}

// RealScanResult contains analysis from real session data.
type RealScanResult struct {
	Skill          string              `json:"skill"`
	Expected       string              `json:"expected"`
	Invocations    int                 `json:"invocations"`
	RealData       bool                `json:"real_data"`
	Structure      *StructuralAnalysis `json:"structure"`
	Behavioral     *WobbleMetrics      `json:"behavioral"`
	Verdict        string              `json:"verdict"`
	Recommendation string              `json:"recommendation"`
	CombinedRisk   float64             `json:"combined_risk"`
}
