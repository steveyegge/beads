package jira

import (
	"fmt"
	"strings"
	"time"
)

// IsJiraExternalRef checks if an external_ref URL matches the configured Jira instance.
// It validates both the URL structure (/browse/PROJECT-123) and optionally the host.
func IsJiraExternalRef(externalRef, jiraURL string) bool {
	// Must contain /browse/ pattern
	if !strings.Contains(externalRef, "/browse/") {
		return false
	}

	// If jiraURL is provided, validate the host matches
	if jiraURL != "" {
		jiraURL = strings.TrimSuffix(jiraURL, "/")
		if !strings.HasPrefix(externalRef, jiraURL) {
			return false
		}
	}

	return true
}

// ExtractJiraKey extracts the Jira issue key from an external_ref URL.
// For example, "https://company.atlassian.net/browse/PROJ-123" returns "PROJ-123".
func ExtractJiraKey(externalRef string) string {
	idx := strings.LastIndex(externalRef, "/browse/")
	if idx == -1 {
		return ""
	}
	return externalRef[idx+len("/browse/"):]
}

// ParseTimestamp parses Jira's timestamp format into a time.Time.
// Jira uses ISO 8601 with timezone: 2024-01-15T10:30:00.000+0000 or 2024-01-15T10:30:00.000Z
func ParseTimestamp(ts string) (time.Time, error) {
	if ts == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}

	// Try common formats
	formats := []string{
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.000Z",
		"2006-01-02T15:04:05-0700",
		"2006-01-02T15:04:05Z",
		time.RFC3339,
		time.RFC3339Nano,
	}

	for _, format := range formats {
		if t, err := time.Parse(format, ts); err == nil {
			return t, nil
		}
	}

	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %s", ts)
}
