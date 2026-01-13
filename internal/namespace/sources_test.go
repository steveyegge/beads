package namespace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSourcesConfig(t *testing.T) {
	cfg := &SourcesConfig{
		Sources: map[string]SourceConfig{
			"beads": {
				Upstream: "github.com/steveyegge/beads",
				Fork:     "github.com/matt/beads",
			},
		},
	}

	// Test GetSourceURL
	t.Run("GetSourceURL prefers fork over upstream", func(t *testing.T) {
		beadsCfg := cfg.Sources["beads"]
		url := beadsCfg.GetSourceURL()
		if url != "github.com/matt/beads" {
			t.Errorf("expected fork URL, got %q", url)
		}
	})

	t.Run("GetSourceURL falls back to upstream", func(t *testing.T) {
		upstreamCfg := SourceConfig{Upstream: "github.com/other/project"}
		url := upstreamCfg.GetSourceURL()
		if url != "github.com/other/project" {
			t.Errorf("expected upstream URL, got %q", url)
		}
	})

	t.Run("GetSourceURL prefers local over fork", func(t *testing.T) {
		localCfg := SourceConfig{
			Upstream: "github.com/steveyegge/beads",
			Fork:     "github.com/matt/beads",
			Local:    "/home/user/beads-local",
		}
		url := localCfg.GetSourceURL()
		if url != "/home/user/beads-local" {
			t.Errorf("expected local URL, got %q", url)
		}
	})
}

func TestAddProject(t *testing.T) {
	cfg := &SourcesConfig{Sources: make(map[string]SourceConfig)}

	err := cfg.AddProject("beads", "github.com/steveyegge/beads")
	if err != nil {
		t.Fatalf("AddProject() error: %v", err)
	}

	if cfg.Sources["beads"].Upstream != "github.com/steveyegge/beads" {
		t.Errorf("upstream not set correctly")
	}
}

func TestSetProjectFork(t *testing.T) {
	cfg := &SourcesConfig{
		Sources: map[string]SourceConfig{
			"beads": {
				Upstream: "github.com/steveyegge/beads",
			},
		},
	}

	err := cfg.SetProjectFork("beads", "github.com/matt/beads")
	if err != nil {
		t.Fatalf("SetProjectFork() error: %v", err)
	}

	if cfg.Sources["beads"].Fork != "github.com/matt/beads" {
		t.Errorf("fork not set correctly")
	}
}

func TestSaveAndLoadSourcesConfig(t *testing.T) {
	// Create temporary directory
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatalf("failed to create temp .beads dir: %v", err)
	}

	// Create and save config
	originalCfg := &SourcesConfig{
		Sources: map[string]SourceConfig{
			"beads": {
				Upstream: "github.com/steveyegge/beads",
				Fork:     "github.com/matt/beads",
			},
		},
	}

	if err := SaveSourcesConfig(beadsDir, originalCfg); err != nil {
		t.Fatalf("SaveSourcesConfig() error: %v", err)
	}

	// Load and verify
	loadedCfg, err := LoadSourcesConfig(beadsDir)
	if err != nil {
		t.Fatalf("LoadSourcesConfig() error: %v", err)
	}

	if loadedCfg.Sources["beads"].Upstream != "github.com/steveyegge/beads" {
		t.Errorf("upstream not loaded correctly")
	}
	if loadedCfg.Sources["beads"].Fork != "github.com/matt/beads" {
		t.Errorf("fork not loaded correctly")
	}
}

func TestLoadSourcesConfigFileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")

	// Don't create the directory - should return empty config, not error
	cfg, err := LoadSourcesConfig(beadsDir)
	if err != nil {
		t.Fatalf("LoadSourcesConfig() should not error when file missing: %v", err)
	}

	if len(cfg.Sources) != 0 {
		t.Errorf("expected empty sources map, got %d entries", len(cfg.Sources))
	}
}

func TestGetProject(t *testing.T) {
	cfg := &SourcesConfig{
		Sources: map[string]SourceConfig{
			"beads": {
				Upstream: "github.com/steveyegge/beads",
			},
		},
	}

	// Existing project
	project, err := cfg.GetProject("beads")
	if err != nil {
		t.Fatalf("GetProject() error: %v", err)
	}
	if project.Upstream != "github.com/steveyegge/beads" {
		t.Errorf("got wrong upstream")
	}

	// Non-existent project
	_, err = cfg.GetProject("nonexistent")
	if err == nil {
		t.Errorf("GetProject() should error for non-existent project")
	}
}

func TestValidateSourceConfig(t *testing.T) {
	tests := []struct {
		name      string
		cfg       SourceConfig
		expectErr bool
	}{
		{
			name:      "valid with upstream",
			cfg:       SourceConfig{Upstream: "github.com/steveyegge/beads"},
			expectErr: false,
		},
		{
			name:      "valid with upstream and fork",
			cfg:       SourceConfig{Upstream: "github.com/steveyegge/beads", Fork: "github.com/matt/beads"},
			expectErr: false,
		},
		{
			name:      "invalid empty upstream",
			cfg:       SourceConfig{},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.expectErr {
				t.Errorf("Validate() error = %v, expectErr %v", err, tt.expectErr)
			}
		})
	}
}
