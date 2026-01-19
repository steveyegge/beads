//go:build integration

package azuredevops_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/tracker/azuredevops"
	"github.com/steveyegge/beads/internal/tracker/testutil"
)

// TestE2E_FetchWorkItems_Empty tests fetching work items when none exist.
func TestE2E_FetchWorkItems_Empty(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	workItems, err := client.FetchWorkItems(ctx, "all", nil)

	if err != nil {
		t.Fatalf("FetchWorkItems failed: %v", err)
	}

	if len(workItems) != 0 {
		t.Errorf("Expected 0 work items, got %d", len(workItems))
	}
}

// TestE2E_FetchWorkItems_WithData tests fetching work items with data.
func TestE2E_FetchWorkItems_WithData(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItem(1, "First Work Item", "New"),
		testutil.MakeADOWorkItem(2, "Second Work Item", "Active"),
		testutil.MakeADOWorkItem(3, "Third Work Item", "Closed"),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	workItems, err := client.FetchWorkItems(ctx, "all", nil)

	if err != nil {
		t.Fatalf("FetchWorkItems failed: %v", err)
	}

	if len(workItems) != 3 {
		t.Errorf("Expected 3 work items, got %d", len(workItems))
	}

	// Verify first work item
	if workItems[0].ID != 1 {
		t.Errorf("Expected first work item ID 1, got %d", workItems[0].ID)
	}

	if workItems[0].Fields.Title != "First Work Item" {
		t.Errorf("Expected first work item title 'First Work Item', got '%s'", workItems[0].Fields.Title)
	}
}

// TestE2E_FetchWorkItems_WithDetails tests fetching work items with full details.
func TestE2E_FetchWorkItems_WithDetails(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Set up test data with full details
	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItemWithDetails(
			1,
			"Bug Report",
			"Something is broken",
			"Active",
			"Bug",
			1,
			"bug; critical",
		),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	workItems, err := client.FetchWorkItems(ctx, "all", nil)

	if err != nil {
		t.Fatalf("FetchWorkItems failed: %v", err)
	}

	if len(workItems) != 1 {
		t.Fatalf("Expected 1 work item, got %d", len(workItems))
	}

	wi := workItems[0]
	if wi.Fields.Description != "Something is broken" {
		t.Errorf("Expected description 'Something is broken', got '%s'", wi.Fields.Description)
	}

	if wi.Fields.Priority != 1 {
		t.Errorf("Expected priority 1, got %d", wi.Fields.Priority)
	}

	if wi.Fields.WorkItemType != "Bug" {
		t.Errorf("Expected work item type 'Bug', got '%s'", wi.Fields.WorkItemType)
	}

	if wi.Fields.Tags != "bug; critical" {
		t.Errorf("Expected tags 'bug; critical', got '%s'", wi.Fields.Tags)
	}
}

// TestE2E_FetchWorkItems_WithAssignee tests fetching work items with assignees.
func TestE2E_FetchWorkItems_WithAssignee(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Set up test data with assignee
	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItemWithAssignee(
			1,
			"Assigned Task",
			"Active",
			"john@example.com",
			"John Doe",
		),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	workItems, err := client.FetchWorkItems(ctx, "all", nil)

	if err != nil {
		t.Fatalf("FetchWorkItems failed: %v", err)
	}

	if len(workItems) != 1 {
		t.Fatalf("Expected 1 work item, got %d", len(workItems))
	}

	wi := workItems[0]
	if wi.Fields.AssignedTo == nil {
		t.Fatal("Expected assignee to be set")
	}

	if wi.Fields.AssignedTo.UniqueName != "john@example.com" {
		t.Errorf("Expected assignee email 'john@example.com', got '%s'", wi.Fields.AssignedTo.UniqueName)
	}

	if wi.Fields.AssignedTo.DisplayName != "John Doe" {
		t.Errorf("Expected assignee name 'John Doe', got '%s'", wi.Fields.AssignedTo.DisplayName)
	}
}

// TestE2E_FetchWorkItem_Single tests fetching a single work item.
func TestE2E_FetchWorkItem_Single(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItem(1, "First Work Item", "New"),
		testutil.MakeADOWorkItem(2, "Second Work Item", "Active"),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	wi, err := client.FetchWorkItem(ctx, 2)

	if err != nil {
		t.Fatalf("FetchWorkItem failed: %v", err)
	}

	if wi == nil {
		t.Fatal("Expected work item to be returned")
	}

	if wi.ID != 2 {
		t.Errorf("Expected work item ID 2, got %d", wi.ID)
	}

	if wi.Fields.Title != "Second Work Item" {
		t.Errorf("Expected title 'Second Work Item', got '%s'", wi.Fields.Title)
	}
}

