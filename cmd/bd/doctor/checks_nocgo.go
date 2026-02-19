//go:build !cgo

package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Non-CGO stubs for doctor checks that require Dolt database access.
// These checks are skipped in non-CGO builds.

func CheckMergeArtifacts(_ string) DoctorCheck {
	return DoctorCheck{Name: "Merge Artifacts", Status: StatusOK, Message: "Requires CGO"}
}

func CheckOrphanedDependencies(_ string) DoctorCheck {
	return DoctorCheck{Name: "Orphaned Dependencies", Status: StatusOK, Message: "Requires CGO"}
}

func CheckDuplicateIssues(_ string, _ bool, _ int) DoctorCheck {
	return DoctorCheck{Name: "Duplicate Issues", Status: StatusOK, Message: "Requires CGO"}
}

func CheckTestPollution(_ string) DoctorCheck {
	return DoctorCheck{Name: "Test Pollution", Status: StatusOK, Message: "Requires CGO"}
}

func CheckChildParentDependencies(_ string) DoctorCheck {
	return DoctorCheck{Name: "Child-Parent Dependencies", Status: StatusOK, Message: "Requires CGO"}
}

func CheckGitConflicts(_ string) DoctorCheck {
	return DoctorCheck{Name: "Git Conflicts", Status: StatusOK, Message: "Requires CGO"}
}

func CheckStaleClosedIssues(_ string) DoctorCheck {
	return DoctorCheck{
		Name:     "Stale Closed Issues",
		Status:   StatusOK,
		Message:  "N/A (requires CGO)",
		Category: CategoryMaintenance,
	}
}

func CheckStaleMolecules(_ string) DoctorCheck {
	return DoctorCheck{Name: "Stale Molecules", Status: StatusOK, Message: "Requires CGO"}
}

func CheckPersistentMolIssues(_ string) DoctorCheck {
	return DoctorCheck{Name: "Persistent Mol Issues", Status: StatusOK, Message: "Requires CGO"}
}

func CheckStaleMQFiles(path string) DoctorCheck {
	mqDir := filepath.Join(path, ".beads", "mq")
	entries, err := os.ReadDir(mqDir)
	if os.IsNotExist(err) {
		return DoctorCheck{
			Name:     "Legacy MQ Files",
			Status:   StatusOK,
			Message:  "No legacy merge queue files",
			Category: CategoryMaintenance,
		}
	}
	if err != nil {
		return DoctorCheck{
			Name:     "Legacy MQ Files",
			Status:   StatusOK,
			Message:  "N/A (unable to read .beads/mq)",
			Category: CategoryMaintenance,
		}
	}

	var staleCount int
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), ".json") {
			staleCount++
		}
	}

	if staleCount == 0 {
		return DoctorCheck{
			Name:     "Legacy MQ Files",
			Status:   StatusOK,
			Message:  "No legacy merge queue files",
			Category: CategoryMaintenance,
		}
	}

	return DoctorCheck{
		Name:     "Legacy MQ Files",
		Status:   StatusWarning,
		Message:  fmt.Sprintf("%d stale .beads/mq/*.json file(s)", staleCount),
		Fix:      "Run 'bd doctor --fix' to remove stale merge queue files",
		Category: CategoryMaintenance,
	}
}

func CheckPatrolPollution(_ string) DoctorCheck {
	return DoctorCheck{Name: "Patrol Pollution", Status: StatusOK, Message: "Requires CGO"}
}

func CheckCompactionCandidates(_ string) DoctorCheck {
	return DoctorCheck{
		Name:     "Compaction Candidates",
		Status:   StatusOK,
		Message:  "N/A (requires CGO)",
		Category: CategoryMaintenance,
	}
}

func FixStaleMQFiles(path string) error {
	mqDir := filepath.Join(path, ".beads", "mq")
	if _, err := os.Stat(mqDir); os.IsNotExist(err) {
		return nil
	}
	return os.RemoveAll(mqDir)
}
