package doctor

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/steveyegge/beads/internal/labelmutex"
	"github.com/steveyegge/beads/internal/storage/factory"
)

// canonicalGroup is a normalized representation of a mutex group for comparison.
type canonicalGroup struct {
	Name     string
	Labels   []string // sorted
	Required bool
}

// canonicalizeYAMLGroups converts parsed YAML MutexGroups into canonical form.
func canonicalizeYAMLGroups(groups []labelmutex.MutexGroup) []canonicalGroup {
	result := make([]canonicalGroup, len(groups))
	for i, g := range groups {
		labels := make([]string, len(g.Labels))
		copy(labels, g.Labels)
		sort.Strings(labels)
		result[i] = canonicalGroup{
			Name:     g.Name,
			Labels:   labels,
			Required: g.Required,
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return strings.Join(result[i].Labels, ",") < strings.Join(result[j].Labels, ",")
	})
	return result
}

// readDBGroups reads label_mutex_groups and label_mutex_members from the
// underlying database and returns them in canonical form.
func readDBGroups(db *sql.DB) ([]canonicalGroup, error) {
	// Check if tables exist (migration may not have run yet).
	var tableCount int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'label_mutex_groups'
	`).Scan(&tableCount)
	if err != nil || tableCount == 0 {
		// Try information_schema for Dolt/MySQL compatibility.
		err2 := db.QueryRow(`
			SELECT COUNT(*) FROM information_schema.tables
			WHERE table_schema = DATABASE() AND table_name = 'label_mutex_groups'
		`).Scan(&tableCount)
		if err2 != nil || tableCount == 0 {
			return nil, nil
		}
	}

	rows, err := db.Query(`
		SELECT g.id, g.name, g.required
		FROM label_mutex_groups g
		ORDER BY g.id
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query label_mutex_groups: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type dbGroup struct {
		id       int64
		name     string
		required bool
	}
	var dbGroups []dbGroup
	for rows.Next() {
		var g dbGroup
		if err := rows.Scan(&g.id, &g.name, &g.required); err != nil {
			return nil, fmt.Errorf("failed to scan group: %w", err)
		}
		dbGroups = append(dbGroups, g)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating groups: %w", err)
	}

	if len(dbGroups) == 0 {
		return nil, nil
	}

	// Fetch members for all groups.
	memberRows, err := db.Query(`
		SELECT group_id, label
		FROM label_mutex_members
		ORDER BY group_id, label
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query label_mutex_members: %w", err)
	}
	defer func() { _ = memberRows.Close() }()

	membersByGroup := make(map[int64][]string)
	for memberRows.Next() {
		var groupID int64
		var label string
		if err := memberRows.Scan(&groupID, &label); err != nil {
			return nil, fmt.Errorf("failed to scan member: %w", err)
		}
		membersByGroup[groupID] = append(membersByGroup[groupID], label)
	}
	if err := memberRows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating members: %w", err)
	}

	result := make([]canonicalGroup, 0, len(dbGroups))
	for _, g := range dbGroups {
		labels := membersByGroup[g.id]
		sort.Strings(labels)
		result = append(result, canonicalGroup{
			Name:     g.name,
			Labels:   labels,
			Required: g.required,
		})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Name != result[j].Name {
			return result[i].Name < result[j].Name
		}
		return strings.Join(result[i].Labels, ",") < strings.Join(result[j].Labels, ",")
	})
	return result, nil
}

// groupsEqual returns true if two canonical group slices are equal.
func groupsEqual(a, b []canonicalGroup) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Required != b[i].Required {
			return false
		}
		if len(a[i].Labels) != len(b[i].Labels) {
			return false
		}
		for j := range a[i].Labels {
			if a[i].Labels[j] != b[i].Labels[j] {
				return false
			}
		}
	}
	return true
}

// CheckLabelMutexPolicy checks whether the DB policy tables match the YAML config.
// Name must be "Label Mutex Policy" to match the fix dispatcher.
func CheckLabelMutexPolicy(path string) DoctorCheck {
	beadsDir := resolveBeadsDir(filepath.Join(path, ".beads"))

	configPath := filepath.Join(beadsDir, "config.yaml")
	yamlGroups, err := labelmutex.ParseMutexGroups(configPath)
	if err != nil {
		return DoctorCheck{
			Name:     "Label Mutex Policy",
			Status:   StatusWarning,
			Message:  "Invalid label mutex config",
			Detail:   err.Error(),
			Fix:      "Fix the validation.labels.mutex section in .beads/config.yaml",
			Category: CategoryData,
		}
	}

	// Open store read-only.
	ctx := context.Background()
	store, err := factory.NewFromConfigWithOptions(ctx, beadsDir, factory.Options{ReadOnly: true})
	if err != nil {
		// No database — if YAML has groups, that's a drift.
		if len(yamlGroups) > 0 {
			return DoctorCheck{
				Name:     "Label Mutex Policy",
				Status:   StatusWarning,
				Message:  fmt.Sprintf("YAML defines %d mutex group(s) but no database available", len(yamlGroups)),
				Category: CategoryData,
			}
		}
		return DoctorCheck{
			Name:     "Label Mutex Policy",
			Status:   StatusOK,
			Message:  "No label mutex policy configured",
			Category: CategoryData,
		}
	}
	defer func() { _ = store.Close() }()

	db := store.UnderlyingDB()
	if db == nil {
		if len(yamlGroups) > 0 {
			return DoctorCheck{
				Name:     "Label Mutex Policy",
				Status:   StatusWarning,
				Message:  "YAML defines mutex groups but backend has no SQL database",
				Category: CategoryData,
			}
		}
		return DoctorCheck{
			Name:     "Label Mutex Policy",
			Status:   StatusOK,
			Message:  "No label mutex policy configured",
			Category: CategoryData,
		}
	}

	dbGroups, err := readDBGroups(db)
	if err != nil {
		return DoctorCheck{
			Name:     "Label Mutex Policy",
			Status:   StatusWarning,
			Message:  "Unable to read DB policy tables",
			Detail:   err.Error(),
			Category: CategoryData,
		}
	}

	yamlCanonical := canonicalizeYAMLGroups(yamlGroups)

	// Both empty → OK, nothing configured.
	if len(yamlCanonical) == 0 && len(dbGroups) == 0 {
		return DoctorCheck{
			Name:     "Label Mutex Policy",
			Status:   StatusOK,
			Message:  "No label mutex policy configured",
			Category: CategoryData,
		}
	}

	if groupsEqual(yamlCanonical, dbGroups) {
		return DoctorCheck{
			Name:     "Label Mutex Policy",
			Status:   StatusOK,
			Message:  fmt.Sprintf("DB policy matches YAML (%d group(s))", len(yamlCanonical)),
			Category: CategoryData,
		}
	}

	// Mismatch — report drift.
	detail := fmt.Sprintf("YAML defines %d group(s), DB has %d group(s)", len(yamlCanonical), len(dbGroups))
	return DoctorCheck{
		Name:     "Label Mutex Policy",
		Status:   StatusWarning,
		Message:  "Label mutex policy drift (YAML != DB)",
		Detail:   detail,
		Fix:      "Run 'bd doctor --fix' to apply label mutex policy to database",
		Category: CategoryData,
	}
}
