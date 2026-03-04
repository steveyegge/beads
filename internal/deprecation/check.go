package deprecation

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Warning describes a deprecated configuration that will be removed in v1.0.0.
type Warning struct {
	ID      string // machine key: "embedded-mode", "sqlite-backend", etc.
	Summary string // one-line headline
	Detail  string // explanation
	Action  string // what user should do
}

// Check scans a .beads directory for deprecated configurations.
// It reads only filesystem/env/config — no DB access.
// Returns nil if beadsDir doesn't exist or no deprecations found.
func Check(beadsDir string) []Warning {
	if beadsDir == "" {
		return nil
	}
	if _, err := os.Stat(beadsDir); os.IsNotExist(err) {
		return nil
	}

	var warnings []Warning
	warnings = append(warnings, checkEmbeddedMode(beadsDir)...)
	warnings = append(warnings, checkSQLiteBackend(beadsDir)...)
	warnings = append(warnings, checkSQLiteArtifacts(beadsDir)...)
	warnings = append(warnings, checkServerModeEnv()...)
	warnings = append(warnings, checkJSONLSyncFiles(beadsDir)...)
	return warnings
}

// metadataFields loads metadata.json and returns the raw JSON map.
func metadataFields(beadsDir string) map[string]json.RawMessage {
	data, err := os.ReadFile(filepath.Join(beadsDir, "metadata.json")) // #nosec G304
	if err != nil {
		return nil
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

func checkEmbeddedMode(beadsDir string) []Warning {
	m := metadataFields(beadsDir)
	if m == nil {
		return nil
	}
	raw, ok := m["dolt_mode"]
	if !ok {
		return nil // absent = defaults to server, no warning needed
	}
	var mode string
	if json.Unmarshal(raw, &mode) != nil {
		return nil
	}
	if mode != "embedded" {
		return nil
	}
	return []Warning{{
		ID:      "embedded-mode",
		Summary: "Embedded Dolt mode is deprecated",
		Detail:  "Server mode is the only supported mode. Embedded mode will be removed in v1.0.0.",
		Action:  "Remove the \"dolt_mode\" field from .beads/metadata.json or set it to \"server\".",
	}}
}

func checkSQLiteBackend(beadsDir string) []Warning {
	m := metadataFields(beadsDir)
	if m == nil {
		return nil
	}
	raw, ok := m["backend"]
	if !ok {
		return nil
	}
	var backend string
	if json.Unmarshal(raw, &backend) != nil {
		return nil
	}
	if backend != "sqlite" {
		return nil
	}
	return []Warning{{
		ID:      "sqlite-backend",
		Summary: "SQLite backend was removed",
		Detail:  "Dolt is the only supported backend. Verify your migration to Dolt completed.",
		Action:  "Run 'bd doctor' to verify, then remove the \"backend\" field from .beads/metadata.json.",
	}}
}

func checkSQLiteArtifacts(beadsDir string) []Warning {
	patterns := []string{"beads.db", "*.db-wal", "*.db-shm"}
	for _, pat := range patterns {
		matches, _ := filepath.Glob(filepath.Join(beadsDir, pat))
		if len(matches) > 0 {
			return []Warning{{
				ID:      "sqlite-artifacts",
				Summary: "Legacy SQLite database files detected",
				Detail:  "SQLite files in .beads/ are no longer used. Data is stored in Dolt.",
				Action:  "Run 'bd doctor --check=artifacts --clean' to remove legacy files.",
			}}
		}
	}
	return nil
}

func checkServerModeEnv() []Warning {
	if os.Getenv("BEADS_DOLT_SERVER_MODE") != "" {
		return []Warning{{
			ID:      "server-mode-env",
			Summary: "BEADS_DOLT_SERVER_MODE env var is deprecated",
			Detail:  "Server mode is always on. This env var has no effect and will be ignored in v1.0.0.",
			Action:  "Remove BEADS_DOLT_SERVER_MODE from your environment.",
		}}
	}
	return nil
}

func checkJSONLSyncFiles(beadsDir string) []Warning {
	for _, name := range []string{"issues.jsonl", "beads.jsonl"} {
		if _, err := os.Stat(filepath.Join(beadsDir, name)); err == nil {
			return []Warning{{
				ID:      "jsonl-sync-files",
				Summary: "Legacy JSONL sync files detected",
				Detail:  "JSONL sync files are no longer used. All data is in Dolt.",
				Action:  "Run 'bd doctor --check=artifacts --clean' to remove legacy files.",
			}}
		}
	}
	return nil
}
