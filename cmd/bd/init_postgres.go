package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/postgres/dsn"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/utils"
)

// Sentinel errors for cmd/bd's init-time flag validation. They surface to
// the user via FatalError; tests assert against the message text.
var (
	errMissingDSN    = errors.New("--backend=postgres requires --dsn=<connection string>")
	errUnexpectedDSN = errors.New("--dsn is only valid with --backend=postgres")
	errFlagConflict  = errors.New("--backend=postgres cannot be combined with --server or --shared-server (postgres has no embedded/server submode)")
)

type postgresInitInput struct {
	dsn        string
	prefixFlag string
	quiet      bool
}

// runPostgresInit implements `bd init --backend=postgres`. It validates the
// DSN, opens the Postgres store via the registry (which runs first-connect
// schema migrations), persists a credential-stripped DSN to metadata.json,
// and seeds the issue prefix + project identity.
//
// Dolt-specific bootstrap (server flags, embedded data dir, federation
// remote, JSONL import) is intentionally skipped — those code paths exist
// in the Dolt branch of the parent init Run handler.
func runPostgresInit(ctx context.Context, in postgresInitInput) error {
	strippedDSN, err := dsn.Strip(in.dsn)
	if err != nil {
		return fmt.Errorf("postgres dsn: %w", err)
	}

	prefix, err := resolveInitPrefix(in.prefixFlag)
	if err != nil {
		return err
	}

	beadsDir, err := resolveBeadsInitDir()
	if err != nil {
		return err
	}

	if err := guardPostgresInit(beadsDir); err != nil {
		return err
	}

	if err := os.MkdirAll(beadsDir, config.BeadsDirPerm); err != nil {
		return fmt.Errorf("create %s: %w", beadsDir, err)
	}

	// At init time the password lives on the --dsn flag, so connect with the
	// raw DSN. The stripped form is what later persists to metadata.json;
	// runtime invocations recombine BEADS_POSTGRES_PASSWORD with the stripped
	// form via dsn.Compose (see store_factory.go).
	store, err := storage.Open(ctx, storage.BackendPostgres, storage.ConnectionConfig{
		BeadsDir: beadsDir,
		DSN:      in.dsn,
	})
	if err != nil {
		return fmt.Errorf("open postgres store: %w", err)
	}
	defer func() { _ = store.Close() }()

	if existing, _ := store.GetConfig(ctx, "issue_prefix"); existing == "" {
		if err := store.SetConfig(ctx, "issue_prefix", prefix); err != nil {
			return fmt.Errorf("seed issue_prefix: %w", err)
		}
	}

	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		return fmt.Errorf("load metadata.json: %w", err)
	}
	if cfg == nil {
		cfg = configfile.DefaultConfig()
		// Database is a Dolt-specific top-level field; PG carries the database
		// name in the DSN. Clear it so PG configs don't ship a stale "beads.db"
		// pointer that would confuse downstream tooling.
		cfg.Database = ""
	}
	cfg.Backend = configfile.BackendPostgres
	cfg.PostgresDSN = strippedDSN

	if cfg.ProjectID == "" {
		// Adopt an existing project ID from the database when present
		// (e.g. multiple rigs sharing a Postgres database). Otherwise mint
		// a new one.
		if existingID, err := mustConfig(store).GetMetadata(ctx, "_project_id"); err == nil && existingID != "" {
			cfg.ProjectID = existingID
		} else {
			cfg.ProjectID = configfile.GenerateProjectID()
		}
	}

	if err := cfg.Save(beadsDir); err != nil {
		return fmt.Errorf("save metadata.json: %w", err)
	}

	if cfg.ProjectID != "" {
		if err := mustConfig(store).SetMetadata(ctx, "_project_id", cfg.ProjectID); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to write project ID to database: %v\n", err)
		}
	}

	if err := store.SetLocalMetadata(ctx, "bd_version", Version); err != nil && !in.quiet {
		fmt.Fprintf(os.Stderr, "Warning: failed to write bd_version local metadata: %v\n", err)
	}

	if !in.quiet {
		fmt.Printf("\n%s bd initialized successfully!\n\n", ui.RenderPass("✓"))
		fmt.Printf("  Backend: %s\n", ui.RenderAccent("postgres"))
		fmt.Printf("  DSN: %s\n", ui.RenderAccent(strippedDSN))
		fmt.Printf("  Issue prefix: %s\n", ui.RenderAccent(prefix))
		fmt.Printf("  Issues will be named: %s\n\n", ui.RenderAccent(prefix+"-<hash> (e.g., "+prefix+"-a3f2dd)"))
		fmt.Printf("Run %s to get started.\n\n", ui.RenderAccent("bd quickstart"))
		printPostgresPostInitNote()
	}

	return nil
}

