package templates

import (
	"bytes"
	"encoding/json"
	"html/template"
)

// BasePageData captures the dynamic state required to render the UI shell.
type BasePageData struct {
	AppTitle             string
	Theme                string
	InitialFiltersJSON   template.JS
	EventStreamURL       string
	StaticPrefix         string
	DisableEventStream   bool
	LiveUpdatesAvailable bool
}

// RenderBasePage executes the base layout template with the provided data.
func RenderBasePage(data BasePageData) ([]byte, error) {
	if data.AppTitle == "" {
		data.AppTitle = "Beads"
	}
	if data.Theme == "" {
		data.Theme = "white"
	}
	if data.StaticPrefix == "" {
		data.StaticPrefix = "/.assets"
	}
	if len(data.InitialFiltersJSON) == 0 {
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
		if err == nil {
			data.InitialFiltersJSON = template.JS(jsonBytes)
		}
	}
	if data.DisableEventStream {
		data.EventStreamURL = ""
	} else if data.EventStreamURL == "" {
		data.EventStreamURL = "/events"
	}
	data.LiveUpdatesAvailable = !data.DisableEventStream

	tmpl, err := Parse("index.html.tmpl")
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
