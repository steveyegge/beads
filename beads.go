// Package beads provides a minimal public API for extending bd with custom orchestration.
//
// Most extensions should use direct SQL queries against bd's database.
// This package exports only the essential types and functions needed for
// Go-based extensions that want to use bd's storage layer programmatically.
//
// For detailed guidance on extending bd, see EXTENDING.md.
package beads

import (
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/sqlite"
	"github.com/steveyegge/beads/internal/types"
)

// Core types for working with issues
type (
	Issue      = types.Issue
	Status     = types.Status
	IssueType  = types.IssueType
	WorkFilter = types.WorkFilter
)

// Status constants
const (
	StatusOpen       = types.StatusOpen
	StatusInProgress = types.StatusInProgress
	StatusClosed     = types.StatusClosed
	StatusBlocked    = types.StatusBlocked
)

// IssueType constants
const (
	TypeBug     = types.TypeBug
	TypeFeature = types.TypeFeature
	TypeTask    = types.TypeTask
	TypeEpic    = types.TypeEpic
	TypeChore   = types.TypeChore
)

// Storage provides the minimal interface for extension orchestration
type Storage = storage.Storage

// NewSQLiteStorage opens a bd SQLite database for programmatic access.
// Most extensions should use this to query ready work and update issue status.
func NewSQLiteStorage(dbPath string) (Storage, error) {
	return sqlite.New(dbPath)
}
