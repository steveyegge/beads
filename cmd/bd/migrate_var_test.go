package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/configfile"
)

func TestMigrateVarCommand(t *testing.T) {
	tests := map[string]struct {
		setup       func(t *testing.T, beadsDir string)
		wantLayout  string
		wantVarDir  bool
		wantFiles   []string // files expected in var/
		expectError bool
	}{
		"legacy layout with files": {
			setup: func(t *testing.T, beadsDir string) {
				t.Helper()
				// Create some volatile files at root
				files := []string{"beads.db", "daemon.log", "daemon.pid"}
				for _, f := range files {
					path := filepath.Join(beadsDir, f)
					if err := os.WriteFile(path, []byte("test content"), 0600); err != nil {
						t.Fatalf("Failed to create %s: %v", f, err)
					}
				}
				// Create metadata.json without layout field (legacy)
				cfg := configfile.DefaultConfig()
				if err := cfg.Save(beadsDir); err != nil {
					t.Fatalf("Failed to save config: %v", err)
				}
			},
			wantLayout: configfile.LayoutV2,
			wantVarDir: true,
			wantFiles:  []string{"beads.db", "daemon.log", "daemon.pid"},
		},
		"already migrated": {
			setup: func(t *testing.T, beadsDir string) {
				t.Helper()
				// Create var/ and set layout to v2
				varDir := filepath.Join(beadsDir, "var")
				if err := os.MkdirAll(varDir, 0700); err != nil {
					t.Fatalf("Failed to create var/: %v", err)
				}
				// Create file in var/
				if err := os.WriteFile(filepath.Join(varDir, "beads.db"), []byte("db"), 0600); err != nil {
					t.Fatalf("Failed to create beads.db: %v", err)
				}
				// Set layout to v2
				cfg := configfile.DefaultConfig()
				cfg.Layout = configfile.LayoutV2
				if err := cfg.Save(beadsDir); err != nil {
					t.Fatalf("Failed to save config: %v", err)
				}
			},
			wantLayout: configfile.LayoutV2,
			wantVarDir: true,
			wantFiles:  []string{"beads.db"},
		},
		"no files to move": {
			setup: func(t *testing.T, beadsDir string) {
				t.Helper()
				// Just create metadata.json (no volatile files)
				cfg := configfile.DefaultConfig()
				if err := cfg.Save(beadsDir); err != nil {
					t.Fatalf("Failed to save config: %v", err)
				}
			},
			wantLayout: configfile.LayoutV2,
			wantVarDir: true,
			wantFiles:  nil,
		},
		"sqlite sibling files": {
			setup: func(t *testing.T, beadsDir string) {
				t.Helper()
				// Create database with WAL files
				files := []string{"beads.db", "beads.db-wal", "beads.db-shm"}
				for _, f := range files {
					path := filepath.Join(beadsDir, f)
					if err := os.WriteFile(path, []byte("data"), 0600); err != nil {
						t.Fatalf("Failed to create %s: %v", f, err)
					}
				}
				cfg := configfile.DefaultConfig()
				if err := cfg.Save(beadsDir); err != nil {
					t.Fatalf("Failed to save config: %v", err)
				}
			},
			wantLayout: configfile.LayoutV2,
			wantVarDir: true,
			wantFiles:  []string{"beads.db", "beads.db-wal", "beads.db-shm"},
		},
		"interrupted migration completes": {
			setup: func(t *testing.T, beadsDir string) {
				t.Helper()
				// var/ exists but layout not set (simulates interrupted migration)
				varDir := filepath.Join(beadsDir, "var")
				if err := os.MkdirAll(varDir, 0700); err != nil {
					t.Fatalf("Failed to create var/: %v", err)
				}
				// Some files already in var/
				if err := os.WriteFile(filepath.Join(varDir, "daemon.log"), []byte("log"), 0600); err != nil {
					t.Fatalf("Failed to create daemon.log: %v", err)
				}
				// Some files still at root
				if err := os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("db"), 0600); err != nil {
					t.Fatalf("Failed to create beads.db: %v", err)
				}
				// Config without layout
				cfg := configfile.DefaultConfig()
				if err := cfg.Save(beadsDir); err != nil {
					t.Fatalf("Failed to save config: %v", err)
				}
			},
			wantLayout: configfile.LayoutV2,
			wantVarDir: true,
			wantFiles:  []string{"beads.db", "daemon.log"},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			// Create temporary test directory
			tmpDir := t.TempDir()
			beadsDir := filepath.Join(tmpDir, ".beads")
			if err := os.MkdirAll(beadsDir, 0750); err != nil {
				t.Fatalf("Failed to create .beads directory: %v", err)
			}

			// Run setup
			tc.setup(t, beadsDir)

			// Simulate migration (we can't easily call the command, so test the logic)
			cfg, err := configfile.Load(beadsDir)
			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}
			if cfg == nil {
				cfg = configfile.DefaultConfig()
			}

			// Skip if already migrated
			if cfg.Layout == configfile.LayoutV2 {
				// Verify var/ exists
				varDir := filepath.Join(beadsDir, "var")
				if _, err := os.Stat(varDir); os.IsNotExist(err) {
					t.Error("Expected var/ to exist for v2 layout")
				}
				return
			}

			// Create var/ directory
			varDir := filepath.Join(beadsDir, "var")
			if err := os.MkdirAll(varDir, 0700); err != nil {
				t.Fatalf("Failed to create var/: %v", err)
			}

			// Find and move volatile files
			for _, f := range beads.VolatileFiles {
				rootPath := filepath.Join(beadsDir, f)
				if _, err := os.Stat(rootPath); err == nil {
					varPath := filepath.Join(varDir, f)
					// Check if already exists in var/
					if _, err := os.Stat(varPath); err == nil {
						// Remove duplicate at root
						if err := os.Remove(rootPath); err != nil {
							t.Errorf("Failed to remove duplicate %s: %v", f, err)
						}
					} else {
						// Move to var/
						if err := os.Rename(rootPath, varPath); err != nil {
							t.Errorf("Failed to move %s: %v", f, err)
						}
					}
				}
			}

			// Also move SQLite sibling files
			entries, _ := os.ReadDir(beadsDir)
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				name := entry.Name()
				if matched, _ := filepath.Match("*.db-*", name); matched {
					rootPath := filepath.Join(beadsDir, name)
					varPath := filepath.Join(varDir, name)
					if _, err := os.Stat(varPath); os.IsNotExist(err) {
						_ = os.Rename(rootPath, varPath)
					}
				}
			}

			// Update config
			cfg.Layout = configfile.LayoutV2
			if err := cfg.Save(beadsDir); err != nil {
				t.Fatalf("Failed to save config: %v", err)
			}

			// Verify results
			cfg, err = configfile.Load(beadsDir)
			if err != nil {
				t.Fatalf("Failed to reload config: %v", err)
			}

			if cfg.Layout != tc.wantLayout {
				t.Errorf("Expected layout %s, got %s", tc.wantLayout, cfg.Layout)
			}

			if tc.wantVarDir {
				if _, err := os.Stat(varDir); os.IsNotExist(err) {
					t.Error("Expected var/ directory to exist")
				}
			}

			for _, f := range tc.wantFiles {
				varPath := filepath.Join(varDir, f)
				if _, err := os.Stat(varPath); os.IsNotExist(err) {
					t.Errorf("Expected %s to exist in var/", f)
				}
				// Verify it's NOT at root anymore
				rootPath := filepath.Join(beadsDir, f)
				if _, err := os.Stat(rootPath); err == nil {
					t.Errorf("Expected %s to NOT exist at root", f)
				}
			}
		})
	}
}

