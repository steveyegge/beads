package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"
)

// exportAutoStateSnapshot is a minimal view of export-state.json used to show
// the archive last-run timestamp and issue count in bd doctor output.
type exportAutoStateSnapshot struct {
	Timestamp time.Time `json:"timestamp"`
	Issues    int       `json:"issues"`
}

// CheckArchiveConfig reports the current archive configuration as a doctor
// check. A disabled archive (format=none) is shown as OK — disabled is a
// deliberate choice, not an error. StatusWarning is returned only when the
// deprecated export.auto key is present without archive.* migration.
func CheckArchiveConfig(repoPath string) DoctorCheck {
	beadsDir := ResolveBeadsDirForRepo(repoPath)
	configPath := filepath.Join(beadsDir, "config.yaml")

	v := viper.New()
	v.SetConfigFile(configPath)
	_ = v.ReadInConfig()

	hasArchiveFormat := v.IsSet("archive.format")
	hasExportAuto := v.IsSet("export.auto")

	// Deprecation warning: export.auto present without archive.* migration.
	if hasExportAuto && !hasArchiveFormat {
		return DoctorCheck{
			Name:    "Archive",
			Status:  StatusWarning,
			Message: "deprecated key 'export.auto' found without archive.* — migrate with:",
			Fix:     "bd config set archive.format jsonl",
		}
	}

	format := v.GetString("archive.format")
	if format == "" {
		// Unset — derive from export.auto for display when present, else none.
		if hasExportAuto {
			if v.GetBool("export.auto") {
				format = "jsonl"
			} else {
				format = "none"
			}
		} else {
			format = "none"
		}
	}

	if format == "none" {
		return DoctorCheck{
			Name:    "Archive",
			Status:  StatusOK,
			Message: "none (disabled)",
		}
	}

	// Enabled: show path, throttle, and last-run stats.
	archivePath := v.GetString("archive.path")
	if archivePath == "" {
		archivePath = ".beads/issues.jsonl"
	}
	throttle := v.GetInt("archive.throttle_seconds")
	if throttle == 0 {
		throttle = 60
	}

	// Probe the archive file for its size.
	fullPath := filepath.Join(beadsDir, archivePath)
	var sizeStr string
	if fi, err := os.Stat(fullPath); err == nil {
		sizeStr = humanBytes(fi.Size())
	}

	// Read the last-run state file.
	var lastRunStr string
	stateData, err := os.ReadFile(filepath.Join(beadsDir, "export-state.json")) //nolint:gosec
	if err == nil {
		var snap exportAutoStateSnapshot
		if json.Unmarshal(stateData, &snap) == nil && !snap.Timestamp.IsZero() {
			ago := time.Since(snap.Timestamp).Round(time.Second)
			lastRunStr = fmt.Sprintf("%s (%s ago)", snap.Timestamp.Format(time.RFC3339), ago)
		}
	}

	message := fmt.Sprintf("%s — %s, %ds throttle", format, archivePath, throttle)
	detail := ""
	if lastRunStr != "" || sizeStr != "" {
		if lastRunStr != "" && sizeStr != "" {
			detail = fmt.Sprintf("last run: %s · size: %s", lastRunStr, sizeStr)
		} else if lastRunStr != "" {
			detail = "last run: " + lastRunStr
		} else {
			detail = "size: " + sizeStr
		}
	}

	return DoctorCheck{
		Name:    "Archive",
		Status:  StatusOK,
		Message: message,
		Detail:  detail,
	}
}

// humanBytes formats a byte count as a human-readable string (KB, MB, GB).
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
