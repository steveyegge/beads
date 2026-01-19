//go:build integration

package testutil

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/tracker/azuredevops"
)

// AzureDevOpsMockServer provides Azure DevOps-specific mock functionality.
type AzureDevOpsMockServer struct {
	*MockTrackerServer
	workItems            []azuredevops.WorkItem
	createWorkItemResult *azuredevops.WorkItem
	projects             []azuredevops.Project
	nextWorkItemID       int
}

// NewAzureDevOpsMockServer creates a new Azure DevOps mock server.
func NewAzureDevOpsMockServer() *AzureDevOpsMockServer {
	m := &AzureDevOpsMockServer{
		MockTrackerServer: NewMockTrackerServer(),
		workItems:         []azuredevops.WorkItem{},
		projects:          []azuredevops.Project{},
		nextWorkItemID:    1000,
	}

	// Set up the default handler for Azure DevOps API routes
	m.SetDefaultHandler(m.handleADORequest)

	return m
}

// handleADORequest handles Azure DevOps-specific API routes.
func (m *AzureDevOpsMockServer) handleADORequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// WIQL query endpoint
	if strings.Contains(path, "/_apis/wit/wiql") && r.Method == "POST" {
		m.handleWIQLQuery(w, r)
		return
	}

	// Batch get work items
	if strings.Contains(path, "/_apis/wit/workitems") && r.Method == "GET" && strings.Contains(r.URL.RawQuery, "ids=") {
		m.handleGetWorkItems(w, r)
		return
	}

	// Get single work item
	if strings.Contains(path, "/_apis/wit/workitems/") && r.Method == "GET" {
		m.handleGetWorkItem(w, r)
		return
	}

	// Create work item
	if strings.Contains(path, "/_apis/wit/workitems/$") && r.Method == "POST" {
		m.handleCreateWorkItem(w, r)
		return
	}

	// Update work item
	if strings.Contains(path, "/_apis/wit/workitems/") && r.Method == "PATCH" {
		m.handleUpdateWorkItem(w, r)
		return
	}

	// List projects
	if strings.Contains(path, "/_apis/projects") && r.Method == "GET" {
		m.handleListProjects(w, r)
		return
	}

	// Default: not found
	w.WriteHeader(http.StatusNotFound)
	writeJSON(w, map[string]string{"error": "Not found"})
}

