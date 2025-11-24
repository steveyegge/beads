package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/steveyegge/beads/internal/storage"
)

// Tip represents a contextual hint that can be shown to users after successful commands
type Tip struct {
	ID          string
	Condition   func() bool   // Should this tip be eligible?
	Message     string        // The tip message to display
	Frequency   time.Duration // Minimum gap between showings
	Priority    int           // Higher = shown first when eligible
	Probability float64       // 0.0 to 1.0 - chance of showing when eligible
}

var (
	// tips is the registry of all available tips
	tips []Tip

	// tipsMutex protects the tips registry for thread-safe access
	tipsMutex sync.RWMutex

	// tipRand is the random number generator for probability rolls
	// Can be seeded deterministically via BEADS_TIP_SEED for testing
	tipRand *rand.Rand

	// tipRandOnce ensures we only initialize the RNG once
	tipRandOnce sync.Once
)

// initTipRand initializes the random number generator for tip selection
// Uses BEADS_TIP_SEED env var for deterministic testing if set
func initTipRand() {
	tipRandOnce.Do(func() {
		seed := time.Now().UnixNano()
		if seedStr := os.Getenv("BEADS_TIP_SEED"); seedStr != "" {
			if parsedSeed, err := strconv.ParseInt(seedStr, 10, 64); err == nil {
				seed = parsedSeed
			}
		}
		// Use deprecated rand.NewSource for Go 1.19 compatibility
		// nolint:gosec,staticcheck // G404: deterministic seed via env var is intentional for testing
		tipRand = rand.New(rand.NewSource(seed))
	})
}

// maybeShowTip selects and displays an eligible tip based on priority and probability
// Respects --json and --quiet flags
func maybeShowTip(store storage.Storage) {
	// Skip tips in JSON output mode or quiet mode
	if jsonOutput || quietFlag {
		return
	}

	// Initialize RNG if needed
	initTipRand()

	// Select next tip
	tip := selectNextTip(store)
	if tip == nil {
		return
	}

	// Display tip to stdout (informational, not an error)
	fmt.Fprintf(os.Stdout, "\n💡 Tip: %s\n", tip.Message)

	// Record that we showed this tip
	recordTipShown(store, tip.ID)
}

// selectNextTip finds the next tip to show based on conditions, frequency, priority, and probability
// Returns nil if no tip should be shown
func selectNextTip(store storage.Storage) *Tip {
	if store == nil {
		return nil
	}

	now := time.Now()
	var eligibleTips []Tip

	// Lock for reading the tip registry
	tipsMutex.RLock()
	defer tipsMutex.RUnlock()

	// Filter to eligible tips (condition + frequency check)
	for _, tip := range tips {
		// Check if tip's condition is met
		if !tip.Condition() {
			continue
		}

		// Check if enough time has passed since last showing
		lastShown := getLastShown(store, tip.ID)
		if !lastShown.IsZero() && now.Sub(lastShown) < tip.Frequency {
			continue
		}

		eligibleTips = append(eligibleTips, tip)
	}

	if len(eligibleTips) == 0 {
		return nil
	}

	// Sort by priority (highest first)
	sort.Slice(eligibleTips, func(i, j int) bool {
		return eligibleTips[i].Priority > eligibleTips[j].Priority
	})

	// Apply probability roll (in priority order)
	// Higher priority tips get first chance to show
	for i := range eligibleTips {
		if tipRand.Float64() < eligibleTips[i].Probability {
			return &eligibleTips[i]
		}
	}

	return nil // No tips won probability roll
}

// getLastShown retrieves the timestamp when a tip was last shown
// Returns zero time if never shown
func getLastShown(store storage.Storage, tipID string) time.Time {
	key := fmt.Sprintf("tip_%s_last_shown", tipID)
	value, err := store.GetMetadata(context.Background(), key)
	if err != nil || value == "" {
		return time.Time{}
	}

	// Parse RFC3339 timestamp
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}

	return t
}

// recordTipShown records the timestamp when a tip was shown
func recordTipShown(store storage.Storage, tipID string) {
	key := fmt.Sprintf("tip_%s_last_shown", tipID)
	value := time.Now().Format(time.RFC3339)
	_ = store.SetMetadata(context.Background(), key, value)
}
