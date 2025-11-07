package e2e

import (
	"encoding/json"
	"html/template"
	"testing"

	"github.com/steveyegge/beads/internal/ui/templates"
)

type BasePageOption func(*templates.BasePageData)

// WithFilters replaces the initial filters JSON payload with the provided map.
func WithFilters(filters map[string]any) BasePageOption {
	return func(data *templates.BasePageData) {
		jsonBytes, err := json.Marshal(filters)
		if err != nil {
			panic(err)
		}
		data.InitialFiltersJSON = template.JS(jsonBytes)
	}
}

// WithEventStreamURL overrides the SSE endpoint exposed to the client.
func WithEventStreamURL(url string) BasePageOption {
	return func(data *templates.BasePageData) {
		data.EventStreamURL = url
	}
}

// WithoutEventStream disables live updates for test harnesses that stub SSE.
func WithoutEventStream() BasePageOption {
	return func(data *templates.BasePageData) {
		data.DisableEventStream = true
	}
}

// WithStaticPrefix overrides the static asset prefix.
func WithStaticPrefix(prefix string) BasePageOption {
	return func(data *templates.BasePageData) {
		data.StaticPrefix = prefix
	}
}

func renderBasePage(t testing.TB, title string, opts ...BasePageOption) []byte {
	t.Helper()

	defaultFilters := map[string]any{
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
	jsonBytes, err := json.Marshal(defaultFilters)
	if err != nil {
		t.Fatalf("marshal default filters: %v", err)
	}

	data := templates.BasePageData{
		AppTitle:           title,
		InitialFiltersJSON: template.JS(jsonBytes),
		EventStreamURL:     "/events",
		StaticPrefix:       "/.assets",
	}

	for _, opt := range opts {
		opt(&data)
	}

	html, err := templates.RenderBasePage(data)
	if err != nil {
		t.Fatalf("render base page: %v", err)
	}
	return html
}
