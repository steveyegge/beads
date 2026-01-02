package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadLocalConfig(t *testing.T) {
	tests := []struct {
		name       string
		configYAML string
		wantNoDb   bool
		wantBranch string
		wantPrefix string
	}{
		{
			name:       "empty config",
			configYAML: "",
			wantNoDb:   false,
			wantBranch: "",
			wantPrefix: "",
		},
		{
			name:       "no-db true",
			configYAML: "no-db: true\n",
			wantNoDb:   true,
			wantBranch: "",
			wantPrefix: "",
		},
		{
			name:       "no-db false",
			configYAML: "no-db: false\n",
			wantNoDb:   false,
			wantBranch: "",
			wantPrefix: "",
		},
		{
			name:       "no-db in comment should not match",
			configYAML: "# no-db: true\nissue-prefix: test\n",
			wantNoDb:   false,
			wantBranch: "",
			wantPrefix: "test",
		},
		{
			name:       "sync-branch without quotes",
			configYAML: "sync-branch: my-branch\n",
			wantNoDb:   false,
			wantBranch: "my-branch",
			wantPrefix: "",
		},
		{
			name:       "sync-branch with double quotes",
			configYAML: `sync-branch: "my-quoted-branch"` + "\n",
			wantNoDb:   false,
			wantBranch: "my-quoted-branch",
			wantPrefix: "",
		},
		{
			name:       "sync-branch with single quotes",
			configYAML: `sync-branch: 'single-quoted'` + "\n",
			wantNoDb:   false,
			wantBranch: "single-quoted",
			wantPrefix: "",
		},
		{
			name:       "mixed config",
			configYAML: "issue-prefix: bd\nno-db: true\nauthor: steve\nsync-branch: beads-sync\n",
			wantNoDb:   true,
			wantBranch: "beads-sync",
			wantPrefix: "bd",
		},
		{
			name:       "sync-branch indented under section (not top-level)",
			configYAML: "settings:\n  sync-branch: nested-branch\n",
			wantNoDb:   false,
			wantBranch: "", // Only top-level sync-branch should be read
			wantPrefix: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create temp directory
			tmpDir := t.TempDir()

			// Create config.yaml if content is provided
			if tt.configYAML != "" {
				configPath := filepath.Join(tmpDir, "config.yaml")
				if err := os.WriteFile(configPath, []byte(tt.configYAML), 0600); err != nil {
					t.Fatalf("Failed to write config.yaml: %v", err)
				}
			}

			cfg := LoadLocalConfig(tmpDir)

			if cfg.NoDb != tt.wantNoDb {
				t.Errorf("NoDb = %v, want %v", cfg.NoDb, tt.wantNoDb)
			}
			if cfg.SyncBranch != tt.wantBranch {
				t.Errorf("SyncBranch = %q, want %q", cfg.SyncBranch, tt.wantBranch)
			}
			if cfg.IssuePrefix != tt.wantPrefix {
				t.Errorf("IssuePrefix = %q, want %q", cfg.IssuePrefix, tt.wantPrefix)
			}
		})
	}
}

func TestLoadLocalConfigWithEnv(t *testing.T) {
	// Create temp directory with config.yaml
	tmpDir := t.TempDir()
	configYAML := "sync-branch: config-branch\n"
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(configYAML), 0600); err != nil {
		t.Fatalf("Failed to write config.yaml: %v", err)
	}

	t.Run("env var overrides config file", func(t *testing.T) {
		os.Setenv("BEADS_SYNC_BRANCH", "env-branch")
		defer os.Unsetenv("BEADS_SYNC_BRANCH")

		cfg := LoadLocalConfigWithEnv(tmpDir)
		if cfg.SyncBranch != "env-branch" {
			t.Errorf("SyncBranch = %q, want %q (env var should override)", cfg.SyncBranch, "env-branch")
		}
	})

	t.Run("no env var uses config file", func(t *testing.T) {
		os.Unsetenv("BEADS_SYNC_BRANCH")

		cfg := LoadLocalConfigWithEnv(tmpDir)
		if cfg.SyncBranch != "config-branch" {
			t.Errorf("SyncBranch = %q, want %q", cfg.SyncBranch, "config-branch")
		}
	})
}

func TestIsNoDbModeConfigured(t *testing.T) {
	t.Run("returns true when no-db: true", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte("no-db: true\n"), 0600); err != nil {
			t.Fatalf("Failed to write config.yaml: %v", err)
		}

		if !IsNoDbModeConfigured(tmpDir) {
			t.Error("IsNoDbModeConfigured() = false, want true")
		}
	})

	t.Run("returns false when no-db: false", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte("no-db: false\n"), 0600); err != nil {
			t.Fatalf("Failed to write config.yaml: %v", err)
		}

		if IsNoDbModeConfigured(tmpDir) {
			t.Error("IsNoDbModeConfigured() = true, want false")
		}
	})

	t.Run("returns false when no config file", func(t *testing.T) {
		tmpDir := t.TempDir()

		if IsNoDbModeConfigured(tmpDir) {
			t.Error("IsNoDbModeConfigured() = true, want false (no config file)")
		}
	})
}

func TestGetLocalSyncBranch(t *testing.T) {
	t.Run("returns sync-branch from config", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte("sync-branch: beads-sync\n"), 0600); err != nil {
			t.Fatalf("Failed to write config.yaml: %v", err)
		}

		branch := GetLocalSyncBranch(tmpDir)
		if branch != "beads-sync" {
			t.Errorf("GetLocalSyncBranch() = %q, want %q", branch, "beads-sync")
		}
	})

	t.Run("env var takes precedence", func(t *testing.T) {
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "config.yaml")
		if err := os.WriteFile(configPath, []byte("sync-branch: config-value\n"), 0600); err != nil {
			t.Fatalf("Failed to write config.yaml: %v", err)
		}

		os.Setenv("BEADS_SYNC_BRANCH", "env-value")
		defer os.Unsetenv("BEADS_SYNC_BRANCH")

		branch := GetLocalSyncBranch(tmpDir)
		if branch != "env-value" {
			t.Errorf("GetLocalSyncBranch() = %q, want %q (env var should take precedence)", branch, "env-value")
		}
	})
}
