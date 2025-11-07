package rpc

import (
	"fmt"
	"strings"
	"time"
)

const (
	closedQueueOrder       = "closed"
	legacyClosedQueueOrder = "closed_desc"
)

func parseClosedCursor(cursor string) (time.Time, string, error) {
	trimmed := strings.TrimSpace(cursor)
	if trimmed == "" {
		return time.Time{}, "", fmt.Errorf("cursor empty")
	}

	parts := strings.SplitN(trimmed, "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor format")
	}

	ts, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(parts[0]))
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor timestamp: %w", err)
	}

	id := strings.TrimSpace(parts[1])
	if id == "" {
		return time.Time{}, "", fmt.Errorf("missing cursor id")
	}

	return ts, id, nil
}
