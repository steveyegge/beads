package dolt

import (
	"reflect"
	"strings"
	"testing"
)

func TestWispCycleDetectionTablesUseWispOnlyForWispTarget(t *testing.T) {
	got := wispCycleDetectionTables(true)
	want := []string{"wisp_dependencies"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWispCycleDetectionTablesUseBothTablesForPermanentTarget(t *testing.T) {
	got := wispCycleDetectionTables(false)
	want := []string{"dependencies", "wisp_dependencies"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestWispCycleReachabilityQuerySingleTableJoinsDirectly(t *testing.T) {
	query := wispCycleReachabilityQuery([]string{"wisp_dependencies"})
	if !strings.Contains(query, "JOIN wisp_dependencies d ON d.issue_id = r.node") {
		t.Fatalf("query does not join wisp_dependencies directly:\n%s", query)
	}
	if strings.Contains(query, "JOIN (SELECT") {
		t.Fatalf("single-table wisp cycle query should not materialize a derived dependency table:\n%s", query)
	}
	if !strings.Contains(query, "d.type = 'blocks'") {
		t.Fatalf("query does not filter blocks at the direct join:\n%s", query)
	}
	if strings.Contains(query, "UNION ALL") || strings.Contains(query, "depth") {
		t.Fatalf("wisp cycle query should traverse unique nodes, not enumerate paths:\n%s", query)
	}
}

func TestWispCycleReachabilityQueryMultipleTablesTraversesUniqueNodes(t *testing.T) {
	query := wispCycleReachabilityQuery([]string{"dependencies", "wisp_dependencies"})
	if strings.Contains(query, "UNION ALL") || strings.Contains(query, "depth") {
		t.Fatalf("multi-table wisp cycle query should traverse unique nodes, not enumerate paths:\n%s", query)
	}
	if !strings.Contains(query, "SELECT issue_id, depends_on_id FROM dependencies") {
		t.Fatalf("query does not include dependencies table:\n%s", query)
	}
	if !strings.Contains(query, "SELECT issue_id, depends_on_id FROM wisp_dependencies") {
		t.Fatalf("query does not include wisp_dependencies table:\n%s", query)
	}
}
