package postgres

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestOpen_ErrorFormat_WithOverride verifies that Open() wraps connection
// failures with both the redacted target and the applied override fields.
// Uses a non-routable hostname so no real Postgres instance is required;
// DNS failure is deterministic for .invalid TLD (RFC 2606).
func TestOpen_ErrorFormat_WithOverride(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := Open(ctx,
		"postgres://nonexistent.invalid:5432/db",
		"postgres://nonexistent.invalid:5432/db",
		[]string{"host"})
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
	if !strings.Contains(err.Error(), "overrides applied: host") {
		t.Errorf("error missing override list: %v", err)
	}
	if !strings.Contains(err.Error(), "nonexistent.invalid") {
		t.Errorf("error missing redacted target: %v", err)
	}
}
