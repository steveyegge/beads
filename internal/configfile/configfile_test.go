package configfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/config"
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

	t.Run("stale database name is ignored (split-brain fix)", func(t *testing.T) {
		// Stale values like "town", "wyvern", "beads_rig" must resolve to "dolt"
		for _, staleName := range []string{"town", "wyvern", "beads_rig", "random"} {
			cfg := &Config{Database: staleName, Backend: BackendDolt}
			got := cfg.DatabasePath(beadsDir)
			want := filepath.Join(beadsDir, "dolt")
			if got != want {
				t.Errorf("DatabasePath(%q) = %q, want %q", staleName, got, want)
			}
		}
	})

	t.Run("empty database field resolves to dolt", func(t *testing.T) {
		cfg := &Config{Database: "", Backend: BackendDolt}
		got := cfg.DatabasePath(beadsDir)
		want := filepath.Join(beadsDir, "dolt")
		if got != want {
			t.Errorf("DatabasePath() = %q, want %q", got, want)
		}
	})

	t.Run("absolute path is honored", func(t *testing.T) {
		cfg := &Config{Database: "/custom/path/dolt", Backend: BackendDolt}
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
	t.Run("IsDoltServerMode", func(t *testing.T) {
		tests := []struct {
			name string
			cfg  *Config
			want bool
		}{
			{
				name: "empty backend",
				cfg:  &Config{Backend: ""},
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

	t.Run("GetDoltServerHost_config_yaml", func(t *testing.T) {
		// Mirror of the dolt.port config.yaml fix (GH#2073) for host.
		// Precedence: env > metadata.json > config.yaml > default.

		// Ensure no host env var leaks into the test.
		t.Setenv("BEADS_DOLT_SERVER_HOST", "")

		configDir := t.TempDir()
		configYaml := filepath.Join(configDir, "config.yaml")
		if err := os.WriteFile(configYaml,
			[]byte("dolt.host: 100.64.0.1\n"), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("BEADS_DIR", configDir)
		if err := config.Initialize(); err != nil {
			t.Fatalf("config.Initialize: %v", err)
		}
		t.Cleanup(config.ResetForTesting)

		// config.yaml wins when metadata.json leaves host unset.
		emptyCfg := &Config{}
		if got := emptyCfg.GetDoltServerHost(); got != "100.64.0.1" {
			t.Errorf("empty cfg + config.yaml: GetDoltServerHost() = %q, want 100.64.0.1", got)
		}

		// metadata.json wins over config.yaml when both set.
		metaCfg := &Config{DoltServerHost: "192.168.1.100"}
		if got := metaCfg.GetDoltServerHost(); got != "192.168.1.100" {
			t.Errorf("metadata over config.yaml: GetDoltServerHost() = %q, want 192.168.1.100", got)
		}

		// env var wins over config.yaml.
		t.Setenv("BEADS_DOLT_SERVER_HOST", "10.0.0.1")
		if got := emptyCfg.GetDoltServerHost(); got != "10.0.0.1" {
			t.Errorf("env over config.yaml: GetDoltServerHost() = %q, want 10.0.0.1", got)
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

// TestIsDoltServerModeEnvVar tests env var overrides for IsDoltServerMode
func TestIsDoltServerModeEnvVar(t *testing.T) {
	t.Run("env var override with dolt backend", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_SERVER_MODE", "1")
		cfg := &Config{Backend: BackendDolt}
		if !cfg.IsDoltServerMode() {
			t.Error("IsDoltServerMode() = false, want true when env var set with dolt backend")
		}
	})

	t.Run("env var with dolt backend enables server mode", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_SERVER_MODE", "1")
		cfg := &Config{Backend: ""}
		if !cfg.IsDoltServerMode() {
			t.Error("IsDoltServerMode() = false, want true when env var set with default backend")
		}
	})

	t.Run("env var not set", func(t *testing.T) {
		cfg := &Config{Backend: BackendDolt}
		if cfg.IsDoltServerMode() {
			t.Error("IsDoltServerMode() = true, want false when no config or env var")
		}
	})
}

// TestDoltProxiedServerMode covers the IsDoltProxiedServerMode predicate and
// the GetCapabilities branch that treats proxied-server as multi-process-safe
// (the proxy daemon serializes writers).
func TestDoltProxiedServerMode(t *testing.T) {
	t.Run("IsDoltProxiedServerMode", func(t *testing.T) {
		tests := []struct {
			name string
			cfg  *Config
			want bool
		}{
			{
				name: "empty backend",
				cfg:  &Config{Backend: ""},
				want: false,
			},
			{
				name: "embedded mode",
				cfg:  &Config{Backend: BackendDolt, DoltMode: DoltModeEmbedded},
				want: false,
			},
			{
				name: "server mode",
				cfg:  &Config{Backend: BackendDolt, DoltMode: DoltModeServer},
				want: false,
			},
			{
				name: "proxied-server mode",
				cfg:  &Config{Backend: BackendDolt, DoltMode: DoltModeProxiedServer},
				want: true,
			},
			{
				name: "proxied-server, mixed case",
				cfg:  &Config{Backend: BackendDolt, DoltMode: "Proxied-Server"},
				want: true,
			},
			{
				name: "default (no DoltMode)",
				cfg:  &Config{Backend: BackendDolt},
				want: false,
			},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				if got := tc.cfg.IsDoltProxiedServerMode(); got != tc.want {
					t.Errorf("IsDoltProxiedServerMode() = %v, want %v", got, tc.want)
				}
			})
		}
	})

	t.Run("ServerAndProxiedAreMutuallyExclusive", func(t *testing.T) {
		cfg := &Config{Backend: BackendDolt, DoltMode: DoltModeProxiedServer}
		if cfg.IsDoltServerMode() {
			t.Error("IsDoltServerMode() should be false for proxied-server mode")
		}
		if !cfg.IsDoltProxiedServerMode() {
			t.Error("IsDoltProxiedServerMode() should be true for proxied-server mode")
		}
	})

	t.Run("GetCapabilities_ProxiedServerNotSingleProcess", func(t *testing.T) {
		cfg := &Config{Backend: BackendDolt, DoltMode: DoltModeProxiedServer}
		caps := cfg.GetCapabilities()
		if caps.SingleProcessOnly {
			t.Error("proxied-server should report SingleProcessOnly=false (proxy multiplexes writers)")
		}
	})

	t.Run("GetDoltModePreservesProxiedValue", func(t *testing.T) {
		cfg := &Config{Backend: BackendDolt, DoltMode: DoltModeProxiedServer}
		if got := cfg.GetDoltMode(); got != DoltModeProxiedServer {
			t.Errorf("GetDoltMode() = %q, want %q", got, DoltModeProxiedServer)
		}
	})

	t.Run("RoundtripPersistsProxiedMode", func(t *testing.T) {
		dir := t.TempDir()
		original := &Config{
			Database:     "dolt",
			Backend:      BackendDolt,
			DoltMode:     DoltModeProxiedServer,
			DoltDatabase: "myproj",
			ProjectID:    "abc-123",
		}
		if err := original.Save(dir); err != nil {
			t.Fatalf("Save: %v", err)
		}
		loaded, err := Load(dir)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if loaded == nil {
			t.Fatal("Load returned nil")
		}
		if loaded.DoltMode != DoltModeProxiedServer {
			t.Errorf("DoltMode = %q, want %q", loaded.DoltMode, DoltModeProxiedServer)
		}
		if !loaded.IsDoltProxiedServerMode() {
			t.Error("IsDoltProxiedServerMode() = false after roundtrip")
		}
		if loaded.IsDoltServerMode() {
			t.Error("IsDoltServerMode() = true after roundtrip; should be false")
		}
	})
}

// TestDoltProxiedServerConfig_RoundtripStripsAbsolutePath asserts the
// existing absolute-path-stripping invariant (previously enforced for
// dolt_data_dir) is also enforced for dolt_proxied_server_config. Absolute
// paths leak host filesystem layout and shouldn't ride into other clones
// via metadata.json.
func TestDoltProxiedServerConfig_RoundtripStripsAbsolutePath(t *testing.T) {
	t.Run("absolute path is dropped on save", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &Config{
			Backend:                 BackendDolt,
			DoltMode:                DoltModeProxiedServer,
			DoltDatabase:            "myproj",
			DoltProxiedServerConfig: "/etc/dolt/server.yaml",
		}
		if err := cfg.Save(dir); err != nil {
			t.Fatalf("Save: %v", err)
		}
		loaded, err := Load(dir)
		if err != nil || loaded == nil {
			t.Fatalf("Load: %v cfg=%v", err, loaded)
		}
		if loaded.DoltProxiedServerConfig != "" {
			t.Errorf("DoltProxiedServerConfig = %q, want empty (absolute paths must be stripped)", loaded.DoltProxiedServerConfig)
		}
	})

	t.Run("relative path round-trips intact", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &Config{
			Backend:                 BackendDolt,
			DoltMode:                DoltModeProxiedServer,
			DoltDatabase:            "myproj",
			DoltProxiedServerConfig: "configs/server.yaml",
		}
		if err := cfg.Save(dir); err != nil {
			t.Fatalf("Save: %v", err)
		}
		loaded, err := Load(dir)
		if err != nil || loaded == nil {
			t.Fatalf("Load: %v cfg=%v", err, loaded)
		}
		if loaded.DoltProxiedServerConfig != "configs/server.yaml" {
			t.Errorf("DoltProxiedServerConfig = %q, want %q (relative paths must survive)", loaded.DoltProxiedServerConfig, "configs/server.yaml")
		}
	})
}

// TestGetDoltProxiedServerConfig_ResolutionChain locks down the env > field
// (relative to beadsDir) > field (absolute) > empty resolution order.
func TestGetDoltProxiedServerConfig_ResolutionChain(t *testing.T) {
	beadsDir := "/home/user/project/.beads"

	t.Run("env beats field", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_CONFIG", "/etc/from-env.yaml")
		cfg := &Config{DoltProxiedServerConfig: "configs/in-meta.yaml"}
		if got := cfg.GetDoltProxiedServerConfig(beadsDir); got != "/etc/from-env.yaml" {
			t.Errorf("got %q, want env value", got)
		}
	})

	t.Run("field relative joins beadsDir", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_CONFIG", "")
		cfg := &Config{DoltProxiedServerConfig: "configs/server.yaml"}
		want := filepath.Join(beadsDir, "configs/server.yaml")
		if got := cfg.GetDoltProxiedServerConfig(beadsDir); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("field absolute returns as-is", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_CONFIG", "")
		cfg := &Config{DoltProxiedServerConfig: "/etc/dolt/server.yaml"}
		if got := cfg.GetDoltProxiedServerConfig(beadsDir); got != "/etc/dolt/server.yaml" {
			t.Errorf("got %q, want absolute as-is", got)
		}
	})

	t.Run("empty returns empty", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_CONFIG", "")
		cfg := &Config{}
		if got := cfg.GetDoltProxiedServerConfig(beadsDir); got != "" {
			t.Errorf("got %q, want empty (caller layers default)", got)
		}
	})
}

// TestDoltProxiedServerLog_RoundtripStripsAbsolutePath mirrors the
// --proxied-server-config strip test: absolute paths in dolt_proxied_server_log
// must be dropped on Save (machine-specific, can't survive across clones)
// while relative paths round-trip intact.
func TestDoltProxiedServerLog_RoundtripStripsAbsolutePath(t *testing.T) {
	t.Run("absolute path is dropped on save", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &Config{
			Backend:              BackendDolt,
			DoltMode:             DoltModeProxiedServer,
			DoltDatabase:         "myproj",
			DoltProxiedServerLog: "/var/log/beads/server.log",
		}
		if err := cfg.Save(dir); err != nil {
			t.Fatalf("Save: %v", err)
		}
		loaded, err := Load(dir)
		if err != nil || loaded == nil {
			t.Fatalf("Load: %v cfg=%v", err, loaded)
		}
		if loaded.DoltProxiedServerLog != "" {
			t.Errorf("DoltProxiedServerLog = %q, want empty (absolute paths must be stripped)", loaded.DoltProxiedServerLog)
		}
	})

	t.Run("relative path round-trips intact", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &Config{
			Backend:              BackendDolt,
			DoltMode:             DoltModeProxiedServer,
			DoltDatabase:         "myproj",
			DoltProxiedServerLog: "logs/server.log",
		}
		if err := cfg.Save(dir); err != nil {
			t.Fatalf("Save: %v", err)
		}
		loaded, err := Load(dir)
		if err != nil || loaded == nil {
			t.Fatalf("Load: %v cfg=%v", err, loaded)
		}
		if loaded.DoltProxiedServerLog != "logs/server.log" {
			t.Errorf("DoltProxiedServerLog = %q, want %q (relative paths must survive)", loaded.DoltProxiedServerLog, "logs/server.log")
		}
	})
}

