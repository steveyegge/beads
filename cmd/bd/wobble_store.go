package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/utils"
	"github.com/steveyegge/beads/internal/wobble"
)

const wobbleStoreVersion = 1

type wobbleStore struct {
	Version     int           `json:"version"`
	GeneratedAt time.Time     `json:"generated_at"`
	Skills      []wobbleSkill `json:"skills"`
}

type wobbleSkill struct {
	ID          string   `json:"id"`
	Verdict     string   `json:"verdict"`
	ChangeState string   `json:"change_state"`
	Signals     []string `json:"signals,omitempty"`
	Dependents  []string `json:"dependents,omitempty"`
}

type wobbleHistoryEntry struct {
	Actor          string    `json:"actor"`
	CreatedAt      time.Time `json:"created_at"`
	Stable         int       `json:"stable"`
	Wobbly         int       `json:"wobbly"`
	Unstable       int       `json:"unstable"`
	Skills         []string  `json:"skills,omitempty"`
	WobblySkills   []string  `json:"wobbly_skills,omitempty"`
	UnstableSkills []string  `json:"unstable_skills,omitempty"`
}

func wobbleStorePaths() (string, string, error) {
	dir, err := wobbleStoreDir()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	return filepath.Join(dir, "skills.json"), filepath.Join(dir, "history.json"), nil
}

func wobbleStoreDir() (string, error) {
	if dbPath != "" {
		return filepath.Join(filepath.Dir(dbPath), "wobble"), nil
	}
	if envDir := os.Getenv("BEADS_DIR"); envDir != "" {
		return filepath.Join(utils.CanonicalizePath(envDir), "wobble"), nil
	}
	return filepath.Join(".beads", "wobble"), nil
}

func writeWobbleStore(skillsPath, historyPath string, store wobbleStore, entry wobbleHistoryEntry) error {
	existing, history, err := loadWobbleStore(skillsPath, historyPath)
	if err != nil {
		return err
	}

	if store.Version == 0 {
		store.Version = wobbleStoreVersion
	}
	store.Skills = preserveWobbleDependents(existing.Skills, store.Skills)

	skillsData, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(skillsPath, skillsData, 0644); err != nil {
		return err
	}

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	history = append(history, entry)
	historyData, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(historyPath, historyData, 0644)
}

func loadWobbleStore(skillsPath, historyPath string) (wobbleStore, []wobbleHistoryEntry, error) {
	store := wobbleStore{Version: wobbleStoreVersion}
	if data, err := os.ReadFile(skillsPath); err == nil {
		if err := json.Unmarshal(data, &store); err != nil {
			return wobbleStore{}, nil, err
		}
		if store.Version == 0 {
			store.Version = wobbleStoreVersion
		}
	} else if !os.IsNotExist(err) {
		return wobbleStore{}, nil, err
	}

	history := []wobbleHistoryEntry{}
	if data, err := os.ReadFile(historyPath); err == nil {
		if err := json.Unmarshal(data, &history); err != nil {
			return wobbleStore{}, nil, err
		}
	} else if !os.IsNotExist(err) {
		return wobbleStore{}, nil, err
	}

	return store, history, nil
}

func persistWobbleScan(skills []wobbleSkill, generatedAt time.Time, actor string) error {
	if len(skills) == 0 {
		return nil
	}
	skillsPath, historyPath, err := wobbleStorePaths()
	if err != nil {
		return err
	}
	store := wobbleStore{
		Version:     wobbleStoreVersion,
		GeneratedAt: generatedAt,
		Skills:      skills,
	}
	entry := buildWobbleHistoryEntry(actor, generatedAt, skills)
	return writeWobbleStore(skillsPath, historyPath, store, entry)
}

func buildWobbleHistoryEntry(actor string, createdAt time.Time, skills []wobbleSkill) wobbleHistoryEntry {
	stable := make([]string, 0, len(skills))
	wobbly := make([]string, 0, len(skills))
	unstable := make([]string, 0, len(skills))

	for _, skill := range skills {
		verdict := normalizeWobbleVerdict(skill.Verdict)
		switch verdict {
		case "stable":
			stable = append(stable, skill.ID)
		case "wobbly":
			wobbly = append(wobbly, skill.ID)
		case "unstable":
			unstable = append(unstable, skill.ID)
		}
	}

	sort.Strings(stable)
	sort.Strings(wobbly)
	sort.Strings(unstable)

	return wobbleHistoryEntry{
		Actor:          actor,
		CreatedAt:      createdAt,
		Stable:         len(stable),
		Wobbly:         len(wobbly),
		Unstable:       len(unstable),
		Skills:         stable,
		WobblySkills:   wobbly,
		UnstableSkills: unstable,
	}
}

func normalizeWobbleVerdict(verdict string) string {
	v := strings.ToLower(strings.TrimSpace(verdict))
	switch v {
	case "stable", "wobbly", "unstable":
		return v
	}
	return "unknown"
}

func wobbleSkillsFromSummary(results []wobble.SkillSummary) []wobbleSkill {
	skills := make([]wobbleSkill, 0, len(results))
	for _, result := range results {
		verdict, _ := wobble.GetVerdict(0, result.StructuralRisk)
		normalized := normalizeWobbleVerdict(verdict)
		skills = append(skills, wobbleSkill{
			ID:          result.Name,
			Verdict:     normalized,
			ChangeState: normalized,
		})
	}
	return skills
}

func wobbleSkillsFromScanResult(result *wobble.ScanResult) []wobbleSkill {
	if result == nil {
		return nil
	}
	verdict := normalizeWobbleVerdict(result.Verdict)
	return []wobbleSkill{{
		ID:          result.Skill,
		Verdict:     verdict,
		ChangeState: verdict,
	}}
}

func wobbleSkillsFromRealResults(results []wobble.RealScanResult) []wobbleSkill {
	skills := make([]wobbleSkill, 0, len(results))
	for _, result := range results {
		verdict := normalizeWobbleVerdict(result.Verdict)
		skills = append(skills, wobbleSkill{
			ID:          result.Skill,
			Verdict:     verdict,
			ChangeState: verdict,
		})
	}
	return skills
}

func preserveWobbleDependents(existing, updated []wobbleSkill) []wobbleSkill {
	if len(existing) == 0 || len(updated) == 0 {
		return updated
	}
	existingDeps := map[string][]string{}
	for _, skill := range existing {
		if len(skill.Dependents) > 0 {
			existingDeps[skill.ID] = append([]string{}, skill.Dependents...)
		}
	}
	for i := range updated {
		if len(updated[i].Dependents) == 0 {
			if deps, ok := existingDeps[updated[i].ID]; ok {
				updated[i].Dependents = deps
			}
		}
	}
	return updated
}

func prettyWobbleStoreError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("wobble store: %w", err)
}
