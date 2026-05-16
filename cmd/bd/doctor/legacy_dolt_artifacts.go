package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
)

// CheckLegacyDoltArtifacts detects residual Dolt artifacts on a Postgres workspace.
// This is INFO severity: shown with ℹ icon and ui.WarnStyle, never affects exit code.
// Runs on both backends; only produces output when legacy artifacts are present.
func CheckLegacyDoltArtifacts(beadsDir string) DoctorCheck {
	bi := configfile.ResolveBackendInfo(beadsDir)

	doltDir := filepath.Join(beadsDir, "dolt")
	doltExists := false
	if info, err := os.Stat(doltDir); err == nil && info.IsDir() {
		doltExists = true
	}

	hasLegacyFields := len(bi.LegacyDoltFields) > 0

	if !doltExists && !hasLegacyFields {
		return DoctorCheck{
			Name:     "Legacy Dolt Artifacts",
			Status:   StatusOK,
			Message:  "No legacy Dolt artifacts",
			Category: CategoryData,
		}
	}

	today := time.Now().Format("2006-01-02")
	var details []string

	if doltExists {
		details = append(details, fmt.Sprintf(
			"archive: mv %s %s.legacy.%s",
			doltDir, doltDir, today))
	}

	if hasLegacyFields {
		details = append(details, fmt.Sprintf(
			"metadata.json has Dolt fields while backend=postgres: %s",
			strings.Join(bi.LegacyDoltFields, ", ")))
	}

	msg := ".beads/dolt present (inactive — backend is postgres)"
	if !doltExists && hasLegacyFields {
		msg = fmt.Sprintf("metadata.json has Dolt fields while backend is postgres: %s",
			strings.Join(bi.LegacyDoltFields, ", "))
	}

	return DoctorCheck{
		Name:     "Legacy Dolt Artifacts",
		Status:   StatusInfo,
		Message:  "legacy  " + msg,
		Detail:   strings.Join(details, "\n     "),
		Category: CategoryData,
	}
}
