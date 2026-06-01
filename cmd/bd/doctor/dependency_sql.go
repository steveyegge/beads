package doctor

import (
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/issueops"
)

// doctorDependencyUnionSQL returns a UNION ALL across all six split
// dependency tables projecting (dep_table, issue_id, depends_on_id, type)
// with dep_table being the table name literal and depends_on_id being the
// table's typed target column. Callers wrap this in a parenthesized subquery.
func doctorDependencyUnionSQL() string {
	return doctorDepUnion(false)
}

// doctorDependencyUnionWithThreadSQL is the same as doctorDependencyUnionSQL
// but adds a trailing thread_id column.
func doctorDependencyUnionWithThreadSQL() string {
	return doctorDepUnion(true)
}

func doctorDepUnion(withThread bool) string {
	cols := "'%s' AS dep_table, source_id AS issue_id, %s AS depends_on_id, type"
	if withThread {
		cols += ", thread_id"
	}
	parts := make([]string, 0, 6)
	for _, t := range issueops.AllDepTables() {
		col := issueops.DepTargetColumnForTable(t)
		parts = append(parts, fmt.Sprintf("SELECT "+cols+" FROM %s", t, col, t))
	}
	return "\n\t\t\t" + strings.Join(parts, "\n\t\t\tUNION ALL\n\t\t\t")
}
