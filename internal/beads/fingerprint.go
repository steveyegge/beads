package beads

import (
	"github.com/steveyegge/beads/internal/git"
)

// ComputeRepoID generates a unique identifier for this git repository.
// Delegates to git.ComputeRepoID.
func ComputeRepoID() (string, error) {
	return git.ComputeRepoID()
}

// GetCloneID generates a unique ID for this specific clone (not shared with other clones).
// Delegates to git.GetCloneID.
func GetCloneID() (string, error) {
	return git.GetCloneID()
}