// TestGetDoltProxiedServerLog_ResolutionChain mirrors the --proxied-server-config
// resolver tests: env > field-relative-resolved-against-beadsDir >
// field-absolute > empty.
func TestGetDoltProxiedServerLog_ResolutionChain(t *testing.T) {
	beadsDir := "/home/user/project/.beads"

	t.Run("env beats field", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_LOG", "/var/log/from-env.log")
		cfg := &Config{DoltProxiedServerLog: "logs/in-meta.log"}
		if got := cfg.GetDoltProxiedServerLog(beadsDir); got != "/var/log/from-env.log" {
			t.Errorf("got %q, want env value", got)
		}
	})

	t.Run("field relative joins beadsDir", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_LOG", "")
		cfg := &Config{DoltProxiedServerLog: "logs/server.log"}
		want := filepath.Join(beadsDir, "logs/server.log")
		if got := cfg.GetDoltProxiedServerLog(beadsDir); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("field absolute returns as-is", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_LOG", "")
		cfg := &Config{DoltProxiedServerLog: "/var/log/beads/server.log"}
		if got := cfg.GetDoltProxiedServerLog(beadsDir); got != "/var/log/beads/server.log" {
			t.Errorf("got %q, want absolute as-is", got)
		}
	})

	t.Run("empty returns empty", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_LOG", "")
		cfg := &Config{}
		if got := cfg.GetDoltProxiedServerLog(beadsDir); got != "" {
			t.Errorf("got %q, want empty (caller layers default)", got)
		}
	})
}

