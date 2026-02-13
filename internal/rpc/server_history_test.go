package rpc

import (
	"encoding/json"
	"testing"
)

// TestHistoryIssue_NonVersionedBackend verifies that history_issue returns a clear error
// when the storage backend doesn't support versioned operations (e.g., SQLite).
func TestHistoryIssue_NonVersionedBackend(t *testing.T) {
	t.Skip("Dolt is now the sole backend — non-versioned path no longer exists")
}

// TestHistoryIssue_MissingIssueID verifies validation.
func TestHistoryIssue_MissingIssueID(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.HistoryIssue(&HistoryIssueArgs{})
	if err == nil {
		t.Fatal("Expected error for missing issue_id")
	}
	if !containsStr(err.Error(), "issue_id is required") {
		t.Errorf("Expected 'issue_id is required' error, got: %v", err)
	}
}

// TestHistoryDiff_NonVersionedBackend verifies that history_diff returns a clear error
// when the storage backend doesn't support versioned operations.
func TestHistoryDiff_NonVersionedBackend(t *testing.T) {
	t.Skip("Dolt is now the sole backend — non-versioned path no longer exists")
}

// TestHistoryDiff_MissingRefs verifies validation.
func TestHistoryDiff_MissingRefs(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.HistoryDiff(&HistoryDiffArgs{FromRef: "abc123"})
	if err == nil {
		t.Fatal("Expected error for missing to_ref")
	}
	if !containsStr(err.Error(), "from_ref and to_ref are required") {
		t.Errorf("Expected 'from_ref and to_ref are required' error, got: %v", err)
	}
}

// TestHistoryIssueDiff_NonVersionedBackend verifies that history_issue_diff returns a clear error.
func TestHistoryIssueDiff_NonVersionedBackend(t *testing.T) {
	t.Skip("Dolt is now the sole backend — non-versioned path no longer exists")
}

// TestHistoryIssueDiff_MissingArgs verifies validation.
func TestHistoryIssueDiff_MissingArgs(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.HistoryIssueDiff(&HistoryIssueDiffArgs{IssueID: "bd-test", FromRef: "abc"})
	if err == nil {
		t.Fatal("Expected error for missing to_ref")
	}
	if !containsStr(err.Error(), "issue_id, from_ref, and to_ref are required") {
		t.Errorf("Expected validation error, got: %v", err)
	}
}

// TestHistoryConflicts_NonVersionedBackend verifies that history_conflicts returns a clear error.
func TestHistoryConflicts_NonVersionedBackend(t *testing.T) {
	t.Skip("Dolt is now the sole backend — non-versioned path no longer exists")
}

// TestHistoryResolveConflicts_NonVersionedBackend verifies that history_resolve_conflicts returns a clear error.
func TestHistoryResolveConflicts_NonVersionedBackend(t *testing.T) {
	t.Skip("Dolt is now the sole backend — non-versioned path no longer exists")
}

// TestHistoryResolveConflicts_InvalidStrategy verifies validation.
func TestHistoryResolveConflicts_InvalidStrategy(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.HistoryResolveConflicts(&HistoryResolveConflictsArgs{
		Table:    "issues",
		Strategy: "invalid",
	})
	if err == nil {
		t.Fatal("Expected error for invalid strategy")
	}
	if !containsStr(err.Error(), "strategy must be") {
		t.Errorf("Expected strategy validation error, got: %v", err)
	}
}

// TestHistoryResolveConflicts_MissingTable verifies validation.
func TestHistoryResolveConflicts_MissingTable(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.HistoryResolveConflicts(&HistoryResolveConflictsArgs{
		Strategy: "ours",
	})
	if err == nil {
		t.Fatal("Expected error for missing table")
	}
	if !containsStr(err.Error(), "table is required") {
		t.Errorf("Expected 'table is required' error, got: %v", err)
	}
}

// TestVersionedDiff_NonVersionedBackend verifies that versioned_diff returns a clear error.
func TestVersionedDiff_NonVersionedBackend(t *testing.T) {
	t.Skip("Dolt is now the sole backend — non-versioned path no longer exists")
}

// TestVersionedDiff_MissingRefs verifies validation.
func TestVersionedDiff_MissingRefs(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	_, err := client.VersionedDiff(&VersionedDiffArgs{ToRef: "def456"})
	if err == nil {
		t.Fatal("Expected error for missing from_ref")
	}
	if !containsStr(err.Error(), "from_ref and to_ref are required") {
		t.Errorf("Expected validation error, got: %v", err)
	}
}

