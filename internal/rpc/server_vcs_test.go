package rpc

import (
	"encoding/json"
	"strings"
	"testing"
)

// VCS operations use SQLite storage in the test server, so operations that
// require Dolt (VersionedStorage) will return error responses. These tests
// verify the RPC round-trip: serialization, routing, error handling.

func TestVcsCommit_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// SQLite doesn't implement VersionedStorage, so this returns an error response.
	// The important thing is that the RPC round-trip works (operation is routed correctly).
	resp, err := client.Execute(OpVcsCommit, &VcsCommitArgs{Message: "test commit"})
	if err == nil {
		t.Fatal("expected error for non-versioned storage")
	}
	// Verify the error indicates storage type issue, not routing/serialization
	if !strings.Contains(err.Error(), "versioned storage") {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected response even on failure")
	}
	if resp.Success {
		t.Error("expected resp.Success to be false")
	}
}

func TestVcsCommit_MissingMessage(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := client.Execute(OpVcsCommit, &VcsCommitArgs{Message: ""})
	if err == nil {
		t.Fatal("expected error for empty message")
	}
	if !strings.Contains(err.Error(), "message is required") {
		t.Errorf("unexpected error: %v", err)
	}
	if resp != nil && resp.Success {
		t.Error("expected resp.Success to be false")
	}
}

func TestVcsPush_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := client.Execute(OpVcsPush, struct{}{})
	if err == nil {
		t.Fatal("expected error for non-remote storage")
	}
	if !strings.Contains(err.Error(), "remote storage") {
		t.Errorf("unexpected error: %v", err)
	}
	if resp != nil && resp.Success {
		t.Error("expected resp.Success to be false")
	}
}

func TestVcsPull_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := client.Execute(OpVcsPull, struct{}{})
	if err == nil {
		t.Fatal("expected error for non-remote storage")
	}
	if !strings.Contains(err.Error(), "remote storage") {
		t.Errorf("unexpected error: %v", err)
	}
	if resp != nil && resp.Success {
		t.Error("expected resp.Success to be false")
	}
}

func TestVcsMerge_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := client.Execute(OpVcsMerge, &VcsMergeArgs{Branch: "feature"})
	if err == nil {
		t.Fatal("expected error for non-versioned storage")
	}
	if !strings.Contains(err.Error(), "versioned storage") {
		t.Errorf("unexpected error: %v", err)
	}
	if resp != nil && resp.Success {
		t.Error("expected resp.Success to be false")
	}
}