// TestDoltProxiedServerRootPath_RoundtripStripsAbsolutePath mirrors the
// --proxied-server-config / --proxied-server-log-path strip tests: absolute paths in
// dolt_proxied_server_root_path must be dropped on Save (machine-specific,
// can't survive across clones) while relative paths round-trip intact.
func TestDoltProxiedServerRootPath_RoundtripStripsAbsolutePath(t *testing.T) {
	t.Run("absolute path is dropped on save", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &Config{
			Backend:                   BackendDolt,
			DoltMode:                  DoltModeProxiedServer,
			DoltDatabase:              "myproj",
			DoltProxiedServerRootPath: "/var/lib/beads/proxieddb",
		}
		if err := cfg.Save(dir); err != nil {
			t.Fatalf("Save: %v", err)
		}
		loaded, err := Load(dir)
		if err != nil || loaded == nil {
			t.Fatalf("Load: %v cfg=%v", err, loaded)
		}
		if loaded.DoltProxiedServerRootPath != "" {
			t.Errorf("DoltProxiedServerRootPath = %q, want empty (absolute paths must be stripped)", loaded.DoltProxiedServerRootPath)
		}
	})

	t.Run("relative path round-trips intact", func(t *testing.T) {
		dir := t.TempDir()
		cfg := &Config{
			Backend:                   BackendDolt,
			DoltMode:                  DoltModeProxiedServer,
			DoltDatabase:              "myproj",
			DoltProxiedServerRootPath: "alt-proxieddb",
		}
		if err := cfg.Save(dir); err != nil {
			t.Fatalf("Save: %v", err)
		}
		loaded, err := Load(dir)
		if err != nil || loaded == nil {
			t.Fatalf("Load: %v cfg=%v", err, loaded)
		}
		if loaded.DoltProxiedServerRootPath != "alt-proxieddb" {
			t.Errorf("DoltProxiedServerRootPath = %q, want %q (relative paths must survive)", loaded.DoltProxiedServerRootPath, "alt-proxieddb")
		}
	})
}