// handleWIQLQuery handles WIQL query requests.
func (m *AzureDevOpsMockServer) handleWIQLQuery(w http.ResponseWriter, r *http.Request) {
	// Return work item IDs
	refs := make([]azuredevops.WorkItemRef, len(m.workItems))
	for i, wi := range m.workItems {
		refs[i] = azuredevops.WorkItemRef{
			ID:  wi.ID,
			URL: m.Server.URL + "/_apis/wit/workitems/" + strconv.Itoa(wi.ID),
		}
	}

	response := azuredevops.WIQLQueryResponse{
		QueryType:       "flat",
		QueryResultType: "workItem",
		WorkItems:       refs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetWorkItems handles batch get work items requests.
func (m *AzureDevOpsMockServer) handleGetWorkItems(w http.ResponseWriter, r *http.Request) {
	// Parse IDs from query string
	idsParam := r.URL.Query().Get("ids")
	if idsParam == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	idStrings := strings.Split(idsParam, ",")
	requestedIDs := make(map[int]bool)
	for _, idStr := range idStrings {
		id, err := strconv.Atoi(strings.TrimSpace(idStr))
		if err == nil {
			requestedIDs[id] = true
		}
	}

	// Filter work items by requested IDs
	var filtered []azuredevops.WorkItem
	for _, wi := range m.workItems {
		if requestedIDs[wi.ID] {
			filtered = append(filtered, wi)
		}
	}

	response := azuredevops.WorkItemBatchResponse{
		Count: len(filtered),
		Value: filtered,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetWorkItem handles GET requests for individual work items.
func (m *AzureDevOpsMockServer) handleGetWorkItem(w http.ResponseWriter, r *http.Request) {
	// Extract work item ID from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 2 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Find the ID in the path (last numeric segment before query params)
	var idStr string
	for i := len(parts) - 1; i >= 0; i-- {
		if _, err := strconv.Atoi(parts[i]); err == nil {
			idStr = parts[i]
			break
		}
	}

	if idStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	id, _ := strconv.Atoi(idStr)

	for _, wi := range m.workItems {
		if wi.ID == id {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(wi)
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
	writeJSON(w, map[string]string{"error": "Work item not found"})
}

// handleCreateWorkItem handles POST requests to create work items.
func (m *AzureDevOpsMockServer) handleCreateWorkItem(w http.ResponseWriter, r *http.Request) {
	if m.createWorkItemResult != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(m.createWorkItemResult)
		return
	}

	// Auto-generate work item
	m.nextWorkItemID++
	now := time.Now().Format(time.RFC3339)
	workItem := azuredevops.WorkItem{
		ID:  m.nextWorkItemID,
		Rev: 1,
		URL: m.Server.URL + "/_apis/wit/workitems/" + strconv.Itoa(m.nextWorkItemID),
		Fields: azuredevops.WorkItemFields{
			Title:       "New Work Item",
			State:       "New",
			CreatedDate: now,
			ChangedDate: now,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(workItem)
}

// handleUpdateWorkItem handles PATCH requests to update work items.
func (m *AzureDevOpsMockServer) handleUpdateWorkItem(w http.ResponseWriter, r *http.Request) {
	// Extract work item ID from path
	parts := strings.Split(r.URL.Path, "/")
	var idStr string
	for i := len(parts) - 1; i >= 0; i-- {
		if _, err := strconv.Atoi(parts[i]); err == nil {
			idStr = parts[i]
			break
		}
	}

	if idStr == "" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	id, _ := strconv.Atoi(idStr)

	// Find the work item
	for i, wi := range m.workItems {
		if wi.ID == id {
			// Increment revision and return updated work item
			m.workItems[i].Rev++
			m.workItems[i].Fields.ChangedDate = time.Now().Format(time.RFC3339)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(m.workItems[i])
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
	writeJSON(w, map[string]string{"error": "Work item not found"})
}

// handleListProjects handles GET requests to list projects.
func (m *AzureDevOpsMockServer) handleListProjects(w http.ResponseWriter, r *http.Request) {
	response := azuredevops.ProjectListResponse{
		Count: len(m.projects),
		Value: m.projects,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// SetWorkItems configures the work items that will be returned by queries.
func (m *AzureDevOpsMockServer) SetWorkItems(workItems []azuredevops.WorkItem) {
	m.workItems = workItems
}

// AddWorkItem adds a single work item to the mock data.
func (m *AzureDevOpsMockServer) AddWorkItem(workItem azuredevops.WorkItem) {
	m.workItems = append(m.workItems, workItem)
}

// SetCreateWorkItemResponse configures the response for create work item requests.
func (m *AzureDevOpsMockServer) SetCreateWorkItemResponse(workItem *azuredevops.WorkItem) {
	m.createWorkItemResult = workItem
}

// SetProjects configures the projects that will be returned.
func (m *AzureDevOpsMockServer) SetProjects(projects []azuredevops.Project) {
	m.projects = projects
}

// ClearWorkItems removes all mock work items.
func (m *AzureDevOpsMockServer) ClearWorkItems() {
	m.workItems = []azuredevops.WorkItem{}
}

// Helper functions for creating test data

// MakeADOWorkItem creates a test Azure DevOps work item with common defaults.
func MakeADOWorkItem(id int, title, state string) azuredevops.WorkItem {
	now := time.Now().Format(time.RFC3339)
	return azuredevops.WorkItem{
		ID:  id,
		Rev: 1,
		URL: "https://dev.azure.com/testorg/testproj/_apis/wit/workitems/" + strconv.Itoa(id),
		Fields: azuredevops.WorkItemFields{
			Title:         title,
			State:         state,
			WorkItemType:  "Task",
			Priority:      2,
			AreaPath:      "testproj",
			IterationPath: "testproj\\Sprint 1",
			CreatedDate:   now,
			ChangedDate:   now,
		},
		Links: &azuredevops.WorkItemLinks{
			HTML: azuredevops.Link{
				Href: "https://dev.azure.com/testorg/testproj/_workitems/edit/" + strconv.Itoa(id),
			},
		},
	}
}

// MakeADOWorkItemWithDetails creates a test work item with full details.
func MakeADOWorkItemWithDetails(id int, title, description, state, workItemType string, priority int, tags string) azuredevops.WorkItem {
	wi := MakeADOWorkItem(id, title, state)
	wi.Fields.Description = description
	wi.Fields.WorkItemType = workItemType
	wi.Fields.Priority = priority
	wi.Fields.Tags = tags
	return wi
}

// MakeADOWorkItemWithAssignee creates a test work item with an assignee.
func MakeADOWorkItemWithAssignee(id int, title, state, assigneeEmail, assigneeName string) azuredevops.WorkItem {
	wi := MakeADOWorkItem(id, title, state)
	wi.Fields.AssignedTo = &azuredevops.Identity{
		ID:          "user-123",
		DisplayName: assigneeName,
		UniqueName:  assigneeEmail,
	}
	return wi
}

// MakeADOProject creates a test Azure DevOps project.
func MakeADOProject(id, name string) azuredevops.Project {
	return azuredevops.Project{
		ID:         id,
		Name:       name,
		URL:        "https://dev.azure.com/testorg/_apis/projects/" + id,
		State:      "wellFormed",
		Visibility: "private",
	}
}
