//go:build integration

package testutil

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/tracker/jira"
)

// JiraMockServer provides Jira-specific mock functionality.
type JiraMockServer struct {
	*MockTrackerServer
	issues            []jira.Issue
	createIssueResult *jira.Issue
	nextIssueID       int
}

// NewJiraMockServer creates a new Jira mock server.
func NewJiraMockServer() *JiraMockServer {
	m := &JiraMockServer{
		MockTrackerServer: NewMockTrackerServer(),
		issues:            []jira.Issue{},
		nextIssueID:       1000,
	}

	// Set up the default handler for Jira API routes
	m.SetDefaultHandler(m.handleJiraRequest)

	return m
}

// handleJiraRequest handles Jira-specific API routes.
func (m *JiraMockServer) handleJiraRequest(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	// Search endpoint
	if strings.Contains(path, "/rest/api/3/search") || strings.Contains(path, "/rest/api/3/search/jql") {
		m.handleSearch(w, r)
		return
	}

	// Get/Update single issue
	if strings.HasPrefix(path, "/rest/api/3/issue/") && r.Method == "GET" {
		m.handleGetIssue(w, r)
		return
	}

	if strings.HasPrefix(path, "/rest/api/3/issue/") && r.Method == "PUT" {
		m.handleUpdateIssue(w, r)
		return
	}

	// Create issue
	if path == "/rest/api/3/issue" && r.Method == "POST" {
		m.handleCreateIssue(w, r)
		return
	}

	// Default: not found
	w.WriteHeader(http.StatusNotFound)
	writeJSON(w, map[string]string{"error": "Not found"})
}

// handleSearch handles the search/JQL endpoint.
func (m *JiraMockServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	response := jira.SearchResponse{
		StartAt:    0,
		MaxResults: len(m.issues),
		Total:      len(m.issues),
		Issues:     m.issues,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetIssue handles GET requests for individual issues.
func (m *JiraMockServer) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	// Extract issue key from path: /rest/api/3/issue/{key}
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	key := parts[len(parts)-1]

	for _, issue := range m.issues {
		if issue.Key == key {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(issue)
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
	writeJSON(w, map[string]string{"error": "Issue not found"})
}

// handleCreateIssue handles POST requests to create issues.
func (m *JiraMockServer) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	if m.createIssueResult != nil {
		response := jira.CreateIssueResponse{
			ID:   m.createIssueResult.ID,
			Key:  m.createIssueResult.Key,
			Self: m.createIssueResult.Self,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Auto-generate issue
	m.nextIssueID++
	key := "PROJ-" + string(rune('0'+m.nextIssueID))
	response := jira.CreateIssueResponse{
		ID:   string(rune('0' + m.nextIssueID)),
		Key:  key,
		Self: m.Server.URL + "/rest/api/3/issue/" + key,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(response)
}

// handleUpdateIssue handles PUT requests to update issues.
func (m *JiraMockServer) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	// Extract issue key from path
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 5 {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	key := parts[len(parts)-1]

	// Find the issue
	for _, issue := range m.issues {
		if issue.Key == key {
			// Jira returns 204 No Content on successful update
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}

	w.WriteHeader(http.StatusNotFound)
	writeJSON(w, map[string]string{"error": "Issue not found"})
}

// SetIssues configures the issues that will be returned by search/fetch.
func (m *JiraMockServer) SetIssues(issues []jira.Issue) {
	m.issues = issues
}

// AddIssue adds a single issue to the mock data.
func (m *JiraMockServer) AddIssue(issue jira.Issue) {
	m.issues = append(m.issues, issue)
}

// SetCreateIssueResponse configures the response for create issue requests.
func (m *JiraMockServer) SetCreateIssueResponse(issue *jira.Issue) {
	m.createIssueResult = issue
}

// ClearIssues removes all mock issues.
func (m *JiraMockServer) ClearIssues() {
	m.issues = []jira.Issue{}
}

// Helper functions for creating test data

// MakeJiraIssue creates a test Jira issue with common defaults.
func MakeJiraIssue(key, summary, statusName string) jira.Issue {
	now := time.Now().Format("2006-01-02T15:04:05.000-0700")
	return jira.Issue{
		ID:   "10" + strings.TrimPrefix(key, "PROJ-"),
		Key:  key,
		Self: "https://example.atlassian.net/rest/api/3/issue/" + key,
		Fields: jira.Fields{
			Summary: summary,
			Status: &jira.Status{
				ID:   "1",
				Name: statusName,
				StatusCategory: &jira.StatusCategory{
					ID:   1,
					Key:  "new",
					Name: "To Do",
				},
			},
			IssueType: &jira.IssueType{
				ID:   "10001",
				Name: "Task",
			},
			Priority: &jira.Priority{
				ID:   "3",
				Name: "Medium",
			},
			Created: now,
			Updated: now,
		},
	}
}

// MakeJiraIssueWithDetails creates a test issue with full details.
func MakeJiraIssueWithDetails(key, summary, description, statusName string, priority string, labels []string) jira.Issue {
	issue := MakeJiraIssue(key, summary, statusName)
	issue.Fields.Description = description
	issue.Fields.Labels = labels
	if priority != "" {
		issue.Fields.Priority = &jira.Priority{
			ID:   "2",
			Name: priority,
		}
	}
	return issue
}

// MakeJiraIssueWithAssignee creates a test issue with an assignee.
func MakeJiraIssueWithAssignee(key, summary, statusName, assigneeEmail, assigneeName string) jira.Issue {
	issue := MakeJiraIssue(key, summary, statusName)
	issue.Fields.Assignee = &jira.User{
		AccountID:    "user-123",
		DisplayName:  assigneeName,
		EmailAddress: assigneeEmail,
	}
	return issue
}