// TestGetDoltProxiedServerRootPath_ResolutionChain mirrors the
// --proxied-server-config / --proxied-server-log-path resolver tests: env > field-relative-
// resolved-against-beadsDir > field-absolute > empty.
func TestGetDoltProxiedServerRootPath_ResolutionChain(t *testing.T) {
	beadsDir := "/home/user/project/.beads"

	t.Run("env beats field", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_ROOT_PATH", "/srv/from-env")
		cfg := &Config{DoltProxiedServerRootPath: "alt-in-meta"}
		if got := cfg.GetDoltProxiedServerRootPath(beadsDir); got != "/srv/from-env" {
			t.Errorf("got %q, want env value", got)
		}
	})

	t.Run("field relative joins beadsDir", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_ROOT_PATH", "")
		cfg := &Config{DoltProxiedServerRootPath: "alt-proxieddb"}
		want := filepath.Join(beadsDir, "alt-proxieddb")
		if got := cfg.GetDoltProxiedServerRootPath(beadsDir); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	})

	t.Run("field absolute returns as-is", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_ROOT_PATH", "")
		cfg := &Config{DoltProxiedServerRootPath: "/var/lib/beads/proxieddb"}
		if got := cfg.GetDoltProxiedServerRootPath(beadsDir); got != "/var/lib/beads/proxieddb" {
			t.Errorf("got %q, want absolute as-is", got)
		}
	})

	t.Run("empty returns empty", func(t *testing.T) {
		t.Setenv("BEADS_PROXIED_SERVER_ROOT_PATH", "")
		cfg := &Config{}
		if got := cfg.GetDoltProxiedServerRootPath(beadsDir); got != "" {
			t.Errorf("got %q, want empty (caller layers default)", got)
		}
	})
}

