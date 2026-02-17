//go:build cgo

package dolt

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/types"
)

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

// DetectPeerBackend determines the storage backend type of a peer repository.
// peerRepoPath is the root directory of the peer repository (containing .beads/).
// Returns the detected backend type or an error if the path is invalid.
func DetectPeerBackend(peerRepoPath string) (PeerBackend, error) {
	beadsDir := filepath.Join(peerRepoPath, ".beads")
	info, err := os.Stat(beadsDir)
	if err != nil {
		return PeerBackendUnknown, fmt.Errorf("no .beads directory at %s: %w", peerRepoPath, err)
	}
	if !info.IsDir() {
		return PeerBackendUnknown, fmt.Errorf(".beads at %s is not a directory", peerRepoPath)
	}

	// Check for Dolt database directory
	doltDir := filepath.Join(beadsDir, "dolt")
	if info, err := os.Stat(doltDir); err == nil && info.IsDir() {
		return PeerBackendDolt, nil
	}

	// Check for JSONL file
	jsonlPath := filepath.Join(beadsDir, "issues.jsonl")
	if _, err := os.Stat(jsonlPath); err == nil {
		return PeerBackendJSONL, nil
	}

	// Check metadata.json for backend hint
	cfg, err := configfile.Load(beadsDir)
	if err == nil && cfg != nil {
		if cfg.GetBackend() == configfile.BackendDolt {
			return PeerBackendDolt, nil
		}
	}

	return PeerBackendUnknown, fmt.Errorf("could not determine backend for peer at %s", peerRepoPath)
}

// OpenPeerStore opens a read-only DoltStore for a peer repository.
// peerRepoPath is the root directory of the peer repository (containing .beads/).
// Uses AutoStart to spin up a temporary Dolt server if one isn't running for the peer.
// The caller must close the returned store when done.
func OpenPeerStore(ctx context.Context, peerRepoPath string) (*DoltStore, error) {
	beadsDir := filepath.Join(peerRepoPath, ".beads")

	// Verify the beads directory exists
	if _, err := os.Stat(beadsDir); err != nil {
		return nil, fmt.Errorf("peer repository not found at %s: %w", peerRepoPath, err)
	}

	// Open in read-only mode with AutoStart to spin up a server if needed.
	// Each beads directory gets its own Dolt server on a derived port.
	peerStore, err := NewFromConfigWithOptions(ctx, beadsDir, &Config{
		ReadOnly:  true,
		AutoStart: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open peer database at %s: %w", peerRepoPath, err)
	}

	return peerStore, nil
}

// QueryPeerIssues queries issues from a peer Dolt database with the given filter.
// Results are tagged with the SourceRepo field set to peerRepoPath.
// This is a read-only operation that opens a temporary connection to the peer database.
func QueryPeerIssues(ctx context.Context, peerRepoPath string, filter types.IssueFilter) ([]*types.Issue, error) {
	peerStore, err := OpenPeerStore(ctx, peerRepoPath)
	if err != nil {
		return nil, err
	}
	defer peerStore.Close()

	issues, err := peerStore.SearchIssues(ctx, "", filter)
	if err != nil {
		return nil, fmt.Errorf("failed to query peer issues at %s: %w", peerRepoPath, err)
	}

	// Tag all results with the source repository path
	for _, issue := range issues {
		issue.SourceRepo = peerRepoPath
	}

	return issues, nil
}

// HydrateFromPeerDolt imports issues from a peer Dolt database into the local store.
// Opens the peer database via a read-only Dolt server, queries all issues, and creates
// them locally with source_repo set to the peer path.
//
// The operation is idempotent: existing issues with matching IDs are skipped.
func (s *DoltStore) HydrateFromPeerDolt(ctx context.Context, peerRepoPath string) (*HydrationResult, error) {
	if s.readOnly {
		return nil, fmt.Errorf("cannot hydrate: store is read-only")
	}

	// Resolve absolute path for the peer
	absPath, err := filepath.Abs(peerRepoPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve peer path: %w", err)
	}

	// Detect peer backend
	backend, err := DetectPeerBackend(absPath)
	if err != nil {
		return nil, err
	}
	if backend != PeerBackendDolt {
		return nil, fmt.Errorf("peer at %s uses %s backend, not Dolt", absPath, backend)
	}

	result := &HydrationResult{
		PeerPath: absPath,
	}

	// Query peer issues directly via read-only store open
	peerStore, err := OpenPeerStore(ctx, absPath)
	if err != nil {
		return nil, err
	}
	defer peerStore.Close()

	// Fetch all issues from peer (no filter = all statuses)
	peerIssues, err := peerStore.SearchIssues(ctx, "", types.IssueFilter{})
	if err != nil {
		return nil, fmt.Errorf("failed to query peer issues: %w", err)
	}
	result.TotalPeerIssues = len(peerIssues)

	// Batch check which IDs already exist locally
	peerIDs := make([]string, len(peerIssues))
	for i, issue := range peerIssues {
		peerIDs[i] = issue.ID
	}
	existingIssues, err := s.GetIssuesByIDs(ctx, peerIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing issues: %w", err)
	}
	existingSet := make(map[string]bool, len(existingIssues))
	for _, existing := range existingIssues {
		existingSet[existing.ID] = true
	}

	// Import issues that don't exist locally
	for _, issue := range peerIssues {
		if existingSet[issue.ID] {
			result.Skipped++
			continue
		}

		issue.SourceRepo = peerRepoPath

		if err := s.CreateIssue(ctx, issue, "hydrate"); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("failed to import %s: %v", issue.ID, err))
			continue
		}
		result.Imported++
	}

	return result, nil
}

// HydrationResult contains the outcome of a HydrateFromPeerDolt operation.
type HydrationResult struct {
	PeerPath        string   // Absolute path to the peer repository
	TotalPeerIssues int      // Total issues found in peer
	Imported        int      // Issues successfully imported
	Skipped         int      // Issues skipped (already exist locally)
	Errors          []string // Non-fatal import errors
}
