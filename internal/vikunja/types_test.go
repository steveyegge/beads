// internal/vikunja/types_test.go
package vikunja

import (
	"encoding/json"
	"testing"
)

func TestTaskUnmarshal(t *testing.T) {
	jsonData := `{
		"id": 123,
		"title": "Test Task",
		"description": "A test description",
		"done": false,
		"priority": 2,
		"project_id": 1,
		"created": "2026-01-15T10:00:00Z",
		"updated": "2026-01-16T12:00:00Z"
	}`

	var task Task
	err := json.Unmarshal([]byte(jsonData), &task)
	if err != nil {
		t.Fatalf("Failed to unmarshal task: %v", err)
	}

	if task.ID != 123 {
		t.Errorf("ID = %d, want 123", task.ID)
	}
	if task.Title != "Test Task" {
		t.Errorf("Title = %q, want %q", task.Title, "Test Task")
	}
	if task.Done != false {
		t.Errorf("Done = %v, want false", task.Done)
	}
	if task.Priority != 2 {
		t.Errorf("Priority = %d, want 2", task.Priority)
	}
}

func TestTaskWithRelations(t *testing.T) {
	jsonData := `{
		"id": 1,
		"title": "Parent Task",
		"related_tasks": {
			"subtask": [{"id": 2, "title": "Child Task"}],
			"blocking": [{"id": 3, "title": "Blocked Task"}]
		}
	}`

	var task Task
	err := json.Unmarshal([]byte(jsonData), &task)
	if err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if len(task.RelatedTasks["subtask"]) != 1 {
		t.Errorf("Expected 1 subtask, got %d", len(task.RelatedTasks["subtask"]))
	}
	if len(task.RelatedTasks["blocking"]) != 1 {
		t.Errorf("Expected 1 blocking task, got %d", len(task.RelatedTasks["blocking"]))
	}
}