// TestGetBackendAlwaysDolt tests that GetBackend always returns "dolt".
func TestGetBackendAlwaysDolt(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
	}{
		{name: "explicit dolt", cfg: &Config{Backend: BackendDolt}},
		{name: "empty backend", cfg: &Config{Backend: ""}},
		{name: "legacy config", cfg: &Config{}},
		{name: "stale sqlite value", cfg: &Config{Backend: "sqlite"}},
		{name: "unknown backend", cfg: &Config{Backend: "postgres"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.GetBackend(); got != BackendDolt {
				t.Errorf("GetBackend() = %q, want %q", got, BackendDolt)
			}
		})
	}
}

// TestDatabasePathAlwaysDolt tests that DatabasePath always returns the dolt path.
func TestDatabasePathAlwaysDolt(t *testing.T) {
	beadsDir := "/home/user/project/.beads"

	cfg := &Config{Database: "beads.db", Backend: BackendDolt}
	got := cfg.DatabasePath(beadsDir)
	want := filepath.Join(beadsDir, "dolt")
	if got != want {
		t.Errorf("DatabasePath() = %q, want %q", got, want)
	}
}

// TestCapabilitiesForBackend tests that CapabilitiesForBackend returns
// single-process-only by default.
func TestCapabilitiesForBackend(t *testing.T) {
	caps := CapabilitiesForBackend("anything")
	if !caps.SingleProcessOnly {
		t.Error("CapabilitiesForBackend().SingleProcessOnly = false, want true")
	}
}

