package wobble

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// RiskFactors represents the 7 structural risk factors that contribute to skill wobble.
type RiskFactors struct {
	NoExecuteNow           bool `json:"no_execute_now"`            // Missing "EXECUTE NOW" section
	NoCodeBlock            bool `json:"no_code_block"`             // No bash code blocks
	HasNumberedSteps       bool `json:"has_numbered_steps"`        // Numbered steps present (ambiguity)
	HasOptionsWithoutDefault bool `json:"has_options_without_default"` // "or" without "(default)"
	TooLong                bool `json:"too_long"`                  // Content > 4000 chars
	NoDoNotImprovise       bool `json:"no_do_not_improvise"`       // Missing "DO NOT IMPROVISE" warning
	MultipleActionsUnclear bool `json:"multiple_actions_unclear"`  // >5 sections without default
}

// StructuralAnalysis contains the results of analyzing a skill's structure.
type StructuralAnalysis struct {
	RiskScore     float64     `json:"risk_score"`
	RiskFactors   RiskFactors `json:"risk_factors"`
	ActiveFactors []string    `json:"active_factors"` // Human-readable list of active risk factors
	ContentLength int         `json:"content_length"`
	ActionCount   int         `json:"action_count"` // Number of ### sections
}

var (
	numberedStepsPattern = regexp.MustCompile(`(?i)(?:Step \d|step \d|\d\.)`)
	sectionPattern       = regexp.MustCompile(`###`)
	executeNowPattern    = regexp.MustCompile(`(?i)EXECUTE\s+NOW`)
	defaultPattern       = regexp.MustCompile(`(?i)\(default\)`)
	bashBlockPattern     = regexp.MustCompile("```(?:bash|sh)?")
	doNotImprovisePattern = regexp.MustCompile(`(?i)do\s+not\s+improvise`)
)

// AnalyzeSkillStructure analyzes a skill directory for structural risk factors.
func AnalyzeSkillStructure(skillPath string) (*StructuralAnalysis, error) {
	content, err := readSkillContent(skillPath)
	if err != nil {
		return nil, err
	}

	return analyzeContent(content), nil
}

// readSkillContent reads all markdown content from a skill directory or file.
func readSkillContent(skillPath string) (string, error) {
	info, err := os.Stat(skillPath)
	if err != nil {
		return "", err
	}

	var content strings.Builder

	if info.IsDir() {
		// Read all .md files in directory
		entries, err := os.ReadDir(skillPath)
		if err != nil {
			return "", err
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
				data, err := os.ReadFile(filepath.Join(skillPath, entry.Name()))
				if err != nil {
					continue
				}
				content.Write(data)
				content.WriteString("\n")
			}
		}
	} else {
		// Single file skill
		data, err := os.ReadFile(skillPath)
		if err != nil {
			return "", err
		}
		content.Write(data)
	}

	return content.String(), nil
}

// analyzeContent performs structural analysis on skill content.
func analyzeContent(content string) *StructuralAnalysis {
	contentLower := strings.ToLower(content)

	factors := RiskFactors{
		NoExecuteNow:           !executeNowPattern.MatchString(content),
		NoCodeBlock:            !bashBlockPattern.MatchString(content) && !strings.Contains(content, "```"),
		HasNumberedSteps:       numberedStepsPattern.MatchString(content),
		HasOptionsWithoutDefault: strings.Contains(contentLower, " or ") && !defaultPattern.MatchString(content),
		TooLong:                len(content) > 4000,
		NoDoNotImprovise:       !doNotImprovisePattern.MatchString(content),
		MultipleActionsUnclear: sectionPattern.FindAllStringIndex(content, -1) != nil &&
			len(sectionPattern.FindAllStringIndex(content, -1)) > 5 &&
			!defaultPattern.MatchString(content),
	}

	// Calculate risk score as fraction of active factors
	activeCount := 0
	var activeFactors []string

	if factors.NoExecuteNow {
		activeCount++
		activeFactors = append(activeFactors, "no execute now")
	}
	if factors.NoCodeBlock {
		activeCount++
		activeFactors = append(activeFactors, "no code block")
	}
	if factors.HasNumberedSteps {
		activeCount++
		activeFactors = append(activeFactors, "has numbered steps")
	}
	if factors.HasOptionsWithoutDefault {
		activeCount++
		activeFactors = append(activeFactors, "options without default")
	}
	if factors.TooLong {
		activeCount++
		activeFactors = append(activeFactors, "too long")
	}
	if factors.NoDoNotImprovise {
		activeCount++
		activeFactors = append(activeFactors, "no do not improvise")
	}
	if factors.MultipleActionsUnclear {
		activeCount++
		activeFactors = append(activeFactors, "multiple actions unclear")
	}

	riskScore := float64(activeCount) / 7.0

	// Count action sections
	actionCount := len(sectionPattern.FindAllStringIndex(content, -1))

	return &StructuralAnalysis{
		RiskScore:     riskScore,
		RiskFactors:   factors,
		ActiveFactors: activeFactors,
		ContentLength: len(content),
		ActionCount:   actionCount,
	}
}

// ExtractDefaultCommand extracts the default command from a skill's content.
// It looks for patterns like "EXECUTE NOW" sections or "(default)" markers
// followed by bash code blocks.
func ExtractDefaultCommand(skillPath string) (string, error) {
	content, err := readSkillContent(skillPath)
	if err != nil {
		return "", err
	}

	return extractDefaultFromContent(content), nil
}

// extractDefaultFromContent extracts the default command from skill content.
func extractDefaultFromContent(content string) string {
	// Patterns to match default commands (in priority order)
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?is)EXECUTE\s+NOW.*?` + "```(?:bash|sh)?\n([^`]+)```"),
		regexp.MustCompile(`(?is)\(default\).*?` + "```(?:bash|sh)?\n([^`]+)```"),
		regexp.MustCompile(`(?is)##\s+.*\(default\).*?` + "```(?:bash|sh)?\n([^`]+)```"),
	}

	for _, pattern := range patterns {
		match := pattern.FindStringSubmatch(content)
		if len(match) > 1 {
			cmd := strings.TrimSpace(match[1])
			// Take first non-comment line
			lines := strings.Split(cmd, "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "#") {
					return line
				}
			}
		}
	}

	return ""
}
