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

	// bd-6xd: issues.jsonl is the canonical name
	if cfg.JSONLExport != "issues.jsonl" {
		t.Errorf("JSONLExport = %q, want issues.jsonl", cfg.JSONLExport)
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

	if loaded.JSONLExport != cfg.JSONLExport {
		t.Errorf("JSONLExport = %q, want %q", loaded.JSONLExport, cfg.JSONLExport)
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
	cfg := &Config{Database: "beads.db"}

	got := cfg.DatabasePath(beadsDir)
	want := filepath.Join(beadsDir, "beads.db")

	if got != want {
		t.Errorf("DatabasePath() = %q, want %q", got, want)
	}
}

func TestDatabasePath_Dolt(t *testing.T) {
	beadsDir := "/home/user/project/.beads"

	t.Run("explicit dolt dir", func(t *testing.T) {
		cfg := &Config{Database: "dolt", Backend: BackendDolt}
		got := cfg.DatabasePath(beadsDir)
		want := filepath.Join(beadsDir, "dolt")
		if got != want {
			t.Errorf("DatabasePath() = %q, want %q", got, want)
		}
	})

	t.Run("backward compat: dolt backend with beads.db field", func(t *testing.T) {
		cfg := &Config{Database: "beads.db", Backend: BackendDolt}
		got := cfg.DatabasePath(beadsDir)
		want := filepath.Join(beadsDir, "dolt")
		if got != want {
			t.Errorf("DatabasePath() = %q, want %q", got, want)
		}
	})
}

func TestJSONLPath(t *testing.T) {
	beadsDir := "/home/user/project/.beads"

	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "default",
			cfg:  &Config{JSONLExport: "issues.jsonl"},
			want: filepath.Join(beadsDir, "issues.jsonl"),
		},
		{
			name: "custom",
			cfg:  &Config{JSONLExport: "custom.jsonl"},
			want: filepath.Join(beadsDir, "custom.jsonl"),
		},
		{
			name: "empty falls back to default",
			cfg:  &Config{JSONLExport: ""},
			want: filepath.Join(beadsDir, "issues.jsonl"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.JSONLPath(beadsDir)
			if got != tt.want {
				t.Errorf("JSONLPath() = %q, want %q", got, tt.want)
			}
		})
	}
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
	t.Run("IsDoltServerMode", func(t *testing.T) {
		tests := []struct {
			name string
			cfg  *Config
			want bool
		}{
			{
				name: "sqlite backend",
				cfg:  &Config{Backend: BackendSQLite},
				want: false,
			},
			{
				name: "dolt embedded mode",
				cfg:  &Config{Backend: BackendDolt, DoltMode: DoltModeEmbedded},
				want: false,
			},
			{
				name: "dolt server mode",
				cfg:  &Config{Backend: BackendDolt, DoltMode: DoltModeServer},
				want: true,
			},
			{
				name: "dolt default mode",
				cfg:  &Config{Backend: BackendDolt},
				want: false, // Default is embedded
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := tt.cfg.IsDoltServerMode()
				if got != tt.want {
					t.Errorf("IsDoltServerMode() = %v, want %v", got, tt.want)
				}
			})
		}
	})

	t.Run("GetDoltMode", func(t *testing.T) {
		tests := []struct {
			name string
			cfg  *Config
			want string
		}{
			{
				name: "empty defaults to embedded",
				cfg:  &Config{},
				want: DoltModeEmbedded,
			},
			{
				name: "explicit embedded",
				cfg:  &Config{DoltMode: DoltModeEmbedded},
				want: DoltModeEmbedded,
			},
			{
				name: "explicit server",
				cfg:  &Config{DoltMode: DoltModeServer},
				want: DoltModeServer,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := tt.cfg.GetDoltMode()
				if got != tt.want {
					t.Errorf("GetDoltMode() = %q, want %q", got, tt.want)
				}
			})
		}
	})

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
				name: "zero defaults to 3306",
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

// TestDoltServerModeRoundtrip tests that server mode config survives save/load
func TestDoltServerModeRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("failed to create .beads directory: %v", err)
	}

	cfg := &Config{
		Database:       "dolt",
		Backend:        BackendDolt,
		DoltMode:       DoltModeServer,
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

	if !loaded.IsDoltServerMode() {
		t.Error("IsDoltServerMode() = false after load, want true")
	}
	if loaded.GetDoltMode() != DoltModeServer {
		t.Errorf("GetDoltMode() = %q, want %q", loaded.GetDoltMode(), DoltModeServer)
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
