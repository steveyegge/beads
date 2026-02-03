package rpc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
)

// setupConfigTestServer creates a test server with SQLite storage for config tests
func setupConfigTestServer(t *testing.T) (*Server, storage.Storage) {
	t.Helper()
	ctx := context.Background()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	store, err := sqlite.New(ctx, dbPath)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() { store.Close() })

	// Initialize database with required config
	if err := store.SetConfig(ctx, "issue_prefix", "bd"); err != nil {
		t.Fatalf("failed to set issue_prefix: %v", err)
	}

	server := NewServer("/tmp/test.sock", store, tmpDir, dbPath)
	return server, store
}

func TestHandleConfigSet_Success(t *testing.T) {
	server, store := setupConfigTestServer(t)
	ctx := context.Background()

	args := ConfigSetArgs{
		Key:   "test.key",
		Value: "test-value",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpConfigSet,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleConfigSet(req)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result ConfigSetResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Key != "test.key" {
		t.Errorf("expected key 'test.key', got '%s'", result.Key)
	}
	if result.Value != "test-value" {
		t.Errorf("expected value 'test-value', got '%s'", result.Value)
	}

	// Verify the value was actually set in storage
	stored, err := store.GetConfig(ctx, "test.key")
	if err != nil {
		t.Fatalf("failed to get config: %v", err)
	}
	if stored != "test-value" {
		t.Errorf("stored value mismatch: expected 'test-value', got '%s'", stored)
	}
}

func TestHandleConfigSet_InvalidArgs(t *testing.T) {
	server, _ := setupConfigTestServer(t)

	req := &Request{
		Operation: OpConfigSet,
		Args:      []byte(`{"invalid json`),
		Actor:     "test",
	}

	resp := server.handleConfigSet(req)
	if resp.Success {
		t.Error("expected failure for invalid JSON args")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

func TestHandleGetConfig_Success(t *testing.T) {
	server, store := setupConfigTestServer(t)
	ctx := context.Background()

	// First set a value
	if err := store.SetConfig(ctx, "test.get.key", "test-get-value"); err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	args := GetConfigArgs{
		Key: "test.get.key",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpGetConfig,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleGetConfig(req)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result GetConfigResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Key != "test.get.key" {
		t.Errorf("expected key 'test.get.key', got '%s'", result.Key)
	}
	if result.Value != "test-get-value" {
		t.Errorf("expected value 'test-get-value', got '%s'", result.Value)
	}
}

func TestHandleGetConfig_NotFound(t *testing.T) {
	server, _ := setupConfigTestServer(t)

	args := GetConfigArgs{
		Key: "nonexistent.key",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpGetConfig,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleGetConfig(req)
	if !resp.Success {
		t.Fatalf("expected success (empty value for missing key), got error: %s", resp.Error)
	}

	var result GetConfigResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Value != "" {
		t.Errorf("expected empty value for nonexistent key, got '%s'", result.Value)
	}
}

func TestHandleConfigList_Success(t *testing.T) {
	server, store := setupConfigTestServer(t)
	ctx := context.Background()

	// Set multiple config values
	if err := store.SetConfig(ctx, "list.key1", "value1"); err != nil {
		t.Fatalf("failed to set config: %v", err)
	}
	if err := store.SetConfig(ctx, "list.key2", "value2"); err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	req := &Request{
		Operation: OpConfigList,
		Args:      []byte(`{}`),
		Actor:     "test",
	}

	resp := server.handleConfigList(req)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result ConfigListResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Config == nil {
		t.Fatal("expected non-nil config map")
	}

	// Check that our test keys are present (there's also issue_prefix from setup)
	if result.Config["list.key1"] != "value1" {
		t.Errorf("expected list.key1='value1', got '%s'", result.Config["list.key1"])
	}
	if result.Config["list.key2"] != "value2" {
		t.Errorf("expected list.key2='value2', got '%s'", result.Config["list.key2"])
	}
}

func TestHandleConfigList_ReturnsMap(t *testing.T) {
	// This test verifies that config list returns a valid map
	// Note: SQLite storage may have default config entries
	server, _ := setupConfigTestServer(t)

	req := &Request{
		Operation: OpConfigList,
		Args:      []byte(`{}`),
		Actor:     "test",
	}

	resp := server.handleConfigList(req)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result ConfigListResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	// Config map should be non-nil (may have defaults from storage setup)
	if result.Config == nil {
		t.Error("expected non-nil config map")
	}

	// The issue_prefix should be present from setup
	if result.Config["issue_prefix"] != "bd" {
		t.Errorf("expected issue_prefix='bd' from setup, got '%s'", result.Config["issue_prefix"])
	}
}

func TestHandleConfigUnset_Success(t *testing.T) {
	server, store := setupConfigTestServer(t)
	ctx := context.Background()

	// First set a value
	if err := store.SetConfig(ctx, "unset.key", "to-be-deleted"); err != nil {
		t.Fatalf("failed to set config: %v", err)
	}

	// Verify it exists
	value, _ := store.GetConfig(ctx, "unset.key")
	if value != "to-be-deleted" {
		t.Fatalf("setup failed: expected 'to-be-deleted', got '%s'", value)
	}

	args := ConfigUnsetArgs{
		Key: "unset.key",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpConfigUnset,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleConfigUnset(req)
	if !resp.Success {
		t.Fatalf("expected success, got error: %s", resp.Error)
	}

	var result ConfigUnsetResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if result.Key != "unset.key" {
		t.Errorf("expected key 'unset.key', got '%s'", result.Key)
	}

	// Verify the value was actually deleted
	stored, _ := store.GetConfig(ctx, "unset.key")
	if stored != "" {
		t.Errorf("expected empty value after unset, got '%s'", stored)
	}
}

func TestHandleConfigUnset_InvalidArgs(t *testing.T) {
	server, _ := setupConfigTestServer(t)

	req := &Request{
		Operation: OpConfigUnset,
		Args:      []byte(`{"invalid json`),
		Actor:     "test",
	}

	resp := server.handleConfigUnset(req)
	if resp.Success {
		t.Error("expected failure for invalid JSON args")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

func TestHandleConfigUnset_NonexistentKey(t *testing.T) {
	server, _ := setupConfigTestServer(t)

	args := ConfigUnsetArgs{
		Key: "nonexistent.key",
	}
	argsJSON, _ := json.Marshal(args)

	req := &Request{
		Operation: OpConfigUnset,
		Args:      argsJSON,
		Actor:     "test",
	}

	resp := server.handleConfigUnset(req)
	// Unsetting a nonexistent key should succeed (idempotent operation)
	if !resp.Success {
		t.Fatalf("expected success for nonexistent key, got error: %s", resp.Error)
	}
}
