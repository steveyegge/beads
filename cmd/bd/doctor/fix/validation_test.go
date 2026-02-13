package fix

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage/dolt"
	"github.com/steveyegge/beads/internal/types"
)

// TestFixFunctions_RequireBeadsDir verifies all fix functions properly validate
// that a .beads directory exists before attempting fixes.
// This replaces 10+ individual "missing .beads directory" subtests.
func TestFixFunctions_RequireBeadsDir(t *testing.T) {
	funcs := []struct {
		name string
		fn   func(string) error
	}{
		{"GitHooks", GitHooks},
		{"MergeDriver", MergeDriver},
		{"Daemon", Daemon},
		{"DBJSONLSync", DBJSONLSync},
		{"DatabaseVersion", DatabaseVersion},
		{"SchemaCompatibility", SchemaCompatibility},
		{"SyncBranchConfig", SyncBranchConfig},
		{"SyncBranchHealth", func(dir string) error { return SyncBranchHealth(dir, "beads-sync") }},
		{"UntrackedJSONL", UntrackedJSONL},
		{"MigrateTombstones", MigrateTombstones},
		{"ChildParentDependencies", func(dir string) error { return ChildParentDependencies(dir, false) }},
		{"OrphanedDependencies", func(dir string) error { return OrphanedDependencies(dir, false) }},
	}

	for _, tc := range funcs {
		t.Run(tc.name, func(t *testing.T) {
			// Use a temp directory without .beads
			dir := t.TempDir()
			err := tc.fn(dir)
			if err == nil {
				t.Errorf("%s should return error for missing .beads directory", tc.name)
			}
		})
	}
}

// skipIfNoDolt skips the test if dolt binary is not available.
func skipIfNoDolt(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dolt"); err != nil {
		t.Skip("dolt binary not in PATH, skipping test")
	}
}

// setupDoltStore creates a Dolt store in dir/.beads/dolt with the given issues
// and dependencies. Returns the store's underlying *sql.DB for verification queries
// and a cleanup function. The store is closed after setup so ChildParentDependencies
// can open its own connection.
func setupDoltStore(t *testing.T, dir string, issues []*types.Issue, deps []*types.Dependency) {
	t.Helper()

	beadsDir := filepath.Join(dir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write metadata.json so the factory knows to use Dolt
	metadataPath := filepath.Join(beadsDir, "metadata.json")
	if err := os.WriteFile(metadataPath, []byte(`{"backend":"dolt"}`), 0644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	doltPath := filepath.Join(beadsDir, "dolt")

	// Disable server mode for tests
	t.Setenv("BEADS_DOLT_SERVER_MODE", "0")

	cfg := &dolt.Config{
		Path:              doltPath,
		CommitterName:     "test",
		CommitterEmail:    "test@example.com",
		Database:          "beads",
		SkipDirtyTracking: true,
	}

	store, err := dolt.New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to create Dolt store: %v", err)
	}

	// Set prefix
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		store.Close()
		t.Fatalf("failed to set prefix: %v", err)
	}

	// Create issues
	for _, issue := range issues {
		if err := store.CreateIssue(ctx, issue, "test"); err != nil {
			store.Close()
			t.Fatalf("failed to create issue %s: %v", issue.ID, err)
		}
	}

	// Add dependencies
	for _, dep := range deps {
		if err := store.AddDependency(ctx, dep, "test"); err != nil {
			store.Close()
			t.Fatalf("failed to add dependency %s->%s: %v", dep.IssueID, dep.DependsOnID, err)
		}
	}

	store.Close()
}

// reopenDB opens a Dolt store in beadsDir and returns the underlying *sql.DB.
func reopenDB(t *testing.T, dir string) *dolt.DoltStore {
	t.Helper()
	beadsDir := filepath.Join(dir, ".beads")
	doltPath := filepath.Join(beadsDir, "dolt")

	ctx := context.Background()
	cfg := &dolt.Config{
		Path:              doltPath,
		CommitterName:     "test",
		CommitterEmail:    "test@example.com",
		Database:          "beads",
		SkipDirtyTracking: true,
	}

	store, err := dolt.New(ctx, cfg)
	if err != nil {
		t.Fatalf("failed to reopen Dolt store: %v", err)
	}
	return store
}

