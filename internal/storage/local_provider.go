// Package storage defines the interface for issue storage backends.
package storage

import (
	"context"

	"github.com/steveyegge/beads/internal/types"
)

// LocalProvider implements types.IssueProvider using a Storage backend.
// This is used for cross-repo orphan detection when --db flag points to an external database.
type LocalProvider struct {
	store  Storage
	prefix string
}

// NewLocalProvider creates a provider backed by a Storage instance.
// The caller should pass a store opened from the target beads directory.
func NewLocalProvider(store Storage) (*LocalProvider, error) {
	ctx := context.Background()
	prefix, err := store.GetConfig(ctx, "issue_prefix")
	if err != nil || prefix == "" {
		prefix = "bd" // default
	}

	return &LocalProvider{store: store, prefix: prefix}, nil
}

// GetOpenIssues returns issues that are open or in_progress.
func (p *LocalProvider) GetOpenIssues(ctx context.Context) ([]*types.Issue, error) {
	issues, err := p.store.SearchIssues(ctx, "", types.IssueFilter{
		ExcludeStatus: []types.Status{types.StatusClosed},
	})
	if err != nil {
		return nil, err
	}
	return issues, nil
}

// GetIssuePrefix returns the configured issue prefix.
func (p *LocalProvider) GetIssuePrefix() string {
	return p.prefix
}

// Close closes the underlying storage.
func (p *LocalProvider) Close() error {
	if p.store != nil {
		return p.store.Close()
	}
	return nil
}

// Ensure LocalProvider implements types.IssueProvider
var _ types.IssueProvider = (*LocalProvider)(nil)
