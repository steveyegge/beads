package rpc

import (
	"encoding/json"
	"testing"
)

// Federation operations require Dolt backend. Since test infrastructure uses SQLite,
// these tests verify:
// 1. Operations are correctly routed through the RPC server
// 2. Argument parsing and validation work
// 3. Non-federated backends return proper errors
// 4. HTTP transport mappings are registered

// TestFedListRemotes_NonDoltBackend verifies proper error when using SQLite backend.
func TestFedListRemotes_NonDoltBackend(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FedListRemotes(&FedListRemotesArgs{})
	if err == nil {
		t.Error("expected error for non-Dolt backend")
	}
}

// TestFedSync_NonDoltBackend verifies proper error when using SQLite backend.
func TestFedSync_NonDoltBackend(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FedSync(&FedSyncArgs{Peer: "town-beta", Strategy: "ours"})
	if err == nil {
		t.Error("expected error for non-Dolt backend")
	}
}

// TestFedSync_MissingPeer verifies peer argument validation.
func TestFedSync_MissingPeer(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedSyncArgs{})
	req := &Request{
		Operation: OpFedSync,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure when peer is missing")
	}
	if resp.Error != "peer is required" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestFedSync_InvalidStrategy verifies strategy validation.
func TestFedSync_InvalidStrategy(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedSyncArgs{Peer: "town-beta", Strategy: "invalid"})
	req := &Request{
		Operation: OpFedSync,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure for invalid strategy")
	}
}

// TestFedSyncStatus_NonDoltBackend verifies proper error.
func TestFedSyncStatus_NonDoltBackend(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FedSyncStatus(&FedSyncStatusArgs{Peer: "town-beta"})
	if err == nil {
		t.Error("expected error for non-Dolt backend")
	}
}

// TestFedSyncStatus_MissingPeer verifies peer argument validation.
func TestFedSyncStatus_MissingPeer(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedSyncStatusArgs{})
	req := &Request{
		Operation: OpFedSyncStatus,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure when peer is missing")
	}
	if resp.Error != "peer is required" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestFedFetch_NonDoltBackend verifies proper error.
func TestFedFetch_NonDoltBackend(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FedFetch(&FedFetchArgs{Peer: "town-beta"})
	if err == nil {
		t.Error("expected error for non-Dolt backend")
	}
}

// TestFedFetch_MissingPeer verifies peer argument validation.
func TestFedFetch_MissingPeer(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedFetchArgs{})
	req := &Request{
		Operation: OpFedFetch,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure when peer is missing")
	}
	if resp.Error != "peer is required" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestFedPushTo_NonDoltBackend verifies proper error.
func TestFedPushTo_NonDoltBackend(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FedPushTo(&FedPushToArgs{Peer: "town-beta"})
	if err == nil {
		t.Error("expected error for non-Dolt backend")
	}
}

// TestFedPushTo_MissingPeer verifies peer argument validation.
func TestFedPushTo_MissingPeer(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedPushToArgs{})
	req := &Request{
		Operation: OpFedPushTo,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure when peer is missing")
	}
	if resp.Error != "peer is required" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestFedPullFrom_NonDoltBackend verifies proper error.
func TestFedPullFrom_NonDoltBackend(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FedPullFrom(&FedPullFromArgs{Peer: "town-beta"})
	if err == nil {
		t.Error("expected error for non-Dolt backend")
	}
}

// TestFedPullFrom_MissingPeer verifies peer argument validation.
func TestFedPullFrom_MissingPeer(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedPullFromArgs{})
	req := &Request{
		Operation: OpFedPullFrom,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure when peer is missing")
	}
	if resp.Error != "peer is required" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestFedAddRemote_NonDoltBackend verifies proper error.
func TestFedAddRemote_NonDoltBackend(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FedAddRemote(&FedAddRemoteArgs{Name: "town-beta", URL: "dolthub://acme/beads"})
	if err == nil {
		t.Error("expected error for non-Dolt backend")
	}
}

// TestFedAddRemote_MissingName verifies name argument validation.
func TestFedAddRemote_MissingName(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedAddRemoteArgs{URL: "dolthub://acme/beads"})
	req := &Request{
		Operation: OpFedAddRemote,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure when name is missing")
	}
	if resp.Error != "name is required" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestFedAddRemote_MissingURL verifies url argument validation.
func TestFedAddRemote_MissingURL(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedAddRemoteArgs{Name: "town-beta"})
	req := &Request{
		Operation: OpFedAddRemote,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure when url is missing")
	}
	if resp.Error != "url is required" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestFedRemoveRemote_NonDoltBackend verifies proper error.
func TestFedRemoveRemote_NonDoltBackend(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FedRemoveRemote(&FedRemoveRemoteArgs{Name: "town-beta"})
	if err == nil {
		t.Error("expected error for non-Dolt backend")
	}
}

