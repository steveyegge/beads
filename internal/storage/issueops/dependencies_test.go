package issueops

import (
	"reflect"
	"strings"
	"testing"
)

func TestCycleDetectionTablesUseBothTablesByDefault(t *testing.T) {
	got := cycleDetectionTables()
	want := AllDepTables()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestCycleReachabilityQuerySingleTableJoinsDirectly(t *testing.T) {
	query := cycleReachabilityQuery([]string{"wisp_issue_dependencies"})
	if !strings.Contains(query, "JOIN wisp_issue_dependencies d ON d.source_id = r.node") {
		t.Fatalf("query does not join wisp_issue_dependencies directly:\n%s", query)
	}
	if !strings.Contains(query, "SELECT d.depends_on_issue_id") {
		t.Fatalf("single-table cycle query should project the typed target column:\n%s", query)
	}
	if strings.Contains(query, "JOIN (SELECT") {
		t.Fatalf("single-table cycle query should not materialize a derived dependency table:\n%s", query)
	}
	if !strings.Contains(query, "d.type IN ('blocks', 'conditional-blocks')") {
		t.Fatalf("query does not filter blocking dependency types at the direct join:\n%s", query)
	}
	if strings.Contains(query, "UNION ALL") || strings.Contains(query, "depth") {
		t.Fatalf("cycle query should traverse unique nodes, not enumerate paths:\n%s", query)
	}
}

func TestCycleReachabilityQueryMultipleTablesTraversesUniqueNodes(t *testing.T) {
	query := cycleReachabilityQuery(AllDepTables())
	if strings.Contains(query, "UNION ALL") || strings.Contains(query, "depth") {
		t.Fatalf("multi-table cycle query should traverse unique nodes, not enumerate paths:\n%s", query)
	}
	for _, table := range AllDepTables() {
		if !strings.Contains(query, "FROM "+table) {
			t.Fatalf("query does not include %s table:\n%s", table, query)
		}
		col := DepTargetColumnForTable(table)
		if !strings.Contains(query, col+" AS depends_on_id FROM "+table) {
			t.Fatalf("query does not project typed column %s from %s:\n%s", col, table, query)
		}
	}
}
