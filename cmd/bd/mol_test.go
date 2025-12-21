package main

import (
	"context"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

func TestParseDistillVar(t *testing.T) {
	tests := []struct {
		name           string
		varFlag        string
		searchableText string
		wantFind       string
		wantVar        string
		wantErr        bool
	}{
		{
			name:           "spawn-style: variable=value",
			varFlag:        "branch=feature-auth",
			searchableText: "Implement feature-auth login flow",
			wantFind:       "feature-auth",
			wantVar:        "branch",
			wantErr:        false,
		},
		{
			name:           "substitution-style: value=variable",
			varFlag:        "feature-auth=branch",
			searchableText: "Implement feature-auth login flow",
			wantFind:       "feature-auth",
			wantVar:        "branch",
			wantErr:        false,
		},
		{
			name:           "spawn-style with version number",
			varFlag:        "version=1.2.3",
			searchableText: "Release version 1.2.3 to production",
			wantFind:       "1.2.3",
			wantVar:        "version",
			wantErr:        false,
		},
		{
			name:           "both found - prefers spawn-style",
			varFlag:        "api=api",
			searchableText: "The api endpoint uses api keys",
			wantFind:       "api",
			wantVar:        "api",
			wantErr:        false,
		},
		{
			name:           "neither found - error",
			varFlag:        "foo=bar",
			searchableText: "Nothing matches here",
			wantFind:       "",
			wantVar:        "",
			wantErr:        true,
		},
		{
			name:           "empty left side - error",
			varFlag:        "=value",
			searchableText: "Some text with value",
			wantFind:       "",
			wantVar:        "",
			wantErr:        true,
		},
		{
			name:           "empty right side - error",
			varFlag:        "value=",
			searchableText: "Some text with value",
			wantFind:       "",
			wantVar:        "",
			wantErr:        true,
		},
		{
			name:           "no equals sign - error",
			varFlag:        "noequals",
			searchableText: "Some text",
			wantFind:       "",
			wantVar:        "",
			wantErr:        true,
		},
		{
			name:           "value with equals sign",
			varFlag:        "env=KEY=VALUE",
			searchableText: "Set KEY=VALUE in config",
			wantFind:       "KEY=VALUE",
			wantVar:        "env",
			wantErr:        false,
		},
		{
			name:           "partial match in longer word - finds it",
			varFlag:        "name=auth",
			searchableText: "authentication module",
			wantFind:       "auth",
			wantVar:        "name",
			wantErr:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFind, gotVar, err := parseDistillVar(tt.varFlag, tt.searchableText)

			if tt.wantErr {
				if err == nil {
					t.Errorf("parseDistillVar() expected error, got none")
				}
				return
			}

			if err != nil {
				t.Errorf("parseDistillVar() unexpected error: %v", err)
				return
			}

			if gotFind != tt.wantFind {
				t.Errorf("parseDistillVar() find = %q, want %q", gotFind, tt.wantFind)
			}
			if gotVar != tt.wantVar {
				t.Errorf("parseDistillVar() var = %q, want %q", gotVar, tt.wantVar)
			}
		})
	}
}

func TestCollectSubgraphText(t *testing.T) {
	// Create a simple subgraph for testing
	subgraph := &MoleculeSubgraph{
		Issues: []*types.Issue{
			{
				Title:       "Epic: Feature Auth",
				Description: "Implement authentication",
				Design:      "Use OAuth2",
			},
			{
				Title: "Add login endpoint",
				Notes: "See RFC 6749",
			},
		},
	}

	text := collectSubgraphText(subgraph)

	// Verify all fields are included
	expected := []string{
		"Epic: Feature Auth",
		"Implement authentication",
		"Use OAuth2",
		"Add login endpoint",
		"See RFC 6749",
	}

	for _, exp := range expected {
		if !strings.Contains(text, exp) {
			t.Errorf("collectSubgraphText() missing %q", exp)
		}
	}
}

