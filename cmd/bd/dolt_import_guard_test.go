package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoDoltImportsInCmdBd validates that cmd/bd/ does not import
// internal/storage/dolt except in the 5 legitimate exception files.
// This prevents regression of the RPC migration (bd-ma0s.8).
func TestNoDoltImportsInCmdBd(t *testing.T) {
	// Legitimate exceptions that require direct Dolt access
	allowedFiles := map[string]bool{
		"daemon_event_loop.go":           true, // Daemon process needs direct access
		"dolt_server_cgo.go":             true, // Dolt server lifecycle (CGO)
		"init.go":                        true, // Initial database setup
		"migrate_dolt.go":                true, // One-time migration tool
		"doctor/federation.go":           true, // Diagnostics with AllowWithRemoteDaemon
		"doctor/fix/validation_test.go":  true, // Test file that creates Dolt stores
		"dolt_import_guard_test.go":      true, // This test file itself
		"test_helpers_test.go":           true, // Test helpers that create Dolt stores
		"version_tracking_test.go":       true, // Tests auto-migration with Dolt store
		"worktree_daemon_test.go":        true, // Tests worktree detection with Dolt store
	}

	const doltImportPrefix = "github.com/steveyegge/beads/internal/storage/dolt"

	// Walk cmd/bd/ looking for .go files with dolt imports
	cmdBdDir := "."
	var violations []string

	err := filepath.Walk(cmdBdDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		// Skip allowed files
		relPath := path
		if strings.HasPrefix(relPath, "./") {
			relPath = relPath[2:]
		}
		if allowedFiles[relPath] {
			return nil
		}

		// Parse the file to check imports
		fset := token.NewFileSet()
		f, parseErr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if parseErr != nil {
			// Skip files that can't be parsed (e.g., build-constrained)
			return nil
		}

		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, `"`)
			if strings.HasPrefix(importPath, doltImportPrefix) {
				violations = append(violations, relPath+": "+importPath)
			}
		}

		return nil
	})

	if err != nil {
		t.Fatalf("failed to walk cmd/bd/: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("found unauthorized dolt imports in cmd/bd/ (use daemon RPC instead):\n")
		for _, v := range violations {
			t.Errorf("  %s", v)
		}
		t.Errorf("\nAllowed exceptions: daemon_event_loop.go, dolt_server_cgo.go, init.go, migrate_dolt.go, doctor/federation.go")
	}
}
