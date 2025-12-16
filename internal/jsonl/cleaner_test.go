package jsonl

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

func TestDeduplicateIssues(t *testing.T) {
	now := time.Now()
	older := now.Add(-1 * time.Hour)

	issues := []*types.Issue{
		{
			ID:        "bd-123",
			Title:     "First version",
			UpdatedAt: older,
		},
		{
			ID:        "bd-123",
			Title:     "Second version (newer)",
			UpdatedAt: now,
		},
		{
			ID:        "bd-456",
			Title:     "Unique",
			UpdatedAt: now,
		},
	}

	result, cleaned := deduplicateIssues(issues)

	if result.Count != 2 {
		t.Errorf("Expected 2 issues after dedup, got %d", result.Count)
	}
	if result.DuplicateIDCount != 1 {
		t.Errorf("Expected 1 duplicate removed, got %d", result.DuplicateIDCount)
	}

	// Check that we kept the newer version
	for _, issue := range cleaned {
		if issue.ID == "bd-123" && issue.Title != "Second version (newer)" {
			t.Errorf("Did not keep newest version of bd-123")
		}
	}
}

func TestFilterTestPollution(t *testing.T) {
	issues := []*types.Issue{
		{ID: "bd-123", Title: "Real issue"},
		{ID: "bd-9f86-baseline-1", Title: "Baseline pollution"},
		{ID: "bd-da96-baseline-test", Title: "Another baseline"},
		{ID: "bd-456-test-abc", Title: "Test pollution"},
		{ID: "bd-789", Title: "Another real issue"},
	}

	count := 0
	cleaned, rejected := filterTestPollution(issues, &count)

	if count != 3 {
		t.Errorf("Expected 3 test issues removed, got %d", count)
	}
	if len(cleaned) != 2 {
		t.Errorf("Expected 2 real issues, got %d", len(cleaned))
	}
	if len(rejected) != 3 {
		t.Errorf("Expected 3 rejected issues recorded, got %d", len(rejected))
	}

	// Check that real issues were preserved
	for _, issue := range cleaned {
		if issue.ID == "bd-123" || issue.ID == "bd-789" {
			continue
		}
		t.Errorf("Unexpected issue in cleaned list: %s", issue.ID)
	}
}

func TestRepairBrokenReferences(t *testing.T) {
	issues := []*types.Issue{
		{
			ID:    "bd-123",
			Title: "Parent",
		},
		{
			ID:    "bd-456",
			Title: "Child",
			Dependencies: []*types.Dependency{
				{
					IssueID:     "bd-456",
					DependsOnID: "bd-123",
					Type:        types.DepBlocks,
				},
				{
					IssueID:     "bd-456",
					DependsOnID: "deleted:bd-999",
					Type:        types.DepBlocks,
				},
				{
					IssueID:     "bd-456",
					DependsOnID: "bd-nonexistent",
					Type:        types.DepBlocks,
				},
			},
		},
	}

	result := repairBrokenReferences(issues)

	if result.Count != 2 {
		t.Errorf("Expected 2 broken references, got %d", result.Count)
	}

	// Check that valid dependency was preserved
	if len(issues[1].Dependencies) != 1 {
		t.Errorf("Expected 1 valid dependency, got %d", len(issues[1].Dependencies))
	}
	if issues[1].Dependencies[0].DependsOnID != "bd-123" {
		t.Errorf("Valid dependency was removed")
	}
}

func TestCleanIssuesEndToEnd(t *testing.T) {
	now := time.Now()
	older := now.Add(-1 * time.Hour)

	issues := []*types.Issue{
		{
			ID:        "bd-123",
			Title:     "Real issue",
			UpdatedAt: now,
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
		},
		{
			ID:        "bd-123",
			Title:     "Duplicate (old)",
			UpdatedAt: older,
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
		},
		{
			ID:        "bd-9f86-baseline-1",
			Title:     "Test pollution",
			UpdatedAt: now,
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
		},
		{
			ID:        "bd-456",
			Title:     "Issue with broken ref",
			UpdatedAt: now,
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
			Dependencies: []*types.Dependency{
				{
					IssueID:     "bd-456",
					DependsOnID: "bd-123",
					Type:        types.DepBlocks,
				},
				{
					IssueID:     "bd-456",
					DependsOnID: "deleted:bd-999",
					Type:        types.DepBlocks,
				},
			},
		},
	}

	opts := DefaultCleanerOptions()
	cleanResult, cleaned, err := CleanIssues(issues, opts)

	if err != nil {
		t.Fatalf("CleanIssues failed: %v", err)
	}

	// Verify results
	if cleanResult.OriginalCount != 4 {
		t.Errorf("Expected 4 original issues, got %d", cleanResult.OriginalCount)
	}
	if cleanResult.FinalCount != 2 {
		t.Errorf("Expected 2 final issues, got %d", cleanResult.FinalCount)
	}
	if cleanResult.DuplicateIDCount != 1 {
		t.Errorf("Expected 1 duplicate removed, got %d", cleanResult.DuplicateIDCount)
	}
	if cleanResult.TestPollutionCount != 1 {
		t.Errorf("Expected 1 test issue removed, got %d", cleanResult.TestPollutionCount)
	}
	if cleanResult.BrokenReferencesRemoved != 1 {
		t.Errorf("Expected 1 broken reference removed, got %d", cleanResult.BrokenReferencesRemoved)
	}
	if len(cleanResult.RejectedDuplicates) != 1 {
		t.Errorf("Expected 1 duplicate removal recorded, got %d", len(cleanResult.RejectedDuplicates))
	}
	if len(cleanResult.RejectedTestPollution) != 1 {
		t.Errorf("Expected 1 test pollution rejection recorded, got %d", len(cleanResult.RejectedTestPollution))
	}

	// Verify the cleaned issues
	ids := make(map[string]bool)
	for _, issue := range cleaned {
		ids[issue.ID] = true
	}

	if !ids["bd-123"] {
		t.Errorf("Real issue bd-123 was removed")
	}
	if !ids["bd-456"] {
		t.Errorf("Real issue bd-456 was removed")
	}
	if ids["bd-9f86-baseline-1"] {
		t.Errorf("Test pollution issue was not removed")
	}

	// Check that bd-456's broken reference was removed
	for _, issue := range cleaned {
		if issue.ID == "bd-456" {
			if len(issue.Dependencies) != 1 {
				t.Errorf("Expected 1 dependency in bd-456, got %d", len(issue.Dependencies))
			}
		}
	}
}

