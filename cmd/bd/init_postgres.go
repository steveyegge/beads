package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	pgstore "github.com/steveyegge/beads/internal/storage/postgres"
	pgdsn "github.com/steveyegge/beads/internal/storage/postgres/dsn"
	"github.com/steveyegge/beads/internal/ui"
)

// pgInitParams carries the flag values for a postgres backend init.
type pgInitParams struct {
	rawDSN     string // --dsn: used verbatim (password stripped) when non-empty
	host       string // --pg-host
	port       int    // --pg-port
	user       string // --pg-user
	database   string // --pg-database
	sslmode    string // --pg-sslmode
	clusterDir string // resolved from --pg-cluster-dir or XDG default
}

// pgSystemDatabases are Postgres reserved database names that beads must not own.
var pgSystemDatabases = map[string]bool{
	"postgres":  true,
	"template0": true,
	"template1": true,
}

// pgResolveClusterDir returns the effective cluster directory for auto-discovery.
// Prefers the explicit flag, then $XDG_DATA_HOME/beads/postgres/data,
// then $HOME/.local/share/beads/postgres/data.
func pgResolveClusterDir(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "beads", "postgres", "data")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "beads", "postgres", "data")
}

// runPostgresInit resolves the Postgres DSN, probes the server, writes
// metadata.json and .discovery_log, then prints the success banner.
// Calls FatalError on any unrecoverable error.
func runPostgresInit(ctx context.Context, beadsDir, prefix string, p pgInitParams) {
	strippedDSN, decision, err := resolvePostgresDSN(p, prefix)
	if err != nil {
		FatalError("%v", err)
	}

	_, _, db, _ := pgdsn.ParseConnectionTarget(strippedDSN)
	if pgSystemDatabases[db] {
		FatalError("cannot use system database %q as beads database; choose a dedicated database (e.g. %q)", db, sanitizeDBName(prefix))
	}

	host, _, _, _ := pgdsn.ParseConnectionTarget(strippedDSN)
	if !pgIsLoopback(host) && strings.Contains(strippedDSN, "sslmode=disable") {
		fmt.Fprintf(os.Stderr, "WARNING: sslmode=disable on non-loopback host %q; connection will be unencrypted\n", host)
	}

	password := os.Getenv("BEADS_POSTGRES_PASSWORD")
	fullDSN := pgdsn.Compose(strippedDSN, password)
	if _, err := pgstore.Open(ctx, fullDSN, strippedDSN, nil); err != nil {
		FatalError("%v", err)
	}

	cfg := &configfile.Config{
		Backend:     configfile.BackendPostgres,
		PostgresDSN: strippedDSN,
	}
	if err := cfg.Save(beadsDir); err != nil {
		FatalError("failed to write metadata.json: %v", err)
	}

	pgWriteDiscoveryLog(beadsDir, decision, strippedDSN)
	pgEnsureGitignored(beadsDir, ".discovery_log")

	target := pgdsn.RenderRedacted(strippedDSN)
	fmt.Printf("  %s bd initialized; backend: postgres; target: %s\n", ui.RenderPass("✓"), target)
}

// resolvePostgresDSN implements the priority chain: --dsn → --pg-* flags → discovery.
// Returns the stripped (password-free) DSN and a short decision label for the log.
func resolvePostgresDSN(p pgInitParams, prefix string) (strippedDSN, decision string, err error) {
	if p.rawDSN != "" {
		// Priority 1: --dsn supplied; strip password by composing with empty password.
		stripped := pgdsn.Compose(p.rawDSN, "")
		return stripped, "explicit-dsn", nil
	}

	host := p.host
	port := p.port
	decision = "flags"

	if host == "" || port == 0 {
		discHost, discPort, found := discoverLocalPostgres(p.clusterDir)
		if !found && (host == "" || port == 0) {
			return "", "", fmt.Errorf(
				"no local Postgres cluster found at %s; either start it, pass --pg-host=<remote>, or pass --dsn=<full>",
				p.clusterDir,
			)
		}
		if host == "" && found {
			host = discHost
		}
		if port == 0 && found {
			port = discPort
		}
		decision = "discovery"
	}

	user := p.user
	if user == "" {
		user = "beads"
	}
	db := p.database
	if db == "" {
		db = sanitizeDBName(prefix)
		if db == "" {
			db = "beads"
		}
	}
	sslmode := p.sslmode
	if sslmode == "" {
		if pgIsLoopback(host) {
			sslmode = "disable"
		} else {
			sslmode = "require"
		}
	}

	stripped := pgdsn.BuildFromFields(host, port, user, db, sslmode)
	return stripped, decision, nil
}

// pgIsLoopback returns true when host is a loopback address or "localhost".
func pgIsLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// pgWriteDiscoveryLog appends a single-line record to .beads/.discovery_log.
// The record contains no credentials (NFR-4).
func pgWriteDiscoveryLog(beadsDir, decision, strippedDSN string) {
	logPath := filepath.Join(beadsDir, ".discovery_log")
	entry := fmt.Sprintf("%s decision=%s target=%s\n",
		time.Now().UTC().Format(time.RFC3339),
		decision,
		pgdsn.RenderRedacted(strippedDSN),
	)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600) // #nosec G304: path constrained to user-controlled beadsDir
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(entry)
}

// pgEnsureGitignored appends entry to .beads/.gitignore when not already present.
func pgEnsureGitignored(beadsDir, entry string) {
	path := filepath.Join(beadsDir, ".gitignore")
	data, _ := os.ReadFile(path) // #nosec G304: path constrained to user-controlled beadsDir
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600) // #nosec G304: path constrained to user-controlled beadsDir
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "\n# Postgres init log (no credentials)\n%s\n", entry)
}
