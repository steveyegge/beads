//go:build !cgo

package dolt

import (
	"context"
	"errors"

	"github.com/steveyegge/beads/internal/types"
)

var errNoCGO = errors.New("peer database queries require CGO (Dolt database)")

// PeerBackend represents the detected storage backend of a peer repository.
type PeerBackend string

const (
	// PeerBackendDolt indicates the peer uses a Dolt database.
	PeerBackendDolt PeerBackend = "dolt"
	// PeerBackendJSONL indicates the peer uses JSONL file storage.
	PeerBackendJSONL PeerBackend = "jsonl"
	// PeerBackendUnknown indicates the peer backend could not be determined.
	PeerBackendUnknown PeerBackend = "unknown"
)

// HydrationResult contains the outcome of a HydrateFromPeerDolt operation.
type HydrationResult struct {
	PeerPath        string
	TotalPeerIssues int
	Imported        int
	Skipped         int
	Errors          []string
}

// DetectPeerBackend determines the storage backend type of a peer repository.
func DetectPeerBackend(_ string) (PeerBackend, error) {
	return PeerBackendUnknown, errNoCGO
}

// OpenPeerStore opens a read-only DoltStore for a peer repository.
func OpenPeerStore(_ context.Context, _ string) (*DoltStore, error) {
	return nil, errNoCGO
}

// QueryPeerIssues queries issues from a peer Dolt database with the given filter.
func QueryPeerIssues(_ context.Context, _ string, _ types.IssueFilter) ([]*types.Issue, error) {
	return nil, errNoCGO
}

// HydrateFromPeerDolt imports issues from a peer Dolt database into the local store.
func (s *DoltStore) HydrateFromPeerDolt(_ context.Context, _ string) (*HydrationResult, error) {
	return nil, errNoCGO
}
