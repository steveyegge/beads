package fix

import (
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/storage/issueops"
)

func fixDependencyUnionSQL() string {
	parts := make([]string, 0, 6)
	for _, t := range issueops.AllDepTables() {
		col := issueops.DepTargetColumnForTable(t)
		parts = append(parts, fmt.Sprintf(
			"SELECT '%s' AS dep_table, source_id AS issue_id, %s AS depends_on_id, type FROM %s",
			t, col, t))
	}
	return "\n\t\t\t" + strings.Join(parts, "\n\t\t\tUNION ALL\n\t\t\t")
}

func fixDeleteByTable(table string) (sqlText, targetCol string, ok bool) {
	col := issueops.DepTargetColumnForTable(table)
	if col == "" {
		return "", "", false
	}
	//nolint:gosec // G201: table and col come from issueops routing helpers (closed set).
	return fmt.Sprintf("DELETE FROM %s WHERE source_id = ? AND %s = ?", table, col), col, true
}

func fixDeleteByTableWithType(table string) (sqlText, targetCol string, ok bool) {
	col := issueops.DepTargetColumnForTable(table)
	if col == "" {
		return "", "", false
	}
	//nolint:gosec // G201: table and col come from issueops routing helpers (closed set).
	return fmt.Sprintf("DELETE FROM %s WHERE source_id = ? AND %s = ? AND type = ?", table, col), col, true
}
