package spec

import (
	"crypto/sha256"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
)

// SkillScanner discovers and analyzes skills across all layers
type SkillScanner struct {
	projectRoot string
}

// NewSkillScanner creates a new skill scanner
func NewSkillScanner(projectRoot string) *SkillScanner {
	return &SkillScanner{projectRoot: projectRoot}
}

// ScanAllLayers scans all skill layers (claude, codex-superpowers, codex-local)
func (s *SkillScanner) ScanAllLayers() ([]ScannedSkill, error) {
	var allSkills []ScannedSkill

	// Scan Claude skills (.claude/skills/)
	claudeSkills, err := s.scanLayer(SkillLayerClaude, filepath.Join(s.projectRoot, ".claude", "skills"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("error scanning claude skills: %w", err)
	}
	allSkills = append(allSkills, claudeSkills...)

	// Scan Codex Superpowers (~/.codex/superpowers/skills/)
	home, err := os.UserHomeDir()
	if err == nil {
		codexSuperSkills, err := s.scanLayer(SkillLayerCodexSuperpowers, filepath.Join(home, ".codex", "superpowers", "skills"))
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("error scanning codex superpowers skills: %w", err)
		}
		allSkills = append(allSkills, codexSuperSkills...)

		// Scan Codex Local (~/.codex/skills/)
		codexLocalSkills, err := s.scanLayer(SkillLayerCodexLocal, filepath.Join(home, ".codex", "skills"))
		if err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("error scanning codex local skills: %w", err)
		}
		allSkills = append(allSkills, codexLocalSkills...)
	}

	return allSkills, nil
}

// scanLayer scans a single skill layer directory
func (s *SkillScanner) scanLayer(layer SkillLayer, layerPath string) ([]ScannedSkill, error) {
	var skills []ScannedSkill

	entries, err := ioutil.ReadDir(layerPath)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		// Skip hidden and system directories
		if entry.Name()[0] == '_' || entry.Name()[0] == '.' {
			continue
		}
		if entry.Name() == "dist" {
			continue // Skip dist directory
		}

		skillPath := filepath.Join(layerPath, entry.Name())
		if !entry.IsDir() {
			continue
		}

		// Look for SKILL.md or skill.md
		skillMD := s.findSkillMarkdown(skillPath)
		if skillMD == "" {
			continue
		}

		// Extract metadata from SKILL.md
		title, description, version, err := s.extractSkillMetadata(skillMD)
		if err != nil {
			continue // Skip on extraction error
		}

		// Compute directory hash
		hash, err := s.hashDir(skillPath)
		if err != nil {
			continue
		}

		// Get file info
		info, err := os.Stat(skillPath)
		if err != nil {
			continue
		}

		skill := ScannedSkill{
			SkillID:     entry.Name(),
			Layer:       layer,
			Path:        skillPath,
			Title:       title,
			Description: description,
			Version:     version,
			SHA256:      hash,
			Mtime:       info.ModTime(),
		}

		skills = append(skills, skill)
	}

	return skills, nil
}

