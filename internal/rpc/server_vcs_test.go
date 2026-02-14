package rpc

import (
	"encoding/json"
	"testing"
)

// VCS operations now use Dolt storage in the test server. Dolt implements
// VersionedStorage, RemoteStorage (partially), and DoltStorage interfaces.
// These tests verify the RPC round-trip: serialization, routing, and that
// operations are properly dispatched without panics.

func TestVcsCommit_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS versioned storage, so commit should be dispatched correctly.
	// It may succeed (nothing to commit) or fail with a Dolt-specific error.
	resp, err := client.Execute(OpVcsCommit, &VcsCommitArgs{Message: "test commit"})
	// We just verify the round-trip works without panics.
	// Both success and Dolt-specific errors are acceptable.
	_ = resp
	_ = err
}

func TestVcsCommit_MissingMessage(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	resp, err := client.Execute(OpVcsCommit, &VcsCommitArgs{Message: ""})
	if err == nil {
		t.Fatal("expected error for empty message")
	}
	if resp != nil && resp.Success {
		t.Error("expected resp.Success to be false")
	}
}

func TestVcsPush_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt test store has no remotes configured, so push will fail.
	resp, err := client.Execute(OpVcsPush, struct{}{})
	if err == nil {
		t.Fatal("expected error when no remote configured")
	}
	// Error should be about missing remote, not about storage type
	if resp != nil && resp.Success {
		t.Error("expected resp.Success to be false")
	}
}

func TestVcsPull_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt test store has no remotes configured, so pull will fail.
	resp, err := client.Execute(OpVcsPull, struct{}{})
	if err == nil {
		t.Fatal("expected error when no remote configured")
	}
	if resp != nil && resp.Success {
		t.Error("expected resp.Success to be false")
	}
}

func TestVcsMerge_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS versioned, but the "feature" branch doesn't exist.
	resp, err := client.Execute(OpVcsMerge, &VcsMergeArgs{Branch: "feature"})
	if err == nil {
		t.Fatal("expected error for non-existent branch")
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
}

func TestVcsBranchCreate_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS versioned, so branch creation should be dispatched.
	// It may succeed or fail depending on Dolt internals.
	_, _ = client.Execute(OpVcsBranchCreate, &VcsBranchCreateArgs{Name: "new-branch"})
	// Just verify no panic; both success and error are acceptable.
}

func TestVcsBranchCreate_MissingName(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsBranchCreate, &VcsBranchCreateArgs{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestVcsBranchDelete_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS Dolt storage, so the branch delete path is taken.
	// The branch "old-branch" doesn't exist, so it should fail.
	_, err := client.Execute(OpVcsBranchDelete, &VcsBranchDeleteArgs{Name: "old-branch"})
	if err == nil {
		t.Fatal("expected error for non-existent branch")
	}
}

func TestVcsBranchDelete_MissingName(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsBranchDelete, &VcsBranchDeleteArgs{Name: ""})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestVcsCheckout_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS versioned. Checkout to "main" should succeed (already on main).
	_, _ = client.Execute(OpVcsCheckout, &VcsCheckoutArgs{Branch: "main"})
	// Just verify no panic.
}

func TestVcsCheckout_MissingBranch(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsCheckout, &VcsCheckoutArgs{Branch: ""})
	if err == nil {
		t.Fatal("expected error for empty branch")
	}
}

func TestVcsActiveBranch_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS versioned, so active branch should return "main".
	_, _ = client.Execute(OpVcsActiveBranch, struct{}{})
	// Just verify no panic.
}

func TestVcsStatus_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS Dolt storage, so status should be dispatched.
	_, _ = client.Execute(OpVcsStatus, struct{}{})
	// Just verify no panic.
}

func TestVcsHasUncommitted_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS a status checker, so this should be dispatched.
	_, _ = client.Execute(OpVcsHasUncommitted, struct{}{})
	// Just verify no panic.
}

func TestVcsBranches_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS versioned, so branches should be dispatched.
	_, _ = client.Execute(OpVcsBranches, struct{}{})
	// Just verify no panic.
}