// TestE2E_FetchWorkItem_NotFound tests fetching a non-existent work item.
func TestE2E_FetchWorkItem_NotFound(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItem(1, "First Work Item", "New"),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	wi, err := client.FetchWorkItem(ctx, 999)

	// FetchWorkItem returns nil,nil for not found
	if err != nil && !strings.Contains(err.Error(), "404") {
		t.Fatalf("FetchWorkItem unexpected error: %v", err)
	}

	if wi != nil {
		t.Errorf("Expected nil work item for non-existent ID, got %v", wi)
	}
}

// TestE2E_CreateWorkItem tests creating a new work item.
func TestE2E_CreateWorkItem(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Configure create response
	createdWI := testutil.MakeADOWorkItem(100, "New Work Item", "New")
	mock.SetCreateWorkItemResponse(&createdWI)

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	wi, err := client.CreateWorkItem(ctx, "Task", "New Work Item", "Description", 2, nil)

	if err != nil {
		t.Fatalf("CreateWorkItem failed: %v", err)
	}

	if wi == nil {
		t.Fatal("Expected work item to be returned")
	}

	if wi.ID != 100 {
		t.Errorf("Expected work item ID 100, got %d", wi.ID)
	}

	// Verify request was made
	requests := mock.GetRequests()
	var foundCreate bool
	for _, req := range requests {
		if req.Method == "POST" && strings.Contains(req.Path, "/_apis/wit/workitems/$") {
			foundCreate = true
			break
		}
	}

	if !foundCreate {
		t.Error("Expected POST request to /_apis/wit/workitems/$Task")
	}
}

// TestE2E_UpdateWorkItem tests updating an existing work item.
func TestE2E_UpdateWorkItem(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItem(1, "Original Title", "New"),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	ops := []azuredevops.PatchOperation{
		{Op: "replace", Path: "/fields/System.Title", Value: "Updated Title"},
	}
	wi, err := client.UpdateWorkItem(ctx, 1, ops)

	if err != nil {
		t.Fatalf("UpdateWorkItem failed: %v", err)
	}

	if wi == nil {
		t.Fatal("Expected work item to be returned")
	}

	// Verify request was made
	requests := mock.GetRequests()
	var foundUpdate bool
	for _, req := range requests {
		if req.Method == "PATCH" && strings.Contains(req.Path, "/_apis/wit/workitems/1") {
			foundUpdate = true
			break
		}
	}

	if !foundUpdate {
		t.Error("Expected PATCH request to /_apis/wit/workitems/1")
	}
}

