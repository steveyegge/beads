package beads

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVarPath(t *testing.T) {
	tests := map[string]struct {
		setupFunc   func(beadsDir string) // Setup function to create files/dirs
		layout      string                // Layout field value
		envLegacy   bool                  // Whether to set BD_LEGACY_LAYOUT=1
		filename    string                // File to look up
		wantSubpath string                // Expected subpath relative to beadsDir
	}{
		"legacy_layout_no_var_dir": {
			setupFunc:   func(beadsDir string) {},
			layout:      "",
			filename:    "beads.db",
			wantSubpath: "beads.db",
		},
		"legacy_layout_file_at_root": {
			setupFunc: func(beadsDir string) {
				_ = os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("test"), 0600)
			},
			layout:      "",
			filename:    "beads.db",
			wantSubpath: "beads.db",
		},
		"var_layout_file_in_var": {
			setupFunc: func(beadsDir string) {
				varDir := filepath.Join(beadsDir, "var")
				_ = os.MkdirAll(varDir, 0700)
				_ = os.WriteFile(filepath.Join(varDir, "beads.db"), []byte("test"), 0600)
			},
			layout:      LayoutV2,
			filename:    "beads.db",
			wantSubpath: "var/beads.db",
		},
		"var_layout_file_at_root_fallback": {
			setupFunc: func(beadsDir string) {
				varDir := filepath.Join(beadsDir, "var")
				_ = os.MkdirAll(varDir, 0700)
				_ = os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("test"), 0600)
			},
			layout:      LayoutV2,
			filename:    "beads.db",
			wantSubpath: "beads.db", // Falls back to root when file not in var/
		},
		"var_layout_file_in_both_prefers_var": {
			setupFunc: func(beadsDir string) {
				varDir := filepath.Join(beadsDir, "var")
				_ = os.MkdirAll(varDir, 0700)
				_ = os.WriteFile(filepath.Join(varDir, "beads.db"), []byte("var"), 0600)
				_ = os.WriteFile(filepath.Join(beadsDir, "beads.db"), []byte("root"), 0600)
			},
			layout:      LayoutV2,
			filename:    "beads.db",
			wantSubpath: "var/beads.db", // Prefers var/ when file exists in both
		},
		"var_layout_new_file_uses_var": {
			setupFunc: func(beadsDir string) {
				varDir := filepath.Join(beadsDir, "var")
				_ = os.MkdirAll(varDir, 0700)
			},
			layout:      LayoutV2,
			filename:    "beads.db",
			wantSubpath: "var/beads.db", // New file goes to var/
		},
		"env_override_forces_root": {
			setupFunc: func(beadsDir string) {
				varDir := filepath.Join(beadsDir, "var")
				_ = os.MkdirAll(varDir, 0700)
				_ = os.WriteFile(filepath.Join(varDir, "beads.db"), []byte("var"), 0600)
			},
			layout:      LayoutV2,
			envLegacy:   true,
			filename:    "beads.db",
			wantSubpath: "beads.db", // Env override forces legacy path
		},
		"var_dir_exists_but_layout_v1": {
			setupFunc: func(beadsDir string) {
				varDir := filepath.Join(beadsDir, "var")
				_ = os.MkdirAll(varDir, 0700)
			},
			layout:      LayoutV1,
			filename:    "beads.db",
			wantSubpath: "beads.db", // Explicit v1 layout uses root
		},
		"var_dir_exists_empty_layout_fallback": {
			setupFunc: func(beadsDir string) {
				varDir := filepath.Join(beadsDir, "var")
				_ = os.MkdirAll(varDir, 0700)
			},
			layout:      "", // Empty layout but var/ exists
			filename:    "beads.db",
			wantSubpath: "var/beads.db", // Falls back to checking var/ dir
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			// Create temp directory
			beadsDir := t.TempDir()

			// Setup the test environment
			if tt.setupFunc != nil {
				tt.setupFunc(beadsDir)
			}

			// Set environment variable if needed
			if tt.envLegacy {
				t.Setenv("BD_LEGACY_LAYOUT", "1")
			}

			// Call VarPath
			got := VarPath(beadsDir, tt.filename, tt.layout)
			want := filepath.Join(beadsDir, tt.wantSubpath)

			if got != want {
				t.Errorf("VarPath() = %q, want %q", got, want)
			}
		})
	}
}

func TestVarPathForWrite(t *testing.T) {
	tests := map[string]struct {
		setupFunc   func(beadsDir string)
		layout      string
		envLegacy   bool
		filename    string
		wantSubpath string
	}{
		"legacy_layout_writes_to_root": {
			setupFunc:   func(beadsDir string) {},
			layout:      "",
			filename:    "beads.db",
			wantSubpath: "beads.db",
		},
		"var_layout_writes_to_var": {
			setupFunc: func(beadsDir string) {
				varDir := filepath.Join(beadsDir, "var")
				_ = os.MkdirAll(varDir, 0700)
			},
			layout:      LayoutV2,
			filename:    "beads.db",
			wantSubpath: "var/beads.db",
		},
		"env_override_writes_to_root": {
			setupFunc: func(beadsDir string) {
				varDir := filepath.Join(beadsDir, "var")
				_ = os.MkdirAll(varDir, 0700)
			},
			layout:      LayoutV2,
			envLegacy:   true,
			filename:    "beads.db",
			wantSubpath: "beads.db",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			beadsDir := t.TempDir()

			if tt.setupFunc != nil {
				tt.setupFunc(beadsDir)
			}

			if tt.envLegacy {
				t.Setenv("BD_LEGACY_LAYOUT", "1")
			}

			got := VarPathForWrite(beadsDir, tt.filename, tt.layout)
			want := filepath.Join(beadsDir, tt.wantSubpath)

			if got != want {
				t.Errorf("VarPathForWrite() = %q, want %q", got, want)
			}
		})
	}
}