// TestFedRemoveRemote_MissingName verifies name argument validation.
func TestFedRemoveRemote_MissingName(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedRemoveRemoteArgs{})
	req := &Request{
		Operation: OpFedRemoveRemote,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure when name is missing")
	}
	if resp.Error != "name is required" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestFedAddPeer_NonDoltBackend verifies proper error.
func TestFedAddPeer_NonDoltBackend(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.FedAddPeer(&FedAddPeerArgs{
		Name:     "town-beta",
		URL:      "http://192.168.1.100:3306/beads",
		Username: "sync-bot",
		Password: "secret",
	})
	if err == nil {
		t.Error("expected error for non-Dolt backend")
	}
}

// TestFedAddPeer_MissingName verifies name argument validation.
func TestFedAddPeer_MissingName(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedAddPeerArgs{URL: "http://example.com/beads"})
	req := &Request{
		Operation: OpFedAddPeer,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure when name is missing")
	}
	if resp.Error != "name is required" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestFedAddPeer_MissingURL verifies url argument validation.
func TestFedAddPeer_MissingURL(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	args, _ := json.Marshal(FedAddPeerArgs{Name: "town-beta"})
	req := &Request{
		Operation: OpFedAddPeer,
		Args:      args,
	}

	resp := server.executeOperation(req)
	if resp.Success {
		t.Error("expected failure when url is missing")
	}
	if resp.Error != "url is required" {
		t.Errorf("unexpected error: %s", resp.Error)
	}
}

// TestFedOperations_InvalidJSON verifies all federation operations handle invalid JSON.
func TestFedOperations_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	ops := []string{
		OpFedSync,
		OpFedSyncStatus,
		OpFedFetch,
		OpFedPushTo,
		OpFedPullFrom,
		OpFedAddRemote,
		OpFedRemoveRemote,
		OpFedAddPeer,
	}

	for _, op := range ops {
		t.Run(op, func(t *testing.T) {
			req := &Request{
				Operation: op,
				Args:      []byte(`{"invalid json`),
			}
			resp := server.executeOperation(req)
			if resp.Success {
				t.Errorf("expected failure for invalid JSON on %s", op)
			}
		})
	}
}

// TestFedOperations_RoundTrip verifies all federation operations are correctly routed
// through the RPC server (operation dispatch, arg parsing, error response).
func TestFedOperations_RoundTrip(t *testing.T) {
	server, _, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name string
		op   string
		args interface{}
	}{
		{"list_remotes", OpFedListRemotes, &FedListRemotesArgs{}},
		{"sync", OpFedSync, &FedSyncArgs{Peer: "town-beta"}},
		{"sync_status", OpFedSyncStatus, &FedSyncStatusArgs{Peer: "town-beta"}},
		{"fetch", OpFedFetch, &FedFetchArgs{Peer: "town-beta"}},
		{"push_to", OpFedPushTo, &FedPushToArgs{Peer: "town-beta"}},
		{"pull_from", OpFedPullFrom, &FedPullFromArgs{Peer: "town-beta"}},
		{"add_remote", OpFedAddRemote, &FedAddRemoteArgs{Name: "town-beta", URL: "dolthub://acme/beads"}},
		{"remove_remote", OpFedRemoveRemote, &FedRemoveRemoteArgs{Name: "town-beta"}},
		{"add_peer", OpFedAddPeer, &FedAddPeerArgs{Name: "town-beta", URL: "http://example.com/beads"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			argsJSON, err := json.Marshal(tt.args)
			if err != nil {
				t.Fatalf("failed to marshal args: %v", err)
			}

			req := &Request{
				Operation: tt.op,
				Args:      argsJSON,
			}

			resp := server.executeOperation(req)

			// All should fail with "federation requires Dolt backend" since test uses SQLite
			if resp.Success {
				t.Errorf("expected failure for %s on SQLite backend", tt.op)
			}
			if resp.Error == "" {
				t.Errorf("expected non-empty error for %s", tt.op)
			}
			// Verify the error is about federation/Dolt, not "unknown operation"
			if resp.Error == "unknown operation: "+tt.op {
				t.Errorf("operation %s not registered in executeOperation switch", tt.op)
			}
		})
	}
}

// TestFedHTTPMappings verifies all federation operations have HTTP transport mappings.
func TestFedHTTPMappings(t *testing.T) {
	ops := []string{
		OpFedListRemotes,
		OpFedSync,
		OpFedSyncStatus,
		OpFedFetch,
		OpFedPushTo,
		OpFedPullFrom,
		OpFedAddRemote,
		OpFedRemoveRemote,
		OpFedAddPeer,
	}

	for _, op := range ops {
		t.Run(op, func(t *testing.T) {
			method := operationToHTTPMethod(op)
			if method == "" {
				t.Errorf("no HTTP mapping for operation %s", op)
			}
		})
	}
}
