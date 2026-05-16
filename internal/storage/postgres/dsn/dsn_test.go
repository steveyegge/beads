package dsn_test

import (
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/steveyegge/beads/internal/storage/postgres/dsn"
)

// TestBuildFromFields_Matrix covers 8 input combinations:
// loopback/remote × user-only/user+sslmode × empty/explicit-db.
// Each case verifies that the output round-trips through pgconn.ParseConfig.
func TestBuildFromFields_Matrix(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		port    int
		user    string
		db      string
		sslmode string
		wantDB  string
	}{
		{"loopback user-only empty-db", "127.0.0.1", 5432, "beads", "", "", ""},
		{"loopback user-only explicit-db", "127.0.0.1", 5432, "beads", "myproject", "", "myproject"},
		{"loopback user+sslmode empty-db", "127.0.0.1", 5432, "beads", "", "disable", ""},
		{"loopback user+sslmode explicit-db", "127.0.0.1", 5432, "beads", "myproject", "disable", "myproject"},
		{"remote user-only empty-db", "remote.example.com", 5433, "beads", "", "", ""},
		{"remote user-only explicit-db", "remote.example.com", 5433, "beads", "proddb", "", "proddb"},
		{"remote user+sslmode empty-db", "remote.example.com", 5433, "beads", "", "require", ""},
		{"remote user+sslmode explicit-db", "remote.example.com", 5433, "beads", "proddb", "require", "proddb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := dsn.BuildFromFields(tc.host, tc.port, tc.user, tc.db, tc.sslmode)
			if result == "" {
				t.Fatal("BuildFromFields returned empty string")
			}
			// Round-trip through pgconn.ParseConfig.
			cfg, err := pgconn.ParseConfig(result)
			if err != nil {
				t.Fatalf("pgconn.ParseConfig(%q): %v", result, err)
			}
			if cfg.Host != tc.host {
				t.Errorf("host: got %q, want %q", cfg.Host, tc.host)
			}
			if tc.port != 0 && cfg.Port != uint16(tc.port) {
				t.Errorf("port: got %d, want %d", cfg.Port, tc.port)
			}
			if cfg.User != tc.user {
				t.Errorf("user: got %q, want %q", cfg.User, tc.user)
			}
			if cfg.Database != tc.wantDB {
				t.Errorf("database: got %q, want %q", cfg.Database, tc.wantDB)
			}
			// Password must never appear.
			if cfg.Password != "" {
				t.Errorf("password leaked into DSN: %q", cfg.Password)
			}
		})
	}
}

// TestBuildFromFields_DefaultSSLMode verifies that an empty sslmode defaults to "disable".
func TestBuildFromFields_DefaultSSLMode(t *testing.T) {
	result := dsn.BuildFromFields("127.0.0.1", 5432, "beads", "db", "")
	cfg, err := pgconn.ParseConfig(result)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// sslmode=disable → pgconn sets TLSConfig to nil.
	if cfg.TLSConfig != nil {
		t.Errorf("expected sslmode=disable (nil TLSConfig), got non-nil TLSConfig")
	}
}

