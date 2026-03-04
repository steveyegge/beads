package doctor

import (
	"path/filepath"

	"github.com/steveyegge/beads/internal/configfile"
)

// getBackendAndBeadsDir resolves the effective .beads directory (following redirects)
// and returns the configured storage backend (always "dolt").
func getBackendAndBeadsDir(repoPath string) (backend string, beadsDir string) {
	beadsDir = resolveBeadsDir(filepath.Join(repoPath, ".beads"))
	return configfile.BackendDolt, beadsDir
}
