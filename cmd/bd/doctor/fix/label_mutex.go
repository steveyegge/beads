package fix

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/steveyegge/beads/internal/labelmutex"
	"github.com/steveyegge/beads/internal/storage/factory"
	"github.com/steveyegge/beads/internal/types"
)

// LabelMutexPolicy syncs YAML label mutex config to the DB policy tables.
// This activates DB-level enforcement (triggers consult these tables).
//
// Safety: refuses to apply if there are existing conflict violations,
// because enabling enforcement on a dirty DB would cause hard failures on
// subsequent label operations. Missing-required violations are ignored
// (required is soft-only, not enforced by DB triggers).
func LabelMutexPolicy(path string) error {
	if err := validateBeadsWorkspace(path); err != nil {
		return err
	}

	beadsDir := resolveBeadsDir(filepath.Join(path, ".beads"))
	configPath := filepath.Join(beadsDir, "config.yaml")

	// 1. Parse YAML config.
	groups, err := labelmutex.ParseMutexGroups(configPath)
	if err != nil {
		return fmt.Errorf("failed to parse label mutex config: %w", err)
	}

	// 2. Open store read-write.
	ctx := context.Background()
	store, err := factory.NewFromConfig(ctx, beadsDir)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer func() { _ = store.Close() }()

	// 3. Audit for existing conflict violations before applying.
	if len(groups) > 0 {
		issues, err := store.SearchIssues(ctx, "", types.IssueFilter{})
		if err != nil {
			return fmt.Errorf("failed to query issues for audit: %w", err)
		}

		// Filter excluded issues (same logic as the doctor check).
		var filtered []*types.Issue
		for _, issue := range issues {
			if !labelmutex.ShouldExcludeIssue(issue) {
				filtered = append(filtered, issue)
			}
		}

		if len(filtered) > 0 {
			issueIDs := make([]string, len(filtered))
			for i, issue := range filtered {
				issueIDs[i] = issue.ID
			}
			labelsMap, err := store.GetLabelsForIssues(ctx, issueIDs)
			if err != nil {
				return fmt.Errorf("failed to query labels for audit: %w", err)
			}

			violations := labelmutex.FindViolations(filtered, labelsMap, groups)
			// Only block on conflict violations, not missing-required.
			var conflicts int
			var firstIDs []string
			for _, v := range violations {
				if v.Kind == "conflict" {
					conflicts++
					if len(firstIDs) < 3 {
						firstIDs = append(firstIDs, v.IssueID)
					}
				}
			}
			if conflicts > 0 {
				return fmt.Errorf("cannot apply label mutex policy: %d conflict violation(s) exist (e.g. %s); resolve them first",
					conflicts, joinIDs(firstIDs))
			}
		}
	}

	// 4. Replace DB policy tables in a transaction.
	db := store.UnderlyingDB()
	if db == nil {
		return fmt.Errorf("backend does not expose a SQL database")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `DELETE FROM label_mutex_members`)
	if err != nil {
		return fmt.Errorf("failed to clear label_mutex_members: %w", err)
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM label_mutex_groups`)
	if err != nil {
		return fmt.Errorf("failed to clear label_mutex_groups: %w", err)
	}

	for _, g := range groups {
		required := 0
		if g.Required {
			required = 1
		}
		result, err := tx.ExecContext(ctx,
			`INSERT INTO label_mutex_groups (name, required) VALUES (?, ?)`,
			g.Name, required)
		if err != nil {
			return fmt.Errorf("failed to insert group %q: %w", g.Name, err)
		}
		groupID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("failed to get group ID for %q: %w", g.Name, err)
		}
		for _, label := range g.Labels {
			_, err = tx.ExecContext(ctx,
				`INSERT INTO label_mutex_members (group_id, label) VALUES (?, ?)`,
				groupID, label)
			if err != nil {
				return fmt.Errorf("failed to insert member %q in group %q: %w", label, g.Name, err)
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit policy: %w", err)
	}

	if len(groups) == 0 {
		fmt.Println("  Cleared label mutex policy (no groups in YAML)")
	} else {
		fmt.Printf("  Applied %d label mutex group(s) to database\n", len(groups))
	}

	return nil
}

func joinIDs(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	result := ids[0]
	for i := 1; i < len(ids); i++ {
		result += ", " + ids[i]
	}
	return result
}
