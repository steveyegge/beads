package fix

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/doltserver"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/doltutil"
)

type remoteConsistencyContext struct {
	beadsDir string
	doltDir  string
	dbDir    string
	cfg      *configfile.Config
}

// RemoteConsistency fixes remote discrepancies between SQL server and CLI.
// For one-side-only remotes, it adds the missing side.
// Conflicts (different URLs) are skipped — they require manual resolution.
//
// Also handles the wrong-directory case where remotes were added at the dolt
// server root (.beads/dolt/) instead of the database subdirectory
// (.beads/dolt/<db>/): those are migrated into the database dir so CLI
// push/pull can find them. The dolt server itself already persists remotes in
// .dolt/repo_state.json and reloads them on startup, so no per-store-open
// sync is needed.
func RemoteConsistency(repoPath string) error {
	ctx, err := resolveRemoteConsistencyContext(repoPath)
	if err != nil {
		return err
	}

	// Migrate any remotes stranded in the server root into the database dir
	// before diffing — otherwise they'd show up as missing on the CLI side.
	migrateServerRootRemotes(ctx)

	// Get SQL remotes
	db, err := openFixDB(ctx.beadsDir, ctx.cfg)
	if err != nil {
		return fmt.Errorf("cannot connect to Dolt server: %w", err)
	}
	defer db.Close()

	sqlRemotes, err := queryFixRemotes(db)
	if err != nil {
		return fmt.Errorf("failed to query SQL remotes: %w", err)
	}

	// Get CLI remotes
	cliRemotes, err := doltutil.ListCLIRemotes(ctx.dbDir)
	if err != nil {
		return fmt.Errorf("failed to query CLI remotes: %w", err)
	}

	sqlMap := doltutil.ToRemoteNameMap(sqlRemotes)
	cliMap := doltutil.ToRemoteNameMap(cliRemotes)

	fixed := 0

	// SQL-only: add to CLI
	for name, url := range sqlMap {
		if _, inCLI := cliMap[name]; !inCLI {
			if err := doltutil.AddCLIRemote(ctx.dbDir, name, url); err != nil {
				fmt.Printf("  Warning: could not add CLI remote %s: %v\n", name, err)
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

func resolveRemoteConsistencyContext(repoPath string) (remoteConsistencyContext, error) {
	beadsDir, err := resolvedWorkspaceBeadsDir(repoPath)
	if err != nil {
		return remoteConsistencyContext{}, err
	}

	cfg, err := configfile.Load(beadsDir)
	if err != nil || cfg == nil {
		return remoteConsistencyContext{}, fmt.Errorf("failed to load config: %w", err)
	}

	doltDir := doltserver.ResolveDoltDir(beadsDir)
	dbName := cfg.GetDoltDatabase()

	return remoteConsistencyContext{
		beadsDir: beadsDir,
		doltDir:  doltDir,
		dbDir:    filepath.Join(doltDir, dbName),
		cfg:      cfg,
	}, nil
}

// migrateServerRootRemotes copies any remotes that were added at the dolt
// server root (.beads/dolt/) into the database subdirectory (.beads/dolt/<db>/)
// where CLI push/pull actually targets. This addresses the common user error
// of running `dolt remote add` one directory up from where the dolt CLI looks
// for it. Best-effort: errors are logged but not fatal.
func migrateServerRootRemotes(ctx remoteConsistencyContext) {
	rootDir := ctx.doltDir
	if rootDir == "" || rootDir == ctx.dbDir {
		return
	}
	if _, err := os.Stat(filepath.Join(rootDir, ".dolt")); err != nil {
		return
	}
	if _, err := os.Stat(filepath.Join(ctx.dbDir, ".dolt")); err != nil {
		return
	}
	rootRemotes, err := doltutil.ListCLIRemotes(rootDir)
	if err != nil || len(rootRemotes) == 0 {
		return
	}
	for _, r := range rootRemotes {
		if doltutil.FindCLIRemote(ctx.dbDir, r.Name) != "" {
			continue
		}
		if err := doltutil.AddCLIRemote(ctx.dbDir, r.Name, r.URL); err != nil {
			fmt.Printf("  Warning: could not migrate root remote %s: %v\n", r.Name, err)
			continue
		}
		fmt.Printf("  Migrated remote from server root to database dir: %s → %s\n", r.Name, r.URL)
	}
}

func openFixDB(beadsDir string, cfg *configfile.Config) (*sql.DB, error) {
	host := cfg.GetDoltServerHost()
	user := cfg.GetDoltServerUser()
	database := cfg.GetDoltDatabase()
	password := cfg.GetDoltServerPassword()
	port := doltserver.DefaultConfig(beadsDir).Port

	connStr := doltutil.ServerDSN{
		Host:     host,
		Port:     port,
		User:     user,
		Password: password,
		Database: database,
		TLS:      cfg.GetDoltServerTLS(),
	}.String()
	return sql.Open("mysql", connStr)
}

func queryFixRemotes(db *sql.DB) ([]storage.RemoteInfo, error) {
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
