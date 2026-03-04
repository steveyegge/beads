package configfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Database != "beads.db" {
		t.Errorf("Database = %q, want beads.db", cfg.Database)
	}
}

func TestLoadSaveRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("failed to create .beads directory: %v", err)
	}

	cfg := DefaultConfig()

	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	loaded, err := Load(beadsDir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if loaded == nil {
		t.Fatal("Load() returned nil config")
	}

	if loaded.Database != cfg.Database {
		t.Errorf("Database = %q, want %q", loaded.Database, cfg.Database)
	}
}

func TestLoadNonexistent(t *testing.T) {
	tmpDir := t.TempDir()

	cfg, err := Load(tmpDir)
	if err != nil {
		t.Fatalf("Load() returned error for nonexistent config: %v", err)
	}

	if cfg != nil {
		t.Errorf("Load() = %v, want nil for nonexistent config", cfg)
	}
}

func TestDatabasePath(t *testing.T) {
	beadsDir := "/home/user/project/.beads"
	// DatabasePath always returns dolt path regardless of Database field
	cfg := &Config{Database: "beads.db"}

	got := cfg.DatabasePath(beadsDir)
	want := filepath.Join(beadsDir, "dolt")

	if got != want {
		t.Errorf("DatabasePath() = %q, want %q", got, want)
	}
}

func TestDatabasePath_Dolt(t *testing.T) {
	beadsDir := "/home/user/project/.beads"

	t.Run("explicit dolt dir", func(t *testing.T) {
		cfg := &Config{Database: "dolt"}
		got := cfg.DatabasePath(beadsDir)
		want := filepath.Join(beadsDir, "dolt")
		if got != want {
			t.Errorf("DatabasePath() = %q, want %q", got, want)
		}
	})

	t.Run("backward compat: beads.db field", func(t *testing.T) {
		cfg := &Config{Database: "beads.db"}
		got := cfg.DatabasePath(beadsDir)
		want := filepath.Join(beadsDir, "dolt")
		if got != want {
			t.Errorf("DatabasePath() = %q, want %q", got, want)
		}
	})

	t.Run("stale database name is ignored (split-brain fix)", func(t *testing.T) {
		// Stale values like "town", "wyvern", "beads_rig" must resolve to "dolt"
		for _, staleName := range []string{"town", "wyvern", "beads_rig", "random"} {
			cfg := &Config{Database: staleName}
			got := cfg.DatabasePath(beadsDir)
			want := filepath.Join(beadsDir, "dolt")
			if got != want {
				t.Errorf("DatabasePath(%q) = %q, want %q", staleName, got, want)
			}
		}
	})

	t.Run("empty database field resolves to dolt", func(t *testing.T) {
		cfg := &Config{Database: ""}
		got := cfg.DatabasePath(beadsDir)
		want := filepath.Join(beadsDir, "dolt")
		if got != want {
			t.Errorf("DatabasePath() = %q, want %q", got, want)
		}
	})

	t.Run("absolute path is honored", func(t *testing.T) {
		cfg := &Config{Database: "/custom/path/dolt"}
		got := cfg.DatabasePath(beadsDir)
		want := "/custom/path/dolt"
		if got != want {
			t.Errorf("DatabasePath() = %q, want %q", got, want)
		}
	})
}

