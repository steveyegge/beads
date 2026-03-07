package dolt

import (
	"errors"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
)

func TestAddAndGetRemoteCredentials(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Add credentials
	err := store.AddRemoteCredentials(ctx, "origin", "alice", "s3cret")
	if err != nil {
		t.Fatalf("AddRemoteCredentials failed: %v", err)
	}

	// Retrieve credentials
	username, password, err := store.GetRemoteCredentials(ctx, "origin")
	if err != nil {
		t.Fatalf("GetRemoteCredentials failed: %v", err)
	}
	if username != "alice" {
		t.Errorf("username = %q, want %q", username, "alice")
	}
	if password != "s3cret" {
		t.Errorf("password = %q, want %q", password, "s3cret")
	}
}

func TestGetRemoteCredentialsNotFound(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	_, _, err := store.GetRemoteCredentials(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent remote, got nil")
	}
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestAddRemoteCredentialsUpsert(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Add initial credentials
	if err := store.AddRemoteCredentials(ctx, "origin", "alice", "oldpass"); err != nil {
		t.Fatalf("AddRemoteCredentials failed: %v", err)
	}

	// Update credentials (upsert)
	if err := store.AddRemoteCredentials(ctx, "origin", "bob", "newpass"); err != nil {
		t.Fatalf("AddRemoteCredentials (upsert) failed: %v", err)
	}

	username, password, err := store.GetRemoteCredentials(ctx, "origin")
	if err != nil {
		t.Fatalf("GetRemoteCredentials failed: %v", err)
	}
	if username != "bob" {
		t.Errorf("username = %q after upsert, want %q", username, "bob")
	}
	if password != "newpass" {
		t.Errorf("password = %q after upsert, want %q", password, "newpass")
	}
}

func TestRemoveRemoteCredentials(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Add then remove
	if err := store.AddRemoteCredentials(ctx, "origin", "alice", "s3cret"); err != nil {
		t.Fatalf("AddRemoteCredentials failed: %v", err)
	}
	if err := store.RemoveRemoteCredentials(ctx, "origin"); err != nil {
		t.Fatalf("RemoveRemoteCredentials failed: %v", err)
	}

	// Should be gone
	_, _, err := store.GetRemoteCredentials(ctx, "origin")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expected ErrNotFound after remove, got: %v", err)
	}
}

func TestRemoveRemoteCredentialsNonexistent(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Removing a nonexistent remote should not error
	if err := store.RemoveRemoteCredentials(ctx, "nonexistent"); err != nil {
		t.Errorf("RemoveRemoteCredentials on nonexistent remote should not error, got: %v", err)
	}
}

func TestAddRemoteCredentialsEmptyPassword(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Add credentials with empty password
	if err := store.AddRemoteCredentials(ctx, "origin", "alice", ""); err != nil {
		t.Fatalf("AddRemoteCredentials with empty password failed: %v", err)
	}

	username, password, err := store.GetRemoteCredentials(ctx, "origin")
	if err != nil {
		t.Fatalf("GetRemoteCredentials failed: %v", err)
	}
	if username != "alice" {
		t.Errorf("username = %q, want %q", username, "alice")
	}
	if password != "" {
		t.Errorf("password = %q, want empty string", password)
	}
}

func TestAddRemoteCredentialsValidation(t *testing.T) {
	store, cleanup := setupTestStore(t)
	defer cleanup()

	ctx, cancel := testContext(t)
	defer cancel()

	// Empty remote name
	if err := store.AddRemoteCredentials(ctx, "", "alice", "pass"); err == nil {
		t.Error("expected error for empty remote name")
	}

	// Empty username
	if err := store.AddRemoteCredentials(ctx, "origin", "", "pass"); err == nil {
		t.Error("expected error for empty username")
	}
}