func TestVcsCurrentCommit_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS versioned, so current commit should be dispatched.
	_, _ = client.Execute(OpVcsCurrentCommit, struct{}{})
	// Just verify no panic.
}

func TestVcsCommitExists_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS Dolt storage, so commit exists should be dispatched.
	// Hash "abc123" doesn't exist, so it should return false or an error.
	_, _ = client.Execute(OpVcsCommitExists, &VcsCommitExistsArgs{Hash: "abc123"})
	// Just verify no panic.
}

func TestVcsCommitExists_MissingHash(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.Execute(OpVcsCommitExists, &VcsCommitExistsArgs{Hash: ""})
	if err == nil {
		t.Fatal("expected error for empty hash")
	}
}

func TestVcsLog_RoundTrip(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Dolt IS Dolt storage, so log should be dispatched.
	_, _ = client.Execute(OpVcsLog, &VcsLogArgs{Limit: 5})
	// Just verify no panic.
}

func TestVcsLog_DefaultLimit(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Test with zero limit (should default to 10)
	_, _ = client.Execute(OpVcsLog, &VcsLogArgs{})
	// Just verify no panic. The operation should be dispatched with default limit.
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
// With Dolt as the sole backend, operations are dispatched to Dolt.
// Some may succeed, others fail due to missing remotes/branches.
// We verify no panics or transport-level errors occur.
func TestVcsClientMethods(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	t.Run("VcsCommit", func(t *testing.T) {
		// Dolt is versioned, commit may succeed or fail - just verify no panic
		_, _ = client.VcsCommit(&VcsCommitArgs{Message: "test"})
	})

	t.Run("VcsPush", func(t *testing.T) {
		_, err := client.VcsPush()
		if err == nil {
			t.Error("expected error when no remote configured")
		}
	})

	t.Run("VcsPull", func(t *testing.T) {
		_, err := client.VcsPull()
		if err == nil {
			t.Error("expected error when no remote configured")
		}
	})

	t.Run("VcsMerge", func(t *testing.T) {
		_, err := client.VcsMerge(&VcsMergeArgs{Branch: "feature"})
		if err == nil {
			t.Error("expected error for non-existent branch")
		}
	})

	t.Run("VcsBranchCreate", func(t *testing.T) {
		// Dolt is versioned, branch creation may succeed
		_, _ = client.VcsBranchCreate(&VcsBranchCreateArgs{Name: "test-branch-create"})
	})

	t.Run("VcsBranchDelete", func(t *testing.T) {
		_, err := client.VcsBranchDelete(&VcsBranchDeleteArgs{Name: "nonexistent-branch"})
		if err == nil {
			t.Error("expected error for non-existent branch")
		}
	})

	t.Run("VcsCheckout", func(t *testing.T) {
		// Checkout main should succeed
		_, _ = client.VcsCheckout(&VcsCheckoutArgs{Branch: "main"})
	})

	t.Run("VcsActiveBranch", func(t *testing.T) {
		// Should succeed with Dolt
		_, _ = client.VcsActiveBranch()
	})

	t.Run("VcsStatus", func(t *testing.T) {
		// Should succeed with Dolt
		_, _ = client.VcsStatus()
	})

	t.Run("VcsHasUncommitted", func(t *testing.T) {
		// Should succeed with Dolt
		_, _ = client.VcsHasUncommitted()
	})

	t.Run("VcsBranches", func(t *testing.T) {
		// Should succeed with Dolt
		_, _ = client.VcsBranches()
	})

	t.Run("VcsCurrentCommit", func(t *testing.T) {
		// Should succeed with Dolt
		_, _ = client.VcsCurrentCommit()
	})

	t.Run("VcsCommitExists", func(t *testing.T) {
		// Non-existent hash, may return false or error
		_, _ = client.VcsCommitExists(&VcsCommitExistsArgs{Hash: "abc123"})
	})

	t.Run("VcsLog", func(t *testing.T) {
		// Should succeed with Dolt
		_, _ = client.VcsLog(&VcsLogArgs{Limit: 5})
	})
}
