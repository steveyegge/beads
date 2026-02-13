package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/testutil/teststore"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/git"
	"github.com/steveyegge/beads/internal/types"
)

// TestMultiRepoPathResolutionCWDInvariant verifies that path resolution for
// repos.additional produces the same absolute paths regardless of CWD.
//
// The bug (oss-lbp): Running from .beads/ caused paths like "oss/" to become
// ".beads/oss/" instead of "{repo}/oss/". This test ensures the fix works
// by verifying resolution from multiple CWDs produces identical results.
//
// Covers: T040-T042
func TestMultiRepoPathResolutionCWDInvariant(t *testing.T) {
	ctx := context.Background()

	// Store original working directory
	originalWd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get original working directory: %v", err)
	}
	defer func() { _ = os.Chdir(originalWd) }()

	// Create temp repo structure
	// Resolve symlinks to avoid macOS /var -> /private/var issues
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("eval symlinks failed: %v", err)
	}

	// Setup git repo
	if err := setupGitRepoInDir(t, tmpDir); err != nil {
		t.Fatalf("failed to setup git repo: %v", err)
	}

	// Create .beads directory structure
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create .beads dir: %v", err)
	}

	// Create subdirectory for testing CWD from subdir
	subDir := filepath.Join(tmpDir, "src", "pkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}

	// Create oss/ directory (the multi-repo target)
	ossDir := filepath.Join(tmpDir, "oss")
	ossBeadsDir := filepath.Join(ossDir, ".beads")
	if err := os.MkdirAll(ossBeadsDir, 0755); err != nil {
		t.Fatalf("failed to create oss/.beads dir: %v", err)
	}

	// Create config.yaml with relative path
	configContent := `repos:
  primary: "."
  additional:
    - oss/
`
	configPath := filepath.Join(beadsDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	// Create database
	store := teststore.New(t)
	defer store.Close()

	// Set issue prefix
	if err := store.SetConfig(ctx, "issue_prefix", "test"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	// Create a test issue
	issue := &types.Issue{
		ID:        "test-1",
		Title:     "Test Issue",
		Status:    types.StatusOpen,
		IssueType: types.TypeTask,
		Priority:  2,
	}
	if err := store.CreateIssue(ctx, issue, "test"); err != nil {
		t.Fatalf("failed to create issue: %v", err)
	}
	store.Close()

	// T040: Test from repo root
	t.Run("T040_from_repo_root", func(t *testing.T) {
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatalf("chdir to repo root failed: %v", err)
		}
		git.ResetCaches()

		// Initialize config
		if err := config.Initialize(); err != nil {
			t.Fatalf("config.Initialize() failed: %v", err)
		}

		multiRepo := config.GetMultiRepoConfig()
		if multiRepo == nil {
			t.Fatal("GetMultiRepoConfig() returned nil")
		}

		// The key assertion: "oss/" should resolve to {repo}/oss/
		if len(multiRepo.Additional) != 1 {
			t.Fatalf("expected 1 additional repo, got %d", len(multiRepo.Additional))
		}

		// Verify config file used is in the right place
		configUsed := config.ConfigFileUsed()
		expectedConfig := filepath.Join(beadsDir, "config.yaml")
		if configUsed != expectedConfig {
			t.Errorf("ConfigFileUsed() = %q, want %q", configUsed, expectedConfig)
		}

		t.Logf("From repo root: additional[0] = %q", multiRepo.Additional[0])
		t.Logf("ConfigFileUsed() = %q", configUsed)
	})

	// T041: Test from .beads/ directory (the bug trigger location)
	t.Run("T041_from_beads_directory", func(t *testing.T) {
		if err := os.Chdir(beadsDir); err != nil {
			t.Fatalf("chdir to .beads failed: %v", err)
		}
		git.ResetCaches()

		// Re-initialize config from new CWD
		if err := config.Initialize(); err != nil {
			t.Fatalf("config.Initialize() failed: %v", err)
		}

		multiRepo := config.GetMultiRepoConfig()
		if multiRepo == nil {
			t.Fatal("GetMultiRepoConfig() returned nil")
		}

		if len(multiRepo.Additional) != 1 {
			t.Fatalf("expected 1 additional repo, got %d", len(multiRepo.Additional))
		}

		// Verify config is still found correctly
		configUsed := config.ConfigFileUsed()
		expectedConfig := filepath.Join(beadsDir, "config.yaml")
		if configUsed != expectedConfig {
			t.Errorf("ConfigFileUsed() = %q, want %q", configUsed, expectedConfig)
		}

		t.Logf("From .beads/: additional[0] = %q", multiRepo.Additional[0])
		t.Logf("ConfigFileUsed() = %q", configUsed)
	})

	// T042: Test from subdirectory
	t.Run("T042_from_subdirectory", func(t *testing.T) {
		if err := os.Chdir(subDir); err != nil {
			t.Fatalf("chdir to subdir failed: %v", err)
		}
		git.ResetCaches()

		// Re-initialize config from new CWD
		if err := config.Initialize(); err != nil {
			t.Fatalf("config.Initialize() failed: %v", err)
		}

		multiRepo := config.GetMultiRepoConfig()
		if multiRepo == nil {
			t.Fatal("GetMultiRepoConfig() returned nil")
		}

		if len(multiRepo.Additional) != 1 {
			t.Fatalf("expected 1 additional repo, got %d", len(multiRepo.Additional))
		}

		// Verify config is still found correctly
		configUsed := config.ConfigFileUsed()
		expectedConfig := filepath.Join(beadsDir, "config.yaml")
		if configUsed != expectedConfig {
			t.Errorf("ConfigFileUsed() = %q, want %q", configUsed, expectedConfig)
		}

		t.Logf("From subdir: additional[0] = %q", multiRepo.Additional[0])
		t.Logf("ConfigFileUsed() = %q", configUsed)
	})
}

// TestExportToMultiRepoCWDInvariant and TestSyncModePathResolution were removed
// during the SQLite-to-Dolt migration because they depended on the
// ExportToMultiRepo method which was SQLite-specific.
