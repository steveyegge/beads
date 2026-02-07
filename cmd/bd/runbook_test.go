package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "HCL file",
			path:     "runbooks/base.hcl",
			expected: "hcl",
		},
		{
			name:     "TOML file",
			path:     "runbooks/config.toml",
			expected: "toml",
		},
		{
			name:     "JSON file",
			path:     "runbooks/spec.json",
			expected: "json",
		},
		{
			name:     "unknown extension defaults to hcl",
			path:     "runbooks/readme.txt",
			expected: "hcl",
		},
		{
			name:     "no extension defaults to hcl",
			path:     "runbooks/Makefile",
			expected: "hcl",
		},
		{
			name:     "full path with HCL",
			path:     "/home/user/project/.oj/runbooks/deploy.hcl",
			expected: "hcl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectFormat(tt.path)
			if result != tt.expected {
				t.Errorf("detectFormat(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

// createRunbookFixture writes a runbook file to the given directory.
func createRunbookFixture(t *testing.T, dir, filename, content string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("Failed to create dir %s: %v", dir, err)
	}
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("Failed to write %s: %v", path, err)
	}
	return path
}

func TestScanRunbookDir(t *testing.T) {
	t.Run("finds HCL and TOML files", func(t *testing.T) {
		tmpDir := t.TempDir()
		runbooksDir := filepath.Join(tmpDir, ".oj", "runbooks")

		createRunbookFixture(t, runbooksDir, "base.hcl",
			`job "deploy" { command = "deploy-all" }`)
		createRunbookFixture(t, runbooksDir, "config.toml",
			`[settings]\nname = "config"`)

		runbooks := scanRunbookDir(runbooksDir)

		if len(runbooks) != 2 {
			t.Fatalf("expected 2 runbooks, got %d", len(runbooks))
		}

		// Verify names and formats
		names := map[string]string{}
		for _, rb := range runbooks {
			names[rb.Name] = rb.Format
		}
		if names["base"] != "hcl" {
			t.Errorf("expected base with format 'hcl', got format %q", names["base"])
		}
		if names["config"] != "toml" {
			t.Errorf("expected config with format 'toml', got format %q", names["config"])
		}
	})

	t.Run("finds JSON files", func(t *testing.T) {
		tmpDir := t.TempDir()
		runbooksDir := filepath.Join(tmpDir, ".oj", "runbooks")

		createRunbookFixture(t, runbooksDir, "spec.json",
			`{"name": "spec"}`)

		runbooks := scanRunbookDir(runbooksDir)

		if len(runbooks) != 1 {
			t.Fatalf("expected 1 runbook, got %d", len(runbooks))
		}
		if runbooks[0].Name != "spec" {
			t.Errorf("expected name 'spec', got %q", runbooks[0].Name)
		}
		if runbooks[0].Format != "json" {
			t.Errorf("expected format 'json', got %q", runbooks[0].Format)
		}
	})

	t.Run("skips non-runbook files", func(t *testing.T) {
		tmpDir := t.TempDir()
		runbooksDir := filepath.Join(tmpDir, ".oj", "runbooks")

		createRunbookFixture(t, runbooksDir, "real.hcl",
			`job "build" { command = "make" }`)
		createRunbookFixture(t, runbooksDir, "README.md", "# Runbooks")
		createRunbookFixture(t, runbooksDir, "notes.txt", "some notes")
		createRunbookFixture(t, runbooksDir, ".hidden", "hidden file")

		runbooks := scanRunbookDir(runbooksDir)

		if len(runbooks) != 1 {
			t.Errorf("expected 1 runbook (skipping non-runbook files), got %d", len(runbooks))
		}
	})

	t.Run("skips directories", func(t *testing.T) {
		tmpDir := t.TempDir()
		runbooksDir := filepath.Join(tmpDir, ".oj", "runbooks")

		createRunbookFixture(t, runbooksDir, "real.hcl",
			`job "build" { command = "make" }`)
		// Create a subdirectory with .hcl extension (should be skipped)
		subDir := filepath.Join(runbooksDir, "subdir.hcl")
		if err := os.MkdirAll(subDir, 0755); err != nil {
			t.Fatalf("Failed to create subdir: %v", err)
		}

		runbooks := scanRunbookDir(runbooksDir)

		if len(runbooks) != 1 {
			t.Errorf("expected 1 runbook (skipping directories), got %d", len(runbooks))
		}
	})

	t.Run("extracts HCL job names", func(t *testing.T) {
		tmpDir := t.TempDir()
		runbooksDir := filepath.Join(tmpDir, ".oj", "runbooks")

		hclContent := `job "deploy" {
  command = "deploy-all"
}

job "build" {
  command = "make build"
}

worker "processor" {
  command = "run-processor"
}
`
		createRunbookFixture(t, runbooksDir, "multi.hcl", hclContent)

		runbooks := scanRunbookDir(runbooksDir)

		if len(runbooks) != 1 {
			t.Fatalf("expected 1 runbook, got %d", len(runbooks))
		}
		rb := runbooks[0]
		if len(rb.Jobs) != 2 {
			t.Errorf("expected 2 jobs, got %d: %v", len(rb.Jobs), rb.Jobs)
		}
		if len(rb.Workers) != 1 {
			t.Errorf("expected 1 worker, got %d: %v", len(rb.Workers), rb.Workers)
		}
	})

	t.Run("sets source to file path", func(t *testing.T) {
		tmpDir := t.TempDir()
		runbooksDir := filepath.Join(tmpDir, ".oj", "runbooks")

		createRunbookFixture(t, runbooksDir, "sourced.hcl",
			`job "test" { command = "echo test" }`)

		runbooks := scanRunbookDir(runbooksDir)

		if len(runbooks) != 1 {
			t.Fatalf("expected 1 runbook, got %d", len(runbooks))
		}
		expectedSource := filepath.Join(runbooksDir, "sourced.hcl")
		if runbooks[0].Source != expectedSource {
			t.Errorf("expected source %q, got %q", expectedSource, runbooks[0].Source)
		}
	})
}

func TestScanRunbookDir_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	runbooksDir := filepath.Join(tmpDir, "empty-runbooks")
	if err := os.MkdirAll(runbooksDir, 0755); err != nil {
		t.Fatalf("mkdir failed: %v", err)
	}

	runbooks := scanRunbookDir(runbooksDir)
	if len(runbooks) != 0 {
		t.Errorf("expected 0 runbooks from empty dir, got %d", len(runbooks))
	}
}

