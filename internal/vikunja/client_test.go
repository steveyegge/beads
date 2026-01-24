// internal/vikunja/client_test.go
package vikunja

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	client := NewClient("https://vikunja.example.com/api/v1", "test-token")

	if client.BaseURL != "https://vikunja.example.com/api/v1" {
		t.Errorf("BaseURL = %q, want %q", client.BaseURL, "https://vikunja.example.com/api/v1")
	}
	if client.Token != "test-token" {
		t.Errorf("Token not set correctly")
	}
	if client.HTTPClient == nil {
		t.Error("HTTPClient is nil")
	}
}

func TestClientWithProjectID(t *testing.T) {
	client := NewClient("https://vikunja.example.com/api/v1", "test-token")
	client = client.WithProjectID(123)

	if client.ProjectID != 123 {
		t.Errorf("ProjectID = %d, want 123", client.ProjectID)
	}
}

func TestClientRequest(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer test-token")
		}

		// Verify content type for POST
		if r.Method == "POST" {
			ct := r.Header.Get("Content-Type")
			if ct != "application/json" {
				t.Errorf("Content-Type = %q, want %q", ct, "application/json")
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": 1, "title": "Test"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx := context.Background()

	resp, err := client.request(ctx, "GET", "/test", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["title"] != "Test" {
		t.Errorf("title = %v, want Test", result["title"])
	}
}

func TestClientRequestRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error": "rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"success": true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx := context.Background()

	resp, err := client.request(ctx, "GET", "/test", nil)
	if err != nil {
		t.Fatalf("request failed after retries: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(resp, &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result["success"] != true {
		t.Errorf("success = %v, want true", result["success"])
	}

	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
}

func TestClientRequestError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message": "not found"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx := context.Background()

	_, err := client.request(ctx, "GET", "/nonexistent", nil)
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestFetchTasks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks" {
			t.Errorf("Path = %q, want /tasks", r.URL.Path)
		}

		// Check filter includes project_id
		filter := r.URL.Query().Get("filter")
		if filter == "" || !contains(filter, "project_id = 1") {
			t.Errorf("filter should contain project_id = 1, got %q", filter)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[
			{"id": 1, "title": "Task 1", "done": false},
			{"id": 2, "title": "Task 2", "done": true}
		]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token").
		WithProjectID(1)

	ctx := context.Background()
	tasks, err := client.FetchTasks(ctx, "all")
	if err != nil {
		t.Fatalf("FetchTasks failed: %v", err)
	}

	if len(tasks) != 2 {
		t.Errorf("Got %d tasks, want 2", len(tasks))
	}
	if tasks[0].Title != "Task 1" {
		t.Errorf("First task title = %q, want %q", tasks[0].Title, "Task 1")
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestFetchTasksFilterOpen(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("filter")
		// Should contain both project_id and done = false
		if !contains(filter, "project_id = 1") || !contains(filter, "done = false") {
			t.Errorf("filter should contain project_id and done = false, got %q", filter)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id": 1, "title": "Open Task", "done": false}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token").
		WithProjectID(1)

	ctx := context.Background()
	tasks, err := client.FetchTasks(ctx, "open")
	if err != nil {
		t.Fatalf("FetchTasks failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Got %d tasks, want 1", len(tasks))
	}
}

func TestFetchTasksRequiresProjectID(t *testing.T) {
	client := NewClient("https://example.com", "test-token")
	ctx := context.Background()

	_, err := client.FetchTasks(ctx, "all")
	if err == nil {
		t.Error("expected error when ProjectID is not set")
	}
}

func TestFetchTasksSince(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		filter := r.URL.Query().Get("filter")
		// Should contain project_id and updated > "timestamp"
		if !contains(filter, "project_id = 1") || !contains(filter, "updated >") {
			t.Errorf("filter should contain project_id and updated, got %q", filter)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[{"id": 1, "title": "Updated Task", "done": false}]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token").
		WithProjectID(1)

	ctx := context.Background()
	since := time.Now().Add(-24 * time.Hour)
	tasks, err := client.FetchTasksSince(ctx, "all", since)
	if err != nil {
		t.Fatalf("FetchTasksSince failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Errorf("Got %d tasks, want 1", len(tasks))
	}
}

func TestFetchTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/123" {
			t.Errorf("Path = %q, want /tasks/123", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": 123, "title": "Single Task", "done": false}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx := context.Background()

	task, err := client.FetchTask(ctx, 123)
	if err != nil {
		t.Fatalf("FetchTask failed: %v", err)
	}

	if task.ID != 123 {
		t.Errorf("task ID = %d, want 123", task.ID)
	}
	if task.Title != "Single Task" {
		t.Errorf("task title = %q, want %q", task.Title, "Single Task")
	}
}

func TestCreateTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("Method = %q, want PUT", r.Method)
		}
		if r.URL.Path != "/projects/1/tasks" {
			t.Errorf("Path = %q, want /projects/1/tasks", r.URL.Path)
		}

		var task Task
		json.NewDecoder(r.Body).Decode(&task)

		// Return created task with ID
		task.ID = 999
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(task)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token").WithProjectID(1)
	ctx := context.Background()

	task := &Task{
		Title:       "New Task",
		Description: "Description",
		Priority:    2,
	}

	created, err := client.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	if created.ID != 999 {
		t.Errorf("Created task ID = %d, want 999", created.ID)
	}
}

func TestCreateTaskRequiresProjectID(t *testing.T) {
	client := NewClient("https://example.com", "test-token")
	ctx := context.Background()

	_, err := client.CreateTask(ctx, &Task{Title: "Test"})
	if err == nil {
		t.Error("expected error when ProjectID is not set")
	}
}

func TestUpdateTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("Method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/tasks/123" {
			t.Errorf("Path = %q, want /tasks/123", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"id": 123, "title": "Updated Task", "done": true}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx := context.Background()

	updates := map[string]any{
		"title": "Updated Task",
		"done":  true,
	}

	updated, err := client.UpdateTask(ctx, 123, updates)
	if err != nil {
		t.Fatalf("UpdateTask failed: %v", err)
	}

	if updated.Title != "Updated Task" {
		t.Errorf("Updated task title = %q, want %q", updated.Title, "Updated Task")
	}
	if !updated.Done {
		t.Error("Updated task should be done")
	}
}

func TestFetchProjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects" {
			t.Errorf("Path = %q, want /projects", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`[
			{"id": 1, "title": "Project 1", "identifier": "P1"},
			{"id": 2, "title": "Project 2", "identifier": "P2"}
		]`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx := context.Background()

	projects, err := client.FetchProjects(ctx)
	if err != nil {
		t.Fatalf("FetchProjects failed: %v", err)
	}

	if len(projects) != 2 {
		t.Errorf("Got %d projects, want 2", len(projects))
	}
	if projects[0].Title != "Project 1" {
		t.Errorf("First project title = %q, want %q", projects[0].Title, "Project 1")
	}
}

func TestFetchProject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/projects/1" {
			t.Errorf("Path = %q, want /projects/1", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{
			"id": 1,
			"title": "Project 1",
			"identifier": "P1",
			"views": [
				{"id": 10, "title": "List View", "view_kind": "list"}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx := context.Background()

	project, err := client.FetchProject(ctx, 1)
	if err != nil {
		t.Fatalf("FetchProject failed: %v", err)
	}

	if project.ID != 1 {
		t.Errorf("project ID = %d, want 1", project.ID)
	}
	if project.Title != "Project 1" {
		t.Errorf("project title = %q, want %q", project.Title, "Project 1")
	}
	if len(project.Views) != 1 {
		t.Errorf("Got %d views, want 1", len(project.Views))
	}
}

func TestCreateRelation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			t.Errorf("Method = %q, want PUT", r.Method)
		}
		if r.URL.Path != "/tasks/1/relations" {
			t.Errorf("Path = %q, want /tasks/1/relations", r.URL.Path)
		}

		var relation TaskRelation
		json.NewDecoder(r.Body).Decode(&relation)

		if relation.OtherTaskID != 2 {
			t.Errorf("OtherTaskID = %d, want 2", relation.OtherTaskID)
		}
		if relation.RelationKind != "blocking" {
			t.Errorf("RelationKind = %q, want %q", relation.RelationKind, "blocking")
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"task_id": 1, "other_task_id": 2, "relation_kind": "blocking"}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx := context.Background()

	err := client.CreateRelation(ctx, 1, 2, "blocking")
	if err != nil {
		t.Fatalf("CreateRelation failed: %v", err)
	}
}

func TestDeleteRelation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("Method = %q, want DELETE", r.Method)
		}
		if r.URL.Path != "/tasks/1/relations/blocking/2" {
			t.Errorf("Path = %q, want /tasks/1/relations/blocking/2", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token")
	ctx := context.Background()

	err := client.DeleteRelation(ctx, 1, "blocking", 2)
	if err != nil {
		t.Fatalf("DeleteRelation failed: %v", err)
	}
}

func TestPagination(t *testing.T) {
	page := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		pageParam := r.URL.Query().Get("page")
		perPage := r.URL.Query().Get("per_page")

		if perPage != "50" {
			t.Errorf("per_page = %q, want 50", perPage)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Return full page for first request, partial for second
		if pageParam == "1" {
			// Return DefaultPerPage items
			tasks := make([]map[string]any, 50)
			for i := 0; i < 50; i++ {
				tasks[i] = map[string]any{"id": i + 1, "title": "Task"}
			}
			json.NewEncoder(w).Encode(tasks)
		} else {
			// Return less than DefaultPerPage to signal end
			tasks := []map[string]any{
				{"id": 51, "title": "Last Task"},
			}
			json.NewEncoder(w).Encode(tasks)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-token").
		WithProjectID(1)

	ctx := context.Background()
	tasks, err := client.FetchTasks(ctx, "all")
	if err != nil {
		t.Fatalf("FetchTasks with pagination failed: %v", err)
	}

	if len(tasks) != 51 {
		t.Errorf("Got %d tasks, want 51", len(tasks))
	}
	if page != 2 {
		t.Errorf("Made %d page requests, want 2", page)
	}
}
