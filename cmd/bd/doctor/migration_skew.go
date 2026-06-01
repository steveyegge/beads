package doctor

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/schema"
)

const migrationContentSkewCheckName = "Migration Content Skew"

// CheckMigrationContentSkew detects when this database and its remote applied
// DIFFERENT content for the same migration version — the silent schema fork from
// gastownhall/beads#4259. It compares the local schema_migrations content hashes
// against the already-cached remote-tracking ref (no network fetch) and warns on
// any divergence.
//
// This is a read-only diagnostic; it never gates push/pull. It runs in server
// mode (where `bd doctor` runs) and skips cleanly when there is no remote, no
// cached remote ref, or no recorded hashes to compare.
func CheckMigrationContentSkew(ss *SharedStore) DoctorCheck {
	store := ss.Store()
	if store == nil {
		return DoctorCheck{
			Name:     migrationContentSkewCheckName,
			Status:   StatusOK,
			Message:  "N/A (no database)",
			Category: CategoryData,
		}
	}
	return checkMigrationContentSkew(context.Background(), store.DB())
}

func checkMigrationContentSkew(ctx context.Context, db *sql.DB) DoctorCheck {
	ok := func(msg string) DoctorCheck {
		return DoctorCheck{Name: migrationContentSkewCheckName, Status: StatusOK, Message: msg, Category: CategoryData}
	}

	// Without a remote there is nothing to compare against.
	var remote string
	if err := db.QueryRowContext(ctx,
		"SELECT name FROM dolt_remotes ORDER BY name LIMIT 1").Scan(&remote); err != nil {
		return ok("No remote configured — nothing to compare")
	}

	branch := "main"
	var active string
	if err := db.QueryRowContext(ctx, "SELECT active_branch()").Scan(&active); err == nil && active != "" {
		branch = active
	}

	local, err := readMigrationContentHashes(ctx, db, "", "")
	if err != nil {
		return ok("schema_migrations unavailable")
	}
	remoteHashes, err := readMigrationContentHashes(ctx, db, remote, branch)
	if err != nil {
		// The remote-tracking ref is not cached yet (e.g. never pulled).
		return ok("No cached remote ref to compare")
	}

	skewed := schema.ContentHashSkew(local, remoteHashes)
	if len(skewed) == 0 {
		return ok(fmt.Sprintf("Applied migrations match remote %q", remote))
	}

	versions := make([]string, len(skewed))
	for i, v := range skewed {
		versions[i] = fmt.Sprintf("%04d", v)
	}
	return DoctorCheck{
		Name:    migrationContentSkewCheckName,
		Status:  StatusWarning,
		Message: fmt.Sprintf("This database and remote %q applied different content for migration(s) %s", remote, strings.Join(versions, ", ")),
		Detail:  "Two clones ran different migration content for the same version number — a silent schema fork (gastownhall/beads#4259). `bd dolt pull` may fail to merge cryptically.",
		Fix:     "Upgrade every clone to a bd version that carries the schema-convergence migration. If a merge already fails, make one clone canonical and re-bootstrap the others from the remote.",
		Category: CategoryData,
	}
}

// readMigrationContentHashes reads version -> content_hash from schema_migrations,
// either at HEAD (remote == "") or AS OF the cached remote-tracking ref
// remotes/<remote>/<branch>. NULL/empty hashes are dropped. It returns an error
// when the table or the remote ref is unavailable, which the caller treats as
// "nothing to compare".
func readMigrationContentHashes(ctx context.Context, db *sql.DB, remote, branch string) (map[int]string, error) {
	var rows *sql.Rows
	var err error
	if remote == "" {
		rows, err = db.QueryContext(ctx, "SELECT version, content_hash FROM schema_migrations")
	} else {
		// CONCAT keeps the (user-controlled) remote name out of the SQL text.
		rows, err = db.QueryContext(ctx,
			"SELECT version, content_hash FROM schema_migrations AS OF CONCAT('remotes/', ?, '/', ?)",
			remote, branch)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int]string{}
	for rows.Next() {
		var version int
		var hash sql.NullString
		if err := rows.Scan(&version, &hash); err != nil {
			return nil, err
		}
		if hash.Valid && hash.String != "" {
			out[version] = hash.String
		}
	}
	return out, rows.Err()
}
