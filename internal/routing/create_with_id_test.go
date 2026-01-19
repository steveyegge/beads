package routing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestCreateWithExplicitIDShouldRouteToCorrectDatabase demonstrates the bug where
// bd create --id=pq-xxx fails with "prefix mismatch: database uses 'hq' but you specified 'pq'"
// even though routes.jsonl correctly maps pq- prefix to the pgqueue directory.
//
// Expected behavior: When creating an issue with an explicit ID, the system should:
// 1. Extract the prefix from the ID (e.g., "pq-" from "pq-xxx")
// 2. Look up the prefix in routes.jsonl
// 3. Route to the correct beads directory for that prefix
// 4. Create the issue in that database
//
// Actual behavior: The system validates the ID against the current database's prefix,
// causing a "prefix mismatch" error.
//
// This test simulates the Gas Town scenario where:
// - Root database has prefix "hq-"
// - pgqueue rig has prefix "pq-"
// - routes.jsonl maps: {"prefix":"pq-","path":"pgqueue"}
// - User runs: bd create --id=pq-pgqueue-crew-pgq_crew --title="Test"
// - Expected: Issue created in pgqueue/.beads/beads.db
// - Actual: Error "prefix mismatch: database uses 'hq' but you specified 'pq'"
func TestCreateWithExplicitIDShouldRouteToCorrectDatabase(t *testing.T) {
	// Create temporary directory structure:
	// tmpDir/
	//   mayor/
	//     town.json    <- town root marker
	//   .beads/
	//     beads.db     <- root database with "hq-" prefix
	//     routes.jsonl <- {"prefix":"pq-","path":"pgqueue"}
	//   pgqueue/
	//     .beads/
	//       beads.db   <- pgqueue database with "pq-" prefix
	tmpDir, err := os.MkdirTemp("", "routing-create-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Resolve symlinks in tmpDir (macOS /var -> /private/var)
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create mayor/town.json to mark town root
	mayorDir := filepath.Join(tmpDir, "mayor")
	if err := os.MkdirAll(mayorDir, 0750); err != nil {
		t.Fatal(err)
	}
	townJSON := filepath.Join(mayorDir, "town.json")
	if err := os.WriteFile(townJSON, []byte(`{"name": "test-town"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Create root .beads directory with routes.jsonl
	rootBeadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(rootBeadsDir, 0750); err != nil {
		t.Fatal(err)
	}
	routesContent := `{"prefix":"hq-","path":"."}
{"prefix":"pq-","path":"pgqueue"}
`
	routesPath := filepath.Join(rootBeadsDir, "routes.jsonl")
	if err := os.WriteFile(routesPath, []byte(routesContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Create pgqueue/.beads directory
	pgqueueBeadsDir := filepath.Join(tmpDir, "pgqueue", ".beads")
	if err := os.MkdirAll(pgqueueBeadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Simulate the scenario: User is in town root and runs:
	// bd create --id=pq-pgqueue-crew-pgq_crew --title="Test"

	// Step 1: Extract prefix from explicit ID
	explicitID := "pq-pgqueue-crew-pgq_crew"
	prefix := ExtractPrefix(explicitID)
	if prefix != "pq-" {
		t.Fatalf("ExtractPrefix(%q) = %q, want %q", explicitID, prefix, "pq-")
	}

	// Step 2: Resolve beads directory for this ID
	// This should route to pgqueue/.beads because routes.jsonl maps pq- -> pgqueue
	resolvedBeadsDir, routed, err := ResolveBeadsDirForID(context.Background(), explicitID, rootBeadsDir)
	if err != nil {
		t.Fatalf("ResolveBeadsDirForID() error = %v", err)
	}

	// Step 3: Verify the issue was routed to the correct database
	if !routed {
		t.Errorf("ResolveBeadsDirForID() routed = false, want true (should route to pgqueue)")
	}

	expectedBeadsDir := pgqueueBeadsDir
	if resolvedBeadsDir != expectedBeadsDir {
		t.Errorf("ResolveBeadsDirForID() resolved to:\n  got:  %s\n  want: %s", resolvedBeadsDir, expectedBeadsDir)
	}

	// Step 4: Verify the routes work correctly
	// Load routes and verify pq- prefix maps to pgqueue path
	routes, err := LoadRoutes(rootBeadsDir)
	if err != nil {
		t.Fatalf("LoadRoutes() error = %v", err)
	}

	var foundPqRoute bool
	for _, route := range routes {
		if route.Prefix == "pq-" {
			foundPqRoute = true
			if route.Path != "pgqueue" {
				t.Errorf("Route for pq- has path %q, want %q", route.Path, "pgqueue")
			}
		}
	}

	if !foundPqRoute {
		t.Errorf("No route found for prefix pq-")
	}
}

// TestResolveBeadsDirForIDWithMismatchedPrefix tests the scenario where:
// - User is in root directory (with hq- prefix database)
// - User creates issue with --id=pq-xxx (pq- prefix)
// - System should route to pgqueue/.beads (not validate against root hq- prefix)
func TestResolveBeadsDirForIDWithMismatchedPrefix(t *testing.T) {
	// Create temporary directory structure
	tmpDir, err := os.MkdirTemp("", "routing-mismatch-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Resolve symlinks
	tmpDir, err = filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}

	// Create mayor/town.json
	mayorDir := filepath.Join(tmpDir, "mayor")
	if err := os.MkdirAll(mayorDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mayorDir, "town.json"), []byte(`{"name": "test"}`), 0600); err != nil {
		t.Fatal(err)
	}

	// Create root .beads with routes
	rootBeadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(rootBeadsDir, 0750); err != nil {
		t.Fatal(err)
	}
	routesContent := `{"prefix":"hq-","path":"."}
{"prefix":"pq-","path":"pgqueue"}
`
	if err := os.WriteFile(filepath.Join(rootBeadsDir, "routes.jsonl"), []byte(routesContent), 0600); err != nil {
		t.Fatal(err)
	}

	// Create pgqueue/.beads
	pgqueueBeadsDir := filepath.Join(tmpDir, "pgqueue", ".beads")
	if err := os.MkdirAll(pgqueueBeadsDir, 0750); err != nil {
		t.Fatal(err)
	}

	// Change to town root directory
	t.Chdir(tmpDir)

	// Test: Create issue with pq- prefix while in root context
	testCases := []struct {
		id           string
		wantRouted   bool
		wantBeadsDir string
	}{
		{
			id:           "pq-pgqueue-crew-pgq_crew",
			wantRouted:   true,
			wantBeadsDir: pgqueueBeadsDir,
		},
		{
			id:           "pq-test-123",
			wantRouted:   true,
			wantBeadsDir: pgqueueBeadsDir,
		},
		{
			id:           "hq-test-456",
			wantRouted:   false, // hq- is root prefix, no routing needed
			wantBeadsDir: rootBeadsDir,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.id, func(t *testing.T) {
			gotBeadsDir, gotRouted, err := ResolveBeadsDirForID(context.Background(), tc.id, rootBeadsDir)
			if err != nil {
				t.Fatalf("ResolveBeadsDirForID(%q) error = %v", tc.id, err)
			}

			if gotRouted != tc.wantRouted {
				t.Errorf("ResolveBeadsDirForID(%q) routed = %v, want %v", tc.id, gotRouted, tc.wantRouted)
			}

			if gotBeadsDir != tc.wantBeadsDir {
				t.Errorf("ResolveBeadsDirForID(%q) beadsDir =\n  got:  %s\n  want: %s", tc.id, gotBeadsDir, tc.wantBeadsDir)
			}
		})
	}
}
