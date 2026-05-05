//go:build cgo

package main

import (
	"os"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage/embeddeddolt"
)

// TestNewDoltStoreFromConfig_NoMetadata verifies that newDoltStoreFromConfig
// succeeds when the beads directory has no metadata.json (fresh project).
// Regression test for GH#2988: "no database selected" error.
func TestNewDoltStoreFromConfig_NoMetadata(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt tests")
	}

	beadsDir := t.TempDir()

	// Confirm no config exists.
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		t.Fatalf("unexpected error loading config: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil config for empty dir")
	}

	// This should succeed using the default database name, not fail with
	// "no database selected".
	store, err := newDoltStoreFromConfig(t.Context(), beadsDir)
	if err != nil {
		t.Fatalf("newDoltStoreFromConfig failed: %v", err)
	}
	defer store.Close()
}

// TestEmbeddedOpen_EmptyDatabaseRejected verifies that embeddeddolt.Open fails
// with a clear error when called with an empty database name, rather than
// deferring to a confusing "no database selected" SQL error.
// Belt-and-suspenders defense for be-sy8 / GH#2988.
func TestEmbeddedOpen_EmptyDatabaseRejected(t *testing.T) {
	_, err := embeddeddolt.Open(t.Context(), t.TempDir(), "", "main")
	if err == nil {
		t.Fatal("expected error for empty database name")
	}
	if !strings.Contains(err.Error(), "database name must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestNewDoltStoreFromConfig_HyphenatedDBName verifies that
// newDoltStoreFromConfig auto-sanitizes hyphenated database names for embedded
// mode and persists the fix to metadata.json.
// Regression test for GH#3231: pre-#2142 projects break on embedded upgrade.
func TestNewDoltStoreFromConfig_HyphenatedDBName(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt tests")
	}

	beadsDir := t.TempDir()

	cfg := &configfile.Config{
		Database:     "dolt",
		DoltDatabase: "my-cool-project",
		DoltMode:     configfile.DoltModeEmbedded,
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	store, err := newDoltStoreFromConfig(t.Context(), beadsDir)
	if err != nil {
		t.Fatalf("newDoltStoreFromConfig failed (should have auto-sanitized): %v", err)
	}
	defer store.Close()

	reloaded, err := configfile.Load(beadsDir)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}
	if reloaded.DoltDatabase != "my_cool_project" {
		t.Errorf("expected dolt_database to be sanitized to %q, got %q", "my_cool_project", reloaded.DoltDatabase)
	}
}

// TestNewDoltStoreFromConfig_DottedDBName verifies that dots are also
// auto-sanitized, not just hyphens (GH#3231).
func TestNewDoltStoreFromConfig_DottedDBName(t *testing.T) {
	if os.Getenv("BEADS_TEST_EMBEDDED_DOLT") != "1" {
		t.Skip("set BEADS_TEST_EMBEDDED_DOLT=1 to run embedded dolt tests")
	}

	beadsDir := t.TempDir()

	cfg := &configfile.Config{
		Database:     "dolt",
		DoltDatabase: "my.project",
		DoltMode:     configfile.DoltModeEmbedded,
	}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	store, err := newDoltStoreFromConfig(t.Context(), beadsDir)
	if err != nil {
		t.Fatalf("newDoltStoreFromConfig failed (should have auto-sanitized dots): %v", err)
	}
	defer store.Close()

	reloaded, err := configfile.Load(beadsDir)
	if err != nil {
		t.Fatalf("failed to reload config: %v", err)
	}
	if reloaded.DoltDatabase != "my_project" {
		t.Errorf("expected dolt_database %q, got %q", "my_project", reloaded.DoltDatabase)
	}
}
