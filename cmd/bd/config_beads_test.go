package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

func TestGenerateConfigBeadID(t *testing.T) {
	tests := []struct {
		category string
		scope    string
		expected string
	}{
		{"claude-hooks", "global", "hq-cfg-claude-hooks-global"},
		{"mcp", "global", "hq-cfg-mcp-global"},
		{"claude-hooks", "town:gt11,rig:gastown", "hq-cfg-claude-hooks-gt11-gastown"},
		{"claude-hooks", "town:gt11,rig:gastown,role:crew", "hq-cfg-claude-hooks-gt11-gastown-crew"},
		{"identity", "town:gt11", "hq-cfg-identity-gt11"},
		{"mcp", "", "hq-cfg-mcp-global"},
	}

	for _, tt := range tests {
		result := generateConfigBeadID(tt.category, tt.scope)
		if result != tt.expected {
			t.Errorf("generateConfigBeadID(%q, %q) = %q, want %q", tt.category, tt.scope, result, tt.expected)
		}
	}
}

func TestScopeToSlug(t *testing.T) {
	tests := []struct {
		scope    string
		expected string
	}{
		{"global", "global"},
		{"", "global"},
		{"town:gt11", "gt11"},
		{"town:gt11,rig:gastown", "gt11-gastown"},
		{"town:gt11,rig:gastown,role:crew", "gt11-gastown-crew"},
		{"town:gt11,rig:gastown,agent:slack", "gt11-gastown-slack"},
	}

	for _, tt := range tests {
		result := scopeToSlug(tt.scope)
		if result != tt.expected {
			t.Errorf("scopeToSlug(%q) = %q, want %q", tt.scope, result, tt.expected)
		}
	}
}

func TestScopeToRigAndLabels(t *testing.T) {
	tests := []struct {
		scope         string
		expectedRig   string
		expectedLen   int
		expectedFirst string
	}{
		{"global", "*", 1, "scope:global"},
		{"town:gt11", "gt11", 1, "town:gt11"},
		{"town:gt11,rig:gastown", "gt11/gastown", 2, "town:gt11"},
		{"town:gt11,rig:gastown,role:crew", "gt11/gastown", 3, "town:gt11"},
	}

	for _, tt := range tests {
		rig, labels := scopeToRigAndLabels(tt.scope)
		if rig != tt.expectedRig {
			t.Errorf("scopeToRigAndLabels(%q) rig = %q, want %q", tt.scope, rig, tt.expectedRig)
		}
		if len(labels) != tt.expectedLen {
			t.Errorf("scopeToRigAndLabels(%q) len(labels) = %d, want %d", tt.scope, len(labels), tt.expectedLen)
		}
		if len(labels) > 0 && labels[0] != tt.expectedFirst {
			t.Errorf("scopeToRigAndLabels(%q) labels[0] = %q, want %q", tt.scope, labels[0], tt.expectedFirst)
		}
	}
}

