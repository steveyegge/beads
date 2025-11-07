package templates

import "bytes"

// ListAppendData represents the payload for incremental closed-queue rendering.
type ListAppendData struct {
	Issues       []ListIssue
	HasMore      bool
	LoadMoreURL  string
	LoadMoreVals map[string]any
}

// RenderListAppend renders the append fragment for closed queue pagination.
func RenderListAppend(data ListAppendData) ([]byte, error) {
	tmpl, err := Parse("issue_list_append.tmpl")
	if err != nil {
		return nil, err
	}

	if data.LoadMoreURL == "" {
		data.LoadMoreURL = "/fragments/issues"
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
