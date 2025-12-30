package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPendingMigrations(t *testing.T) {
	tests := []struct {
		name           string
		setup          func(t *testing.T, dir string)
		wantStatus     string
		wantMessage    string
		wantMigrations int
	}{
		{
			name:        "no beads directory",
			setup:       func(t *testing.T, dir string) {},
			wantStatus:  StatusOK,
			wantMessage: "None required",
		},
		{
			name: "empty beads directory",
			setup: func(t *testing.T, dir string) {
				if err := os.MkdirAll(filepath.Join(dir, ".beads"), 0755); err != nil {
					t.Fatalf("failed to create .beads: %v", err)
				}
			},
			wantStatus:  StatusOK,
			wantMessage: "None required",
		},
		{
			name: "deletions.jsonl exists with entries",
			setup: func(t *testing.T, dir string) {
				beadsDir := filepath.Join(dir, ".beads")
				if err := os.MkdirAll(beadsDir, 0755); err != nil {
					t.Fatalf("failed to create .beads: %v", err)
				}
				// Create deletions.jsonl with an entry
				content := `{"id":"bd-test","ts":"2024-01-01T00:00:00Z","by":"test"}`
				if err := os.WriteFile(filepath.Join(beadsDir, "deletions.jsonl"), []byte(content), 0644); err != nil {
					t.Fatalf("failed to create deletions.jsonl: %v", err)
				}
			},
			wantStatus:     StatusWarning,
			wantMigrations: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpDir, err := os.MkdirTemp("", "bd-doctor-migration-*")
			if err != nil {
				t.Fatalf("failed to create temp dir: %v", err)
			}
			defer os.RemoveAll(tmpDir)

			tt.setup(t, tmpDir)

			check := CheckPendingMigrations(tmpDir)

			if check.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q", check.Status, tt.wantStatus)
			}

			if tt.wantMessage != "" && check.Message != tt.wantMessage {
				t.Errorf("message = %q, want %q", check.Message, tt.wantMessage)
			}

			if check.Category != CategoryMaintenance {
				t.Errorf("category = %q, want %q", check.Category, CategoryMaintenance)
			}
		})
	}
}

func TestDetectPendingMigrations(t *testing.T) {
	t.Run("no beads directory returns empty", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-migration-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		migrations := DetectPendingMigrations(tmpDir)
		if len(migrations) != 0 {
			t.Errorf("expected 0 migrations, got %d", len(migrations))
		}
	})

	t.Run("empty beads directory returns empty", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-migration-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		if err := os.MkdirAll(filepath.Join(tmpDir, ".beads"), 0755); err != nil {
			t.Fatalf("failed to create .beads: %v", err)
		}

		migrations := DetectPendingMigrations(tmpDir)
		if len(migrations) != 0 {
			t.Errorf("expected 0 migrations, got %d", len(migrations))
		}
	})

	t.Run("deletions.jsonl triggers tombstones migration", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-migration-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("failed to create .beads: %v", err)
		}

		// Create deletions.jsonl with an entry
		content := `{"id":"bd-test","ts":"2024-01-01T00:00:00Z","by":"test"}`
		if err := os.WriteFile(filepath.Join(beadsDir, "deletions.jsonl"), []byte(content), 0644); err != nil {
			t.Fatalf("failed to create deletions.jsonl: %v", err)
		}

		migrations := DetectPendingMigrations(tmpDir)
		if len(migrations) != 1 {
			t.Errorf("expected 1 migration, got %d", len(migrations))
			return
		}

		if migrations[0].Name != "tombstones" {
			t.Errorf("migration name = %q, want %q", migrations[0].Name, "tombstones")
		}

		if migrations[0].Command != "bd migrate tombstones" {
			t.Errorf("migration command = %q, want %q", migrations[0].Command, "bd migrate tombstones")
		}
	})
}

func TestNeedsTombstonesMigration(t *testing.T) {
	t.Run("no deletions.jsonl returns false", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-migration-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		if needsTombstonesMigration(tmpDir) {
			t.Error("expected false for non-existent deletions.jsonl")
		}
	})

	t.Run("empty deletions.jsonl returns false", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-migration-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		if err := os.WriteFile(filepath.Join(tmpDir, "deletions.jsonl"), []byte(""), 0644); err != nil {
			t.Fatalf("failed to create deletions.jsonl: %v", err)
		}

		if needsTombstonesMigration(tmpDir) {
			t.Error("expected false for empty deletions.jsonl")
		}
	})

	t.Run("deletions.jsonl with entries returns true", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-migration-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		content := `{"id":"bd-test","ts":"2024-01-01T00:00:00Z","by":"test"}`
		if err := os.WriteFile(filepath.Join(tmpDir, "deletions.jsonl"), []byte(content), 0644); err != nil {
			t.Fatalf("failed to create deletions.jsonl: %v", err)
		}

		if !needsTombstonesMigration(tmpDir) {
			t.Error("expected true for deletions.jsonl with entries")
		}
	})
}
