package jira

import (
	"testing"
)

func TestSyncStats(t *testing.T) {
	stats := SyncStats{}

	if stats.Pulled != 0 {
		t.Errorf("expected Pulled to be 0, got %d", stats.Pulled)
	}
	if stats.Pushed != 0 {
		t.Errorf("expected Pushed to be 0, got %d", stats.Pushed)
	}
	if stats.Created != 0 {
		t.Errorf("expected Created to be 0, got %d", stats.Created)
	}
	if stats.Updated != 0 {
		t.Errorf("expected Updated to be 0, got %d", stats.Updated)
	}
	if stats.Skipped != 0 {
		t.Errorf("expected Skipped to be 0, got %d", stats.Skipped)
	}
	if stats.Errors != 0 {
		t.Errorf("expected Errors to be 0, got %d", stats.Errors)
	}
	if stats.Conflicts != 0 {
		t.Errorf("expected Conflicts to be 0, got %d", stats.Conflicts)
	}
}

func TestSyncResult(t *testing.T) {
	result := SyncResult{
		Success: true,
		Stats: SyncStats{
			Created: 5,
			Updated: 3,
		},
		LastSync: "2025-01-15T10:30:00Z",
	}

	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.Stats.Created != 5 {
		t.Errorf("expected Created to be 5, got %d", result.Stats.Created)
	}
	if result.Stats.Updated != 3 {
		t.Errorf("expected Updated to be 3, got %d", result.Stats.Updated)
	}
	if result.LastSync != "2025-01-15T10:30:00Z" {
		t.Errorf("unexpected LastSync value: %s", result.LastSync)
	}
	if result.Error != "" {
		t.Errorf("expected Error to be empty, got %s", result.Error)
	}
	if len(result.Warnings) != 0 {
		t.Errorf("expected Warnings to be empty, got %v", result.Warnings)
	}
}

func TestPullStats(t *testing.T) {
	stats := PullStats{
		Created: 10,
		Updated: 5,
		Skipped: 2,
	}

	if stats.Created != 10 {
		t.Errorf("expected Created to be 10, got %d", stats.Created)
	}
	if stats.Updated != 5 {
		t.Errorf("expected Updated to be 5, got %d", stats.Updated)
	}
	if stats.Skipped != 2 {
		t.Errorf("expected Skipped to be 2, got %d", stats.Skipped)
	}
}

func TestPushStats(t *testing.T) {
	stats := PushStats{
		Created: 8,
		Updated: 4,
		Skipped: 1,
		Errors:  2,
	}

	if stats.Created != 8 {
		t.Errorf("expected Created to be 8, got %d", stats.Created)
	}
	if stats.Updated != 4 {
		t.Errorf("expected Updated to be 4, got %d", stats.Updated)
	}
	if stats.Skipped != 1 {
		t.Errorf("expected Skipped to be 1, got %d", stats.Skipped)
	}
	if stats.Errors != 2 {
		t.Errorf("expected Errors to be 2, got %d", stats.Errors)
	}
}