func TestScoreConfigBead(t *testing.T) {
	tests := []struct {
		name       string
		beadLabels []string
		target     []string
		wantScore  int
		wantApply  bool
	}{
		{
			name:       "global bead, no target scope",
			beadLabels: []string{"scope:global", "config:claude-hooks"},
			target:     nil,
			wantScore:  0,
			wantApply:  true,
		},
		{
			name:       "global bead, with target scope",
			beadLabels: []string{"scope:global", "config:claude-hooks"},
			target:     []string{"scope:global", "town:gt11", "rig:gastown", "role:crew"},
			wantScore:  0,
			wantApply:  true,
		},
		{
			name:       "global + role bead",
			beadLabels: []string{"scope:global", "role:crew", "config:claude-hooks"},
			target:     []string{"scope:global", "town:gt11", "rig:gastown", "role:crew"},
			wantScore:  1,
			wantApply:  true,
		},
		{
			name:       "town + rig bead",
			beadLabels: []string{"town:gt11", "rig:gastown", "config:claude-hooks"},
			target:     []string{"scope:global", "town:gt11", "rig:gastown", "role:crew"},
			wantScore:  2,
			wantApply:  true,
		},
		{
			name:       "town + rig + role bead",
			beadLabels: []string{"town:gt11", "rig:gastown", "role:crew", "config:claude-hooks"},
			target:     []string{"scope:global", "town:gt11", "rig:gastown", "role:crew"},
			wantScore:  3,
			wantApply:  true,
		},
		{
			name:       "town + rig + agent bead",
			beadLabels: []string{"town:gt11", "rig:gastown", "agent:slack", "config:claude-hooks"},
			target:     []string{"scope:global", "town:gt11", "rig:gastown", "agent:slack"},
			wantScore:  4,
			wantApply:  true,
		},
		{
			name:       "wrong rig - not applicable",
			beadLabels: []string{"town:gt11", "rig:otherrig", "config:claude-hooks"},
			target:     []string{"scope:global", "town:gt11", "rig:gastown", "role:crew"},
			wantScore:  0,
			wantApply:  false,
		},
		{
			name:       "wrong town - not applicable",
			beadLabels: []string{"town:gt12", "rig:gastown", "config:claude-hooks"},
			target:     []string{"scope:global", "town:gt11", "rig:gastown", "role:crew"},
			wantScore:  0,
			wantApply:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, applicable := scoreConfigBead(tt.beadLabels, tt.target)
			if applicable != tt.wantApply {
				t.Errorf("scoreConfigBead() applicable = %v, want %v", applicable, tt.wantApply)
			}
			if applicable && score != tt.wantScore {
				t.Errorf("scoreConfigBead() score = %d, want %d", score, tt.wantScore)
			}
		})
	}
}

func TestDeepMergeConfig(t *testing.T) {
	t.Run("override top-level keys", func(t *testing.T) {
		target := map[string]interface{}{
			"editorMode": "normal",
			"theme":      "light",
		}
		source := map[string]interface{}{
			"editorMode": "vim",
		}
		deepMergeConfig(target, source)

		if target["editorMode"] != "vim" {
			t.Errorf("Expected editorMode=vim, got %v", target["editorMode"])
		}
		if target["theme"] != "light" {
			t.Errorf("Expected theme=light preserved, got %v", target["theme"])
		}
	})

	t.Run("null suppresses", func(t *testing.T) {
		target := map[string]interface{}{
			"debugMode": true,
		}
		source := map[string]interface{}{
			"debugMode": nil,
		}
		deepMergeConfig(target, source)

		if target["debugMode"] != nil {
			t.Errorf("Expected debugMode=nil (suppressed), got %v", target["debugMode"])
		}
	})

	t.Run("hooks append", func(t *testing.T) {
		target := map[string]interface{}{
			"hooks": map[string]interface{}{
				"PreCompact": []interface{}{"hookA"},
			},
		}
		source := map[string]interface{}{
			"hooks": map[string]interface{}{
				"PreCompact": []interface{}{"hookB"},
			},
		}
		deepMergeConfig(target, source)

		hooks := target["hooks"].(map[string]interface{})
		preCompact := hooks["PreCompact"].([]interface{})
		if len(preCompact) != 2 {
			t.Errorf("Expected 2 hooks, got %d", len(preCompact))
		}
		if preCompact[0] != "hookA" || preCompact[1] != "hookB" {
			t.Errorf("Expected [hookA, hookB], got %v", preCompact)
		}
	})

	t.Run("hooks null suppresses specific hook", func(t *testing.T) {
		target := map[string]interface{}{
			"hooks": map[string]interface{}{
				"PostToolUse": []interface{}{"hookA"},
				"PreCompact":  []interface{}{"hookB"},
			},
		}
		source := map[string]interface{}{
			"hooks": map[string]interface{}{
				"PostToolUse": nil,
			},
		}
		deepMergeConfig(target, source)

		hooks := target["hooks"].(map[string]interface{})
		if hooks["PostToolUse"] != nil {
			t.Errorf("Expected PostToolUse=nil (suppressed), got %v", hooks["PostToolUse"])
		}
		if hooks["PreCompact"] == nil {
			t.Error("Expected PreCompact preserved, got nil")
		}
	})

	t.Run("hooks new hook type added", func(t *testing.T) {
		target := map[string]interface{}{
			"hooks": map[string]interface{}{
				"PreCompact": []interface{}{"hookA"},
			},
		}
		source := map[string]interface{}{
			"hooks": map[string]interface{}{
				"SessionStart": []interface{}{"hookC"},
			},
		}
		deepMergeConfig(target, source)

		hooks := target["hooks"].(map[string]interface{})
		sessionStart := hooks["SessionStart"].([]interface{})
		if len(sessionStart) != 1 || sessionStart[0] != "hookC" {
			t.Errorf("Expected SessionStart=[hookC], got %v", sessionStart)
		}
		preCompact := hooks["PreCompact"].([]interface{})
		if len(preCompact) != 1 {
			t.Errorf("Expected PreCompact=[hookA] preserved, got %v", preCompact)
		}
	})

	t.Run("no existing hooks", func(t *testing.T) {
		target := map[string]interface{}{
			"editorMode": "normal",
		}
		source := map[string]interface{}{
			"hooks": map[string]interface{}{
				"PreCompact": []interface{}{"hookA"},
			},
		}
		deepMergeConfig(target, source)

		hooks, ok := target["hooks"].(map[string]interface{})
		if !ok {
			t.Fatal("Expected hooks to be added")
		}
		if hooks["PreCompact"] == nil {
			t.Error("Expected PreCompact hook to be set")
		}
	})
}