func TestVarDir(t *testing.T) {
	tests := map[string]struct {
		setupFunc   func(beadsDir string)
		layout      string
		wantSubpath string
	}{
		"legacy_returns_root": {
			setupFunc:   func(beadsDir string) {},
			layout:      "",
			wantSubpath: "",
		},
		"var_layout_returns_var": {
			setupFunc: func(beadsDir string) {
				varDir := filepath.Join(beadsDir, "var")
				_ = os.MkdirAll(varDir, 0700)
			},
			layout:      LayoutV2,
			wantSubpath: "var",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			beadsDir := t.TempDir()

			if tt.setupFunc != nil {
				tt.setupFunc(beadsDir)
			}

			got := VarDir(beadsDir, tt.layout)
			want := filepath.Join(beadsDir, tt.wantSubpath)

			if got != want {
				t.Errorf("VarDir() = %q, want %q", got, want)
			}
		})
	}
}

func TestIsVarLayout(t *testing.T) {
	tests := map[string]struct {
		setupFunc func(beadsDir string)
		layout    string
		envLegacy bool
		want      bool
	}{
		"v2_layout": {
			setupFunc: func(beadsDir string) {},
			layout:    LayoutV2,
			want:      true,
		},
		"v1_layout": {
			setupFunc: func(beadsDir string) {},
			layout:    LayoutV1,
			want:      false,
		},
		"empty_layout_no_var_dir": {
			setupFunc: func(beadsDir string) {},
			layout:    "",
			want:      false,
		},
		"empty_layout_with_var_dir": {
			setupFunc: func(beadsDir string) {
				_ = os.MkdirAll(filepath.Join(beadsDir, "var"), 0700)
			},
			layout: "",
			want:   true,
		},
		"env_override": {
			setupFunc: func(beadsDir string) {
				_ = os.MkdirAll(filepath.Join(beadsDir, "var"), 0700)
			},
			layout:    LayoutV2,
			envLegacy: true,
			want:      false,
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			beadsDir := t.TempDir()

			if tt.setupFunc != nil {
				tt.setupFunc(beadsDir)
			}

			if tt.envLegacy {
				t.Setenv("BD_LEGACY_LAYOUT", "1")
			}

			got := IsVarLayout(beadsDir, tt.layout)
			if got != tt.want {
				t.Errorf("IsVarLayout() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureVarDir(t *testing.T) {
	beadsDir := t.TempDir()

	// Call EnsureVarDir
	err := EnsureVarDir(beadsDir)
	if err != nil {
		t.Fatalf("EnsureVarDir() error = %v", err)
	}

	// Verify var/ was created
	varDir := filepath.Join(beadsDir, "var")
	info, err := os.Stat(varDir)
	if err != nil {
		t.Fatalf("var/ directory not created: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("var is not a directory")
	}

	// Call again to verify idempotence
	err = EnsureVarDir(beadsDir)
	if err != nil {
		t.Fatalf("EnsureVarDir() on existing dir error = %v", err)
	}
}

func TestIsVolatileFile(t *testing.T) {
	tests := map[string]struct {
		filename string
		want     bool
	}{
		"beads.db":              {"beads.db", true},
		"daemon.lock":           {"daemon.lock", true},
		"daemon.log":            {"daemon.log", true},
		"daemon.pid":            {"daemon.pid", true},
		"bd.sock":               {"bd.sock", true},
		"sync_base.jsonl":       {"sync_base.jsonl", true},
		"sync.lock":             {".sync.lock", true},
		"sync-state.json":       {"sync-state.json", true},
		"last-touched":          {"last-touched", true},
		"local_version":         {".local_version", true},
		"export_hashes.db":      {"export_hashes.db", true},
		"beads.db-journal":      {"beads.db-journal", true},
		"beads.db-wal":          {"beads.db-wal", true},
		"beads.db-shm":          {"beads.db-shm", true},
		"issues.jsonl":          {"issues.jsonl", false},
		"metadata.json":         {"metadata.json", false},
		"interactions.jsonl":    {"interactions.jsonl", false},
		"redirect":              {"redirect", false},
		"config.yaml":           {"config.yaml", false},
		"random.db-suffix":      {"random.db-suffix", true}, // Glob pattern match
		"something.db-anything": {"something.db-anything", true},
		"beads.base.jsonl":      {"beads.base.jsonl", true},
		"beads.left.jsonl":      {"beads.left.jsonl", true},
		"beads.right.jsonl":     {"beads.right.jsonl", true},
		"beads.base.meta.json":  {"beads.base.meta.json", true},
		"beads.left.meta.json":  {"beads.left.meta.json", true},
		"beads.right.meta.json": {"beads.right.meta.json", true},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got := IsVolatileFile(tt.filename)
			if got != tt.want {
				t.Errorf("IsVolatileFile(%q) = %v, want %v", tt.filename, got, tt.want)
			}
		})
	}
}
