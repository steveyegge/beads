package rpc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/types"
)

type wobbleStoreSnapshot struct {
	Version     int           `json:"version"`
	GeneratedAt time.Time     `json:"generated_at"`
	Skills      []wobbleSkill `json:"skills"`
}

type wobbleSkill struct {
	ID      string `json:"id"`
	Verdict string `json:"verdict"`
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

type wobbleDriftSummary struct {
	LastScanAt        string `json:"last_scan_at,omitempty"`
	Stable            int    `json:"stable"`
	Wobbly            int    `json:"wobbly"`
	Unstable          int    `json:"unstable"`
	SkillsFixed       int    `json:"skills_fixed"`
	SpecsWithoutBeads int    `json:"specs_without_beads"`
	BeadsWithoutSpecs int    `json:"beads_without_specs"`
}

func (s *Server) handleDriftSummary(req *Request) Response {
	ctx := s.reqCtx(req)

	skillsPath, historyPath := wobbleStorePathsForRPC(s, req)
	storeSnapshot, history, err := loadWobbleStoreForRPC(skillsPath, historyPath)
	if err != nil {
		return Response{Success: false, Error: fmt.Sprintf("wobble store: %v", err)}
	}

	stableCount, wobblyCount, unstableCount := countWobbleVerdicts(storeSnapshot.Skills)
	skillsFixed := skillsFixedFromHistory(history)

	specsWithoutBeads, beadsWithoutSpecs := 0, 0
	if specStore, ok := s.storage.(spec.SpecRegistryStore); ok {
		entries, err := specStore.ListSpecRegistryWithCounts(ctx)
		if err == nil {
			specIDs := make(map[string]struct{}, len(entries))
			for _, entry := range entries {
				specIDs[entry.Spec.SpecID] = struct{}{}
				if entry.BeadCount == 0 {
					specsWithoutBeads++
				}
			}

			issues, err := s.storage.SearchIssues(ctx, "", types.IssueFilter{})
			if err == nil {
				for _, issue := range issues {
					if issue.SpecID == "" {
						beadsWithoutSpecs++
						continue
					}
					if _, ok := specIDs[issue.SpecID]; !ok {
						beadsWithoutSpecs++
					}
				}
			}
		}
	}

	summary := wobbleDriftSummary{
		Stable:            stableCount,
		Wobbly:            wobblyCount,
		Unstable:          unstableCount,
		SkillsFixed:       skillsFixed,
		SpecsWithoutBeads: specsWithoutBeads,
		BeadsWithoutSpecs: beadsWithoutSpecs,
	}
	if !storeSnapshot.GeneratedAt.IsZero() {
		summary.LastScanAt = storeSnapshot.GeneratedAt.UTC().Format(time.RFC3339)
	}

	data, _ := json.Marshal(summary)
	return Response{Success: true, Data: data}
}

func wobbleStorePathsForRPC(s *Server, req *Request) (string, string) {
	dir := wobbleStoreDirForRPC(s, req)
	return filepath.Join(dir, "skills.json"), filepath.Join(dir, "history.json")
}

func wobbleStoreDirForRPC(s *Server, req *Request) string {
	if envDir := os.Getenv("BEADS_DIR"); envDir != "" {
		return filepath.Join(envDir, "wobble")
	}
	root := s.workspacePath
	if root == "" {
		root = req.Cwd
	}
	if root == "" {
		root = "."
	}
	return filepath.Join(root, ".beads", "wobble")
}

func loadWobbleStoreForRPC(skillsPath, historyPath string) (wobbleStoreSnapshot, []wobbleHistoryEntry, error) {
	store := wobbleStoreSnapshot{}
	if data, err := os.ReadFile(skillsPath); err == nil {
		if err := json.Unmarshal(data, &store); err != nil {
			return wobbleStoreSnapshot{}, nil, err
		}
	} else if !os.IsNotExist(err) {
		return wobbleStoreSnapshot{}, nil, err
	}

	history := []wobbleHistoryEntry{}
	if data, err := os.ReadFile(historyPath); err == nil {
		if err := json.Unmarshal(data, &history); err != nil {
			return wobbleStoreSnapshot{}, nil, err
		}
	} else if !os.IsNotExist(err) {
		return wobbleStoreSnapshot{}, nil, err
	}

	return store, history, nil
}

func countWobbleVerdicts(skills []wobbleSkill) (int, int, int) {
	stableCount := 0
	wobblyCount := 0
	unstableCount := 0
	for _, skill := range skills {
		switch normalizeWobbleVerdict(skill.Verdict) {
		case "stable":
			stableCount++
		case "wobbly":
			wobblyCount++
		case "unstable":
			unstableCount++
		}
	}
	return stableCount, wobblyCount, unstableCount
}

func skillsFixedFromHistory(history []wobbleHistoryEntry) int {
	if len(history) < 2 {
		return 0
	}

	sort.Slice(history, func(i, j int) bool {
		return history[i].CreatedAt.Before(history[j].CreatedAt)
	})

	prev := history[len(history)-2]
	curr := history[len(history)-1]
	stableNow := make(map[string]struct{}, len(curr.Skills))
	for _, skill := range curr.Skills {
		stableNow[skill] = struct{}{}
	}

	fixed := 0
	for _, skill := range prev.UnstableSkills {
		if _, ok := stableNow[skill]; ok {
			fixed++
		}
	}
	for _, skill := range prev.WobblySkills {
		if _, ok := stableNow[skill]; ok {
			fixed++
		}
	}

	return fixed
}

func normalizeWobbleVerdict(v string) string {
	switch v {
	case "STABLE", "stable":
		return "stable"
	case "WOBBLY", "wobbly":
		return "wobbly"
	case "UNSTABLE", "unstable":
		return "unstable"
	default:
		return v
	}
}