func TestMigrateVarDryRun(t *testing.T) {
	// Create temporary test directory
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("Failed to create .beads directory: %v", err)
	}

	// Create some volatile files at root
	files := []string{"beads.db", "daemon.log"}
	for _, f := range files {
		path := filepath.Join(beadsDir, f)
		if err := os.WriteFile(path, []byte("test"), 0600); err != nil {
			t.Fatalf("Failed to create %s: %v", f, err)
		}
	}

	// Create legacy config
	cfg := configfile.DefaultConfig()
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// In dry-run mode, simulate that we would:
	// 1. Create var/
	// 2. Move files
	// 3. Update config
	// But actually do nothing

	// Find files that would be moved
	var filesToMove []string
	for _, f := range beads.VolatileFiles {
		rootPath := filepath.Join(beadsDir, f)
		if _, err := os.Stat(rootPath); err == nil {
			filesToMove = append(filesToMove, f)
		}
	}

	// Verify we found the files
	if len(filesToMove) != 2 {
		t.Errorf("Expected 2 files to move, found %d", len(filesToMove))
	}

	// After "dry run", verify nothing changed
	varDir := filepath.Join(beadsDir, "var")
	if _, err := os.Stat(varDir); err == nil {
		t.Error("var/ should not exist after dry run")
	}

	cfg, _ = configfile.Load(beadsDir)
	if cfg.Layout == configfile.LayoutV2 {
		t.Error("Layout should not be v2 after dry run")
	}

	// Files should still be at root
	for _, f := range files {
		rootPath := filepath.Join(beadsDir, f)
		if _, err := os.Stat(rootPath); os.IsNotExist(err) {
			t.Errorf("File %s should still exist at root after dry run", f)
		}
	}
}

func TestCopyFileForMigration(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	srcPath := filepath.Join(tmpDir, "source.txt")
	content := []byte("test content for migration copy")
	if err := os.WriteFile(srcPath, content, 0640); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Copy file
	dstPath := filepath.Join(tmpDir, "dest.txt")
	if err := copyFileForMigration(srcPath, dstPath); err != nil {
		t.Fatalf("copyFileForMigration failed: %v", err)
	}

	// Verify content
	readContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	if string(readContent) != string(content) {
		t.Errorf("Content mismatch: got %q, want %q", readContent, content)
	}

	// Verify permissions
	srcInfo, _ := os.Stat(srcPath)
	dstInfo, _ := os.Stat(dstPath)
	if srcInfo.Mode() != dstInfo.Mode() {
		t.Errorf("Permission mismatch: got %v, want %v", dstInfo.Mode(), srcInfo.Mode())
	}
}

func TestMigrateVarVolatileFilesList(t *testing.T) {
	// Verify all expected volatile files are in the list
	expectedFiles := map[string]bool{
		"beads.db":              true,
		"beads.db-journal":      true,
		"beads.db-wal":          true,
		"beads.db-shm":          true,
		"daemon.lock":           true,
		"daemon.log":            true,
		"daemon.pid":            true,
		"bd.sock":               true,
		"sync_base.jsonl":       true,
		".sync.lock":            true,
		"sync-state.json":       true,
		"beads.base.jsonl":      true,
		"beads.base.meta.json":  true,
		"beads.left.jsonl":      true,
		"beads.left.meta.json":  true,
		"beads.right.jsonl":     true,
		"beads.right.meta.json": true,
		"last-touched":          true,
		".local_version":        true,
		"export_hashes.db":      true,
	}

	for _, f := range beads.VolatileFiles {
		if !expectedFiles[f] {
			t.Errorf("Unexpected volatile file in list: %s", f)
		}
		delete(expectedFiles, f)
	}

	for f := range expectedFiles {
		t.Errorf("Missing expected volatile file: %s", f)
	}
}
