package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/spec"
)

func TestScoreSpecCompactionCandidate(t *testing.T) {
	t.Run("all factors true", func(t *testing.T) {
		score, rec := scoreSpecCompactionCandidate(specCompactionFactors{
			AllIssuesClosed:   true,
			SpecUnchangedDays: 31,
			CodeActivityDays:  90,
			IsSuperseded:      true,
		})
		if score != 1.0 {
			t.Fatalf("score = %v, want 1.0", score)
		}
		if rec != "compact" {
			t.Fatalf("rec = %s, want compact", rec)
		}
	})

	t.Run("review threshold", func(t *testing.T) {
		score, rec := scoreSpecCompactionCandidate(specCompactionFactors{
			AllIssuesClosed:   true,
			SpecUnchangedDays: 31,
			CodeActivityDays:  10,
			IsSuperseded:      false,
		})
		if diff := score - 0.6; diff < -1e-9 || diff > 1e-9 {
			t.Fatalf("score = %v, want 0.6", score)
		}
		if rec != "review" {
			t.Fatalf("rec = %s, want review", rec)
		}
	})

	t.Run("keep threshold", func(t *testing.T) {
		score, rec := scoreSpecCompactionCandidate(specCompactionFactors{
			AllIssuesClosed:   false,
			SpecUnchangedDays: 5,
			CodeActivityDays:  5,
			IsSuperseded:      false,
		})
		if score != 0.0 {
			t.Fatalf("score = %v, want 0.0", score)
		}
		if rec != "keep" {
			t.Fatalf("rec = %s, want keep", rec)
		}
	})
}

func TestSpecUnchangedDays(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "specs", "test.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(specPath, []byte("test"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(specPath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	entry := spec.SpecRegistryEntry{SpecID: filepath.ToSlash("specs/test.md")}
	got := specUnchangedDays(tmpDir, entry)
	if got < 1 {
		t.Fatalf("specUnchangedDays = %d, want >= 1", got)
	}
}

func TestIsSpecSuperseded(t *testing.T) {
	tmpDir := t.TempDir()
	specPath := filepath.Join(tmpDir, "specs", "test.md")
	if err := os.MkdirAll(filepath.Dir(specPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "Status: DEPRECATED\nSUPERSEDES: specs/old.md\n"
	if err := os.WriteFile(specPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	if !isSpecSuperseded(tmpDir, filepath.ToSlash("specs/test.md")) {
		t.Fatalf("expected superseded spec to be detected")
	}
}