func TestValidateIssues(t *testing.T) {
	now := time.Now()

	issues := []*types.Issue{
		{
			ID:        "bd-123",
			Title:     "Good issue",
			UpdatedAt: now,
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
		},
		{
			ID:        "bd-123",
			Title:     "Duplicate",
			UpdatedAt: now,
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
		},
		{
			ID:        "bd-test-1",
			Title:     "Test pollution",
			UpdatedAt: now,
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
		},
		{
			ID:    "bd-456",
			Title: "Issue with broken ref",
			Dependencies: []*types.Dependency{
				{
					IssueID:     "bd-456",
					DependsOnID: "deleted:bd-999",
					Type:        types.DepBlocks,
				},
			},
			UpdatedAt: now,
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
		},
	}

	report := ValidateIssues(issues)

	if report.TotalIssues != 4 {
		t.Errorf("Expected 4 total issues, got %d", report.TotalIssues)
	}
	if len(report.DuplicateIDs) != 1 {
		t.Errorf("Expected 1 duplicate ID, got %d", len(report.DuplicateIDs))
	}
	if len(report.TestPollutionIDs) != 1 {
		t.Errorf("Expected 1 test pollution ID, got %d", len(report.TestPollutionIDs))
	}
	if len(report.BrokenReferences) != 1 {
		t.Errorf("Expected 1 issue with broken references, got %d", len(report.BrokenReferences))
	}

	if !report.HasIssues() {
		t.Errorf("Report should have issues")
	}
}

func TestValidateIssuesClean(t *testing.T) {
	now := time.Now()

	issues := []*types.Issue{
		{
			ID:        "bd-123",
			Title:     "Good issue",
			UpdatedAt: now,
			Status:    types.StatusOpen,
			Priority:  1,
			IssueType: types.TypeBug,
		},
		{
			ID:        "bd-456",
			Title:     "Another good issue",
			UpdatedAt: now,
			Status:    types.StatusClosed,
			Priority:  2,
			IssueType: types.TypeFeature,
			ClosedAt:  &now,
		},
	}

	report := ValidateIssues(issues)

	if report.HasIssues() {
		t.Errorf("Clean report should have no issues. Summary:\n%s", report.Summary())
	}
}

func TestSaveRejectionManifest(t *testing.T) {
	now := time.Now()
	older := now.Add(-1 * time.Hour)
	
	// Create a temporary directory for test output
	tmpDir := t.TempDir()

	// Create a CleanResult with some rejected issues
	result := &CleanResult{
		OriginalCount: 4,
		FinalCount:    2,
		RejectedDuplicates: []*DuplicateRemoval{
			{
				ID: "bd-123",
				KeptVersion: &types.Issue{
					ID:        "bd-123",
					Title:     "Kept version",
					UpdatedAt: now,
				},
				RemovedVersions: []*types.Issue{
					{
						ID:        "bd-123",
						Title:     "Old version",
						UpdatedAt: older,
					},
				},
			},
		},
		RejectedTestPollution: []*RejectedIssue{
			{
				Issue: &types.Issue{
					ID:    "bd-9f86-baseline-1",
					Title: "Test pollution",
				},
				Reason: "matches known baseline prefix: bd-9f86-baseline-",
			},
		},
	}

	// Save the rejection manifest
	err := SaveRejectionManifest(tmpDir, result)
	if err != nil {
		t.Fatalf("SaveRejectionManifest failed: %v", err)
	}

	// Verify the file was created
	manifestPath := filepath.Join(tmpDir, "cleaning-rejects.jsonl")
	data, err := ioutil.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("Could not read manifest file: %v", err)
	}

	// Verify content is valid JSON lines
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 2 {
		t.Errorf("Expected at least 2 rejection lines, got %d", len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		if line == "" {
			continue
		}
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Errorf("Line %d is not valid JSON: %v", i, err)
		}
		// Should have the fields we set
		if _, hasIssue := obj["issue"]; !hasIssue {
			t.Errorf("Line %d missing 'issue' field", i)
		}
		if _, hasReason := obj["rejection_reason"]; !hasReason {
			t.Errorf("Line %d missing 'rejection_reason' field", i)
		}
	}
}