// TestApplyEnvOverrides_Matrix covers all individual overrides, all-five
// together, invalid PORT, invalid SSLMODE, empty env (identity), and
// sorted override-name list.
func TestApplyEnvOverrides_Matrix(t *testing.T) {
	base := dsn.BuildFromFields("127.0.0.1", 5432, "beads", "basedb", "disable")

	t.Run("empty env identity", func(t *testing.T) {
		clearEnv(t)
		got, names := dsn.ApplyEnvOverrides(base)
		if names != nil {
			t.Errorf("want nil overrides, got %v", names)
		}
		cfg, err := pgconn.ParseConfig(got)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if cfg.Host != "127.0.0.1" || cfg.Port != 5432 || cfg.User != "beads" || cfg.Database != "basedb" {
			t.Errorf("identity failed: %v", cfg)
		}
	})

	t.Run("HOST only", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BEADS_POSTGRES_HOST", "db.internal")
		got, names := dsn.ApplyEnvOverrides(base)
		assertNames(t, names, []string{"host"})
		cfg, _ := pgconn.ParseConfig(got)
		if cfg.Host != "db.internal" {
			t.Errorf("host: got %q", cfg.Host)
		}
	})

	t.Run("PORT only", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BEADS_POSTGRES_PORT", "5433")
		got, names := dsn.ApplyEnvOverrides(base)
		assertNames(t, names, []string{"port"})
		cfg, _ := pgconn.ParseConfig(got)
		if cfg.Port != 5433 {
			t.Errorf("port: got %d", cfg.Port)
		}
	})

	t.Run("USER only", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BEADS_POSTGRES_USER", "admin")
		got, names := dsn.ApplyEnvOverrides(base)
		assertNames(t, names, []string{"user"})
		cfg, _ := pgconn.ParseConfig(got)
		if cfg.User != "admin" {
			t.Errorf("user: got %q", cfg.User)
		}
	})

	t.Run("DATABASE only", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BEADS_POSTGRES_DATABASE", "newdb")
		got, names := dsn.ApplyEnvOverrides(base)
		assertNames(t, names, []string{"database"})
		cfg, _ := pgconn.ParseConfig(got)
		if cfg.Database != "newdb" {
			t.Errorf("database: got %q", cfg.Database)
		}
	})

	t.Run("SSLMODE only", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BEADS_POSTGRES_SSLMODE", "require")
		got, names := dsn.ApplyEnvOverrides(base)
		assertNames(t, names, []string{"sslmode"})
		cfg, _ := pgconn.ParseConfig(got)
		if cfg.TLSConfig == nil {
			t.Error("expected non-nil TLSConfig for sslmode=require")
		}
	})

	t.Run("all five together", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BEADS_POSTGRES_HOST", "pg.corp")
		t.Setenv("BEADS_POSTGRES_PORT", "5435")
		t.Setenv("BEADS_POSTGRES_USER", "corpuser")
		t.Setenv("BEADS_POSTGRES_DATABASE", "corpdb")
		t.Setenv("BEADS_POSTGRES_SSLMODE", "verify-full")
		got, names := dsn.ApplyEnvOverrides(base)
		assertNames(t, names, []string{"database", "host", "port", "sslmode", "user"})
		cfg, err := pgconn.ParseConfig(got)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if cfg.Host != "pg.corp" || cfg.Port != 5435 || cfg.User != "corpuser" || cfg.Database != "corpdb" {
			t.Errorf("fields: %+v", cfg)
		}
		if cfg.TLSConfig == nil {
			t.Error("expected non-nil TLSConfig for sslmode=verify-full")
		}
		if cfg.Password != "" {
			t.Errorf("password leaked: %q", cfg.Password)
		}
	})

	t.Run("invalid PORT alpha ignored", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BEADS_POSTGRES_PORT", "notaport")
		_, names := dsn.ApplyEnvOverrides(base)
		for _, n := range names {
			if n == "port" {
				t.Error("invalid PORT should be ignored, but 'port' appeared in overrides")
			}
		}
	})

	t.Run("invalid PORT zero ignored", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BEADS_POSTGRES_PORT", "0")
		_, names := dsn.ApplyEnvOverrides(base)
		for _, n := range names {
			if n == "port" {
				t.Error("PORT=0 should be ignored")
			}
		}
	})

	t.Run("invalid SSLMODE ignored", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("BEADS_POSTGRES_SSLMODE", "bogus")
		_, names := dsn.ApplyEnvOverrides(base)
		for _, n := range names {
			if n == "sslmode" {
				t.Error("invalid SSLMODE should be ignored")
			}
		}
	})

	t.Run("override names are sorted", func(t *testing.T) {
		clearEnv(t)
		// Set in reverse alphabetical order to confirm sorting is not insertion-order.
		t.Setenv("BEADS_POSTGRES_USER", "u")
		t.Setenv("BEADS_POSTGRES_HOST", "h")
		t.Setenv("BEADS_POSTGRES_DATABASE", "d")
		_, names := dsn.ApplyEnvOverrides(base)
		assertNames(t, names, []string{"database", "host", "user"})
	})

	t.Run("libpq vars not read", func(t *testing.T) {
		clearEnv(t)
		t.Setenv("PGHOST", "should-be-ignored")
		t.Setenv("PGPORT", "9999")
		t.Setenv("DATABASE_URL", "postgres://hacked/hacked")
		got, names := dsn.ApplyEnvOverrides(base)
		if names != nil {
			t.Errorf("libpq vars must not be read, but overrides=%v", names)
		}
		cfg, _ := pgconn.ParseConfig(got)
		if cfg.Host != "127.0.0.1" {
			t.Errorf("host changed by PGHOST: got %q", cfg.Host)
		}
	})
}

// clearEnv unsets all BEADS_POSTGRES_* vars for the duration of a sub-test.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, v := range []string{
		"BEADS_POSTGRES_HOST",
		"BEADS_POSTGRES_PORT",
		"BEADS_POSTGRES_USER",
		"BEADS_POSTGRES_DATABASE",
		"BEADS_POSTGRES_SSLMODE",
	} {
		old, set := os.LookupEnv(v)
		if set {
			t.Setenv(v, old) // register cleanup
			os.Unsetenv(v)
		}
	}
}

func assertNames(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("overrides: got %v, want %v", got, want)
		return
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("overrides[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}