func TestExtractScopeFromLabels(t *testing.T) {
	tests := []struct {
		labels   []string
		expected string
	}{
		{[]string{"scope:global", "config:hooks"}, "scope:global"},
		{[]string{"town:gt11", "rig:gastown", "config:mcp"}, "town:gt11, rig:gastown"},
		{[]string{"config:hooks"}, ""},
		{nil, ""},
	}

	for _, tt := range tests {
		result := extractScopeFromLabels(tt.labels)
		if result != tt.expected {
			t.Errorf("extractScopeFromLabels(%v) = %q, want %q", tt.labels, result, tt.expected)
		}
	}
}

func TestExtractCategoryFromLabels(t *testing.T) {
	tests := []struct {
		labels   []string
		expected string
	}{
		{[]string{"scope:global", "config:claude-hooks"}, "claude-hooks"},
		{[]string{"config:mcp", "town:gt11"}, "mcp"},
		{[]string{"scope:global"}, ""},
		{nil, ""},
	}

	for _, tt := range tests {
		result := extractCategoryFromLabels(tt.labels)
		if result != tt.expected {
			t.Errorf("extractCategoryFromLabels(%v) = %q, want %q", tt.labels, result, tt.expected)
		}
	}
}

func TestParseScopeLabels(t *testing.T) {
	tests := []struct {
		scope    string
		expected []string
	}{
		{"global", []string{"scope:global"}},
		{"", nil},
		{"town:gt11,rig:gastown", []string{"town:gt11", "rig:gastown"}},
		{"town:gt11, rig:gastown, role:crew", []string{"town:gt11", "rig:gastown", "role:crew"}},
	}

	for _, tt := range tests {
		result := parseScopeLabels(tt.scope)
		if len(result) != len(tt.expected) {
			t.Errorf("parseScopeLabels(%q) len = %d, want %d", tt.scope, len(result), len(tt.expected))
			continue
		}
		for i, v := range result {
			if v != tt.expected[i] {
				t.Errorf("parseScopeLabels(%q)[%d] = %q, want %q", tt.scope, i, v, tt.expected[i])
			}
		}
	}
}

