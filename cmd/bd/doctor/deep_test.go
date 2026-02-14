package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRunDeepValidation_NoBeadsDir verifies deep validation handles missing .beads directory
func TestRunDeepValidation_NoBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()
	result := RunDeepValidation(tmpDir)

	if len(result.AllChecks) != 1 {
		t.Errorf("Expected 1 check, got %d", len(result.AllChecks))
	}
	if result.AllChecks[0].Status != StatusOK {
		t.Errorf("Status = %q, want %q", result.AllChecks[0].Status, StatusOK)
	}
}

// TestRunDeepValidation_EmptyBeadsDir verifies deep validation with empty .beads directory
func TestRunDeepValidation_EmptyBeadsDir(t *testing.T) {
	tmpDir := t.TempDir()
	beadsDir := filepath.Join(tmpDir, ".beads")
	if err := os.Mkdir(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}

	result := RunDeepValidation(tmpDir)

	// Should return OK with "no database" message
	if len(result.AllChecks) != 1 {
		t.Errorf("Expected 1 check, got %d", len(result.AllChecks))
	}
	if result.AllChecks[0].Status != StatusOK {
		t.Errorf("Status = %q, want %q", result.AllChecks[0].Status, StatusOK)
	}
}

// TestRunDeepValidation_WithDatabase verifies deep validation with a basic database.
// Skipped: requires SQLite which has been removed. Deep validation with Dolt is
// tested via integration tests.
func TestRunDeepValidation_WithDatabase(t *testing.T) {
	t.Skip("Requires SQLite — deep validation with Dolt is tested via integration tests")
}

// TestCheckParentConsistency_OrphanedDeps verifies detection of orphaned parent-child deps.
// Skipped: requires SQLite which has been removed. Parent consistency with Dolt is
// tested via integration tests.
func TestCheckParentConsistency_OrphanedDeps(t *testing.T) {
	t.Skip("Requires SQLite — parent consistency with Dolt is tested via integration tests")
}

// TestCheckEpicCompleteness_CompletedEpic verifies detection of closeable epics.
// Skipped: requires SQLite which has been removed. Epic completeness with Dolt is
// tested via integration tests.
func TestCheckEpicCompleteness_CompletedEpic(t *testing.T) {
	t.Skip("Requires SQLite — epic completeness with Dolt is tested via integration tests")
}

// TestCheckMailThreadIntegrity_ValidThreads verifies valid thread references pass.
// Skipped: requires SQLite which has been removed. Mail thread integrity with Dolt is
// tested via integration tests.
func TestCheckMailThreadIntegrity_ValidThreads(t *testing.T) {
	t.Skip("Requires SQLite — mail thread integrity with Dolt is tested via integration tests")
}

// TestDeepValidationResultJSON verifies JSON serialization
func TestDeepValidationResultJSON(t *testing.T) {
	result := DeepValidationResult{
		TotalIssues:       10,
		TotalDependencies: 5,
		OverallOK:         true,
		AllChecks: []DoctorCheck{
			{Name: "Test", Status: StatusOK, Message: "All good"},
		},
	}

	jsonBytes, err := DeepValidationResultJSON(result)
	if err != nil {
		t.Fatalf("Failed to serialize: %v", err)
	}

	if len(jsonBytes) == 0 {
		t.Error("Expected non-empty JSON output")
	}

	// Should contain expected fields
	jsonStr := string(jsonBytes)
	if !contains(jsonStr, "total_issues") {
		t.Error("JSON should contain total_issues")
	}
	if !contains(jsonStr, "overall_ok") {
		t.Error("JSON should contain overall_ok")
	}
}
