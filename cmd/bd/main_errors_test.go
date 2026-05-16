package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/configfile"
)

// TestCheckMetadataBackendError_UnsupportedBackend verifies that a metadata.json
// declaring backend=postgres triggers a fast-fail with a clear upgrade hint
// (be-y0sm9s: stale binaries pre-PG entered a multi-GB RAM / 30-60s fallback).
func TestCheckMetadataBackendError_UnsupportedBackend(t *testing.T) {
	beadsDir := t.TempDir()
	cfg := &configfile.Config{Backend: "postgres"}
	if err := cfg.Save(beadsDir); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := configfile.Load(beadsDir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	gotErr := checkMetadataBackendError(loaded, beadsDir)
	if gotErr == nil {
		t.Fatal("expected error for backend=postgres, got nil")
	}
	msg := gotErr.Error()
	if !strings.Contains(msg, "postgres") {
		t.Errorf("error should name the unsupported backend, got: %s", msg)
	}
	if !strings.Contains(msg, "Upgrade") {
		t.Errorf("error should contain upgrade hint, got: %s", msg)
	}
	if !strings.Contains(msg, "version") {
		t.Errorf("error should include version info, got: %s", msg)
	}
}

// TestCheckMetadataBackendError_SupportedBackends verifies that dolt (explicit
// and empty/default) never triggers the fail-fast check.
func TestCheckMetadataBackendError_SupportedBackends(t *testing.T) {
	for _, backend := range []string{"", "dolt"} {
		t.Run("backend="+backend, func(t *testing.T) {
			cfg := &configfile.Config{Backend: backend}
			if err := checkMetadataBackendError(cfg, t.TempDir()); err != nil {
				t.Errorf("expected no error for backend %q, got: %v", backend, err)
			}
		})
	}
}

// TestCheckMetadataBackendError_NilConfig verifies a nil config is a no-op.
func TestCheckMetadataBackendError_NilConfig(t *testing.T) {
	if err := checkMetadataBackendError(nil, t.TempDir()); err != nil {
		t.Errorf("expected no error for nil config, got: %v", err)
	}
}

func TestHandleFreshCloneError_UsesBootstrapFirstGuidance(t *testing.T) {
	err := errors.New("post-migration validation failed: required config key missing: issue_prefix")

	origStderr := os.Stderr
	r, w, pipeErr := os.Pipe()
	if pipeErr != nil {
		t.Fatal(pipeErr)
	}
	os.Stderr = w

	handled := handleFreshCloneError(err)
	_ = w.Close()
	os.Stderr = origStderr

	var buf bytes.Buffer
	if _, copyErr := io.Copy(&buf, r); copyErr != nil {
		t.Fatal(copyErr)
	}
	_ = r.Close()

	if !handled {
		t.Fatal("expected fresh clone error to be handled")
	}
	msg := buf.String()
	if !strings.Contains(msg, "bd bootstrap") {
		t.Fatalf("expected bootstrap guidance, got:\n%s", msg)
	}
	if strings.Contains(msg, "To initialize a new database: bd init") {
		t.Fatalf("did not expect init-first guidance for fresh clone recovery, got:\n%s", msg)
	}
	if !strings.Contains(msg, "brand-new database from scratch") {
		t.Fatalf("expected brand-new project fallback note, got:\n%s", msg)
	}
}
