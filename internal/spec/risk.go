package spec

import (
	"math"
	"time"
)

// SummarizeScanEvents counts changed events within a window and returns the last change time.
func SummarizeScanEvents(events []SpecScanEvent, since time.Time) (int, *time.Time) {
	var count int
	var last time.Time
	for _, event := range events {
		if !since.IsZero() && event.ScannedAt.Before(since) {
			continue
		}
		if !event.Changed {
			continue
		}
		count++
		if event.ScannedAt.After(last) {
			last = event.ScannedAt
		}
	}
	if count == 0 {
		return 0, nil
	}
	return count, &last
}

// SummarizeScanEventsWeighted applies exponential decay to change events.
// halfLife <= 0 disables weighting (returns raw count).
func SummarizeScanEventsWeighted(events []SpecScanEvent, since, now time.Time, halfLife time.Duration) (float64, *time.Time) {
	if halfLife <= 0 {
		count, last := SummarizeScanEvents(events, since)
		return float64(count), last
	}

	var (
		weighted float64
		last     time.Time
	)
	ln2 := math.Ln2
	for _, event := range events {
		if !since.IsZero() && event.ScannedAt.Before(since) {
			continue
		}
		if !event.Changed {
			continue
		}
		age := now.Sub(event.ScannedAt)
		if age < 0 {
			age = 0
		}
		weight := math.Exp(-ln2 * float64(age) / float64(halfLife))
		weighted += weight
		if event.ScannedAt.After(last) {
			last = event.ScannedAt
		}
	}
	if weighted == 0 {
		return 0, nil
	}
	return weighted, &last
}
