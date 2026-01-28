package spec

import "time"

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
