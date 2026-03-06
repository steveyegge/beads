//go:build cgo

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadBeadsRefs(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		refsDir := filepath.Join(dir, "refs", "heads")
		if err := os.MkdirAll(refsDir, 0755); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/main\n"), 0644)
		os.WriteFile(filepath.Join(refsDir, "main"), []byte("abc123def456\n"), 0644)

		hash, branch := readBeadsRefs(dir)
		if hash != "abc123def456" {
			t.Errorf("hash = %q, want %q", hash, "abc123def456")
		}
		if branch != "main" {
			t.Errorf("branch = %q, want %q", branch, "main")
		}
	})

	t.Run("missing HEAD file", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		hash, branch := readBeadsRefs(dir)
		if hash != "" || branch != "" {
			t.Errorf("expected empty, got hash=%q branch=%q", hash, branch)
		}
	})

	t.Run("malformed HEAD", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "HEAD"), []byte("not a valid ref\n"), 0644)

		hash, branch := readBeadsRefs(dir)
		if hash != "" || branch != "" {
			t.Errorf("expected empty for malformed HEAD, got hash=%q branch=%q", hash, branch)
		}
	})

	t.Run("HEAD valid but ref file missing", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/develop\n"), 0644)

		hash, branch := readBeadsRefs(dir)
		if hash != "" {
			t.Errorf("hash = %q, want empty", hash)
		}
		if branch != "develop" {
			t.Errorf("branch = %q, want %q", branch, "develop")
		}
	})

	t.Run("whitespace handling", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		refsDir := filepath.Join(dir, "refs", "heads")
		os.MkdirAll(refsDir, 0755)
		os.WriteFile(filepath.Join(dir, "HEAD"), []byte("  ref: refs/heads/main  \n\n"), 0644)
		os.WriteFile(filepath.Join(refsDir, "main"), []byte("  abc123  \n"), 0644)

		hash, branch := readBeadsRefs(dir)
		if hash != "abc123" {
			t.Errorf("hash = %q, want %q (should trim whitespace)", hash, "abc123")
		}
		if branch != "main" {
			t.Errorf("branch = %q, want %q", branch, "main")
		}
	})

	t.Run("slashed branch name", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		refsDir := filepath.Join(dir, "refs", "heads", "feature")
		os.MkdirAll(refsDir, 0755)
		os.WriteFile(filepath.Join(dir, "HEAD"), []byte("ref: refs/heads/feature/foo\n"), 0644)
		os.WriteFile(filepath.Join(refsDir, "foo"), []byte("hash999\n"), 0644)

		hash, branch := readBeadsRefs(dir)
		if hash != "hash999" {
			t.Errorf("hash = %q, want %q", hash, "hash999")
		}
		if branch != "feature/foo" {
			t.Errorf("branch = %q, want %q", branch, "feature/foo")
		}
	})
}

func TestWriteBeadsRefsNilStore(t *testing.T) {
	t.Parallel()
	// Should not panic when store is nil
	writeBeadsRefs(nil, nil)
}

func TestNoMathReferences(t *testing.T) {
	t.Parallel()

	// Walk all .go files in the repo (excluding vendor, .git, website)
	repoRoot := findRepoRoot()
	if repoRoot == "" {
		t.Fatal("could not find repo root")
	}

	var violations []string
	err := filepath.WalkDir(repoRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable dirs
		}
		base := d.Name()
		// Skip non-Go files and irrelevant directories
		if d.IsDir() {
			switch base {
			case ".git", "vendor", "website", "node_modules", ".beads":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(base, ".go") {
			return nil
		}
		// Skip this test file itself
		if strings.HasSuffix(path, "dolt_head_test.go") {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		rel, _ := filepath.Rel(repoRoot, path)

		// Check for math test bed issue IDs (math-xxx patterns)
		if strings.Contains(content, "math-ryv") || strings.Contains(content, "math-r62") ||
			strings.Contains(content, "math-4z8") || strings.Contains(content, "math-5yq") ||
			strings.Contains(content, "math-b5k") || strings.Contains(content, "math-cwm") ||
			strings.Contains(content, "math-cc5") {
			violations = append(violations, rel+": contains math test bed issue ID")
		}

		// Check for math project path references
		if strings.Contains(content, "Projects/math") {
			violations = append(violations, rel+": contains Projects/math path reference")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir failed: %v", err)
	}

	if len(violations) > 0 {
		t.Errorf("Found %d math test bed references in committed code:\n  %s",
			len(violations), strings.Join(violations, "\n  "))
	}
}

