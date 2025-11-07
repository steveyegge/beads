package templates_test

import (
	"encoding/json"
	"html/template"
	"testing"
)

func mustDefaultFiltersJSON(t *testing.T) template.JS {
	t.Helper()

	filters := map[string]any{
		"query":         "",
		"status":        "open",
		"issueType":     "",
		"priority":      "",
		"assignee":      "",
		"labelsAll":     []string{},
		"labelsAny":     []string{},
		"prefix":        "",
		"sortPrimary":   "priority-asc",
		"sortSecondary": "updated-desc",
	}
	data, err := json.Marshal(filters)
	if err != nil {
		t.Fatalf("marshal default filters: %v", err)
	}
	return template.JS(data)
}