func TestChildParentDependencies_NoBadDeps(t *testing.T) {
	skipIfNoDolt(t)

	dir := t.TempDir()

	// Create issues: bd-abc, bd-abc.1, bd-xyz
	// Dependency: bd-abc.1 blocks bd-xyz (NOT a child->parent dep)
	issues := []*types.Issue{
		{ID: "bd-abc", Title: "Parent", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "bd-abc.1", Title: "Child", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "bd-xyz", Title: "Other", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
	}
	deps := []*types.Dependency{
		{IssueID: "bd-abc.1", DependsOnID: "bd-xyz", Type: types.DepBlocks},
	}
	setupDoltStore(t, dir, issues, deps)

	// Run fix - should find no bad deps
	err := ChildParentDependencies(dir, false)
	if err != nil {
		t.Errorf("ChildParentDependencies failed: %v", err)
	}

	// Verify the good dependency still exists
	store := reopenDB(t, dir)
	defer store.Close()
	ctx := context.Background()
	depRecords, err := store.GetDependencyRecords(ctx, "bd-abc.1")
	if err != nil {
		t.Fatalf("GetDependencyRecords failed: %v", err)
	}
	if len(depRecords) != 1 {
		t.Errorf("Expected 1 dependency, got %d", len(depRecords))
	}
}

func TestChildParentDependencies_FixesBadDeps(t *testing.T) {
	skipIfNoDolt(t)

	dir := t.TempDir()

	// Create issues with child->parent blocking dependencies (the anti-pattern)
	issues := []*types.Issue{
		{ID: "bd-abc", Title: "Parent", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "bd-abc.1", Title: "Child 1", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "bd-abc.1.2", Title: "Grandchild", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
	}
	deps := []*types.Dependency{
		{IssueID: "bd-abc.1", DependsOnID: "bd-abc", Type: types.DepBlocks},
		{IssueID: "bd-abc.1.2", DependsOnID: "bd-abc", Type: types.DepBlocks},
		{IssueID: "bd-abc.1.2", DependsOnID: "bd-abc.1", Type: types.DepBlocks},
	}
	setupDoltStore(t, dir, issues, deps)

	// Run fix
	err := ChildParentDependencies(dir, false)
	if err != nil {
		t.Errorf("ChildParentDependencies failed: %v", err)
	}

	// Verify all bad dependencies were removed
	store := reopenDB(t, dir)
	defer store.Close()
	ctx := context.Background()

	// Check each issue's deps - all should be gone
	for _, id := range []string{"bd-abc.1", "bd-abc.1.2"} {
		deps, err := store.GetDependencyRecords(ctx, id)
		if err != nil {
			t.Fatalf("GetDependencyRecords(%s) failed: %v", id, err)
		}
		if len(deps) != 0 {
			t.Errorf("Expected 0 dependencies for %s after fix, got %d", id, len(deps))
		}
	}
}

// TestChildParentDependencies_PreservesParentChildType verifies that legitimate
// parent-child type dependencies are NOT removed (only blocking types are removed).
// Regression test for GitHub issue #750.
//
// Note: Dolt's dependencies table has PRIMARY KEY (issue_id, depends_on_id) without
// including type, so a given (issue_id, depends_on_id) pair can only have ONE type.
// We test with separate issue pairs: one with 'blocks' (should be removed) and
// another with 'parent-child' (should be preserved).
func TestChildParentDependencies_PreservesParentChildType(t *testing.T) {
	skipIfNoDolt(t)

	dir := t.TempDir()

	// bd-abc.1 -> bd-abc as 'blocks' (anti-pattern, should be removed)
	// bd-abc.2 -> bd-abc as 'parent-child' (legitimate, should be preserved)
	issues := []*types.Issue{
		{ID: "bd-abc", Title: "Parent", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "bd-abc.1", Title: "Child 1", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
		{ID: "bd-abc.2", Title: "Child 2", Status: types.StatusOpen, Priority: 2, IssueType: types.TypeTask},
	}
	deps := []*types.Dependency{
		{IssueID: "bd-abc.1", DependsOnID: "bd-abc", Type: types.DepBlocks},
		{IssueID: "bd-abc.2", DependsOnID: "bd-abc", Type: types.DepParentChild},
	}
	setupDoltStore(t, dir, issues, deps)

	// Run fix
	err := ChildParentDependencies(dir, false)
	if err != nil {
		t.Fatalf("ChildParentDependencies failed: %v", err)
	}

	// Verify only 'blocks' type was removed, 'parent-child' preserved
	store := reopenDB(t, dir)
	defer store.Close()
	ctx := context.Background()

	// bd-abc.1 should have no deps left (blocks was removed)
	deps1, err := store.GetDependencyRecords(ctx, "bd-abc.1")
	if err != nil {
		t.Fatalf("GetDependencyRecords(bd-abc.1) failed: %v", err)
	}
	if len(deps1) != 0 {
		t.Errorf("Expected 0 dependencies for bd-abc.1 after fix (blocks removed), got %d", len(deps1))
	}

	// bd-abc.2 should still have its parent-child dep (not removed by fix)
	deps2, err := store.GetDependencyRecords(ctx, "bd-abc.2")
	if err != nil {
		t.Fatalf("GetDependencyRecords(bd-abc.2) failed: %v", err)
	}
	if len(deps2) != 1 || deps2[0].Type != types.DepParentChild {
		t.Errorf("Expected 1 'parent-child' dependency for bd-abc.2, got %v", deps2)
	}
}
