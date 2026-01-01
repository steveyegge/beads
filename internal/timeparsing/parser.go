// Package timeparsing provides layered time parsing for relative date/time expressions.
//
// The parsing follows a layered architecture (ADR-001):
//  1. Compact duration (+6h, -1d, +2w)
//  2. Natural language (tomorrow, next monday) - Phase 3
//  3. Absolute timestamp (RFC3339, date-only) - Phase 3
package timeparsing

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// compactDurationRe matches compact duration patterns: [+-]?(\d+)([hdwmy])
// Examples: +6h, -1d, +2w, 3m, 1y
var compactDurationRe = regexp.MustCompile(`^([+-]?)(\d+)([hdwmy])$`)

// ParseCompactDuration parses compact duration syntax and returns the resulting time.
//
// Format: [+-]?(\d+)([hdwmy])
//
// Units:
//   - h = hours
//   - d = days
//   - w = weeks
//   - m = months
//   - y = years
//
// Examples:
//   - "+6h" -> now + 6 hours
//   - "-1d" -> now - 1 day
//   - "+2w" -> now + 2 weeks
//   - "3m"  -> now + 3 months (no sign = positive)
//   - "1y"  -> now + 1 year
//
// Returns error if input doesn't match the compact duration pattern.
func ParseCompactDuration(s string, now time.Time) (time.Time, error) {
	matches := compactDurationRe.FindStringSubmatch(s)
	if matches == nil {
		return time.Time{}, fmt.Errorf("not a compact duration: %q", s)
	}

	sign := matches[1]
	amountStr := matches[2]
	unit := matches[3]

	amount, err := strconv.Atoi(amountStr)
	if err != nil {
		// Should not happen given regex ensures digits, but handle gracefully
		return time.Time{}, fmt.Errorf("invalid duration amount: %q", amountStr)
	}

	// Apply sign (default positive)
	if sign == "-" {
		amount = -amount
	}

	return applyDuration(now, amount, unit), nil
}

// applyDuration applies the given amount and unit to the base time.
func applyDuration(base time.Time, amount int, unit string) time.Time {
	switch unit {
	case "h":
		return base.Add(time.Duration(amount) * time.Hour)
	case "d":
		return base.AddDate(0, 0, amount)
	case "w":
		return base.AddDate(0, 0, amount*7)
	case "m":
		return base.AddDate(0, amount, 0)
	case "y":
		return base.AddDate(amount, 0, 0)
	default:
		// Should not happen given regex, but return base unchanged
		return base
	}
}

// IsCompactDuration returns true if the string matches compact duration syntax.
func IsCompactDuration(s string) bool {
	return compactDurationRe.MatchString(s)
}
