package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/storage/memory"
	"github.com/steveyegge/beads/internal/types"
)

type driftSummaryOutput struct {
	LastScanAt        string `json:"last_scan_at"`
	Stable            int    `json:"stable"`
	Wobbly            int    `json:"wobbly"`
	Unstable          int    `json:"unstable"`
	SkillsFixed       int    `json:"skills_fixed"`
	SpecsWithoutBeads int    `json:"specs_without_beads"`
	BeadsWithoutSpecs int    `json:"beads_without_specs"`
}

func TestDriftCommand_JSON(t *testing.T) {
	origJsonOutput := jsonOutput
	defer func() { jsonOutput = origJsonOutput }()
	jsonOutput = true

	origAllowStale := allowStale
	defer func() { allowStale = origAllowStale }()
	allowStale = true

	oldStore := store
	defer func() { store = oldStore }()

	oldCtx := rootCtx
	defer func() { rootCtx = oldCtx }()
	rootCtx = context.Background()

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("create beads dir: %v", err)
	}

	if err := os.Setenv("BEADS_DIR", beadsDir); err != nil {
		t.Fatalf("set BEADS_DIR: %v", err)
	}
	defer os.Unsetenv("BEADS_DIR")

	if err := os.Setenv("AGENT_NAME", "tester"); err != nil {
		t.Fatalf("set AGENT_NAME: %v", err)
	}
	defer os.Unsetenv("AGENT_NAME")

	memStore := memory.New(filepath.Join(beadsDir, "issues.jsonl"))
	setStore(memStore)

	now := time.Date(2026, 2, 3, 9, 0, 0, 0, time.UTC)
	entries := []spec.SpecRegistryEntry{
		{SpecID: "specs/active/ALPHA.md", Title: "Alpha", DiscoveredAt: now},
		{SpecID: "specs/active/BETA.md", Title: "Beta", DiscoveredAt: now},
	}
	if err := memStore.UpsertSpecRegistry(rootCtx, entries); err != nil {
		t.Fatalf("upsert spec registry: %v", err)
	}

	issue := &types.Issue{ID: "bd-1", Title: "Alpha", SpecID: "specs/active/ALPHA.md", Status: "open", IssueType: types.TypeTask}
	if err := memStore.CreateIssue(rootCtx, issue, "tester"); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	issueMissing := &types.Issue{ID: "bd-2", Title: "Missing", SpecID: "specs/active/MISSING.md", Status: "open", IssueType: types.TypeTask}
	if err := memStore.CreateIssue(rootCtx, issueMissing, "tester"); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	skillsPath, historyPath, err := wobbleStorePaths()
	if err != nil {
		t.Fatalf("wobble store paths: %v", err)
	}

	firstScan := wobbleStore{
		Version:     1,
		GeneratedAt: now.Add(-2 * time.Hour),
		Skills: []wobbleSkill{{
			ID:          "beads",
			Verdict:     "wobbly",
			ChangeState: "wobbly",
		}},
	}
	firstEntry := buildWobbleHistoryEntry("tester", firstScan.GeneratedAt, firstScan.Skills)
	if err := writeWobbleStore(skillsPath, historyPath, firstScan, firstEntry); err != nil {
		t.Fatalf("write wobble store: %v", err)
	}

	secondScan := wobbleStore{
		Version:     1,
		GeneratedAt: now,
		Skills: []wobbleSkill{{
			ID:          "beads",
			Verdict:     "stable",
			ChangeState: "stable",
		}},
	}
	secondEntry := buildWobbleHistoryEntry("tester", secondScan.GeneratedAt, secondScan.Skills)
	if err := writeWobbleStore(skillsPath, historyPath, secondScan, secondEntry); err != nil {
		t.Fatalf("write wobble store: %v", err)
	}

	output := captureDriftOutput(t)

	var summary driftSummaryOutput
	if err := json.Unmarshal([]byte(output), &summary); err != nil {
		t.Fatalf("parse JSON output: %v\nOutput: %s", err, output)
	}

	if summary.LastScanAt == "" {
		t.Fatalf("expected last_scan_at")
	}
	if summary.SkillsFixed != 1 {
		t.Fatalf("expected skills_fixed 1, got %d", summary.SkillsFixed)
	}
	if summary.SpecsWithoutBeads != 1 {
		t.Fatalf("expected specs_without_beads 1, got %d", summary.SpecsWithoutBeads)
	}
	if summary.BeadsWithoutSpecs != 1 {
		t.Fatalf("expected beads_without_specs 1, got %d", summary.BeadsWithoutSpecs)
	}
}

func captureDriftOutput(t *testing.T) string {
	t.Helper()
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	driftCmd.Run(driftCmd, []string{})

	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = oldStdout

	return buf.String()
}
