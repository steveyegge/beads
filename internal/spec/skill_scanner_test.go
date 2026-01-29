package spec

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractSkillMetadata(t *testing.T) {
	// Create a temporary skill file
	tmpDir := t.TempDir()
	skillPath := filepath.Join(tmpDir, "SKILL.md")

	content := `# Test Skill

description: A test skill for unit testing
version: 1.2.3

## Overview

This skill does testing things.
`

	err := os.WriteFile(skillPath, []byte(content), 0644)
	if err != nil {
		t.Fatal(err)
	}

	scanner := NewSkillScanner(tmpDir)
	title, desc, version, err := scanner.extractSkillMetadata(skillPath)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if title != "Test Skill" {
		t.Errorf("expected title 'Test Skill', got '%s'", title)
	}

	if desc != "A test skill for unit testing" {
		t.Errorf("expected description, got '%s'", desc)
	}

	if version != "1.2.3" {
		t.Errorf("expected version '1.2.3', got '%s'", version)
	}
}

func TestFindSkillMarkdown(t *testing.T) {
	tmpDir := t.TempDir()

	// Create SKILL.md
	skillPath := filepath.Join(tmpDir, "SKILL.md")
	os.WriteFile(skillPath, []byte("# Skill"), 0644)

	scanner := NewSkillScanner(tmpDir)
	found := scanner.findSkillMarkdown(tmpDir)

	if found != skillPath {
		t.Errorf("expected %s, got %s", skillPath, found)
	}
}

func TestScanLayer(t *testing.T) {
	tmpDir := t.TempDir()

	// Create test skill directory
	skillDir := filepath.Join(tmpDir, "test-skill")
	os.Mkdir(skillDir, 0755)

	// Create SKILL.md
	skillMD := filepath.Join(skillDir, "SKILL.md")
	content := `# Test Skill
version: 1.0.0
description: A test skill`
	os.WriteFile(skillMD, []byte(content), 0644)

	scanner := NewSkillScanner(tmpDir)
	skills, err := scanner.scanLayer(SkillLayerClaude, tmpDir)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if len(skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(skills))
	}

	if skills[0].SkillID != "test-skill" {
		t.Errorf("expected skill ID 'test-skill', got '%s'", skills[0].SkillID)
	}

	if skills[0].Version != "1.0.0" {
		t.Errorf("expected version '1.0.0', got '%s'", skills[0].Version)
	}
}

func TestDetectMismatches(t *testing.T) {
	tests := []struct {
		name        string
		skills      []ScannedSkill
		expectCount int
	}{
		{
			name: "no mismatch - single layer",
			skills: []ScannedSkill{
				{SkillID: "test-skill", Layer: SkillLayerClaude, Version: "1.0.0", SHA256: "abc123"},
			},
			expectCount: 0,
		},
		{
			name: "mismatch - different versions",
			skills: []ScannedSkill{
				{SkillID: "test-skill", Layer: SkillLayerClaude, Version: "1.0.0", SHA256: "abc123"},
				{SkillID: "test-skill", Layer: SkillLayerCodexSuperpowers, Version: "2.0.0", SHA256: "def456"},
			},
			expectCount: 1,
		},
		{
			name: "no mismatch - identical across layers",
			skills: []ScannedSkill{
				{SkillID: "test-skill", Layer: SkillLayerClaude, Version: "1.0.0", SHA256: "abc123"},
				{SkillID: "test-skill", Layer: SkillLayerCodexSuperpowers, Version: "1.0.0", SHA256: "abc123"},
			},
			expectCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mismatches := DetectMismatches(tt.skills)
			if len(mismatches) != tt.expectCount {
				t.Errorf("expected %d mismatches, got %d", tt.expectCount, len(mismatches))
			}
		})
	}
}
