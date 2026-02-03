package specarchive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/spec"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// MoveSpecFile moves a spec into specs/archive and returns the new spec_id.
// If the spec is not under specs/ or is already archived, it returns the original spec_id.
func MoveSpecFile(repoRoot, specID string) (string, bool, error) {
	cleanID := strings.TrimSpace(specID)
	if cleanID == "" {
		return specID, false, fmt.Errorf("spec_id required")
	}
	if !spec.IsScannableSpecID(cleanID) {
		return specID, false, nil
	}
	if !strings.HasPrefix(cleanID, "specs/") {
		return specID, false, nil
	}
	if strings.HasPrefix(cleanID, "specs/archive/") {
		return specID, false, nil
	}

	rel := strings.TrimPrefix(cleanID, "specs/")
	newSpecID := filepath.ToSlash(filepath.Join("specs", "archive", filepath.FromSlash(rel)))
	if newSpecID == cleanID {
		return specID, false, nil
	}

	oldPath := filepath.Join(repoRoot, filepath.FromSlash(cleanID))
	newPath := filepath.Join(repoRoot, filepath.FromSlash(newSpecID))
	if _, err := os.Stat(oldPath); err != nil {
		return specID, false, fmt.Errorf("spec file missing: %w", err)
	}
	if _, err := os.Stat(newPath); err == nil {
		return specID, false, fmt.Errorf("archive target exists: %s", newSpecID)
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o750); err != nil {
		return specID, false, fmt.Errorf("create archive dir: %w", err)
	}
	if err := os.Rename(oldPath, newPath); err != nil {
		return specID, false, fmt.Errorf("move spec: %w", err)
	}
	return newSpecID, true, nil
}

// MoveSpecReferences updates spec registry and linked issues to the new spec id.
func MoveSpecReferences(ctx context.Context, store storage.Storage, specStore spec.SpecRegistryStore, fromSpecID, toSpecID string) error {
	if fromSpecID == toSpecID {
		return nil
	}
	if specStore == nil || store == nil {
		return fmt.Errorf("storage not available")
	}
	if err := specStore.MoveSpecRegistry(ctx, fromSpecID, toSpecID, toSpecID); err != nil {
		return err
	}
	issues, err := store.SearchIssues(ctx, "", types.IssueFilter{SpecID: &fromSpecID})
	if err != nil {
		return err
	}
	for _, issue := range issues {
		if issue == nil {
			continue
		}
		updates := map[string]interface{}{"spec_id": toSpecID}
		if err := store.UpdateIssue(ctx, issue.ID, updates, "spec-archive"); err != nil {
			return err
		}
	}
	return nil
}
