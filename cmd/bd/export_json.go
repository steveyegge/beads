package main

import (
	"encoding/json"
	"io"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

// JSONExport is the top-level structured JSON export document.
type JSONExport struct {
	Version  string            `json:"version"`
	Metadata JSONExportMeta    `json:"metadata"`
	Issues   []*types.Issue    `json:"issues"`
}

// JSONExportMeta contains metadata about the export.
type JSONExportMeta struct {
	ExportedAt time.Time         `json:"exported_at"`
	Count      int               `json:"count"`
	Filters    map[string]string `json:"filters,omitempty"`
}

// writeJSONExport writes issues as a structured JSON document.
func writeJSONExport(w io.Writer, issues []*types.Issue, filters map[string]string) error {
	if issues == nil {
		issues = []*types.Issue{}
	}
	doc := JSONExport{
		Version: Version,
		Metadata: JSONExportMeta{
			ExportedAt: time.Now().UTC(),
			Count:      len(issues),
			Filters:    filters,
		},
		Issues: issues,
	}

	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(doc)
}