// resolveInitPrefix mirrors the dolt branch's prefix resolution + sanitization
// so issue IDs are byte-identical across backends. Dots are normalized to
// underscores; leading dots and trailing hyphens are stripped; non-letter
// leading characters are prefixed with "bd_" so the prefix remains a valid
// SQL identifier on either backend.
func resolveInitPrefix(flagPrefix string) (string, error) {
	prefix := flagPrefix
	if prefix == "" {
		prefix = config.GetString("issue-prefix")
	}
	if prefix == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get cwd for prefix auto-detect: %w", err)
		}
		prefix = filepath.Base(cwd)
	}
	prefix = strings.TrimLeft(prefix, ".")
	prefix = strings.TrimRight(prefix, "-")
	prefix = strings.ReplaceAll(prefix, ".", "_")
	if len(prefix) > 0 && !((prefix[0] >= 'a' && prefix[0] <= 'z') || (prefix[0] >= 'A' && prefix[0] <= 'Z') || prefix[0] == '_') {
		prefix = "bd_" + prefix
	}
	return prefix, nil
}

// resolveBeadsInitDir returns the .beads directory path for init, honoring
// BEADS_DIR (canonicalized) and worktree fallbacks consistent with the dolt
// branch. The directory is not created here; callers are responsible for
// MkdirAll.
func resolveBeadsInitDir() (string, error) {
	if env := os.Getenv("BEADS_DIR"); env != "" {
		return utils.CanonicalizePath(env), nil
	}
	if fallback := beads.GetWorktreeFallbackBeadsDir(); fallback != "" {
		return fallback, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}
	return beads.FollowRedirect(filepath.Join(cwd, ".beads")), nil
}

// guardPostgresInit refuses to overwrite an existing Dolt-backed metadata.json
// without --reinit-local. Re-running `bd init --backend=postgres` against an
// existing Postgres-backed config is allowed because the migration runner is
// idempotent.
func guardPostgresInit(beadsDir string) error {
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		return fmt.Errorf("load existing metadata.json: %w", err)
	}
	if cfg == nil {
		return nil
	}
	if cfg.GetBackend() == configfile.BackendDolt && (cfg.DoltDatabase != "" || cfg.DoltMode != "") {
		return fmt.Errorf("%s already initialized with Dolt; refusing to overwrite\n  to switch backends, export first (bd export > backup.jsonl) and reinitialize", beadsDir)
	}
	return nil
}

// printPostgresPostInitNote enumerates Dolt-only commands that error on the
// Postgres backend, plus a reminder about BEADS_POSTGRES_PASSWORD. It writes
// to stderr so a downstream --json consumer reading stdout is unaffected.
func printPostgresPostInitNote() {
	fmt.Fprintf(os.Stderr, "%s Postgres backend in use; the Dolt commit graph is not active.\n", ui.RenderWarn("note:"))
	fmt.Fprintf(os.Stderr, "      The following commands return errors on this backend:\n")
	fmt.Fprintf(os.Stderr, "        bd dolt {push,pull,branch,merge,...}\n")
	fmt.Fprintf(os.Stderr, "        bd federation {add,sync,list,...}\n")
	fmt.Fprintf(os.Stderr, "        bd vc {checkout,merge,log,...}\n")
	fmt.Fprintf(os.Stderr, "        bd flatten, bd gc, bd diff, bd history, bd restore\n")
	fmt.Fprintf(os.Stderr, "      See `bd <subcommand> --help` for backend requirements.\n\n")
	fmt.Fprintf(os.Stderr, "      Set BEADS_POSTGRES_PASSWORD so subsequent bd commands can authenticate;\n")
	fmt.Fprintf(os.Stderr, "      the password from --dsn was stripped before persistence.\n")
}