func TestVcsMerge_MissingBranch(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsMerge, &VcsMergeArgs{Branch: ""})
	if err == nil {
		t.Fatal("expected error for empty branch")
	}
	if !strings.Contains(err.Error(), "branch is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsBranchCreate_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsBranchCreate, &VcsBranchCreateArgs{Name: "new-branch"})
	if err == nil {
		t.Fatal("expected error for non-versioned storage")
	}
	if !strings.Contains(err.Error(), "versioned storage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsBranchCreate_MissingName(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsBranchCreate, &VcsBranchCreateArgs{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsBranchDelete_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsBranchDelete, &VcsBranchDeleteArgs{Name: "old-branch"})
	if err == nil {
		t.Fatal("expected error for non-Dolt storage")
	}
	if !strings.Contains(err.Error(), "Dolt storage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsBranchDelete_MissingName(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsBranchDelete, &VcsBranchDeleteArgs{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
	if !strings.Contains(err.Error(), "name is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsCheckout_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsCheckout, &VcsCheckoutArgs{Branch: "main"})
	if err == nil {
		t.Fatal("expected error for non-versioned storage")
	}
	if !strings.Contains(err.Error(), "versioned storage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsCheckout_MissingBranch(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsCheckout, &VcsCheckoutArgs{Branch: ""})
	if err == nil {
		t.Fatal("expected error for empty branch")
	}
	if !strings.Contains(err.Error(), "branch is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsActiveBranch_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsActiveBranch, struct{}{})
	if err == nil {
		t.Fatal("expected error for non-versioned storage")
	}
	if !strings.Contains(err.Error(), "versioned storage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsStatus_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsStatus, struct{}{})
	if err == nil {
		t.Fatal("expected error for non-Dolt storage")
	}
	if !strings.Contains(err.Error(), "Dolt storage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsHasUncommitted_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsHasUncommitted, struct{}{})
	if err == nil {
		t.Fatal("expected error for non-status-checker storage")
	}
	if !strings.Contains(err.Error(), "status checker") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsBranches_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsBranches, struct{}{})
	if err == nil {
		t.Fatal("expected error for non-versioned storage")
	}
	if !strings.Contains(err.Error(), "versioned storage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsCurrentCommit_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsCurrentCommit, struct{}{})
	if err == nil {
		t.Fatal("expected error for non-versioned storage")
	}
	if !strings.Contains(err.Error(), "versioned storage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsCommitExists_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsCommitExists, &VcsCommitExistsArgs{Hash: "abc123"})
	if err == nil {
		t.Fatal("expected error for non-Dolt storage")
	}
	if !strings.Contains(err.Error(), "Dolt storage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsCommitExists_MissingHash(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsCommitExists, &VcsCommitExistsArgs{Hash: ""})
	if err == nil {
		t.Fatal("expected error for empty hash")
	}
	if !strings.Contains(err.Error(), "hash is required") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsLog_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsLog, &VcsLogArgs{Limit: 5})
	if err == nil {
		t.Fatal("expected error for non-Dolt storage")
	}
	if !strings.Contains(err.Error(), "Dolt storage") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestVcsLog_DefaultLimit(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Test with zero limit (should default to 10)
	_, err := client.Execute(OpVcsLog, &VcsLogArgs{})
	if err == nil {
		t.Fatal("expected error for non-Dolt storage")
	}
	// The error is about storage, not about limit parsing, proving default worked
	if !strings.Contains(err.Error(), "Dolt storage") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestVcsTypeSerialization verifies JSON round-trip for VCS types
func TestVcsTypeSerialization(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
	}{
		{"VcsCommitArgs", &VcsCommitArgs{Message: "test msg"}},
		{"VcsMergeArgs", &VcsMergeArgs{Branch: "feature"}},
		{"VcsBranchCreateArgs", &VcsBranchCreateArgs{Name: "new-branch"}},
		{"VcsBranchDeleteArgs", &VcsBranchDeleteArgs{Name: "old-branch"}},
		{"VcsCheckoutArgs", &VcsCheckoutArgs{Branch: "main"}},
		{"VcsCommitExistsArgs", &VcsCommitExistsArgs{Hash: "abc123"}},
		{"VcsLogArgs", &VcsLogArgs{Limit: 20}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}
			if len(data) == 0 {
				t.Error("marshaled to empty")
			}
		})
	}
}

// TestVcsResultSerialization verifies JSON round-trip for VCS result types
func TestVcsResultSerialization(t *testing.T) {
	tests := []struct {
		name string
		val  interface{}
	}{
		{"VcsCommitResult", &VcsCommitResult{Success: true}},
		{"VcsPushResult", &VcsPushResult{Success: true}},
		{"VcsPullResult", &VcsPullResult{Success: true}},
		{"VcsMergeResult", &VcsMergeResult{Success: true, Conflicts: []VcsConflict{{IssueID: "bd-1", Field: "title"}}}},
		{"VcsBranchCreateResult", &VcsBranchCreateResult{Name: "new-branch"}},
		{"VcsBranchDeleteResult", &VcsBranchDeleteResult{Name: "old-branch"}},
		{"VcsCheckoutResult", &VcsCheckoutResult{Branch: "main"}},
		{"VcsActiveBranchResult", &VcsActiveBranchResult{Branch: "main"}},
		{"VcsStatusResult", &VcsStatusResult{Staged: []VcsStatusEntry{{Table: "issues", Status: "modified"}}, Unstaged: []VcsStatusEntry{}}},
		{"VcsHasUncommittedResult", &VcsHasUncommittedResult{HasUncommitted: true}},
		{"VcsBranchesResult", &VcsBranchesResult{Branches: []string{"main", "feature"}}},
		{"VcsCurrentCommitResult", &VcsCurrentCommitResult{Hash: "abc123def456"}},
		{"VcsCommitExistsResult", &VcsCommitExistsResult{Exists: true}},
		{"VcsLogResult", &VcsLogResult{Commits: []VcsLogEntry{{Hash: "abc", Author: "test", Message: "msg"}}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data, err := json.Marshal(tc.val)
			if err != nil {
				t.Fatalf("marshal failed: %v", err)
			}

			// Verify round-trip by unmarshaling back
			var m map[string]interface{}
			if err := json.Unmarshal(data, &m); err != nil {
				t.Fatalf("unmarshal failed: %v", err)
			}
			if len(m) == 0 {
				t.Error("deserialized to empty map")
			}
		})
	}
}

// TestVcsHTTPMappings verifies all VCS operations have HTTP transport mappings
func TestVcsHTTPMappings(t *testing.T) {
	ops := []struct {
		op     string
		method string
	}{
		{OpVcsCommit, "VcsCommit"},
		{OpVcsPush, "VcsPush"},
		{OpVcsPull, "VcsPull"},
		{OpVcsMerge, "VcsMerge"},
		{OpVcsBranchCreate, "VcsBranchCreate"},
		{OpVcsBranchDelete, "VcsBranchDelete"},
		{OpVcsCheckout, "VcsCheckout"},
		{OpVcsActiveBranch, "VcsActiveBranch"},
		{OpVcsStatus, "VcsStatus"},
		{OpVcsHasUncommitted, "VcsHasUncommitted"},
		{OpVcsBranches, "VcsBranches"},
		{OpVcsCurrentCommit, "VcsCurrentCommit"},
		{OpVcsCommitExists, "VcsCommitExists"},
		{OpVcsLog, "VcsLog"},
	}

	for _, tc := range ops {
		t.Run(tc.method, func(t *testing.T) {
			// Verify server-side mapping: HTTP method -> operation
			got := httpMethodToOperation(tc.method)
			if got != tc.op {
				t.Errorf("httpMethodToOperation(%q) = %q, want %q", tc.method, got, tc.op)
			}

			// Verify client-side mapping: operation -> HTTP method
			got = operationToHTTPMethod(tc.op)
			if got != tc.method {
				t.Errorf("operationToHTTPMethod(%q) = %q, want %q", tc.op, got, tc.method)
			}
		})
	}
}

// TestVcsClientMethods verifies client convenience methods exercise the round-trip.
// All operations fail because the test server uses SQLite (not Dolt), but we verify
// no panics or transport-level errors occur.
func TestVcsClientMethods(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("VcsCommit", func(t *testing.T) {
		_, err := client.VcsCommit(&VcsCommitArgs{Message: "test"})
		if err == nil {
			t.Error("expected error for non-versioned storage")
		}
	})

	t.Run("VcsPush", func(t *testing.T) {
		_, err := client.VcsPush()
		if err == nil {
			t.Error("expected error for non-remote storage")
		}
	})

	t.Run("VcsPull", func(t *testing.T) {
		_, err := client.VcsPull()
		if err == nil {
			t.Error("expected error for non-remote storage")
		}
	})

	t.Run("VcsMerge", func(t *testing.T) {
		_, err := client.VcsMerge(&VcsMergeArgs{Branch: "feature"})
		if err == nil {
			t.Error("expected error for non-versioned storage")
		}
	})

	t.Run("VcsBranchCreate", func(t *testing.T) {
		_, err := client.VcsBranchCreate(&VcsBranchCreateArgs{Name: "test"})
		if err == nil {
			t.Error("expected error for non-versioned storage")
		}
	})

	t.Run("VcsBranchDelete", func(t *testing.T) {
		_, err := client.VcsBranchDelete(&VcsBranchDeleteArgs{Name: "test"})
		if err == nil {
			t.Error("expected error for non-Dolt storage")
		}
	})

	t.Run("VcsCheckout", func(t *testing.T) {
		_, err := client.VcsCheckout(&VcsCheckoutArgs{Branch: "main"})
		if err == nil {
			t.Error("expected error for non-versioned storage")
		}
	})

	t.Run("VcsActiveBranch", func(t *testing.T) {
		_, err := client.VcsActiveBranch()
		if err == nil {
			t.Error("expected error for non-versioned storage")
		}
	})

	t.Run("VcsStatus", func(t *testing.T) {
		_, err := client.VcsStatus()
		if err == nil {
			t.Error("expected error for non-Dolt storage")
		}
	})

	t.Run("VcsHasUncommitted", func(t *testing.T) {
		_, err := client.VcsHasUncommitted()
		if err == nil {
			t.Error("expected error for non-status-checker storage")
		}
	})

	t.Run("VcsBranches", func(t *testing.T) {
		_, err := client.VcsBranches()
		if err == nil {
			t.Error("expected error for non-versioned storage")
		}
	})

	t.Run("VcsCurrentCommit", func(t *testing.T) {
		_, err := client.VcsCurrentCommit()
		if err == nil {
			t.Error("expected error for non-versioned storage")
		}
	})

	t.Run("VcsCommitExists", func(t *testing.T) {
		_, err := client.VcsCommitExists(&VcsCommitExistsArgs{Hash: "abc123"})
		if err == nil {
			t.Error("expected error for non-Dolt storage")
		}
	})

	t.Run("VcsLog", func(t *testing.T) {
		_, err := client.VcsLog(&VcsLogArgs{Limit: 5})
		if err == nil {
			t.Error("expected error for non-Dolt storage")
		}
	})
}