// findSkillMarkdown looks for SKILL.md or skill.md in a directory
func (s *SkillScanner) findSkillMarkdown(dir string) string {
	candidates := []string{
		filepath.Join(dir, "SKILL.md"),
		filepath.Join(dir, "skill.md"),
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// extractSkillMetadata extracts title, description, and version from SKILL.md
func (s *SkillScanner) extractSkillMetadata(filePath string) (title, description, version string, err error) {
	content, err := ioutil.ReadFile(filePath)
	if err != nil {
		return "", "", "", err
	}

	text := string(content)

	// Extract title (first # heading)
	titleRE := regexp.MustCompile(`(?m)^#\s+(.+)$`)
	matches := titleRE.FindStringSubmatch(text)
	if len(matches) > 1 {
		title = matches[1]
	}

	// Extract description from frontmatter or first paragraph
	descRE := regexp.MustCompile(`(?m)^description:\s*(.+)$`)
	matches = descRE.FindStringSubmatch(text)
	if len(matches) > 1 {
		description = matches[1]
	}

	// Extract version from frontmatter
	versionRE := regexp.MustCompile(`(?m)^version:\s*(.+)$`)
	matches = versionRE.FindStringSubmatch(text)
	if len(matches) > 1 {
		version = matches[1]
	} else {
		// Default version if not specified
		version = "0.0.0"
	}

	return
}

// hashDir computes SHA256 hash of all files in a directory
func (s *SkillScanner) hashDir(dir string) (string, error) {
	h := sha256.New()

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip hidden files and directories
		if info.Name()[0] == '.' {
			return nil
		}

		if info.IsDir() {
			return nil
		}

		// Hash file content and name
		relPath, _ := filepath.Rel(dir, path)
		fmt.Fprintf(h, "%s\n", relPath)

		content, err := ioutil.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write(content)

		return nil
	})

	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// DetectMismatches analyzes scanned skills for version mismatches
func DetectMismatches(skills []ScannedSkill) []SkillMismatch {
	var mismatches []SkillMismatch

	// Group skills by ID
	skillsByID := make(map[string][]ScannedSkill)
	for _, skill := range skills {
		skillsByID[skill.SkillID] = append(skillsByID[skill.SkillID], skill)
	}

	// Check for mismatches within each skill ID
	for skillID, versions := range skillsByID {
		if len(versions) <= 1 {
			continue // Only one instance, no mismatch possible
		}

		// Check if all versions are identical
		firstHash := versions[0].SHA256
		firstVersion := versions[0].Version
		hasVersionMismatch := false
		var affectedLayers []SkillLayer
		versionMap := make(map[SkillLayer]string)

		for _, v := range versions {
			versionMap[v.Layer] = v.Version
			if v.SHA256 != firstHash || v.Version != firstVersion {
				hasVersionMismatch = true
				affectedLayers = append(affectedLayers, v.Layer)
			}
		}

		if hasVersionMismatch {
			severity := "MEDIUM"
			if firstVersion != "" && versionMap[SkillLayerCodexSuperpowers] != "" {
				// If codex-superpowers version differs from others, it's high severity
				if versionMap[SkillLayerCodexSuperpowers] != firstVersion {
					severity = "HIGH"
				}
			}

			mismatch := SkillMismatch{
				SkillID:        skillID,
				Reason:         fmt.Sprintf("Found %d different versions across layers", len(affectedLayers)+1),
				AffectedLayers: affectedLayers,
				Versions:       versionMap,
				Severity:       severity,
				RecommendedFix: fmt.Sprintf("Sync %s from codex-superpowers (upstream) to project", skillID),
			}
			mismatches = append(mismatches, mismatch)
		}
	}

	return mismatches
}

// FindSkillReferences searches specs for references to skills
func FindSkillReferences(projectRoot string, skills []ScannedSkill) ([]SkillSpecLink, error) {
	var links []SkillSpecLink

	specsDir := filepath.Join(projectRoot, "specs")

	// Walk all markdown files in specs/
	err := filepath.Walk(specsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && filepath.Ext(path) == ".md" {
			specID, _ := filepath.Rel(projectRoot, path)

			// Read file and search for skill references
			content, err := ioutil.ReadFile(path)
			if err != nil {
				return nil
			}

			text := string(content)

			// For each skill, check if mentioned in spec
			for _, skill := range skills {
				// Look for mentions of skill name (case-insensitive, word boundary)
				pattern := fmt.Sprintf(`(?i)\b%s\b`, regexp.QuoteMeta(skill.SkillID))
				re := regexp.MustCompile(pattern)

				if re.MatchString(text) {
					link := SkillSpecLink{
						SkillID: skill.SkillID,
						SpecID:  specID,
					}
					links = append(links, link)
					break // Only add once per skill per spec
				}
			}
		}

		return nil
	})

	return links, err
}