func TestConfigBeadsSetAndList(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStoreWithPrefix(t, testDB, "hq")
	defer s.Close()

	ctx := context.Background()

	// Configure custom types for config type (bd-find4)
	if err := s.SetConfig(ctx, "types.custom", "config"); err != nil {
		t.Fatalf("Failed to set custom types: %v", err)
	}

	// Create a config bead directly via store
	metadata := json.RawMessage(`{"editorMode":"normal","hooks":{"PreCompact":[{"command":"gt prime --hook"}]}}`)
	issue := &types.Issue{
		ID:        "hq-cfg-hooks-base",
		Title:     "Claude Hooks: base",
		IssueType: "config",
		Status:    types.StatusOpen,
		Rig:       "*",
		Metadata:  metadata,
	}
	if err := s.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("Failed to create config bead: %v", err)
	}
	if err := s.AddLabel(ctx, "hq-cfg-hooks-base", "config:claude-hooks", "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}
	if err := s.AddLabel(ctx, "hq-cfg-hooks-base", "scope:global", "test"); err != nil {
		t.Fatalf("Failed to add label: %v", err)
	}

	// Verify we can find it
	issueType := types.IssueType("config")
	filter := types.IssueFilter{
		IssueType: &issueType,
		Labels:    []string{"config:claude-hooks"},
		Limit:     100,
	}
	results, err := s.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 config bead, got %d", len(results))
	}
	if results[0].ID != "hq-cfg-hooks-base" {
		t.Errorf("Expected ID hq-cfg-hooks-base, got %s", results[0].ID)
	}

	// Verify metadata
	retrieved, err := s.GetIssue(ctx, "hq-cfg-hooks-base")
	if err != nil {
		t.Fatalf("GetIssue failed: %v", err)
	}
	if retrieved.Metadata == nil {
		t.Fatal("Expected metadata to be set")
	}
	var meta map[string]interface{}
	if err := json.Unmarshal(retrieved.Metadata, &meta); err != nil {
		t.Fatalf("Failed to parse metadata: %v", err)
	}
	if meta["editorMode"] != "normal" {
		t.Errorf("Expected editorMode=normal, got %v", meta["editorMode"])
	}
}

