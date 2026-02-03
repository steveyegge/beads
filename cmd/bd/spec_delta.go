package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/ui"
)

type specDeltaCache struct {
	GeneratedAt string             `json:"generated_at"`
	Specs       []spec.SpecSnapshot `json:"specs"`
}

type specDeltaResult struct {
	Since  string          `json:"since,omitempty"`
	Delta  spec.DeltaResult `json:"delta"`
}

var specDeltaCmd = &cobra.Command{
	Use:   "delta",
	Short: "Show spec changes since the last scan",
	Run: func(cmd *cobra.Command, _ []string) {
		if daemonClient != nil {
			FatalErrorRespectJSON("spec delta requires direct access (run with --no-daemon)")
		}

		if err := ensureDatabaseFresh(rootCtx); err != nil {
			FatalErrorRespectJSON("%v", err)
		}
		store, err := getSpecRegistryStore()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		entries, err := store.ListSpecRegistry(rootCtx)
		if err != nil {
			FatalErrorRespectJSON("list spec registry: %v", err)
		}

		cachePath, err := specDeltaCachePath()
		if err != nil {
			FatalErrorRespectJSON("%v", err)
		}

		prevCache, _ := loadSpecDeltaCache(cachePath)
		current := make([]spec.SpecSnapshot, 0, len(entries))
		for _, entry := range entries {
			current = append(current, spec.SpecSnapshot{
				SpecID:    entry.SpecID,
				Title:     entry.Title,
				Lifecycle: entry.Lifecycle,
				SHA256:    entry.SHA256,
				Mtime:     entry.Mtime,
			})
		}

		delta := spec.DeltaResult{}
		since := ""
		if prevCache != nil {
			delta = spec.ComputeDelta(prevCache.Specs, current)
			since = prevCache.GeneratedAt
		} else {
			delta = spec.ComputeDelta(nil, current)
		}

		result := specDeltaResult{
			Since: since,
			Delta: delta,
		}

		if err := writeSpecDeltaCache(cachePath, current); err != nil {
			FatalErrorRespectJSON("write cache: %v", err)
		}

		if jsonOutput {
			outputJSON(result)
			return
		}

		if since == "" {
			fmt.Printf("Delta Since Last Scan: (no prior cache)\n")
		} else {
			fmt.Printf("Delta Since Last Scan (%s)\n", since)
		}
		fmt.Println("────────────────────────────────────────")

		renderDeltaSection("+ Added", delta.Added)
		renderDeltaSection("- Removed", delta.Removed)
		renderDeltaChanges(delta.Changed)

		if len(delta.Added) == 0 && len(delta.Removed) == 0 && len(delta.Changed) == 0 {
			fmt.Printf("%s No changes detected.\n", ui.RenderInfoIcon())
		}
	},
}

func init() {
	specCmd.AddCommand(specDeltaCmd)
}

func specDeltaCachePath() (string, error) {
	beadsDir := beads.FindBeadsDir()
	if beadsDir == "" {
		return "", fmt.Errorf("no .beads directory found")
	}
	return filepath.Join(beadsDir, "spec_scan_cache.json"), nil
}

func loadSpecDeltaCache(path string) (*specDeltaCache, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var cache specDeltaCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, err
	}
	return &cache, nil
}

func writeSpecDeltaCache(path string, specs []spec.SpecSnapshot) error {
	cache := specDeltaCache{
		GeneratedAt: time.Now().UTC().Truncate(time.Second).Format(time.RFC3339),
		Specs:       specs,
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func renderDeltaSection(title string, entries []spec.SpecSnapshot) {
	if len(entries) == 0 {
		return
	}
	fmt.Printf("%s (%d):\n", title, len(entries))
	for _, entry := range entries {
		fmt.Printf("  %s\n", entry.SpecID)
	}
	fmt.Println()
}

func renderDeltaChanges(changes []spec.SpecChange) {
	if len(changes) == 0 {
		return
	}
	fmt.Printf("~ Changed (%d):\n", len(changes))
	for _, change := range changes {
		fmt.Printf("  %s (%s: %s → %s)\n", change.SpecID, change.Field, change.OldValue, change.NewValue)
	}
	fmt.Println()
}
