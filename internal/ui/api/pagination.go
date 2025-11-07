package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/types"
)

const (
	ClosedQueueDefaultLimit = 50
	closedQueueMaxLimit     = 200
	closedQueueOrder        = "closed"
	legacyClosedQueueOrder  = "closed_desc"
	closedCursorSeparator   = "|"
)

func clampClosedLimit(limit int) int {
	if limit <= 0 {
		return 0
	}
	if limit > closedQueueMaxLimit {
		return closedQueueMaxLimit
	}
	return limit
}

func paginateClosedIssues(issues []*types.Issue, pageSize int) ([]*types.Issue, bool, string) {
	if pageSize <= 0 {
		pageSize = ClosedQueueDefaultLimit
	}
	hasMore := len(issues) > pageSize
	trimmed := issues
	if hasMore {
		trimmed = issues[:pageSize]
	}

	nextCursor := ""
	if hasMore && len(trimmed) > 0 {
		nextCursor = encodeClosedCursor(trimmed[len(trimmed)-1])
	}

	return trimmed, hasMore, nextCursor
}

func encodeClosedCursor(issue *types.Issue) string {
	if issue == nil {
		return ""
	}
	timestamp := closedSortTimestamp(issue)
	if timestamp.IsZero() {
		timestamp = time.Now().UTC()
	}
	id := strings.TrimSpace(issue.ID)
	if id == "" {
		id = "unknown"
	}
	return timestamp.Format(time.RFC3339Nano) + closedCursorSeparator + id
}

func parseClosedCursor(cursor string) (time.Time, string, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return time.Time{}, "", fmt.Errorf("empty cursor")
	}
	parts := strings.SplitN(cursor, closedCursorSeparator, 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor format")
	}
	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(parts[0]))
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor timestamp: %w", err)
	}
	id := strings.TrimSpace(parts[1])
	if id == "" {
		return time.Time{}, "", fmt.Errorf("cursor missing issue id")
	}
	return ts, id, nil
}
