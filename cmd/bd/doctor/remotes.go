package doctor

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/doltserver"
	"github.com/steveyegge/beads/internal/storage"
)

// CheckRemoteConsistency compares remotes registered in the SQL server
// vs the filesystem CLI config and reports discrepancies.
// Returns a check with Fix set for cases where --fix can resolve it.
func CheckRemoteConsistency(repoPath string) DoctorCheck {
	beadsDir := resolveBeadsDir(repoPath)

	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil || cfg.GetBackend() != configfile.BackendDolt {
		return DoctorCheck{
			Name:     "Remote Consistency",
			Status:   StatusOK,
			Message:  "N/A (not using Dolt backend)",
			Category: CategoryData,
		}
	}

	// Get SQL remotes via direct connection
	sqlRemotes, sqlErr := querySQLRemotes(beadsDir, cfg)
	if sqlErr != nil {
		return DoctorCheck{
			Name:     "Remote Consistency",
			Status:   StatusOK,
			Message:  "Could not query SQL remotes (server may not be running)",
			Category: CategoryData,
		}
	}

	// Get CLI remotes
	doltDir := doltserver.ResolveDoltDir(beadsDir)
	dbName := cfg.GetDoltDatabase()
	dbDir := filepath.Join(doltDir, dbName)
	cliRemotes, cliErr := queryCLIRemotes(dbDir)
	if cliErr != nil {
		return DoctorCheck{
			Name:     "Remote Consistency",
			Status:   StatusWarning,
			Message:  fmt.Sprintf("Could not query CLI remotes: %v", cliErr),
			Category: CategoryData,
		}
	}

	// No remotes at all
	if len(sqlRemotes) == 0 && len(cliRemotes) == 0 {
		return DoctorCheck{
			Name:     "Remote Consistency",
			Status:   StatusWarning,
			Message:  "No remotes configured",
			Detail:   "Add a remote with: bd dolt remote add origin <url>",
			Category: CategoryData,
		}
	}

	// Compare
	sqlMap := map[string]string{}
	for _, r := range sqlRemotes {
		sqlMap[r.Name] = r.URL
	}
	cliMap := map[string]string{}
	for _, r := range cliRemotes {
		cliMap[r.Name] = r.URL
	}

	var issues []string
	var fixable []string
	hasConflict := false

	// Check all SQL remotes
	for name, sqlURL := range sqlMap {
		cliURL, inCLI := cliMap[name]
		if !inCLI {
			issues = append(issues, fmt.Sprintf("%s: SQL only (%s)", name, sqlURL))
			fixable = append(fixable, name)
		} else if sqlURL != cliURL {
			issues = append(issues, fmt.Sprintf("%s: CONFLICT — SQL=%s, CLI=%s", name, sqlURL, cliURL))
			hasConflict = true
		}
	}

	// Check CLI-only remotes
	for name, cliURL := range cliMap {
		if _, inSQL := sqlMap[name]; !inSQL {
			issues = append(issues, fmt.Sprintf("%s: CLI only (%s)", name, cliURL))
			fixable = append(fixable, name)
		}
	}

	if len(issues) == 0 {
		msg := fmt.Sprintf("%d remote(s) in sync", len(sqlRemotes))
		// Add refs/dolt/data note for git+ssh remotes
		for _, r := range sqlRemotes {
			if isSSHRemoteURL(r.URL) {
				msg += " — git+ssh remotes also support refs/dolt/data (see https://docs.dolthub.com/concepts/dolt/git/remotes)"
				break
			}
		}
		return DoctorCheck{
			Name:     "Remote Consistency",
			Status:   StatusOK,
			Message:  msg,
			Category: CategoryData,
		}
	}

	fix := ""
	if !hasConflict {
		fix = "Remote Consistency"
	}

	return DoctorCheck{
		Name:     "Remote Consistency",
		Status:   StatusWarning,
		Message:  fmt.Sprintf("%d discrepanc(ies) found", len(issues)),
		Detail:   strings.Join(issues, "\n"),
		Fix:      fix,
		Category: CategoryData,
	}
}

// querySQLRemotes gets remotes from the SQL server.
func querySQLRemotes(beadsDir string, cfg *configfile.Config) ([]storage.RemoteInfo, error) {
	db, _, err := openDoltDB(beadsDir)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.Query("SELECT name, url FROM dolt_remotes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var remotes []storage.RemoteInfo
	for rows.Next() {
		var r storage.RemoteInfo
		if err := rows.Scan(&r.Name, &r.URL); err != nil {
			return nil, err
		}
		remotes = append(remotes, r)
	}
	return remotes, rows.Err()
}

// queryCLIRemotes runs `dolt remote -v` in the database directory.
func queryCLIRemotes(dbDir string) ([]storage.RemoteInfo, error) {
	cmd := exec.Command("dolt", "remote", "-v") // #nosec G204 -- fixed command
	cmd.Dir = dbDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("dolt remote -v: %s: %w", strings.TrimSpace(string(out)), err)
	}
	var remotes []storage.RemoteInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			remotes = append(remotes, storage.RemoteInfo{Name: parts[0], URL: parts[1]})
		}
	}
	return remotes, nil
}

func isSSHRemoteURL(url string) bool {
	return strings.HasPrefix(url, "git+ssh://") ||
		strings.HasPrefix(url, "ssh://") ||
		strings.Contains(url, "git@")
}
