package spec

import "time"

type SpecSnapshot struct {
	SpecID    string    `json:"spec_id"`
	Title     string    `json:"title"`
	Lifecycle string    `json:"lifecycle"`
	SHA256    string    `json:"sha256"`
	Mtime     time.Time `json:"mtime"`
}

type SpecChange struct {
	SpecID   string `json:"spec_id"`
	Field    string `json:"field"`
	OldValue string `json:"old_value"`
	NewValue string `json:"new_value"`
}

type DeltaResult struct {
	Added   []SpecSnapshot `json:"added"`
	Removed []SpecSnapshot `json:"removed"`
	Changed []SpecChange   `json:"changed"`
}

func ComputeDelta(previous, current []SpecSnapshot) DeltaResult {
	result := DeltaResult{
		Added:   []SpecSnapshot{},
		Removed: []SpecSnapshot{},
		Changed: []SpecChange{},
	}

	prevByID := make(map[string]SpecSnapshot, len(previous))
	for _, entry := range previous {
		prevByID[entry.SpecID] = entry
	}

	currByID := make(map[string]SpecSnapshot, len(current))
	for _, entry := range current {
		currByID[entry.SpecID] = entry
	}

	for id, curr := range currByID {
		prev, ok := prevByID[id]
		if !ok {
			result.Added = append(result.Added, curr)
			continue
		}
		if prev.Title != curr.Title {
			result.Changed = append(result.Changed, SpecChange{
				SpecID:   id,
				Field:    "title",
				OldValue: prev.Title,
				NewValue: curr.Title,
			})
		}
		if prev.Lifecycle != curr.Lifecycle {
			result.Changed = append(result.Changed, SpecChange{
				SpecID:   id,
				Field:    "lifecycle",
				OldValue: prev.Lifecycle,
				NewValue: curr.Lifecycle,
			})
		}
		if prev.SHA256 != curr.SHA256 {
			result.Changed = append(result.Changed, SpecChange{
				SpecID:   id,
				Field:    "sha256",
				OldValue: prev.SHA256,
				NewValue: curr.SHA256,
			})
		}
		if !prev.Mtime.Equal(curr.Mtime) {
			result.Changed = append(result.Changed, SpecChange{
				SpecID:   id,
				Field:    "mtime",
				OldValue: prev.Mtime.Format(time.RFC3339),
				NewValue: curr.Mtime.Format(time.RFC3339),
			})
		}
	}

	for id, prev := range prevByID {
		if _, ok := currByID[id]; !ok {
			result.Removed = append(result.Removed, prev)
		}
	}

	return result
}
