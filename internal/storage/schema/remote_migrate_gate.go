package schema

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/storage/dberrors"
)

// AllowRemoteMigrateEnv, when set to "1", lets the designated migrator apply
// pending schema migrations to a remote-backed database despite the gate below.
const AllowRemoteMigrateEnv = "BD_ALLOW_REMOTE_MIGRATE"

// RemoteMigrateGateError is returned when bd is about to auto-apply pending
// schema migrations to an existing database that has a remote configured.
//
// gastownhall/beads#4259: bd auto-runs pending migrations the first time a new
// binary opens an existing database. If two clones that sync through a shared
// remote each upgrade independently, both migrate in place and the schema forks
// — `bd dolt pull` then fails to merge with no bd-level recovery. The supported
// flow is "only ONE machine migrates the database; every other client adopts the
// migrated database from the remote". This gate refuses the silent in-place
// migration and makes the operator choose migrate vs. adopt. It applies to both
// server mode and embedded mode (the mode the original report was filed against).
type RemoteMigrateGateError struct {
	CurrentVersion int
	LatestVersion  int
	Pending        int
}

func (e *RemoteMigrateGateError) Error() string {
	unit := "migrations"
	if e.Pending == 1 {
		unit = "migration"
	}
	return fmt.Sprintf("refusing to auto-apply %d pending schema %s to a remote-backed database (v%d -> v%d): migrating clones independently forks the schema (#4259)",
		e.Pending, unit, e.CurrentVersion, e.LatestVersion)
}

// UserMessage returns the full multi-line error block for terminal output.
func (e *RemoteMigrateGateError) UserMessage() string {
	return e.Error() + "\n" +
		"\n" +
		"  This database syncs with a remote. Applying schema migrations on more than\n" +
		"  one clone independently forks the schema so `bd dolt pull` can no longer\n" +
		"  merge — the break is silent and unrecoverable.\n" +
		"\n" +
		"  Choose one:\n" +
		"    • You are the designated migrator (only ONE machine should be): migrate,\n" +
		"      then publish the migrated database to the remote:\n" +
		"        " + AllowRemoteMigrateEnv + "=1 bd <command>\n" +
		"        bd dolt push\n" +
		"    • Another machine has already migrated: adopt its database instead of\n" +
		"      migrating here — re-clone from the remote so you receive the migrated\n" +
		"      schema:\n" +
		"        bd bootstrap\n"
}

// EscapeHint returns the escape-hatch string for JSON error output.
func (e *RemoteMigrateGateError) EscapeHint() string {
	return AllowRemoteMigrateEnv + "=1 bd <command>"
}

// IsRemoteMigrateGateError reports whether err (or any error it wraps) is a
// *RemoteMigrateGateError.
func IsRemoteMigrateGateError(err error) bool {
	var e *RemoteMigrateGateError
	return errors.As(err, &e)
}

// CheckRemoteMigrateGate refuses to auto-apply pending schema migrations when the
// database already has a recorded schema version, has pending migrations, and has
// a remote configured — unless the designated-migrator escape hatch is set. It
// returns nil (allow) for a fresh database, an already-current database, or one
// with no remote. Call it before MigrateUp/MigrateUpWithLock on every read/write
// store open (both server and embedded mode).
func CheckRemoteMigrateGate(ctx context.Context, db DBConn) error {
	if os.Getenv(AllowRemoteMigrateEnv) == "1" {
		fmt.Fprintf(os.Stderr,
			"Warning: applying schema migrations to a remote-backed database (%s=1); only one clone should migrate, then `bd dolt push`\n",
			AllowRemoteMigrateEnv)
		return nil
	}

	// CurrentVersion treats a missing schema_migrations table as version 0, so a
	// brand-new database falls through the current==0 check below — nothing to fork.
	current, err := CurrentVersion(ctx, db)
	if err != nil {
		return fmt.Errorf("remote-migrate gate: read current version: %w", err)
	}
	if current == 0 {
		return nil // fresh database — nothing to fork
	}

	pending, err := PendingVersions(ctx, db)
	if err != nil {
		return fmt.Errorf("remote-migrate gate: read pending versions: %w", err)
	}
	if len(pending) == 0 {
		return nil // already current — nothing to migrate
	}

	hasRemote, err := anyDoltRemoteConfigured(ctx, db)
	if err != nil {
		return fmt.Errorf("remote-migrate gate: read remotes: %w", err)
	}
	if !hasRemote {
		return nil // no remote — no cross-clone fork risk
	}

	return &RemoteMigrateGateError{
		CurrentVersion: current,
		LatestVersion:  LatestVersion(),
		Pending:        len(pending),
	}
}

// anyDoltRemoteConfigured reports whether the database has any Dolt remote
// registered. dolt_remotes is always present in a Dolt database; a
// "table not found" is treated as "no remotes" so a missing system table can
// never wedge every store open.
func anyDoltRemoteConfigured(ctx context.Context, db DBConn) (bool, error) {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM dolt_remotes").Scan(&count); err != nil {
		if dberrors.IsTableNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return count > 0, nil
}
