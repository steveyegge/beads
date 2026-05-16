package doctor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// CheckArchiveStatus reports the current archive configuration and state.
// Runs on both backends; warns when the deprecated export.auto key is present
// without a canonical archive.format key.
func CheckArchiveStatus(repoPath string) DoctorCheck {
	beadsDir := ResolveBeadsDirForRepo(repoPath)

	configPath := filepath.Join(beadsDir, "config.yaml")
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return DoctorCheck{
			Name:     "Archive",
			Status:   StatusOK,
			Message:  "config.yaml not found — using defaults",
			Category: CategoryData,
		}
	}

	v := viper.New()
	v.SetConfigType("yaml")
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		return DoctorCheck{
			Name:     "Archive",
			Status:   StatusWarning,
			Message:  fmt.Sprintf("failed to read config.yaml: %v", err),
			Category: CategoryData,
		}
	}

	hasExportAuto := v.IsSet("export.auto")
	hasArchiveFormat := v.IsSet("archive.format")

	// Deprecated key without canonical replacement.
	if hasExportAuto && !hasArchiveFormat {
		return DoctorCheck{
			Name:     "Archive",
			Status:   StatusWarning,
			Message:  "deprecated config key export.auto found",
			Detail:   "Migrate: bd config set archive.format jsonl (or none)",
			Fix:      "bd config set archive.format jsonl",
			Category: CategoryData,
		}
	}

	// Resolve effective format.
	archiveFormat := "jsonl" // built-in default
	if hasArchiveFormat {
		archiveFormat = strings.TrimSpace(v.GetString("archive.format"))
	} else if hasExportAuto {
		if v.GetBool("export.auto") {
			archiveFormat = "jsonl"
		} else {
			archiveFormat = "none"
		}
	}

	if archiveFormat == "none" {
		return DoctorCheck{
			Name:     "Archive",
			Status:   StatusOK,
			Message:  "disabled",
			Detail:   "format: none",
			Category: CategoryData,
		}
	}

	// format=jsonl — show detailed status.
	archivePath := strings.TrimSpace(v.GetString("archive.path"))
	if archivePath == "" {
		archivePath = "issues.jsonl"
	}
	throttle := v.GetInt("archive.throttle_seconds")
	if throttle == 0 {
		throttle = 60
	}

	fullArchivePath := filepath.Join(beadsDir, archivePath)

	var lines []string
	lines = append(lines, fmt.Sprintf("format:            %s", archiveFormat))
	lines = append(lines, fmt.Sprintf("path:              .beads/%s", archivePath))
	lines = append(lines, fmt.Sprintf("throttle_seconds:  %d", throttle))

	stateFile := filepath.Join(beadsDir, "export-state.json")
	if data, err := os.ReadFile(stateFile); err == nil { //nolint:gosec
		var state struct {
			Timestamp time.Time `json:"timestamp"`
			Issues    int       `json:"issues"`
		}
		if json.Unmarshal(data, &state) == nil && !state.Timestamp.IsZero() {
			ago := time.Since(state.Timestamp).Round(time.Second)
			lines = append(lines, fmt.Sprintf("last run:          %s (%s ago)", state.Timestamp.UTC().Format(time.RFC3339), ago))
		}
	}

	if info, err := os.Stat(fullArchivePath); err == nil {
		lines = append(lines, fmt.Sprintf("last size:         %s", archiveFormatBytes(info.Size())))
	}

	return DoctorCheck{
		Name:     "Archive",
		Status:   StatusOK,
		Message:  fmt.Sprintf("enabled — %s", filepath.Join(".beads", archivePath)),
		Detail:   strings.Join(lines, "\n"),
		Category: CategoryData,
	}
}

func archiveFormatBytes(n int64) string {
	const unit = 1000
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