func TestConfigPath(t *testing.T) {
	beadsDir := "/home/user/project/.beads"
	got := ConfigPath(beadsDir)
	want := filepath.Join(beadsDir, "metadata.json")

	if got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestGetDeletionsRetentionDays(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{
			name: "zero uses default",
			cfg:  &Config{DeletionsRetentionDays: 0},
			want: DefaultDeletionsRetentionDays,
		},
		{
			name: "negative uses default",
			cfg:  &Config{DeletionsRetentionDays: -5},
			want: DefaultDeletionsRetentionDays,
		},
		{
			name: "custom value",
			cfg:  &Config{DeletionsRetentionDays: 14},
			want: 14,
		},
		{
			name: "minimum value 1",
			cfg:  &Config{DeletionsRetentionDays: 1},
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetDeletionsRetentionDays()
			if got != tt.want {
				t.Errorf("GetDeletionsRetentionDays() = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestDoltServerMode tests the Dolt server mode configuration (bd-dolt.2.2)
func TestDoltServerMode(t *testing.T) {
	t.Run("GetDoltServerHost", func(t *testing.T) {
		tests := []struct {
			name string
			cfg  *Config
			want string
		}{
			{
				name: "empty defaults to 127.0.0.1",
				cfg:  &Config{},
				want: DefaultDoltServerHost,
			},
			{
				name: "custom host",
				cfg:  &Config{DoltServerHost: "192.168.1.100"},
				want: "192.168.1.100",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := tt.cfg.GetDoltServerHost()
				if got != tt.want {
					t.Errorf("GetDoltServerHost() = %q, want %q", got, tt.want)
				}
			})
		}
	})

	t.Run("GetDoltServerPort", func(t *testing.T) {
		tests := []struct {
			name string
			cfg  *Config
			want int
		}{
			{
				name: "zero defaults to 3307",
				cfg:  &Config{},
				want: DefaultDoltServerPort,
			},
			{
				name: "custom port",
				cfg:  &Config{DoltServerPort: 13306},
				want: 13306,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := tt.cfg.GetDoltServerPort()
				if got != tt.want {
					t.Errorf("GetDoltServerPort() = %d, want %d", got, tt.want)
				}
			})
		}
	})

	t.Run("GetDoltServerUser", func(t *testing.T) {
		tests := []struct {
			name string
			cfg  *Config
			want string
		}{
			{
				name: "empty defaults to root",
				cfg:  &Config{},
				want: DefaultDoltServerUser,
			},
			{
				name: "custom user",
				cfg:  &Config{DoltServerUser: "beads"},
				want: "beads",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := tt.cfg.GetDoltServerUser()
				if got != tt.want {
					t.Errorf("GetDoltServerUser() = %q, want %q", got, tt.want)
				}
			})
		}
	})
}

// TestDatabasePathAlwaysDolt tests that DatabasePath always returns the dolt path.
func TestDatabasePathAlwaysDolt(t *testing.T) {
	beadsDir := "/home/user/project/.beads"

	cfg := &Config{Database: "beads.db"}
	got := cfg.DatabasePath(beadsDir)
	want := filepath.Join(beadsDir, "dolt")
	if got != want {
		t.Errorf("DatabasePath() = %q, want %q", got, want)
	}
}

// TestGetCapabilities tests that GetCapabilities always returns multi-writer capable.
func TestGetCapabilities(t *testing.T) {
	cfg := &Config{}
	got := cfg.GetCapabilities()
	if got.SingleProcessOnly {
		t.Error("GetCapabilities().SingleProcessOnly = true, want false (always multi-writer)")
	}
}

// TestDoltServerModeRoundtrip tests that server connection config survives save/load
func TestDoltServerModeRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("failed to create .beads directory: %v", err)
	}

	cfg := &Config{
		Database:       "dolt",
		DoltServerHost: "192.168.1.50",
		DoltServerPort: 13306,
		DoltServerUser: "beads_admin",
	}

	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	loaded, err := Load(beadsDir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}

	if loaded.GetDoltServerHost() != "192.168.1.50" {
		t.Errorf("GetDoltServerHost() = %q, want %q", loaded.GetDoltServerHost(), "192.168.1.50")
	}
	if loaded.GetDoltServerPort() != 13306 {
		t.Errorf("GetDoltServerPort() = %d, want %d", loaded.GetDoltServerPort(), 13306)
	}
	if loaded.GetDoltServerUser() != "beads_admin" {
		t.Errorf("GetDoltServerUser() = %q, want %q", loaded.GetDoltServerUser(), "beads_admin")
	}
}

// TestEnvVarOverrides tests env var overrides for getter methods
func TestEnvVarOverrides(t *testing.T) {
	t.Run("host env var overrides config", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_SERVER_HOST", "192.168.1.1")
		cfg := &Config{DoltServerHost: "10.0.0.1"}
		if got := cfg.GetDoltServerHost(); got != "192.168.1.1" {
			t.Errorf("GetDoltServerHost() = %q, want 192.168.1.1", got)
		}
	})

	t.Run("port env var overrides config", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_SERVER_PORT", "3309")
		cfg := &Config{DoltServerPort: 3308}
		if got := cfg.GetDoltServerPort(); got != 3309 {
			t.Errorf("GetDoltServerPort() = %d, want 3309", got)
		}
	})

	t.Run("invalid port env var falls through to config", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_SERVER_PORT", "not-a-number")
		cfg := &Config{DoltServerPort: 3308}
		if got := cfg.GetDoltServerPort(); got != 3308 {
			t.Errorf("GetDoltServerPort() = %d, want 3308", got)
		}
	})

	t.Run("user env var overrides config", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_SERVER_USER", "envuser")
		cfg := &Config{DoltServerUser: "admin"}
		if got := cfg.GetDoltServerUser(); got != "envuser" {
			t.Errorf("GetDoltServerUser() = %q, want envuser", got)
		}
	})

	t.Run("database env var overrides config", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_SERVER_DATABASE", "envdb")
		cfg := &Config{DoltDatabase: "mydb"}
		if got := cfg.GetDoltDatabase(); got != "envdb" {
			t.Errorf("GetDoltDatabase() = %q, want envdb", got)
		}
	})

	t.Run("database default", func(t *testing.T) {
		cfg := &Config{}
		if got := cfg.GetDoltDatabase(); got != DefaultDoltDatabase {
			t.Errorf("GetDoltDatabase() = %q, want %q", got, DefaultDoltDatabase)
		}
	})

	t.Run("database config value", func(t *testing.T) {
		cfg := &Config{DoltDatabase: "mydb"}
		if got := cfg.GetDoltDatabase(); got != "mydb" {
			t.Errorf("GetDoltDatabase() = %q, want mydb", got)
		}
	})
}
