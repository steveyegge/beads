// Store interface for import operations
// This interface abstracts the storage backend to support both SQLite and Dolt.

package importer

import (
	"context"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// ImportStore defines the interface required for import operations.
// Both SQLiteStorage and DoltStore implement this interface.
type ImportStore interface {
	storage.Storage

	// Path returns the database directory path
	Path() string

	// CheckpointWAL checkpoints the WAL (SQLite) or is a no-op (Dolt)
	CheckpointWAL(ctx context.Context) error

	// GetOrphanHandling returns the configured orphan handling mode
	GetOrphanHandling(ctx context.Context) string

	// CreateIssuesWithFullOptions creates issues with full control over options
	CreateIssuesWithFullOptions(ctx context.Context, issues []*types.Issue, actor string, opts BatchCreateOptions) error

	// ImportIssueComment imports a comment preserving the original timestamp
	ImportIssueComment(ctx context.Context, issueID, author, text string, createdAt string) (*types.Comment, error)
}

// BatchCreateOptions controls batch issue creation behavior
type BatchCreateOptions struct {
	SkipValidation     bool // Skip type/status validation
	PreserveDates      bool // Preserve created_at/updated_at from issue
	SkipDirtyTracking  bool // Skip marking issues as dirty
	SkipPrefixCheck    bool // Skip prefix validation
}

// AsImportStore attempts to convert a Storage to ImportStore
func AsImportStore(store storage.Storage) (ImportStore, bool) {
	is, ok := store.(ImportStore)
	return is, ok
}
