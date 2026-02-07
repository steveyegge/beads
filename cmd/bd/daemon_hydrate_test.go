package main

import (
	"context"
	"os"
	"testing"

	"github.com/steveyegge/beads/internal/storage/memory"
)

func TestHydrateDeployConfig(t *testing.T) {
	ctx := context.Background()
	log := newTestLogger()

	t.Run("hydrates unset env vars from deploy config", func(t *testing.T) {
		store := memory.New("test")

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
		store := memory.New("test")

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
		store := memory.New("test")

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
		store := memory.New("test")

		// deploy.ingress_host has no EnvVar mapping
		if err := store.SetConfig(ctx, "deploy.ingress_host", "my-host.example.com"); err != nil {
			t.Fatal(err)
		}

		// Should not panic or error — just skip
		hydrateDeployConfig(ctx, store, log)
	})

	t.Run("empty config is a no-op", func(t *testing.T) {
		store := memory.New("test")
		// No deploy config set — should be a no-op
		hydrateDeployConfig(ctx, store, log)
	})
}
