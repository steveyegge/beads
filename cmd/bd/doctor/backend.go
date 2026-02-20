package doctor

import (
	"path/filepath"

	"github.com/steveyegge/beads/internal/configfile"
)

// getBackendAndBeadsDir resolves the effective .beads directory (following redirects)
// and returns the configured storage backend ("dolt" by default).
func getBackendAndBeadsDir(repoPath string) (backend string, beadsDir string) {
	beadsDir = resolveBeadsDir(filepath.Join(repoPath, ".beads"))

	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		return configfile.BackendDolt, beadsDir
	}
	return cfg.GetBackend(), beadsDir
}

// getDoltDatabase returns the configured Dolt database name for the given
// .beads directory. Falls back to the default ("beads") when the config
// cannot be loaded. This must be passed to dolt.Config.Database so that
// doctor checks work with non-default database names (GH#1904).
func getDoltDatabase(beadsDir string) string {
	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		return configfile.DefaultDoltDatabase
	}
	return cfg.GetDoltDatabase()
}
