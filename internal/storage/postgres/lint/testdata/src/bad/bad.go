// Package bad exercises the pgxsqlsafe analyzer. The Sprintf call below is
// expected to fire; the gosec-suppressed one is expected to pass.
package bad

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

func bad(ctx context.Context, conn *pgx.Conn, table string) error {
	q := fmt.Sprintf("SELECT * FROM %s WHERE id = $1", table) // want `fmt.Sprintf in pgx-importing file: use \$N parameter substitution instead`
	_, err := conn.Exec(ctx, q, "foo")
	return err
}

func ok(ctx context.Context, conn *pgx.Conn, table string) error {
	//nolint:gosec // table is allowlisted by guardTable
	q := fmt.Sprintf("SELECT * FROM %s WHERE id = $1", table)
	_, err := conn.Exec(ctx, q, "foo")
	return err
}
