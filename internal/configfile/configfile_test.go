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

func TestJSONLPath(t *testing.T) {
	beadsDir := "/home/user/project/.beads"
	
	tests := []struct {
		name       string
		cfg        *Config
		want       string
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
		name   string
		cfg    *Config
		want   int
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

func TestGetExportFormat(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "empty defaults to jsonl",
			cfg:  &Config{ExportFormat: ""},
			want: "jsonl",
		},
		{
			name: "jsonl explicit",
			cfg:  &Config{ExportFormat: "jsonl"},
			want: "jsonl",
		},
		{
			name: "toon explicit",
			cfg:  &Config{ExportFormat: "toon"},
			want: "toon",
		},
		{
			name: "invalid format defaults to jsonl",
			cfg:  &Config{ExportFormat: "invalid"},
			want: "jsonl",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetExportFormat()
			if got != tt.want {
				t.Errorf("GetExportFormat() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetExportFilename(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "empty defaults to issues.jsonl",
			cfg:  &Config{ExportFormat: "", JSONLExport: ""},
			want: "issues.jsonl",
		},
		{
			name: "jsonl format",
			cfg:  &Config{ExportFormat: "jsonl", JSONLExport: ""},
			want: "issues.jsonl",
		},
		{
			name: "toon format",
			cfg:  &Config{ExportFormat: "toon", JSONLExport: ""},
			want: "issues.toon",
		},
		{
			name: "custom JSONLExport takes precedence",
			cfg:  &Config{ExportFormat: "toon", JSONLExport: "custom.jsonl"},
			want: "custom.jsonl",
		},
		{
			name: "default JSONLExport ignored when not custom",
			cfg:  &Config{ExportFormat: "toon", JSONLExport: "issues.jsonl"},
			want: "issues.toon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetExportFilename()
			if got != tt.want {
				t.Errorf("GetExportFilename() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetExportPath(t *testing.T) {
	beadsDir := "/home/user/project/.beads"

	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "jsonl format",
			cfg:  &Config{ExportFormat: "jsonl"},
			want: filepath.Join(beadsDir, "issues.jsonl"),
		},
		{
			name: "toon format",
			cfg:  &Config{ExportFormat: "toon"},
			want: filepath.Join(beadsDir, "issues.toon"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetExportPath(beadsDir)
			if got != tt.want {
				t.Errorf("GetExportPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
