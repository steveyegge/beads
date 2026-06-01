package doctor

import (
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/issueops"
)

func doctorDependencyUnionSQL() string {
	return doctorDepUnion(false)
}

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
