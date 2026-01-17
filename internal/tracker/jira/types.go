// Package jira provides a Jira integration plugin for the tracker framework.
package jira

import (
	"time"
)

// API constants
const (
	DefaultTimeout = 30 * time.Second
	MaxPageSize    = 100
)

// Issue represents a Jira issue from the REST API.
type Issue struct {
	ID     string `json:"id"`
	Key    string `json:"key"` // e.g., "PROJ-123"
	Self   string `json:"self"`
	Fields Fields `json:"fields"`
}

// Fields contains the issue field values.
type Fields struct {
	Summary     string          `json:"summary"`
	Description interface{}     `json:"description"` // Can be string or ADF doc
	Status      *Status         `json:"status"`
	Priority    *Priority       `json:"priority"`
	IssueType   *IssueType      `json:"issuetype"`
	Assignee    *User           `json:"assignee"`
	Reporter    *User           `json:"reporter"`
	Labels      []string        `json:"labels"`
	Created     string          `json:"created"`
	Updated     string          `json:"updated"`
	Resolved    string          `json:"resolutiondate"`
	Parent      *ParentRef      `json:"parent"`
	IssueLinks  []IssueLink     `json:"issuelinks"`
}

// Status represents a Jira workflow status.
type Status struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	StatusCategory *StatusCategory `json:"statusCategory"`
}

// StatusCategory represents a Jira status category.
type StatusCategory struct {
	ID   int    `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
}

// Priority represents a Jira priority.
type Priority struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// IssueType represents a Jira issue type.
type IssueType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Subtask bool   `json:"subtask"`
}

// User represents a Jira user.
type User struct {
	AccountID    string `json:"accountId"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	Name         string `json:"name"` // Server/DC only
}

// ParentRef is a reference to a parent issue.
type ParentRef struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// IssueLink represents a link between issues.
type IssueLink struct {
	ID           string     `json:"id"`
	Type         LinkType   `json:"type"`
	InwardIssue  *IssueRef  `json:"inwardIssue,omitempty"`
	OutwardIssue *IssueRef  `json:"outwardIssue,omitempty"`
}

// LinkType describes the type of link.
type LinkType struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Inward  string `json:"inward"`
	Outward string `json:"outward"`
}

// IssueRef is a reference to another issue in a link.
type IssueRef struct {
	ID  string `json:"id"`
	Key string `json:"key"`
}

// SearchResponse is the response from the JQL search endpoint.
type SearchResponse struct {
	StartAt    int     `json:"startAt"`
	MaxResults int     `json:"maxResults"`
	Total      int     `json:"total"`
	Issues     []Issue `json:"issues"`
}

// CreateIssueRequest is the request body for creating an issue.
type CreateIssueRequest struct {
	Fields CreateFields `json:"fields"`
}

// CreateFields contains fields for creating an issue.
type CreateFields struct {
	Project     ProjectRef  `json:"project"`
	Summary     string      `json:"summary"`
	Description interface{} `json:"description,omitempty"`
	IssueType   TypeRef     `json:"issuetype"`
	Priority    *TypeRef    `json:"priority,omitempty"`
	Labels      []string    `json:"labels,omitempty"`
	Assignee    *UserRef    `json:"assignee,omitempty"`
}

// ProjectRef is a reference to a project.
type ProjectRef struct {
	Key string `json:"key"`
}

// TypeRef is a reference by ID or name.
type TypeRef struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// UserRef is a reference to a user.
type UserRef struct {
	AccountID string `json:"accountId,omitempty"`
	Name      string `json:"name,omitempty"` // Server/DC
}

// CreateIssueResponse is the response from creating an issue.
type CreateIssueResponse struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Self string `json:"self"`
}

// UpdateIssueRequest is the request body for updating an issue.
type UpdateIssueRequest struct {
	Fields map[string]interface{} `json:"fields"`
}

// ADFDocument represents an Atlassian Document Format document.
type ADFDocument struct {
	Version int       `json:"version"`
	Type    string    `json:"type"`
	Content []ADFNode `json:"content"`
}

// ADFNode represents a node in an ADF document.
type ADFNode struct {
	Type    string                 `json:"type"`
	Text    string                 `json:"text,omitempty"`
	Attrs   map[string]interface{} `json:"attrs,omitempty"`
	Content []ADFNode              `json:"content,omitempty"`
	Marks   []ADFMark              `json:"marks,omitempty"`
}

// ADFMark represents formatting marks on text.
type ADFMark struct {
	Type  string                 `json:"type"`
	Attrs map[string]interface{} `json:"attrs,omitempty"`
}
