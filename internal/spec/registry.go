package spec

import (
	"context"
	"fmt"
	"time"
)

// UpdateRegistry syncs scanned specs into storage and marks changed beads.
func UpdateRegistry(ctx context.Context, store SpecRegistryStore, scanned []ScannedSpec, now time.Time) (SpecScanResult, error) {
	result := SpecScanResult{
		Scanned: len(scanned),
	}

	existing, err := store.ListSpecRegistry(ctx)
	if err != nil {
		return result, fmt.Errorf("list spec registry: %w", err)
	}
	existingByID := make(map[string]SpecRegistryEntry, len(existing))
	for _, spec := range existing {
		existingByID[spec.SpecID] = spec
	}

	scannedIDs := make([]string, 0, len(scanned))
	scannedSet := make(map[string]struct{}, len(scanned))
	upsert := make([]SpecRegistryEntry, 0, len(scanned))
	scanEvents := make([]SpecScanEvent, 0, len(scanned))

	for _, spec := range scanned {
		scannedIDs = append(scannedIDs, spec.SpecID)
		scannedSet[spec.SpecID] = struct{}{}

		if current, ok := existingByID[spec.SpecID]; ok {
			changed := current.SHA256 != spec.SHA256
			if current.SHA256 != spec.SHA256 {
				result.Updated++
				result.ChangedSpecIDs = append(result.ChangedSpecIDs, spec.SpecID)
			} else {
				result.Unchanged++
			}
			scanEvents = append(scanEvents, SpecScanEvent{
				SpecID:    spec.SpecID,
				ScannedAt: now,
				SHA256:    spec.SHA256,
				Changed:   changed,
			})
			upsert = append(upsert, SpecRegistryEntry{
				SpecID:        spec.SpecID,
				Path:          spec.SpecID,
				Title:         spec.Title,
				SHA256:        spec.SHA256,
				Mtime:         spec.Mtime,
				GitStatus:     spec.GitStatus,
				DiscoveredAt:  current.DiscoveredAt,
				LastScannedAt: now,
				MissingAt:     nil,
			})
			continue
		}

		result.Added++
		scanEvents = append(scanEvents, SpecScanEvent{
			SpecID:    spec.SpecID,
			ScannedAt: now,
			SHA256:    spec.SHA256,
			Changed:   false,
		})
		upsert = append(upsert, SpecRegistryEntry{
			SpecID:        spec.SpecID,
			Path:          spec.SpecID,
			Title:         spec.Title,
			SHA256:        spec.SHA256,
			Mtime:         spec.Mtime,
			GitStatus:     spec.GitStatus,
			DiscoveredAt:  now,
			LastScannedAt: now,
			MissingAt:     nil,
		})
	}

	if err := store.UpsertSpecRegistry(ctx, upsert); err != nil {
		return result, fmt.Errorf("upsert spec registry: %w", err)
	}

	if err := store.AddSpecScanEvents(ctx, scanEvents); err != nil {
		return result, fmt.Errorf("record spec scan events: %w", err)
	}

	// Mark missing specs
	missingIDs := make([]string, 0)
	for _, spec := range existing {
		if _, ok := scannedSet[spec.SpecID]; ok {
			continue
		}
		missingIDs = append(missingIDs, spec.SpecID)
	}
	if len(missingIDs) > 0 {
		result.Missing = len(missingIDs)
		if err := store.MarkSpecsMissing(ctx, missingIDs, now); err != nil {
			return result, fmt.Errorf("mark missing specs: %w", err)
		}
	}
	if len(scannedIDs) > 0 {
		if err := store.ClearSpecsMissing(ctx, scannedIDs); err != nil {
			return result, fmt.Errorf("clear missing specs: %w", err)
		}
	}

	if len(result.ChangedSpecIDs) > 0 {
		updated, err := store.MarkSpecChangedBySpecIDs(ctx, result.ChangedSpecIDs, now)
		if err != nil {
			return result, fmt.Errorf("mark changed beads: %w", err)
		}
		result.MarkedBeads = updated
	}

	return result, nil
}

// PurgeMissing removes registry entries for specs whose files are no longer on disk.
func PurgeMissing(ctx context.Context, store SpecRegistryStore) (int, error) {
	entries, err := store.ListSpecRegistry(ctx)
	if err != nil {
		return 0, fmt.Errorf("list for purge: %w", err)
	}
	var missingIDs []string
	for _, e := range entries {
		if e.MissingAt != nil {
			missingIDs = append(missingIDs, e.SpecID)
		}
	}
	if len(missingIDs) == 0 {
		return 0, nil
	}
	return store.DeleteSpecRegistryByIDs(ctx, missingIDs)
}
