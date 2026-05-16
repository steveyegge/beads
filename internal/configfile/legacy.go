package configfile

import (
	"os"
	"path/filepath"
)

// DetectLegacyDoltState reports stale Dolt artifacts when the configured backend
// is Postgres. It returns the path to a stale .beads/dolt directory (if present)
// and the names of any populated dolt_* fields in cfg that are inactive for a
// Postgres workspace.
//
// Returns empty values when backend is not Postgres (Dolt artifacts are not
// legacy in that case) or when no stale artifacts are found.
func DetectLegacyDoltState(beadsDir string, cfg *Config) (legacyDir string, legacyFields []string) {
	if cfg == nil || cfg.GetBackend() != BackendPostgres {
		return
	}

	doltDir := filepath.Join(beadsDir, "dolt")
	if info, err := os.Stat(doltDir); err == nil && info.IsDir() {
		legacyDir = doltDir
	}

	// Report populated dolt_* fields that are irrelevant when backend=postgres.
	if cfg.DoltMode != "" {
		legacyFields = append(legacyFields, "dolt_mode")
	}
	if cfg.DoltServerHost != "" {
		legacyFields = append(legacyFields, "dolt_server_host")
	}
	if cfg.DoltServerPort != 0 {
		legacyFields = append(legacyFields, "dolt_server_port")
	}
	if cfg.DoltServerUser != "" {
		legacyFields = append(legacyFields, "dolt_server_user")
	}
	if cfg.DoltDatabase != "" {
		legacyFields = append(legacyFields, "dolt_database")
	}
	if cfg.DoltDataDir != "" {
		legacyFields = append(legacyFields, "dolt_data_dir")
	}
	if cfg.GlobalDoltDatabase != "" {
		legacyFields = append(legacyFields, "global_dolt_database")
	}

	return
}
