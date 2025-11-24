package main

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/memory"
)

func TestTipSelection(t *testing.T) {
	// Set deterministic seed for testing
	os.Setenv("BEADS_TIP_SEED", "12345")
	defer os.Unsetenv("BEADS_TIP_SEED")

	// Reset RNG
	tipRandOnce = sync.Once{}
	initTipRand()

	// Reset tip registry for testing
	tipsMutex.Lock()
	tips = []Tip{}
	tipsMutex.Unlock()

	store := memory.New("")

	// Test 1: No tips registered
	tip := selectNextTip(store)
	if tip != nil {
		t.Errorf("Expected nil with no tips registered, got %v", tip)
	}

	// Test 2: Single tip with condition = true
	tipsMutex.Lock()
	tips = append(tips, Tip{
		ID:          "test_tip_1",
		Condition:   func() bool { return true },
		Message:     "Test tip 1",
		Frequency:   1 * time.Hour,
		Priority:    100,
		Probability: 1.0, // Always show
	})
	tipsMutex.Unlock()

	tip = selectNextTip(store)
	if tip == nil {
		t.Fatal("Expected tip to be selected")
	}
	if tip.ID != "test_tip_1" {
		t.Errorf("Expected tip ID 'test_tip_1', got %q", tip.ID)
	}

	// Test 3: Frequency limit - should not show again immediately
	recordTipShown(store, "test_tip_1")
	tip = selectNextTip(store)
	if tip != nil {
		t.Errorf("Expected nil due to frequency limit, got %v", tip)
	}

	// Test 4: Multiple tips - priority order
	tipsMutex.Lock()
	tips = []Tip{
		{
			ID:          "low_priority",
			Condition:   func() bool { return true },
			Message:     "Low priority tip",
			Frequency:   1 * time.Hour,
			Priority:    10,
			Probability: 1.0,
		},
		{
			ID:          "high_priority",
			Condition:   func() bool { return true },
			Message:     "High priority tip",
			Frequency:   1 * time.Hour,
			Priority:    100,
			Probability: 1.0,
		},
	}
	tipsMutex.Unlock()

	tip = selectNextTip(store)
	if tip == nil {
		t.Fatal("Expected tip to be selected")
	}
	if tip.ID != "high_priority" {
		t.Errorf("Expected high_priority tip to be selected first, got %q", tip.ID)
	}

	// Test 5: Condition = false
	tipsMutex.Lock()
	tips = []Tip{
		{
			ID:          "never_show",
			Condition:   func() bool { return false },
			Message:     "Never shown",
			Frequency:   1 * time.Hour,
			Priority:    100,
			Probability: 1.0,
		},
	}
	tipsMutex.Unlock()

	tip = selectNextTip(store)
	if tip != nil {
		t.Errorf("Expected nil due to condition=false, got %v", tip)
	}
}

func TestTipProbability(t *testing.T) {
	// Set deterministic seed
	os.Setenv("BEADS_TIP_SEED", "99999")
	defer os.Unsetenv("BEADS_TIP_SEED")

	// Reset RNG by creating a new Once
	tipRandOnce = sync.Once{}
	initTipRand()

	tipsMutex.Lock()
	tips = []Tip{
		{
			ID:          "rare_tip",
			Condition:   func() bool { return true },
			Message:     "Rare tip",
			Frequency:   1 * time.Hour,
			Priority:    100,
			Probability: 0.01, // 1% chance
		},
	}
	tipsMutex.Unlock()

	store := memory.New("")

	// Run selection multiple times
	shownCount := 0
	for i := 0; i < 100; i++ {
		// Clear last shown timestamp to make tip eligible
		_ = store.SetMetadata(context.Background(), "tip_rare_tip_last_shown", "")

		tip := selectNextTip(store)
		if tip != nil {
			shownCount++
		}
	}

	// With 1% probability, we expect ~1 show out of 100
	// Allow some variance (0-10 is reasonable for low probability)
	if shownCount > 10 {
		t.Errorf("Expected ~1 tip shown with 1%% probability, got %d", shownCount)
	}
}

func TestGetLastShown(t *testing.T) {
	store := memory.New("")

	// Test 1: Never shown
	lastShown := getLastShown(store, "never_shown")
	if !lastShown.IsZero() {
		t.Errorf("Expected zero time for never shown tip, got %v", lastShown)
	}

	// Test 2: Recently shown
	now := time.Now()
	_ = store.SetMetadata(context.Background(), "tip_test_last_shown", now.Format(time.RFC3339))

	lastShown = getLastShown(store, "test")
	if lastShown.IsZero() {
		t.Error("Expected non-zero time for shown tip")
	}

	// Should be within 1 second (accounting for rounding)
	diff := now.Sub(lastShown)
	if diff < 0 {
		diff = -diff
	}
	if diff > time.Second {
		t.Errorf("Expected last shown time to be close to now, got diff %v", diff)
	}
}

func TestRecordTipShown(t *testing.T) {
	store := memory.New("")

	recordTipShown(store, "test_tip")

	// Verify it was recorded
	lastShown := getLastShown(store, "test_tip")
	if lastShown.IsZero() {
		t.Error("Expected tip to be recorded as shown")
	}

	// Should be very recent
	if time.Since(lastShown) > time.Second {
		t.Errorf("Expected recent timestamp, got %v", lastShown)
	}
}

func TestMaybeShowTip_RespectsFlags(t *testing.T) {
	// Set deterministic seed
	os.Setenv("BEADS_TIP_SEED", "54321")
	defer os.Unsetenv("BEADS_TIP_SEED")

	tipsMutex.Lock()
	tips = []Tip{
		{
			ID:          "always_show",
			Condition:   func() bool { return true },
			Message:     "Always show tip",
			Frequency:   1 * time.Hour,
			Priority:    100,
			Probability: 1.0,
		},
	}
	tipsMutex.Unlock()

	store := memory.New("")

	// Test 1: Should not show in JSON mode
	jsonOutput = true
	maybeShowTip(store) // Should not panic or show output
	jsonOutput = false

	// Test 2: Should not show in quiet mode
	quietFlag = true
	maybeShowTip(store) // Should not panic or show output
	quietFlag = false

	// Test 3: Should show in normal mode (no assertions, just testing it doesn't panic)
	maybeShowTip(store)
}

func TestTipFrequency(t *testing.T) {
	store := memory.New("")

	tipsMutex.Lock()
	tips = []Tip{
		{
			ID:          "frequent_tip",
			Condition:   func() bool { return true },
			Message:     "Frequent tip",
			Frequency:   5 * time.Second,
			Priority:    100,
			Probability: 1.0,
		},
	}
	tipsMutex.Unlock()

	// First selection should work
	tip := selectNextTip(store)
	if tip == nil {
		t.Fatal("Expected tip to be selected")
	}

	// Record it as shown
	recordTipShown(store, tip.ID)

	// Should not show again immediately (within frequency window)
	tip = selectNextTip(store)
	if tip != nil {
		t.Errorf("Expected nil due to frequency limit, got %v", tip)
	}

	// Manually set last shown to past (simulate time passing)
	past := time.Now().Add(-10 * time.Second)
	_ = store.SetMetadata(context.Background(), "tip_frequent_tip_last_shown", past.Format(time.RFC3339))

	// Should show again now
	tip = selectNextTip(store)
	if tip == nil {
		t.Error("Expected tip to be selected after frequency window passed")
	}
}
