package rpc

import (
	"encoding/json"
	"testing"
)

// TestConfigSet_EmitsConfigEvent verifies that config_set exercises the
// emitConfigEvent code path without panicking (bd-hkgu).
func TestConfigSet_EmitsConfigEvent(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	resp, err := client.Execute(OpConfigSet, &ConfigSetArgs{
		Key:   "deploy.test_key",
		Value: "test_value",
	})
	if err != nil {
		t.Fatalf("Execute OpConfigSet failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("OpConfigSet failed: %s", resp.Error)
	}

	var result ConfigSetResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result.Key != "deploy.test_key" {
		t.Errorf("expected key=deploy.test_key, got %s", result.Key)
	}
	if result.Value != "test_value" {
		t.Errorf("expected value=test_value, got %s", result.Value)
	}
}

// TestConfigUnset_EmitsConfigEvent verifies that config_unset exercises the
// emitConfigEvent code path without panicking (bd-hkgu).
func TestConfigUnset_EmitsConfigEvent(t *testing.T) {
	_, client, _, cleanup := setupTestServerWithStore(t)
	defer cleanup()

	// Set a key first
	resp, err := client.Execute(OpConfigSet, &ConfigSetArgs{
		Key:   "deploy.remove_me",
		Value: "temporary",
	})
	if err != nil {
		t.Fatalf("Execute OpConfigSet failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("OpConfigSet failed: %s", resp.Error)
	}

	// Unset it
	resp, err = client.Execute(OpConfigUnset, &ConfigUnsetArgs{
		Key: "deploy.remove_me",
	})
	if err != nil {
		t.Fatalf("Execute OpConfigUnset failed: %v", err)
	}
	if !resp.Success {
		t.Fatalf("OpConfigUnset failed: %s", resp.Error)
	}

	var result ConfigUnsetResponse
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if result.Key != "deploy.remove_me" {
		t.Errorf("expected key=deploy.remove_me, got %s", result.Key)
	}
}
