package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/config"
)

// getDoltBackupInterval returns the configured backup interval.
// Returns 0 for "off" or empty (disabled). Returns an error for invalid durations.
func getDoltBackupInterval() (time.Duration, error) {
	val := strings.TrimSpace(doltBackupInterval)
	if val == "" {
		val = config.GetString("dolt.backup.interval")
	}
	val = strings.TrimSpace(strings.ToLower(val))

	if val == "" || val == "off" || val == "0" {
		return 0, nil
	}

	d, err := time.ParseDuration(val)
	if err != nil {
		return 0, fmt.Errorf("invalid dolt.backup.interval=%q: %w", val, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("invalid dolt.backup.interval=%q: must be positive", val)
	}
	return d, nil
}