func TestConfigBeadsMergeIntegration(t *testing.T) {
	tmpDir := t.TempDir()
	testDB := filepath.Join(tmpDir, ".beads", "beads.db")
	s := newTestStoreWithPrefix(t, testDB, "hq")
	defer s.Close()

	ctx := context.Background()

	if err := s.SetConfig(ctx, "types.custom", "config"); err != nil {
		t.Fatalf("Failed to set custom types: %v", err)
	}

	// Create base global config
	baseMetadata := json.RawMessage(`{"editorMode":"normal","hooks":{"PreCompact":[{"command":"gt prime --hook"}]}}`)
	base := &types.Issue{
		ID:        "hq-cfg-hooks-base",
		Title:     "Claude Hooks: base",
		IssueType: "config",
		Status:    types.StatusOpen,
		Rig:       "*",
		Metadata:  baseMetadata,
	}
	if err := s.CreateIssue(ctx, base, "test"); err != nil {
		t.Fatalf("Failed to create base config: %v", err)
	}
	_ = s.AddLabel(ctx, base.ID, "config:claude-hooks", "test")
	_ = s.AddLabel(ctx, base.ID, "scope:global", "test")

	// Create crew override
	crewMetadata := json.RawMessage(`{"editorMode":"vim","hooks":{"SessionStart":[{"command":"gt prime && gt nudge"}]}}`)
	crew := &types.Issue{
		ID:        "hq-cfg-hooks-crew",
		Title:     "Claude Hooks: crew",
		IssueType: "config",
		Status:    types.StatusOpen,
		Rig:       "*",
		Metadata:  crewMetadata,
	}
	if err := s.CreateIssue(ctx, crew, "test"); err != nil {
		t.Fatalf("Failed to create crew config: %v", err)
	}
	_ = s.AddLabel(ctx, crew.ID, "config:claude-hooks", "test")
	_ = s.AddLabel(ctx, crew.ID, "scope:global", "test")
	_ = s.AddLabel(ctx, crew.ID, "role:crew", "test")

	// Create rig-specific override with null suppress
	rigMetadata := json.RawMessage(`{"hooks":{"PreCompact":null,"PreToolUse":[{"command":"gt tap guard"}]}}`)
	rigCfg := &types.Issue{
		ID:        "hq-cfg-hooks-gastown-crew",
		Title:     "Claude Hooks: gastown crew",
		IssueType: "config",
		Status:    types.StatusOpen,
		Rig:       "gt11/gastown",
		Metadata:  rigMetadata,
	}
	if err := s.CreateIssue(ctx, rigCfg, "test"); err != nil {
		t.Fatalf("Failed to create rig config: %v", err)
	}
	_ = s.AddLabel(ctx, rigCfg.ID, "config:claude-hooks", "test")
	_ = s.AddLabel(ctx, rigCfg.ID, "town:gt11", "test")
	_ = s.AddLabel(ctx, rigCfg.ID, "rig:gastown", "test")
	_ = s.AddLabel(ctx, rigCfg.ID, "role:crew", "test")

	// Query all config beads for claude-hooks
	issueType := types.IssueType("config")
	filter := types.IssueFilter{
		IssueType: &issueType,
		Labels:    []string{"config:claude-hooks"},
		Limit:     100,
	}
	issues, err := s.SearchIssues(ctx, "", filter)
	if err != nil {
		t.Fatalf("SearchIssues failed: %v", err)
	}
	if len(issues) != 3 {
		t.Fatalf("Expected 3 config beads, got %d", len(issues))
	}

	// Populate labels
	for _, issue := range issues {
		issue.Labels, _ = s.GetLabels(ctx, issue.ID)
	}

	// Score and filter for gastown crew
	targetScope := []string{"scope:global", "town:gt11", "rig:gastown", "role:crew"}

	type scoredIssue struct {
		issue *types.Issue
		score int
	}
	var scored []scoredIssue
	for _, issue := range issues {
		score, applicable := scoreConfigBead(issue.Labels, targetScope)
		if applicable {
			scored = append(scored, scoredIssue{issue: issue, score: score})
		}
	}

	if len(scored) != 3 {
		t.Fatalf("Expected 3 applicable beads, got %d", len(scored))
	}

	// Sort by specificity
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score < scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	// Verify scoring order
	if scored[0].score != 0 {
		t.Errorf("Expected base score 0, got %d", scored[0].score)
	}
	if scored[1].score != 1 {
		t.Errorf("Expected crew score 1, got %d", scored[1].score)
	}
	if scored[2].score != 3 {
		t.Errorf("Expected rig+crew score 3, got %d", scored[2].score)
	}

	// Deep merge
	merged := make(map[string]interface{})
	for _, si := range scored {
		var data map[string]interface{}
		if err := json.Unmarshal(si.issue.Metadata, &data); err != nil {
			t.Fatalf("Failed to parse metadata for %s: %v", si.issue.ID, err)
		}
		deepMergeConfig(merged, data)
	}

	// Verify merge results
	// editorMode should be "vim" (crew override wins over base "normal")
	if merged["editorMode"] != "vim" {
		t.Errorf("Expected editorMode=vim (crew override), got %v", merged["editorMode"])
	}

	// hooks.PreCompact should be nil (rig config suppresses it)
	hooks, ok := merged["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected hooks to be a map")
	}
	if hooks["PreCompact"] != nil {
		t.Errorf("Expected PreCompact=nil (suppressed by rig), got %v", hooks["PreCompact"])
	}

	// hooks.SessionStart should have 1 entry (from crew)
	sessionStart, ok := hooks["SessionStart"].([]interface{})
	if !ok {
		t.Fatal("Expected SessionStart to be an array")
	}
	if len(sessionStart) != 1 {
		t.Errorf("Expected 1 SessionStart hook, got %d", len(sessionStart))
	}

	// hooks.PreToolUse should have 1 entry (from rig)
	preToolUse, ok := hooks["PreToolUse"].([]interface{})
	if !ok {
		t.Fatal("Expected PreToolUse to be an array")
	}
	if len(preToolUse) != 1 {
		t.Errorf("Expected 1 PreToolUse hook, got %d", len(preToolUse))
	}
}

func TestTruncateConfigMeta(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"this is a long string", 10, "this is..."},
		{"exact10chr", 10, "exact10chr"},
		{"", 5, ""},
	}

	for _, tt := range tests {
		result := truncateConfigMeta(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncateConfigMeta(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

// setupConfigTestDB creates a test database with custom types configured.
func setupConfigTestDB(t *testing.T) (*sqlite.SQLiteStorage, func()) {
	tmpDir, err := os.MkdirTemp("", "bd-test-config-beads-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}

	testDB := filepath.Join(tmpDir, "test.db")
	store, err := sqlite.New(context.Background(), testDB)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create test database: %v", err)
	}

	ctx := context.Background()
	if err := store.SetConfig(ctx, "issue_prefix", "hq"); err != nil {
		store.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to set issue_prefix: %v", err)
	}
	if err := store.SetConfig(ctx, "types.custom", "config"); err != nil {
		store.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to set custom types: %v", err)
	}

	cleanup := func() {
		store.Close()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}
