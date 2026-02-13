package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/testutil/teststore"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage/factory"
)

func TestHydrateDeployConfig(t *testing.T) {
	ctx := context.Background()
	log := newTestLogger()

	t.Run("hydrates unset env vars from deploy config", func(t *testing.T) {
		store := teststore.New(t)

		// Seed deploy.* keys in config
		if err := store.SetConfig(ctx, "deploy.redis_url", "redis://my-redis:6379"); err != nil {
			t.Fatal(err)
		}
		if err := store.SetConfig(ctx, "deploy.nats_url", "nats://my-nats:4222"); err != nil {
			t.Fatal(err)
		}

		// Ensure env vars are unset
		os.Unsetenv("BD_REDIS_URL")
		os.Unsetenv("BD_NATS_URL")
		defer os.Unsetenv("BD_REDIS_URL")
		defer os.Unsetenv("BD_NATS_URL")

		hydrateDeployConfig(ctx, store, log)

		if got := os.Getenv("BD_REDIS_URL"); got != "redis://my-redis:6379" {
			t.Errorf("BD_REDIS_URL = %q, want %q", got, "redis://my-redis:6379")
		}
		if got := os.Getenv("BD_NATS_URL"); got != "nats://my-nats:4222" {
			t.Errorf("BD_NATS_URL = %q, want %q", got, "nats://my-nats:4222")
		}
	})

	t.Run("env vars already set take precedence", func(t *testing.T) {
		store := teststore.New(t)

		if err := store.SetConfig(ctx, "deploy.redis_url", "redis://from-db:6379"); err != nil {
			t.Fatal(err)
		}

		// Pre-set the env var — should NOT be overwritten
		os.Setenv("BD_REDIS_URL", "redis://from-env:6379")
		defer os.Unsetenv("BD_REDIS_URL")

		hydrateDeployConfig(ctx, store, log)

		if got := os.Getenv("BD_REDIS_URL"); got != "redis://from-env:6379" {
			t.Errorf("BD_REDIS_URL = %q, want %q (env should take precedence)", got, "redis://from-env:6379")
		}
	})

	t.Run("non-deploy keys are ignored", func(t *testing.T) {
		store := teststore.New(t)

		if err := store.SetConfig(ctx, "jira.url", "https://jira.example.com"); err != nil {
			t.Fatal(err)
		}
		if err := store.SetConfig(ctx, "deploy.slack_channel", "C12345"); err != nil {
			t.Fatal(err)
		}

		os.Unsetenv("SLACK_CHANNEL")
		defer os.Unsetenv("SLACK_CHANNEL")

		hydrateDeployConfig(ctx, store, log)

		if got := os.Getenv("SLACK_CHANNEL"); got != "C12345" {
			t.Errorf("SLACK_CHANNEL = %q, want %q", got, "C12345")
		}
	})

	t.Run("deploy key with no env mapping is skipped", func(t *testing.T) {
		store := teststore.New(t)

		// deploy.ingress_host has no EnvVar mapping
		if err := store.SetConfig(ctx, "deploy.ingress_host", "my-host.example.com"); err != nil {
			t.Fatal(err)
		}

		// Should not panic or error — just skip
		hydrateDeployConfig(ctx, store, log)
	})

	t.Run("empty config is a no-op", func(t *testing.T) {
		store := teststore.New(t)
		// No deploy config set — should be a no-op
		hydrateDeployConfig(ctx, store, log)
	})
}

func TestWaitForStore_ImmediateSuccess(t *testing.T) {
	// Clear BD_DAEMON_HOST so factory doesn't block direct database access
	t.Setenv("BD_DAEMON_HOST", "")

	// Set up a Dolt embedded store in a temp directory — should connect immediately
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	doltDir := filepath.Join(beadsDir, "dolt")
	if err := os.MkdirAll(doltDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write metadata.json for Dolt embedded backend
	cfg := configfile.DefaultConfig()
	cfg.Backend = configfile.BackendDolt
	cfg.DoltMode = configfile.DoltModeEmbedded
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	log := newTestLogger()
	store, err := waitForStore(ctx, beadsDir, factory.Options{}, log)
	if err != nil {
		t.Fatalf("waitForStore failed: %v", err)
	}
	defer store.Close()
}

func TestWaitForStore_ContextCanceled(t *testing.T) {
	// Use a canceled context — should fail immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Write metadata.json pointing to unreachable Dolt server
	cfg := &configfile.Config{
		Backend:        configfile.BackendDolt,
		Database:       "dolt",
		DoltMode:       configfile.DoltModeServer,
		DoltServerHost: "unreachable-host",
		DoltServerPort: 39999,
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatal(err)
	}

	log := newTestLogger()

	// Set retries to 1 via env to speed up the test
	os.Setenv("BEADS_DOLT_CONNECT_RETRIES", "1")
	defer os.Unsetenv("BEADS_DOLT_CONNECT_RETRIES")

	_, err := waitForStore(ctx, beadsDir, factory.Options{}, log)
	if err == nil {
		t.Fatal("expected error from waitForStore with canceled context")
	}
}
