// Package tracker provides a plugin-based architecture for issue tracker integrations.
// It defines common interfaces and types that allow different trackers (Linear, Jira,
// Azure DevOps, etc.) to be integrated with a consistent API.
package tracker

import (
	"time"
)

// SyncStats tracks statistics for a sync operation with any external tracker.
// These statistics are accumulated during pull and push operations.
type SyncStats struct {
	Pulled    int `json:"pulled"`    // Issues pulled from external tracker
	Pushed    int `json:"pushed"`    // Issues pushed to external tracker
	Created   int `json:"created"`   // New issues created (either locally or remotely)
	Updated   int `json:"updated"`   // Existing issues updated
	Skipped   int `json:"skipped"`   // Issues skipped (no changes, filtered, etc.)
	Errors    int `json:"errors"`    // Operations that failed
	Conflicts int `json:"conflicts"` // Conflicts detected during bidirectional sync
}

// SyncResult represents the result of a complete sync operation.
type SyncResult struct {
	Success  bool      `json:"success"`            // Whether the overall sync succeeded
	Stats    SyncStats `json:"stats"`              // Accumulated statistics
	LastSync string    `json:"last_sync,omitempty"` // Timestamp of this sync (RFC3339)
	Error    string    `json:"error,omitempty"`    // Error message if failed
	Warnings []string  `json:"warnings,omitempty"` // Non-fatal warnings
}

// PullStats tracks statistics for a pull operation.
type PullStats struct {
	Created     int    // New issues created locally
	Updated     int    // Existing issues updated
	Skipped     int    // Issues skipped (no changes)
	Incremental bool   // Whether this was an incremental sync
	SyncedSince string // Timestamp we synced since (if incremental)
}

// PushStats tracks statistics for a push operation.
type PushStats struct {
	Created int // New issues created in external tracker
	Updated int // Existing issues updated in external tracker
	Skipped int // Issues skipped (no changes, filtered, etc.)
	Errors  int // Operations that failed
}

// Conflict represents a conflict between local and external tracker versions.
// A conflict occurs when both the local and external versions have been modified
// since the last sync.
type Conflict struct {
	IssueID           string    // Beads issue ID
	LocalUpdated      time.Time // When the local version was last modified
	ExternalUpdated   time.Time // When the external version was last modified
	ExternalRef       string    // URL or identifier for the external issue
	ExternalID        string    // External tracker's identifier (e.g., "TEAM-123", "PROJ-456")
	ExternalInternalID string    // External tracker's internal ID (e.g., UUID for API calls)
}

// ConflictResolution specifies how to handle conflicts during sync.
type ConflictResolution string

const (
	// ConflictResolutionTimestamp resolves conflicts by keeping the newer version.
	ConflictResolutionTimestamp ConflictResolution = "timestamp"
	// ConflictResolutionLocal always keeps the local version.
	ConflictResolutionLocal ConflictResolution = "local"
	// ConflictResolutionExternal always keeps the external tracker's version.
	ConflictResolutionExternal ConflictResolution = "external"
)

// DependencyInfo represents a dependency to be created after issue import.
// Stored separately since we need all issues imported before linking dependencies.
type DependencyInfo struct {
	FromExternalID string // External identifier of the dependent issue
	ToExternalID   string // External identifier of the dependency target
	Type           string // Beads dependency type (blocks, related, duplicates, parent-child)
}

// IssueConversion holds the result of converting an external tracker issue to Beads.
// It includes the issue and any dependencies that should be created.
type IssueConversion struct {
	Issue        interface{}      // *types.Issue - using interface to avoid circular import
	Dependencies []DependencyInfo // Dependencies to create after all issues are imported
}

// ErrNotInitialized is returned when a tracker is used before Init is called.
type ErrNotInitialized struct {
	Tracker string
}

func (e *ErrNotInitialized) Error() string {
	return e.Tracker + " tracker not initialized; call Init() first"
}