// TestHistoryRPC_ProtocolTypes verifies that all history Args/Result types
// serialize and deserialize correctly through JSON.
func TestHistoryRPC_ProtocolTypes(t *testing.T) {
	// Test HistoryIssueArgs round-trip
	t.Run("HistoryIssueArgs", func(t *testing.T) {
		args := HistoryIssueArgs{IssueID: "bd-test"}
		data, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var decoded HistoryIssueArgs
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if decoded.IssueID != args.IssueID {
			t.Errorf("Expected IssueID %s, got %s", args.IssueID, decoded.IssueID)
		}
	})

	// Test HistoryDiffArgs round-trip
	t.Run("HistoryDiffArgs", func(t *testing.T) {
		args := HistoryDiffArgs{FromRef: "abc123", ToRef: "def456"}
		data, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var decoded HistoryDiffArgs
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if decoded.FromRef != args.FromRef || decoded.ToRef != args.ToRef {
			t.Errorf("Expected %+v, got %+v", args, decoded)
		}
	})

	// Test HistoryIssueDiffArgs round-trip
	t.Run("HistoryIssueDiffArgs", func(t *testing.T) {
		args := HistoryIssueDiffArgs{IssueID: "bd-test", FromRef: "abc", ToRef: "def"}
		data, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var decoded HistoryIssueDiffArgs
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if decoded.IssueID != args.IssueID || decoded.FromRef != args.FromRef || decoded.ToRef != args.ToRef {
			t.Errorf("Expected %+v, got %+v", args, decoded)
		}
	})

	// Test HistoryResolveConflictsArgs round-trip
	t.Run("HistoryResolveConflictsArgs", func(t *testing.T) {
		args := HistoryResolveConflictsArgs{Table: "issues", Strategy: "ours"}
		data, err := json.Marshal(args)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var decoded HistoryResolveConflictsArgs
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if decoded.Table != args.Table || decoded.Strategy != args.Strategy {
			t.Errorf("Expected %+v, got %+v", args, decoded)
		}
	})

	// Test HistoryIssueResult round-trip
	t.Run("HistoryIssueResult", func(t *testing.T) {
		result := HistoryIssueResult{
			Entries: []HistoryEntryRPC{
				{CommitHash: "abc123", Committer: "alice"},
			},
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var decoded HistoryIssueResult
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if len(decoded.Entries) != 1 || decoded.Entries[0].CommitHash != "abc123" {
			t.Errorf("Expected 1 entry with hash abc123, got %+v", decoded.Entries)
		}
	})

	// Test HistoryDiffResult round-trip
	t.Run("HistoryDiffResult", func(t *testing.T) {
		result := HistoryDiffResult{
			Entries: []HistoryDiffEntryRPC{
				{TableName: "issues", DiffType: "modified", FromCommit: "abc", ToCommit: "def"},
			},
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var decoded HistoryDiffResult
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if len(decoded.Entries) != 1 || decoded.Entries[0].TableName != "issues" {
			t.Errorf("Expected 1 entry with table issues, got %+v", decoded.Entries)
		}
	})

	// Test HistoryConflictsResult round-trip
	t.Run("HistoryConflictsResult", func(t *testing.T) {
		result := HistoryConflictsResult{
			Conflicts: []HistoryConflictRPC{
				{TableName: "issues", NumConflicts: 3},
			},
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatalf("Marshal failed: %v", err)
		}
		var decoded HistoryConflictsResult
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("Unmarshal failed: %v", err)
		}
		if len(decoded.Conflicts) != 1 || decoded.Conflicts[0].NumConflicts != 3 {
			t.Errorf("Expected 1 conflict with 3 conflicts, got %+v", decoded.Conflicts)
		}
	})
}

// TestHistoryRPC_HTTPMethodMapping verifies that HTTP method names map to the correct operations.
func TestHistoryRPC_HTTPMethodMapping(t *testing.T) {
	tests := []struct {
		method   string
		expected string
	}{
		{"HistoryIssue", OpHistoryIssue},
		{"HistoryDiff", OpHistoryDiff},
		{"HistoryIssueDiff", OpHistoryIssueDiff},
		{"HistoryConflicts", OpHistoryConflicts},
		{"HistoryResolveConflicts", OpHistoryResolveConflicts},
		{"VersionedDiff", OpVersionedDiff},
	}

	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			op := httpMethodToOperation(tt.method)
			if op != tt.expected {
				t.Errorf("httpMethodToOperation(%q) = %q, want %q", tt.method, op, tt.expected)
			}
		})
	}
}

// TestHistoryIssueDiff_InvalidRefFormat verifies ref validation.
func TestHistoryIssueDiff_InvalidRefFormat(t *testing.T) {
	_, client, cleanup := setupTestServer(t)
	defer cleanup()

	// Even though storage.IsVersioned will fail first with SQLite,
	// we test the ref validation by checking the args parsing.
	// With a Dolt backend, this would trigger the ref validation.
	_, err := client.HistoryIssueDiff(&HistoryIssueDiffArgs{
		IssueID: "bd-test",
		FromRef: "abc; DROP TABLE",
		ToRef:   "def456",
	})
	if err == nil {
		t.Fatal("Expected error for invalid ref or non-versioned backend")
	}
	// Either "versioned storage not available" (SQLite) or "invalid ref format" (Dolt)
	// Both are acceptable errors
}

// containsStr checks if s contains substr (helper to avoid importing strings in test).
func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