// TestGetCapabilities tests that GetCapabilities properly handles server mode
func TestGetCapabilities(t *testing.T) {
	tests := []struct {
		name           string
		cfg            *Config
		wantSingleProc bool
	}{
		{
			name:           "dolt embedded is single-process",
			cfg:            &Config{Backend: BackendDolt, DoltMode: DoltModeEmbedded},
			wantSingleProc: true,
		},
		{
			name:           "dolt default (empty) is single-process",
			cfg:            &Config{Backend: BackendDolt},
			wantSingleProc: true,
		},
		{
			name:           "dolt server mode is multi-process",
			cfg:            &Config{Backend: BackendDolt, DoltMode: DoltModeServer},
			wantSingleProc: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.GetCapabilities().SingleProcessOnly
			if got != tt.wantSingleProc {
				t.Errorf("GetCapabilities().SingleProcessOnly = %v, want %v", got, tt.wantSingleProc)
			}
		})
	}
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

	t.Run("BEADS_DOLT_PORT fallback when SERVER_PORT not set", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_PORT", "3307")
		cfg := &Config{}
		if got := cfg.GetDoltServerPort(); got != 3307 {
			t.Errorf("GetDoltServerPort() = %d, want 3307", got)
		}
	})

	t.Run("BEADS_DOLT_SERVER_PORT takes priority over BEADS_DOLT_PORT", func(t *testing.T) {
		t.Setenv("BEADS_DOLT_SERVER_PORT", "3309")
		t.Setenv("BEADS_DOLT_PORT", "3307")
		cfg := &Config{}
		if got := cfg.GetDoltServerPort(); got != 3309 {
			t.Errorf("GetDoltServerPort() = %d, want 3309", got)
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

// --- Upgrade regression tests (GH#2949) ---

func TestIsDoltServerMode_SharedServerOverridesEmbedded(t *testing.T) {
	// GH#2949: shared-server env var must override stale dolt_mode=embedded
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "1")
	t.Setenv("BEADS_DOLT_SERVER_MODE", "")

	cfg := &Config{Backend: BackendDolt, DoltMode: DoltModeEmbedded}
	if !cfg.IsDoltServerMode() {
		t.Error("IsDoltServerMode() = false with BEADS_DOLT_SHARED_SERVER=1 + stale embedded, want true")
	}
}

func TestIsDoltServerMode_SharedServerTrue(t *testing.T) {
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "true")
	t.Setenv("BEADS_DOLT_SERVER_MODE", "")

	cfg := &Config{Backend: BackendDolt, DoltMode: DoltModeEmbedded}
	if !cfg.IsDoltServerMode() {
		t.Error("IsDoltServerMode() = false with BEADS_DOLT_SHARED_SERVER=true + stale embedded, want true")
	}
}

func TestIsDoltServerMode_NoEnvRespectsMetadata(t *testing.T) {
	t.Setenv("BEADS_DOLT_SHARED_SERVER", "")
	t.Setenv("BEADS_DOLT_SERVER_MODE", "")

	cfg := &Config{Backend: BackendDolt, DoltMode: DoltModeEmbedded}
	if cfg.IsDoltServerMode() {
		t.Error("IsDoltServerMode() = true with no env overrides + embedded metadata, want false")
	}
}

func TestGlobalDoltDatabase_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("failed to create .beads directory: %v", err)
	}

	cfg := DefaultConfig()
	cfg.GlobalDoltDatabase = "beads_global"

	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	loaded, err := Load(beadsDir)
	if err != nil {
		t.Fatalf("Load() failed: %v", err)
	}
	if loaded.GlobalDoltDatabase != "beads_global" {
		t.Errorf("GlobalDoltDatabase = %q, want %q", loaded.GlobalDoltDatabase, "beads_global")
	}
	if loaded.GetGlobalDoltDatabase() != "beads_global" {
		t.Errorf("GetGlobalDoltDatabase() = %q, want %q", loaded.GetGlobalDoltDatabase(), "beads_global")
	}
}

func TestGlobalDoltDatabase_EmptyByDefault(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.GetGlobalDoltDatabase() != "" {
		t.Errorf("GetGlobalDoltDatabase() = %q, want empty string for default config", cfg.GetGlobalDoltDatabase())
	}
}

func TestGlobalDoltDatabase_OmittedFromJSON(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0750); err != nil {
		t.Fatalf("failed to create .beads directory: %v", err)
	}

	cfg := DefaultConfig()
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("Save() failed: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(beadsDir, ConfigFileName))
	if err != nil {
		t.Fatalf("ReadFile() failed: %v", err)
	}

	if strings.Contains(string(data), "global_dolt_database") {
		t.Error("global_dolt_database should be omitted from JSON when empty")
	}
}