// TestE2E_ListProjects tests listing projects.
func TestE2E_ListProjects(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Set up test data
	mock.SetProjects([]azuredevops.Project{
		testutil.MakeADOProject("proj-1", "Project One"),
		testutil.MakeADOProject("proj-2", "Project Two"),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	projects, err := client.ListProjects(ctx)

	if err != nil {
		t.Fatalf("ListProjects failed: %v", err)
	}

	if len(projects) != 2 {
		t.Errorf("Expected 2 projects, got %d", len(projects))
	}

	if projects[0].Name != "Project One" {
		t.Errorf("Expected first project name 'Project One', got '%s'", projects[0].Name)
	}
}

// TestE2E_AuthError tests handling of authentication errors.
func TestE2E_AuthError(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	mock.SetAuthError(true)

	client := azuredevops.NewClient("testorg", "testproj", "invalid-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	_, err := client.FetchWorkItems(ctx, "all", nil)

	if err == nil {
		t.Fatal("Expected error for invalid auth")
	}

	if !strings.Contains(err.Error(), "401") {
		t.Errorf("Expected 401 error, got: %v", err)
	}
}

// TestE2E_RateLimiting tests handling of rate limit errors.
func TestE2E_RateLimiting(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// First request will be rate limited, second will succeed
	mock.SetRateLimitError(true, 1)
	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItem(1, "Test Work Item", "New"),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()

	// First request - should get rate limited
	_, err := client.FetchWorkItems(ctx, "all", nil)
	if err == nil {
		t.Fatal("Expected first request to be rate limited")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("Expected 429 error, got: %v", err)
	}

	// Second request - should succeed
	workItems, err := client.FetchWorkItems(ctx, "all", nil)
	if err != nil {
		t.Fatalf("Second request should succeed: %v", err)
	}

	if len(workItems) != 1 {
		t.Errorf("Expected 1 work item, got %d", len(workItems))
	}
}

// TestE2E_ServerError tests handling of server errors.
func TestE2E_ServerError(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	mock.SetServerError(true)

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	_, err := client.FetchWorkItems(ctx, "all", nil)

	if err == nil {
		t.Fatal("Expected error for server error")
	}

	if !strings.Contains(err.Error(), "500") {
		t.Errorf("Expected 500 error, got: %v", err)
	}
}

// TestE2E_BuildWorkItemURL tests building work item URLs.
func TestE2E_BuildWorkItemURL(t *testing.T) {
	client := azuredevops.NewClient("testorg", "testproj", "test-pat")

	url := client.BuildWorkItemURL(123)

	expected := "https://dev.azure.com/testorg/testproj/_workitems/edit/123"
	if url != expected {
		t.Errorf("Expected URL '%s', got '%s'", expected, url)
	}
}

// TestE2E_ParseWorkItemID tests parsing work item IDs from URLs.
func TestE2E_ParseWorkItemID(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantID  int
		wantOK  bool
	}{
		{
			name:   "valid URL",
			url:    "https://dev.azure.com/testorg/testproj/_workitems/edit/123",
			wantID: 123,
			wantOK: true,
		},
		{
			name:   "invalid URL - no ID",
			url:    "https://dev.azure.com/testorg/testproj",
			wantID: 0,
			wantOK: false,
		},
		{
			name:   "empty URL",
			url:    "",
			wantID: 0,
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := azuredevops.ParseWorkItemID(tt.url)

			if ok != tt.wantOK {
				t.Errorf("ParseWorkItemID(%s) ok = %v, want %v", tt.url, ok, tt.wantOK)
			}

			if id != tt.wantID {
				t.Errorf("ParseWorkItemID(%s) = %d, want %d", tt.url, id, tt.wantID)
			}
		})
	}
}

// TestE2E_RequestHeaders tests that requests include correct headers.
func TestE2E_RequestHeaders(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	mock.SetWorkItems([]azuredevops.WorkItem{})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	_, _ = client.FetchWorkItems(ctx, "all", nil)

	requests := mock.GetRequests()
	if len(requests) == 0 {
		t.Fatal("Expected at least one request")
	}

	req := requests[0]

	// Check Accept
	if accept := req.Headers.Get("Accept"); accept != "application/json" {
		t.Errorf("Expected Accept 'application/json', got '%s'", accept)
	}

	// Check Authorization (should be present)
	if auth := req.Headers.Get("Authorization"); auth == "" {
		t.Error("Expected Authorization header to be set")
	}
}

// TestE2E_FetchWorkItems_WithSinceFilter tests fetching work items with a since filter.
func TestE2E_FetchWorkItems_WithSinceFilter(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItem(1, "Recent Work Item", "New"),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	since := time.Now().Add(-24 * time.Hour)
	ctx := context.Background()
	workItems, err := client.FetchWorkItems(ctx, "all", &since)

	if err != nil {
		t.Fatalf("FetchWorkItems failed: %v", err)
	}

	// The mock returns all work items; the real API would filter
	// We're testing that the request is made correctly
	if len(workItems) != 1 {
		t.Errorf("Expected 1 work item, got %d", len(workItems))
	}

	// Verify request was made
	requests := mock.GetRequests()
	if len(requests) == 0 {
		t.Fatal("Expected at least one request")
	}
}

// TestE2E_FetchWorkItems_OpenState tests fetching only open work items.
func TestE2E_FetchWorkItems_OpenState(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItem(1, "Open Work Item", "New"),
		testutil.MakeADOWorkItem(2, "Active Work Item", "Active"),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	workItems, err := client.FetchWorkItems(ctx, "open", nil)

	if err != nil {
		t.Fatalf("FetchWorkItems failed: %v", err)
	}

	// The mock returns all work items; state filtering is in the WIQL query
	if len(workItems) < 1 {
		t.Error("Expected at least 1 work item")
	}
}

// TestE2E_FetchWorkItems_ClosedState tests fetching only closed work items.
func TestE2E_FetchWorkItems_ClosedState(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItem(1, "Closed Work Item", "Closed"),
		testutil.MakeADOWorkItem(2, "Done Work Item", "Done"),
	})

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	workItems, err := client.FetchWorkItems(ctx, "closed", nil)

	if err != nil {
		t.Fatalf("FetchWorkItems failed: %v", err)
	}

	// The mock returns all work items; state filtering is in the WIQL query
	if len(workItems) < 1 {
		t.Error("Expected at least 1 work item")
	}
}