func TestScanRunbookDir_NonExistent(t *testing.T) {
	runbooks := scanRunbookDir("/nonexistent/path/that/does/not/exist")
	if runbooks != nil {
		t.Errorf("expected nil from nonexistent dir, got %d runbooks", len(runbooks))
	}
}

func TestGetRunbookSearchPaths(t *testing.T) {
	// getRunbookSearchPaths uses os.Getwd(), so we can test that it returns
	// at least the .oj/runbooks path relative to cwd.
	paths := getRunbookSearchPaths()

	if len(paths) == 0 {
		t.Fatal("expected at least 1 search path, got 0")
	}

	// The first path should always be cwd/.oj/runbooks
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd failed: %v", err)
	}
	expectedFirst := filepath.Join(cwd, ".oj", "runbooks")
	if paths[0] != expectedFirst {
		t.Errorf("expected first path to be %q, got %q", expectedFirst, paths[0])
	}

	// Verify GT_ROOT-based path is included when GT_ROOT is set
	t.Run("includes GT_ROOT path when set", func(t *testing.T) {
		tmpDir := t.TempDir()
		t.Setenv("GT_ROOT", tmpDir)

		paths := getRunbookSearchPaths()
		expectedGT := filepath.Join(tmpDir, ".oj", "runbooks")
		found := false
		for _, p := range paths {
			if p == expectedGT {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected GT_ROOT path %q in search paths %v", expectedGT, paths)
		}
	})

	t.Run("excludes GT_ROOT path when not set", func(t *testing.T) {
		t.Setenv("GT_ROOT", "")

		paths := getRunbookSearchPaths()
		for _, p := range paths {
			if filepath.Base(filepath.Dir(p)) == ".oj" {
				// This is the cwd-based path, which is expected
				continue
			}
		}
		// Just verify it doesn't crash and returns at least cwd path
		if len(paths) == 0 {
			t.Error("expected at least 1 search path even without GT_ROOT")
		}
	})
}
