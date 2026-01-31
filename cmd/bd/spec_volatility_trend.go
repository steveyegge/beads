package main

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/ui"
)

type trendBucket struct {
	Label   string `json:"label"`
	Changes int    `json:"changes"`
}

type specTrendResult struct {
	SpecID     string        `json:"spec_id"`
	Buckets    []trendBucket `json:"buckets"`
	Slope      float64       `json:"slope"`
	Direction  string        `json:"direction"`
	Prediction string        `json:"prediction,omitempty"`
}

func renderSpecTrend(ctx context.Context, specID string) error {
	_, specStore, cleanup, err := openVolatilityStores(ctx)
	if err != nil {
		return err
	}
	defer cleanup()

	if daemonClient == nil {
		if err := ensureDatabaseFresh(ctx); err != nil {
			return err
		}
	}

	entry, err := specStore.GetSpecRegistry(ctx, specID)
	if err != nil {
		return err
	}
	if entry == nil || entry.MissingAt != nil {
		return fmt.Errorf("spec not found: %s", specID)
	}

	now := time.Now().UTC()
	start := startOfWeek(now.AddDate(0, 0, -21))
	events, err := specStore.ListSpecScanEvents(ctx, specID, start)
	if err != nil {
		return err
	}

	buckets := make([]trendBucket, 0, 4)
	for i := 0; i < 4; i++ {
		weekStart := start.AddDate(0, 0, 7*i)
		weekEnd := weekStart.AddDate(0, 0, 7)
		count := 0
		for _, ev := range events {
			if ev.Changed && (ev.ScannedAt.Equal(weekStart) || (ev.ScannedAt.After(weekStart) && ev.ScannedAt.Before(weekEnd))) {
				count++
			}
		}
		label := fmt.Sprintf("Week %d", i+1)
		buckets = append(buckets, trendBucket{Label: label, Changes: count})
	}

	slope := linearSlope(buckets)
	direction := trendDirection(slope)
	prediction := ""
	lastCount := buckets[len(buckets)-1].Changes
	if slope < -0.2 && lastCount > 0 {
		weeksToZero := math.Ceil(float64(lastCount) / math.Abs(slope))
		prediction = fmt.Sprintf("Safe to resume work in ~%.0f days", weeksToZero*7)
	}

	if jsonOutput {
		outputJSON(specTrendResult{
			SpecID:     specID,
			Buckets:    buckets,
			Slope:      slope,
			Direction:  direction,
			Prediction: prediction,
		})
		return nil
	}

	fmt.Printf("VOLATILITY TREND (%s):\n\n", specID)
	maxCount := 0
	for _, b := range buckets {
		if b.Changes > maxCount {
			maxCount = b.Changes
		}
	}
	for _, b := range buckets {
		bar := trendBar(b.Changes, maxCount)
		fmt.Printf("  %s: %s  %d changes\n", b.Label, bar, b.Changes)
	}
	fmt.Printf("\nStatus: %s\n", direction)
	if prediction != "" {
		fmt.Printf("Prediction: %s\n", prediction)
	}
	return nil
}

func startOfWeek(t time.Time) time.Time {
	weekday := int(t.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	dayStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	return dayStart.AddDate(0, 0, -weekday+1)
}

func trendBar(count, max int) string {
	if max == 0 {
		return "░░░░░░░░░░"
	}
	width := 10
	filled := int(math.Round(float64(count) / float64(max) * float64(width)))
	if filled == 0 && count > 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func linearSlope(buckets []trendBucket) float64 {
	n := float64(len(buckets))
	if n == 0 {
		return 0
	}
	var sumX, sumY, sumXY, sumXX float64
	for i, b := range buckets {
		x := float64(i)
		y := float64(b.Changes)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}

func trendDirection(slope float64) string {
	switch {
	case slope > 0.3:
		return ui.RenderWarn("INCREASING")
	case slope < -0.3:
		return ui.RenderPass("DECREASING")
	default:
		return ui.RenderMuted("STABLE")
	}
}
