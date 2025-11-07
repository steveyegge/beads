package templates

import (
	"bytes"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"
)

// ListFragmentData represents the dynamic values rendered into the issue list fragment.
type ListFragmentData struct {
	Heading         string
	SelectedIssueID string
	Filters         url.Values
	RefreshURL      string
	EmptyMessage    string
	Issues          []ListIssue
	HasMore         bool
	NextCursor      string
	LoadMoreURL     string
	LoadMoreVals    map[string]any
}

// ListIssue contains the per-row metadata for the issue list.
type ListIssue struct {
	ID              string
	Title           string
	Status          string
	IssueType       string
	IssueTypeLabel  string
	IssueTypeClass  string
	Priority        int
	PriorityLabel   string
	PriorityClass   string
	UpdatedISO      string
	UpdatedRelative string
	Active          bool
	Index           int
	DetailURL       string
}

// ExecuteListTemplate renders the issue list fragment using the provided template.
// When tmpl is nil, the embedded template is parsed for each invocation.
func ExecuteListTemplate(tmpl *template.Template, data ListFragmentData) ([]byte, error) {
	if strings.TrimSpace(data.Heading) == "" {
		data.Heading = "Filtered issues"
	}
	if data.EmptyMessage == "" {
		data.EmptyMessage = "No issues match the current filters."
	}
	if data.RefreshURL == "" {
		data.RefreshURL = BuildListRefreshURL(data.SelectedIssueID, data.Filters)
	}

	if data.LoadMoreURL == "" {
		data.LoadMoreURL = data.RefreshURL
	}

	var err error
	if tmpl == nil {
		tmpl, err = Parse("issue_list.tmpl")
		if err != nil {
			return nil, err
		}
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RenderListFragment executes the issue list fragment template with the provided data.
func RenderListFragment(data ListFragmentData) ([]byte, error) {
	return ExecuteListTemplate(nil, data)
}

// BuildListRefreshURL constructs the refresh endpoint including filters.
func BuildListRefreshURL(selectedID string, filters url.Values) string {
	base := "/fragments/issues"
	var parts []string

	if filters != nil && len(filters) > 0 {
		encoded := filters.Encode()
		if encoded != "" {
			parts = append(parts, encoded)
		}
	}

	if strings.TrimSpace(selectedID) != "" {
		parts = append(parts, "selected="+url.QueryEscape(selectedID))
	}

	if len(parts) == 0 {
		return base
	}
	return base + "?" + strings.Join(parts, "&")
}

// RelativeTimeString formats a relative time suitable for display in the UI.
func RelativeTimeString(now, then time.Time) string {
	diff := now.Sub(then)
	if diff < 0 {
		diff = -diff
	}

	switch {
	case diff < 45*time.Second:
		return "just now"
	case diff < time.Minute:
		return "less than a minute ago"
	case diff < time.Hour:
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case diff < 24*time.Hour:
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	case diff < 30*24*time.Hour:
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case diff < 365*24*time.Hour:
		months := int(diff.Hours() / (24 * 30))
		if months <= 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	default:
		years := int(diff.Hours() / (24 * 365))
		if years <= 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", years)
	}
}