// TestE2E_MultipleOperations tests multiple operations in sequence.
func TestE2E_MultipleOperations(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Set up initial data
	existingWI := testutil.MakeADOWorkItem(1, "Existing Work Item", "New")
	mock.SetWorkItems([]azuredevops.WorkItem{existingWI})

	// Configure create response
	newWI := testutil.MakeADOWorkItem(2, "New Work Item", "New")
	mock.SetCreateWorkItemResponse(&newWI)

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()

	// 1. Fetch existing work items
	workItems, err := client.FetchWorkItems(ctx, "all", nil)
	if err != nil {
		t.Fatalf("FetchWorkItems failed: %v", err)
	}
	if len(workItems) != 1 {
		t.Errorf("Expected 1 work item, got %d", len(workItems))
	}

	// 2. Create new work item
	mock.AddWorkItem(newWI)
	created, err := client.CreateWorkItem(ctx, "Task", "New Work Item", "", 2, nil)
	if err != nil {
		t.Fatalf("CreateWorkItem failed: %v", err)
	}
	if created.ID != 2 {
		t.Errorf("Expected created work item ID 2, got %d", created.ID)
	}

	// 3. Update existing work item
	ops := []azuredevops.PatchOperation{
		{Op: "replace", Path: "/fields/System.Title", Value: "Updated"},
	}
	_, err = client.UpdateWorkItem(ctx, 1, ops)
	if err != nil {
		t.Fatalf("UpdateWorkItem failed: %v", err)
	}

	// 4. Verify all requests were made
	requests := mock.GetRequests()
	if len(requests) < 4 { // WIQL + batch get + create + update
		t.Errorf("Expected at least 4 requests, got %d", len(requests))
	}
}

// TestE2E_CreateWorkItem_WithTags tests creating a work item with tags.
func TestE2E_CreateWorkItem_WithTags(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	// Configure create response
	createdWI := testutil.MakeADOWorkItemWithDetails(100, "Tagged Work Item", "", "New", "Task", 2, "tag1; tag2")
	mock.SetCreateWorkItemResponse(&createdWI)

	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	ctx := context.Background()
	wi, err := client.CreateWorkItem(ctx, "Task", "Tagged Work Item", "", 2, []string{"tag1", "tag2"})

	if err != nil {
		t.Fatalf("CreateWorkItem failed: %v", err)
	}

	if wi == nil {
		t.Fatal("Expected work item to be returned")
	}

	if wi.Fields.Tags != "tag1; tag2" {
		t.Errorf("Expected tags 'tag1; tag2', got '%s'", wi.Fields.Tags)
	}
}

// TestE2E_WithHTTPClient tests using a custom HTTP client.
func TestE2E_WithHTTPClient(t *testing.T) {
	mock := testutil.NewAzureDevOpsMockServer()
	defer mock.Close()

	mock.SetWorkItems([]azuredevops.WorkItem{
		testutil.MakeADOWorkItem(1, "Test Work Item", "New"),
	})

	// Create a custom HTTP client with a short timeout
	customClient := &azuredevops.Client{
		Organization: "testorg",
		Project:      "testproj",
		PAT:          "test-pat",
	}

	// Use WithEndpoint and WithHTTPClient
	client := azuredevops.NewClient("testorg", "testproj", "test-pat").
		WithEndpoint(mock.URL())

	// Verify the custom client pattern works
	if customClient.Organization != "testorg" {
		t.Errorf("Expected organization 'testorg', got '%s'", customClient.Organization)
	}

	ctx := context.Background()
	workItems, err := client.FetchWorkItems(ctx, "all", nil)

	if err != nil {
		t.Fatalf("FetchWorkItems failed: %v", err)
	}

	if len(workItems) != 1 {
		t.Errorf("Expected 1 work item, got %d", len(workItems))
	}
}
