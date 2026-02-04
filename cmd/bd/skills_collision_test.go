package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSkillFrontmatterList(t *testing.T) {
	tmpDir := t.TempDir()
	skillDir := filepath.Join(tmpDir, "alpha")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatal(err)
	}

	content := `---
name: alpha
supersedes:
  - beta
  - gamma
---

# Alpha Skill
`
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	list, err := parseSkillFrontmatterList(tmpDir, "alpha", "supersedes")
	if err != nil {
		t.Fatalf("parseSkillFrontmatterList error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 supersedes entries, got %d", len(list))
	}
	if list[0] != "beta" || list[1] != "gamma" {
		t.Fatalf("unexpected supersedes list: %v", list)
	}
}

func TestFindPrefixCollisions(t *testing.T) {
	project := map[string]SkillInfo{
		"backend": {Name: "backend", Source: "project", Path: "/project/backend"},
	}
	globals := map[string]SkillInfo{
		"backend-meta": {Name: "backend-meta", Source: "global-claude", Path: "/global/backend-meta"},
	}

	results := findPrefixCollisions(project, globals, "global-claude")
	if len(results) != 1 {
		t.Fatalf("expected 1 collision, got %d", len(results))
	}
	if results[0].Name != "backend-meta" {
		t.Fatalf("unexpected collision: %v", results[0])
	}
}