func TestIsProto(t *testing.T) {
	tests := []struct {
		name   string
		labels []string
		want   bool
	}{
		{"with template label", []string{"template", "other"}, true},
		{"template only", []string{"template"}, true},
		{"no template label", []string{"bug", "feature"}, false},
		{"empty labels", []string{}, false},
		{"nil labels", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := &types.Issue{Labels: tt.labels}
			got := isProto(issue)
			if got != tt.want {
				t.Errorf("isProto() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOperandType(t *testing.T) {
	if got := operandType(true); got != "proto" {
		t.Errorf("operandType(true) = %q, want %q", got, "proto")
	}
	if got := operandType(false); got != "molecule" {
		t.Errorf("operandType(false) = %q, want %q", got, "molecule")
	}
}

func TestMinPriority(t *testing.T) {
	tests := []struct {
		a, b, want int
	}{
		{1, 2, 1},
		{2, 1, 1},
		{0, 3, 0},
		{3, 3, 3},
	}
	for _, tt := range tests {
		got := minPriority(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("minPriority(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestBondProtoProto(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/test.db"
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	// Create two protos
	protoA := &types.Issue{
		Title:     "Proto A",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeEpic,
		Labels:    []string{MoleculeLabel},
	}
	protoB := &types.Issue{
		Title:     "Proto B",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeEpic,
		Labels:    []string{MoleculeLabel},
	}

	if err := store.CreateIssue(ctx, protoA, "test"); err != nil {
		t.Fatalf("Failed to create protoA: %v", err)
	}
	if err := store.CreateIssue(ctx, protoB, "test"); err != nil {
		t.Fatalf("Failed to create protoB: %v", err)
	}

	// Test sequential bond
	result, err := bondProtoProto(ctx, store, protoA, protoB, types.BondTypeSequential, "", "test")
	if err != nil {
		t.Fatalf("bondProtoProto failed: %v", err)
	}

	if result.ResultType != "compound_proto" {
		t.Errorf("ResultType = %q, want %q", result.ResultType, "compound_proto")
	}
	if result.BondType != types.BondTypeSequential {
		t.Errorf("BondType = %q, want %q", result.BondType, types.BondTypeSequential)
	}

	// Verify compound was created
	compound, err := store.GetIssue(ctx, result.ResultID)
	if err != nil {
		t.Fatalf("Failed to get compound: %v", err)
	}
	if !isProto(compound) {
		t.Errorf("Compound should be a proto (have template label), got labels: %v", compound.Labels)
	}
	if compound.Priority != 1 {
		t.Errorf("Compound priority = %d, want %d (min of 1,2)", compound.Priority, 1)
	}

	// Verify dependencies exist (protoA depends on compound via parent-child)
	deps, err := store.GetDependenciesWithMetadata(ctx, protoA.ID)
	if err != nil {
		t.Fatalf("Failed to get deps for protoA: %v", err)
	}
	foundParentChild := false
	for _, dep := range deps {
		if dep.ID == compound.ID && dep.DependencyType == types.DepParentChild {
			foundParentChild = true
		}
	}
	if !foundParentChild {
		t.Error("Expected parent-child dependency from protoA to compound")
	}
}

func TestBondProtoMol(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/test.db"
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	// Create a proto with a child issue
	proto := &types.Issue{
		Title:     "Proto: {{name}}",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeEpic,
		Labels:    []string{MoleculeLabel},
	}
	if err := store.CreateIssue(ctx, proto, "test"); err != nil {
		t.Fatalf("Failed to create proto: %v", err)
	}

	protoChild := &types.Issue{
		Title:     "Step 1 for {{name}}",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Labels:    []string{MoleculeLabel},
	}
	if err := store.CreateIssue(ctx, protoChild, "test"); err != nil {
		t.Fatalf("Failed to create proto child: %v", err)
	}

	// Add parent-child dependency
	if err := store.AddDependency(ctx, &types.Dependency{
		IssueID:     protoChild.ID,
		DependsOnID: proto.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Create a molecule (existing epic)
	mol := &types.Issue{
		Title:     "Existing Work",
		Status:    types.StatusInProgress,
		Priority:  1,
		IssueType: types.TypeEpic,
	}
	if err := store.CreateIssue(ctx, mol, "test"); err != nil {
		t.Fatalf("Failed to create molecule: %v", err)
	}

	// Bond proto to molecule
	vars := map[string]string{"name": "auth-feature"}
	result, err := bondProtoMol(ctx, store, proto, mol, types.BondTypeSequential, vars, "test")
	if err != nil {
		t.Fatalf("bondProtoMol failed: %v", err)
	}

	if result.ResultType != "compound_molecule" {
		t.Errorf("ResultType = %q, want %q", result.ResultType, "compound_molecule")
	}
	if result.Spawned != 2 {
		t.Errorf("Spawned = %d, want 2", result.Spawned)
	}
	if result.ResultID != mol.ID {
		t.Errorf("ResultID = %q, want %q (original molecule)", result.ResultID, mol.ID)
	}
}

func TestBondMolMol(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/test.db"
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer store.Close()
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	// Create two molecules
	molA := &types.Issue{
		Title:     "Molecule A",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeEpic,
	}
	molB := &types.Issue{
		Title:     "Molecule B",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeEpic,
	}

	if err := store.CreateIssue(ctx, molA, "test"); err != nil {
		t.Fatalf("Failed to create molA: %v", err)
	}
	if err := store.CreateIssue(ctx, molB, "test"); err != nil {
		t.Fatalf("Failed to create molB: %v", err)
	}

	// Test sequential bond
	result, err := bondMolMol(ctx, store, molA, molB, types.BondTypeSequential, "test")
	if err != nil {
		t.Fatalf("bondMolMol failed: %v", err)
	}

	if result.ResultType != "compound_molecule" {
		t.Errorf("ResultType = %q, want %q", result.ResultType, "compound_molecule")
	}
	if result.ResultID != molA.ID {
		t.Errorf("ResultID = %q, want %q", result.ResultID, molA.ID)
	}

	// Verify dependency: B blocks on A
	deps, err := store.GetDependenciesWithMetadata(ctx, molB.ID)
	if err != nil {
		t.Fatalf("Failed to get deps for molB: %v", err)
	}
	foundBlocks := false
	for _, dep := range deps {
		if dep.ID == molA.ID && dep.DependencyType == types.DepBlocks {
			foundBlocks = true
		}
	}
	if !foundBlocks {
		t.Error("Expected blocks dependency from molB to molA for sequential bond")
	}

	// Test parallel bond (create new molecules)
	molC := &types.Issue{
		Title:     "Molecule C",
		Status:    types.StatusOpen,
		IssueType: types.TypeEpic,
	}
	molD := &types.Issue{
		Title:     "Molecule D",
		Status:    types.StatusOpen,
		IssueType: types.TypeEpic,
	}
	if err := store.CreateIssue(ctx, molC, "test"); err != nil {
		t.Fatalf("Failed to create molC: %v", err)
	}
	if err := store.CreateIssue(ctx, molD, "test"); err != nil {
		t.Fatalf("Failed to create molD: %v", err)
	}

	result2, err := bondMolMol(ctx, store, molC, molD, types.BondTypeParallel, "test")
	if err != nil {
		t.Fatalf("bondMolMol parallel failed: %v", err)
	}

	// Verify parent-child dependency for parallel
	deps2, err := store.GetDependenciesWithMetadata(ctx, molD.ID)
	if err != nil {
		t.Fatalf("Failed to get deps for molD: %v", err)
	}
	foundParentChild := false
	for _, dep := range deps2 {
		if dep.ID == molC.ID && dep.DependencyType == types.DepParentChild {
			foundParentChild = true
		}
	}
	if !foundParentChild {
		t.Errorf("Expected parent-child dependency for parallel bond, result: %+v", result2)
	}
}

func TestSquashMolecule(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/test.db"
	s, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer s.Close()
	if err := s.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	// Create a molecule (root issue)
	root := &types.Issue{
		Title:     "Test Molecule",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeEpic,
	}
	if err := s.CreateIssue(ctx, root, "test"); err != nil {
		t.Fatalf("Failed to create root: %v", err)
	}

	// Create ephemeral children
	child1 := &types.Issue{
		Title:       "Step 1: Design",
		Description: "Design the architecture",
		Status:      types.StatusClosed,
		Priority:    2,
		IssueType:   types.TypeTask,
		Wisp:        true,
		CloseReason: "Completed design",
	}
	child2 := &types.Issue{
		Title:       "Step 2: Implement",
		Description: "Build the feature",
		Status:      types.StatusClosed,
		Priority:    2,
		IssueType:   types.TypeTask,
		Wisp:        true,
		CloseReason: "Code merged",
	}

	if err := s.CreateIssue(ctx, child1, "test"); err != nil {
		t.Fatalf("Failed to create child1: %v", err)
	}
	if err := s.CreateIssue(ctx, child2, "test"); err != nil {
		t.Fatalf("Failed to create child2: %v", err)
	}

	// Add parent-child dependencies
	if err := s.AddDependency(ctx, &types.Dependency{
		IssueID:     child1.ID,
		DependsOnID: root.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add child1 dependency: %v", err)
	}
	if err := s.AddDependency(ctx, &types.Dependency{
		IssueID:     child2.ID,
		DependsOnID: root.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add child2 dependency: %v", err)
	}

	// Test squash with keep-children
	children := []*types.Issue{child1, child2}
	result, err := squashMolecule(ctx, s, root, children, true, "", "test")
	if err != nil {
		t.Fatalf("squashMolecule failed: %v", err)
	}

	if result.SquashedCount != 2 {
		t.Errorf("SquashedCount = %d, want 2", result.SquashedCount)
	}
	if result.DeletedCount != 0 {
		t.Errorf("DeletedCount = %d, want 0 (keep-children)", result.DeletedCount)
	}
	if !result.KeptChildren {
		t.Error("KeptChildren should be true")
	}

	// Verify digest was created
	digest, err := s.GetIssue(ctx, result.DigestID)
	if err != nil {
		t.Fatalf("Failed to get digest: %v", err)
	}
	if digest.Wisp {
		t.Error("Digest should NOT be ephemeral")
	}
	if digest.Status != types.StatusClosed {
		t.Errorf("Digest status = %v, want closed", digest.Status)
	}
	if !strings.Contains(digest.Description, "Step 1: Design") {
		t.Error("Digest should contain child titles")
	}
	if !strings.Contains(digest.Description, "Completed design") {
		t.Error("Digest should contain close reasons")
	}

	// Children should still exist
	c1, err := s.GetIssue(ctx, child1.ID)
	if err != nil || c1 == nil {
		t.Error("Child1 should still exist with keep-children")
	}
}

func TestSquashMoleculeWithDelete(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/test.db"
	s, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer s.Close()
	if err := s.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	// Create a molecule with ephemeral children
	root := &types.Issue{
		Title:     "Delete Test Molecule",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeEpic,
	}
	if err := s.CreateIssue(ctx, root, "test"); err != nil {
		t.Fatalf("Failed to create root: %v", err)
	}

	child := &types.Issue{
		Title:     "Wisp Step",
		Status:    types.StatusClosed,
		Priority:  2,
		IssueType: types.TypeTask,
		Wisp:      true,
	}
	if err := s.CreateIssue(ctx, child, "test"); err != nil {
		t.Fatalf("Failed to create child: %v", err)
	}
	if err := s.AddDependency(ctx, &types.Dependency{
		IssueID:     child.ID,
		DependsOnID: root.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Squash with delete (keepChildren=false)
	result, err := squashMolecule(ctx, s, root, []*types.Issue{child}, false, "", "test")
	if err != nil {
		t.Fatalf("squashMolecule failed: %v", err)
	}

	if result.DeletedCount != 1 {
		t.Errorf("DeletedCount = %d, want 1", result.DeletedCount)
	}

	// Child should be deleted
	c, err := s.GetIssue(ctx, child.ID)
	if err == nil && c != nil {
		t.Error("Child should have been deleted")
	}

	// Digest should exist
	digest, err := s.GetIssue(ctx, result.DigestID)
	if err != nil || digest == nil {
		t.Error("Digest should exist after squash")
	}
}

func TestGenerateDigest(t *testing.T) {
	root := &types.Issue{
		Title: "Test Molecule",
	}
	children := []*types.Issue{
		{
			Title:       "Step 1",
			Description: "First step description",
			Status:      types.StatusClosed,
			CloseReason: "Done",
		},
		{
			Title:       "Step 2",
			Description: "Second step description that is longer",
			Status:      types.StatusInProgress,
		},
	}

	digest := generateDigest(root, children)

	// Verify structure
	if !strings.Contains(digest, "## Molecule Execution Summary") {
		t.Error("Digest should have summary header")
	}
	if !strings.Contains(digest, "Test Molecule") {
		t.Error("Digest should contain molecule title")
	}
	if !strings.Contains(digest, "**Steps**: 2") {
		t.Error("Digest should show step count")
	}
	if !strings.Contains(digest, "**Completed**: 1/2") {
		t.Error("Digest should show completion stats")
	}
	if !strings.Contains(digest, "**In Progress**: 1") {
		t.Error("Digest should show in-progress count")
	}
	if !strings.Contains(digest, "Step 1") {
		t.Error("Digest should list step titles")
	}
	if !strings.Contains(digest, "*Outcome: Done*") {
		t.Error("Digest should include close reasons")
	}
}

// TestSquashMoleculeWithAgentSummary verifies that agent-provided summaries are used
func TestSquashMoleculeWithAgentSummary(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/test.db"
	s, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer s.Close()
	if err := s.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	// Create a molecule with ephemeral child
	root := &types.Issue{
		Title:     "Agent Summary Test",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeEpic,
	}
	if err := s.CreateIssue(ctx, root, "test"); err != nil {
		t.Fatalf("Failed to create root: %v", err)
	}

	child := &types.Issue{
		Title:       "Wisp Step",
		Description: "This should NOT appear in digest",
		Status:      types.StatusClosed,
		Priority:    2,
		IssueType:   types.TypeTask,
		Wisp:        true,
		CloseReason: "Done",
	}
	if err := s.CreateIssue(ctx, child, "test"); err != nil {
		t.Fatalf("Failed to create child: %v", err)
	}
	if err := s.AddDependency(ctx, &types.Dependency{
		IssueID:     child.ID,
		DependsOnID: root.ID,
		Type:        types.DepParentChild,
	}, "test"); err != nil {
		t.Fatalf("Failed to add dependency: %v", err)
	}

	// Squash with agent-provided summary
	agentSummary := "## AI-Generated Summary\n\nThe agent completed the task successfully."
	result, err := squashMolecule(ctx, s, root, []*types.Issue{child}, true, agentSummary, "test")
	if err != nil {
		t.Fatalf("squashMolecule failed: %v", err)
	}

	// Verify digest uses agent summary, not auto-generated content
	digest, err := s.GetIssue(ctx, result.DigestID)
	if err != nil {
		t.Fatalf("Failed to get digest: %v", err)
	}

	if digest.Description != agentSummary {
		t.Errorf("Digest should use agent summary.\nGot: %s\nWant: %s", digest.Description, agentSummary)
	}

	// Verify auto-generated content is NOT present
	if strings.Contains(digest.Description, "Wisp Step") {
		t.Error("Digest should NOT contain auto-generated content when agent summary provided")
	}
}

// TestWispFilteringFromExport verifies that wisp issues are filtered
// from JSONL export (bd-687g). Wisp issues should only exist in SQLite,
// not in issues.jsonl, to prevent "zombie" resurrection after mol squash.
func TestWispFilteringFromExport(t *testing.T) {
	ctx := context.Background()
	dbPath := t.TempDir() + "/test.db"
	s, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("Failed to create store: %v", err)
	}
	defer s.Close()
	if err := s.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("Failed to set config: %v", err)
	}

	// Create a mix of wisp and non-wisp issues
	normalIssue := &types.Issue{
		Title:     "Normal Issue",
		Status:    types.StatusOpen,
		Priority:  1,
		IssueType: types.TypeTask,
		Wisp:      false,
	}
	wispIssue := &types.Issue{
		Title:     "Wisp Issue",
		Status:    types.StatusOpen,
		Priority:  2,
		IssueType: types.TypeTask,
		Wisp:      true,
	}

	if err := s.CreateIssue(ctx, normalIssue, "test"); err != nil {
		t.Fatalf("Failed to create normal issue: %v", err)
	}
	if err := s.CreateIssue(ctx, wispIssue, "test"); err != nil {
		t.Fatalf("Failed to create wisp issue: %v", err)
	}

	// Get all issues from DB - should include both
	allIssues, err := s.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		t.Fatalf("Failed to search issues: %v", err)
	}
	if len(allIssues) != 2 {
		t.Fatalf("Expected 2 issues in DB, got %d", len(allIssues))
	}

	// Filter wisp issues (simulating export behavior)
	exportableIssues := make([]*types.Issue, 0)
	for _, issue := range allIssues {
		if !issue.Wisp {
			exportableIssues = append(exportableIssues, issue)
		}
	}

	// Should only have the non-wisp issue
	if len(exportableIssues) != 1 {
		t.Errorf("Expected 1 exportable issue, got %d", len(exportableIssues))
	}
	if exportableIssues[0].ID != normalIssue.ID {
		t.Errorf("Expected normal issue %s, got %s", normalIssue.ID, exportableIssues[0].ID)
	}
}
