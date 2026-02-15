// Package jira provides client, types, and utilities for Jira integration.
package jira

import "time"

// SyncStats tracks statistics for a Jira sync operation.
type SyncStats struct {
	Pulled    int `json:"pulled"`
	Pushed    int `json:"pushed"`
	Created   int `json:"created"`
	Updated   int `json:"updated"`
	Skipped   int `json:"skipped"`
	Errors    int `json:"errors"`
	Conflicts int `json:"conflicts"`
}

// SyncResult represents the result of a Jira sync operation.
type SyncResult struct {
	Success  bool      `json:"success"`
	Stats    SyncStats `json:"stats"`
	LastSync string    `json:"last_sync,omitempty"`
	Error    string    `json:"error,omitempty"`
	Warnings []string  `json:"warnings,omitempty"`
}

// PullStats tracks pull operation statistics.
type PullStats struct {
	Created int
	Updated int
	Skipped int
}

// PushStats tracks push operation statistics.
type PushStats struct {
	Created int
	Updated int
	Skipped int
	Errors  int
}

// Conflict represents a conflict between local and Jira versions.
type Conflict struct {
	IssueID         string
	LocalUpdated    time.Time
	JiraUpdated     time.Time
	JiraExternalRef string
}
