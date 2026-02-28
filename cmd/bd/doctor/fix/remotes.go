package fix

import (
	"database/sql"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/doltserver"
)

// RemoteConsistency fixes remote discrepancies between SQL server and CLI.
// For one-side-only remotes, it adds the missing side.
// Conflicts (different URLs) are skipped — they require manual resolution.
func RemoteConsistency(repoPath string) error {
	beadsDir := resolveBeadsDir(repoPath)
	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	doltDir := doltserver.ResolveDoltDir(beadsDir)
	dbName := cfg.GetDoltDatabase()
	dbDir := filepath.Join(doltDir, dbName)

	// Get SQL remotes
	db, err := openFixDB(beadsDir, cfg)
	if err != nil {
		return fmt.Errorf("cannot connect to Dolt server: %w", err)
	}
	defer db.Close()

	sqlRemotes, err := queryRemotes(db)
	if err != nil {
		return fmt.Errorf("failed to query SQL remotes: %w", err)
	}

	// Get CLI remotes
	cliRemotes, err := queryCLIRemotesForFix(dbDir)
	if err != nil {
		return fmt.Errorf("failed to query CLI remotes: %w", err)
	}

	sqlMap := map[string]string{}
	for _, r := range sqlRemotes {
		sqlMap[r.name] = r.url
	}
	cliMap := map[string]string{}
	for _, r := range cliRemotes {
		cliMap[r.name] = r.url
	}

	fixed := 0

	// SQL-only: add to CLI
	for name, url := range sqlMap {
		if _, inCLI := cliMap[name]; !inCLI {
			cmd := exec.Command("dolt", "remote", "add", name, url) // #nosec G204
			cmd.Dir = dbDir
			if out, err := cmd.CombinedOutput(); err != nil {
				fmt.Printf("  Warning: could not add CLI remote %s: %s\n", name, strings.TrimSpace(string(out)))
			} else {
				fmt.Printf("  Added CLI remote: %s → %s\n", name, url)
				fixed++
			}
		}
	}

	// CLI-only: add to SQL
	for name, url := range cliMap {
		if _, inSQL := sqlMap[name]; !inSQL {
			if _, err := db.Exec("CALL DOLT_REMOTE('add', ?, ?)", name, url); err != nil {
				fmt.Printf("  Warning: could not add SQL remote %s: %v\n", name, err)
			} else {
				fmt.Printf("  Added SQL remote: %s → %s\n", name, url)
				fixed++
			}
		}
	}

	// Conflicts: skip
	for name, sqlURL := range sqlMap {
		if cliURL, ok := cliMap[name]; ok && sqlURL != cliURL {
			fmt.Printf("  Skipped %s: conflicting URLs (SQL=%s, CLI=%s) — resolve manually\n", name, sqlURL, cliURL)
		}
	}

	if fixed == 0 {
		fmt.Printf("  No fixable discrepancies found\n")
	}
	return nil
}

type remoteInfo struct {
	name string
	url  string
}

func openFixDB(beadsDir string, cfg *configfile.Config) (*sql.DB, error) {
	host := cfg.GetDoltServerHost()
	user := cfg.GetDoltServerUser()
	database := cfg.GetDoltDatabase()
	password := cfg.GetDoltServerPassword()
	port := doltserver.DefaultConfig(beadsDir).Port

	var connStr string
	if password != "" {
		connStr = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true&timeout=5s",
			user, password, host, port, database)
	} else {
		connStr = fmt.Sprintf("%s@tcp(%s:%d)/%s?parseTime=true&timeout=5s",
			user, host, port, database)
	}
	return sql.Open("mysql", connStr)
}

func queryRemotes(db *sql.DB) ([]remoteInfo, error) {
	rows, err := db.Query("SELECT name, url FROM dolt_remotes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var remotes []remoteInfo
	for rows.Next() {
		var r remoteInfo
		if err := rows.Scan(&r.name, &r.url); err != nil {
			return nil, err
		}
		remotes = append(remotes, r)
	}
	return remotes, rows.Err()
}

func queryCLIRemotesForFix(dbDir string) ([]remoteInfo, error) {
	cmd := exec.Command("dolt", "remote", "-v") // #nosec G204
	cmd.Dir = dbDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.TrimSpace(string(out)), err)
	}
	var remotes []remoteInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			remotes = append(remotes, remoteInfo{name: parts[0], url: parts[1]})
		}
	}
	return remotes, nil
}
