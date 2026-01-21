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

func TestNeedsVarMigration(t *testing.T) {
	t.Run("already using var layout returns false", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		varDir := filepath.Join(beadsDir, "var")
		if err := os.MkdirAll(varDir, 0755); err != nil {
			t.Fatalf("failed to create var/: %v", err)
		}

		// Create volatile file in var/ (correct location)
		if err := os.WriteFile(filepath.Join(varDir, "beads.db"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create beads.db: %v", err)
		}

		if needsVarMigration(beadsDir) {
			t.Error("expected false when already using var/ layout")
		}
	})

	t.Run("legacy layout with volatile files returns true", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("failed to create .beads: %v", err)
		}

		// Create volatile file at root (legacy location)
		if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create beads.db: %v", err)
		}

		if !needsVarMigration(beadsDir) {
			t.Error("expected true when legacy layout has volatile files")
		}
	})

	t.Run("empty beads directory returns false", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("failed to create .beads: %v", err)
		}

		if needsVarMigration(beadsDir) {
			t.Error("expected false for empty beads directory")
		}
	})
}

func TestFilesInWrongLocation(t *testing.T) {
	t.Run("not using var layout returns nil", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("failed to create .beads: %v", err)
		}

		// Create file at root (legacy layout)
		if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create beads.db: %v", err)
		}

		files := FilesInWrongLocation(beadsDir)
		if files != nil {
			t.Errorf("expected nil for legacy layout, got %v", files)
		}
	})

	t.Run("var layout with files at root returns them", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		varDir := filepath.Join(beadsDir, "var")
		if err := os.MkdirAll(varDir, 0755); err != nil {
			t.Fatalf("failed to create var/: %v", err)
		}

		// Create file at root (wrong location for var/ layout)
		if err := os.WriteFile(filepath.Join(beadsDir, "daemon.pid"), []byte("123"), 0644); err != nil {
			t.Fatalf("failed to create daemon.pid: %v", err)
		}

		files := FilesInWrongLocation(beadsDir)
		if len(files) != 1 || files[0] != "daemon.pid" {
			t.Errorf("expected [daemon.pid], got %v", files)
		}
	})

	t.Run("var layout with no files at root returns empty", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		varDir := filepath.Join(beadsDir, "var")
		if err := os.MkdirAll(varDir, 0755); err != nil {
			t.Fatalf("failed to create var/: %v", err)
		}

		// Create file in var/ (correct location)
		if err := os.WriteFile(filepath.Join(varDir, "beads.db"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create beads.db: %v", err)
		}

		files := FilesInWrongLocation(beadsDir)
		if len(files) != 0 {
			t.Errorf("expected empty slice, got %v", files)
		}
	})
}

func TestDetectPendingMigrations_VarLayout(t *testing.T) {
	t.Run("legacy layout with volatile files suggests var migration", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		if err := os.MkdirAll(beadsDir, 0755); err != nil {
			t.Fatalf("failed to create .beads: %v", err)
		}

		// Create volatile file at root
		if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("test"), 0644); err != nil {
			t.Fatalf("failed to create beads.db: %v", err)
		}

		migrations := DetectPendingMigrations(tmpDir)

		var foundVar bool
		for _, m := range migrations {
			if m.Name == "var-layout" {
				foundVar = true
				if m.Priority != 2 {
					t.Errorf("var-layout priority = %d, want 2 (warning)", m.Priority)
				}
				if m.Command != "bd migrate layout" {
					t.Errorf("var-layout command = %q, want %q", m.Command, "bd migrate layout")
				}
			}
		}

		if !foundVar {
			t.Error("expected var-layout migration to be detected")
		}
	})

	t.Run("var layout with stray files detects them", func(t *testing.T) {
		tmpDir, err := os.MkdirTemp("", "bd-doctor-var-*")
		if err != nil {
			t.Fatalf("failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tmpDir)

		beadsDir := filepath.Join(tmpDir, ".beads")
		varDir := filepath.Join(beadsDir, "var")
		if err := os.MkdirAll(varDir, 0755); err != nil {
			t.Fatalf("failed to create var/: %v", err)
		}

		// Create stray file at root (wrong location)
		if err := os.WriteFile(filepath.Join(beadsDir, "daemon.pid"), []byte("123"), 0644); err != nil {
			t.Fatalf("failed to create daemon.pid: %v", err)
		}

		migrations := DetectPendingMigrations(tmpDir)

		var foundStray bool
		for _, m := range migrations {
			if m.Name == "stray-files" {
				foundStray = true
				if m.Priority != 2 {
					t.Errorf("stray-files priority = %d, want 2 (warning)", m.Priority)
				}
				if m.Command != "bd doctor --fix" {
					t.Errorf("stray-files command = %q, want %q", m.Command, "bd doctor --fix")
				}
			}
		}

		if !foundStray {
			t.Error("expected stray-files migration to be detected")
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
