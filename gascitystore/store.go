//go:build cgo

// Package gascitystore exposes the small Beads storage surface Gas City needs
// without making Gas City import Beads internal packages.
package gascitystore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/storage/doltlite"
	"github.com/steveyegge/beads/internal/types"
)

type Store struct {
	inner storage.DoltStorage
	actor string
}

type Issue struct {
	ID           string
	Title        string
	Status       string
	IssueType    string
	Priority     *int
	CreatedAt    time.Time
	Assignee     string
	ParentID     string
	Description  string
	Labels       []string
	Metadata     map[string]string
	Dependencies []Dependency
}

type Dependency struct {
	IssueID     string
	DependsOnID string
	Type        string
}

type Update struct {
	Title        *string
	Status       *string
	IssueType    *string
	Priority     *int
	Description  *string
	ParentID     *string
	Assignee     *string
	Labels       []string
	RemoveLabels []string
	Metadata     map[string]string
}

func Open(ctx context.Context, workspaceDir string, actor string) (*Store, error) {
	beadsDir := filepath.Join(workspaceDir, ".beads")
	cfg, err := configfile.Load(beadsDir)
	if err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.IsDoltliteBackend() {
		return nil, fmt.Errorf("not a doltlite beads store")
	}
	db := strings.TrimSpace(cfg.GetDoltDatabase())
	if db == "" {
		db = configfile.DefaultDoltDatabase
	}
	inner, err := doltlite.New(ctx, beadsDir, db, "main")
	if err != nil {
		return nil, err
	}
	if actor == "" {
		actor = "gascity"
	}
	return &Store{inner: inner, actor: actor}, nil
}

func IsDoltliteWorkspace(workspaceDir string) bool {
	data, err := os.ReadFile(filepath.Join(workspaceDir, ".beads", "metadata.json"))
	if err != nil {
		return false
	}
	var meta struct {
		Backend  string `json:"backend"`
		Database string `json:"database"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return false
	}
	return strings.TrimSpace(meta.Backend) == "doltlite" || strings.TrimSpace(meta.Database) == "doltlite"
}

func (s *Store) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

func (s *Store) Create(ctx context.Context, issue Issue) (Issue, error) {
	raw := toInternal(issue)
	if raw.Status == "" {
		raw.Status = types.StatusOpen
	}
	if raw.IssueType == "" {
		raw.IssueType = types.TypeTask
	}
	if issue.Priority == nil {
		raw.Priority = 2
	}
	if err := s.inner.CreateIssue(ctx, raw, s.actor); err != nil {
		return Issue{}, err
	}
	if issue.ParentID != "" {
		if err := s.inner.AddDependency(ctx, &types.Dependency{
			IssueID:     raw.ID,
			DependsOnID: issue.ParentID,
			Type:        types.DepParentChild,
		}, s.actor); err != nil {
			return Issue{}, err
		}
	}
	for _, dep := range issue.Dependencies {
		if strings.TrimSpace(dep.DependsOnID) == "" {
			continue
		}
		depType := dep.Type
		if depType == "" {
			depType = string(types.DepBlocks)
		}
		if err := s.inner.AddDependency(ctx, &types.Dependency{
			IssueID:     raw.ID,
			DependsOnID: dep.DependsOnID,
			Type:        types.DependencyType(depType),
		}, s.actor); err != nil {
			return Issue{}, err
		}
	}
	return s.Get(ctx, raw.ID)
}

func (s *Store) Get(ctx context.Context, id string) (Issue, error) {
	issue, err := s.inner.GetIssue(ctx, id)
	if err != nil {
		return Issue{}, err
	}
	return fromInternal(ctx, s.inner, issue), nil
}

func (s *Store) Update(ctx context.Context, id string, update Update) error {
	updates := make(map[string]interface{})
	if update.Title != nil {
		updates["title"] = *update.Title
	}
	if update.Status != nil {
		updates["status"] = *update.Status
	}
	if update.IssueType != nil {
		updates["issue_type"] = *update.IssueType
	}
	if update.Priority != nil {
		updates["priority"] = *update.Priority
	}
	if update.Description != nil {
		updates["description"] = *update.Description
	}
	if update.Assignee != nil {
		updates["assignee"] = *update.Assignee
	}
	if len(update.Metadata) > 0 {
		current, err := s.inner.GetIssue(ctx, id)
		if err != nil {
			return err
		}
		meta := rawMetadataToMap(current.Metadata)
		for k, v := range update.Metadata {
			meta[k] = v
		}
		raw, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		updates["metadata"] = json.RawMessage(raw)
	}
	if len(updates) > 0 {
		if err := s.inner.UpdateIssue(ctx, id, updates, s.actor); err != nil {
			return err
		}
	}
	for _, label := range update.Labels {
		if err := s.inner.AddLabel(ctx, id, label, s.actor); err != nil {
			return err
		}
	}
	for _, label := range update.RemoveLabels {
		if err := s.inner.RemoveLabel(ctx, id, label, s.actor); err != nil {
			return err
		}
	}
	if update.ParentID != nil {
		return replaceParent(ctx, s.inner, id, *update.ParentID, s.actor)
	}
	return nil
}

func (s *Store) CloseIssue(ctx context.Context, id string) error {
	return s.inner.CloseIssue(ctx, id, "", s.actor, "")
}

func (s *Store) Reopen(ctx context.Context, id string) error {
	return s.inner.ReopenIssue(ctx, id, "", s.actor)
}

func (s *Store) Delete(ctx context.Context, id string) error {
	return s.inner.DeleteIssue(ctx, id)
}

func (s *Store) AddDependency(ctx context.Context, issueID, dependsOnID, depType string) error {
	if depType == "" {
		depType = string(types.DepBlocks)
	}
	return s.inner.AddDependency(ctx, &types.Dependency{
		IssueID:     issueID,
		DependsOnID: dependsOnID,
		Type:        types.DependencyType(depType),
	}, s.actor)
}

func (s *Store) RemoveDependency(ctx context.Context, issueID, dependsOnID string) error {
	return s.inner.RemoveDependency(ctx, issueID, dependsOnID, s.actor)
}

func replaceParent(ctx context.Context, store storage.DoltStorage, id, parentID, actor string) error {
	if depsStore, ok := store.(interface {
		GetDependencyRecords(context.Context, string) ([]*types.Dependency, error)
	}); ok {
		deps, err := depsStore.GetDependencyRecords(ctx, id)
		if err != nil {
			return err
		}
		for _, dep := range deps {
			if dep.Type == types.DepParentChild {
				if err := store.RemoveDependency(ctx, id, dep.DependsOnID, actor); err != nil {
					return err
				}
			}
		}
	}
	if parentID == "" {
		return nil
	}
	return store.AddDependency(ctx, &types.Dependency{
		IssueID:     id,
		DependsOnID: parentID,
		Type:        types.DepParentChild,
	}, actor)
}

func toInternal(issue Issue) *types.Issue {
	priority := 0
	if issue.Priority != nil {
		priority = *issue.Priority
	}
	rawMeta, _ := json.Marshal(issue.Metadata)
	return &types.Issue{
		ID:          issue.ID,
		Title:       issue.Title,
		Status:      types.Status(issue.Status),
		IssueType:   types.IssueType(issue.IssueType),
		Priority:    priority,
		CreatedAt:   issue.CreatedAt,
		Assignee:    issue.Assignee,
		Description: issue.Description,
		Labels:      append([]string(nil), issue.Labels...),
		Metadata:    json.RawMessage(rawMeta),
	}
}

func fromInternal(ctx context.Context, store storage.DoltStorage, issue *types.Issue) Issue {
	if issue == nil {
		return Issue{}
	}
	priority := issue.Priority
	out := Issue{
		ID:          issue.ID,
		Title:       issue.Title,
		Status:      string(issue.Status),
		IssueType:   string(issue.IssueType),
		Priority:    &priority,
		CreatedAt:   issue.CreatedAt,
		Assignee:    issue.Assignee,
		Description: issue.Description,
		Labels:      append([]string(nil), issue.Labels...),
		Metadata:    rawMetadataToMap(issue.Metadata),
	}
	if deps, err := store.GetDependenciesWithMetadata(ctx, issue.ID); err == nil {
		for _, dep := range deps {
			d := Dependency{IssueID: issue.ID, DependsOnID: dep.ID, Type: string(dep.DependencyType)}
			out.Dependencies = append(out.Dependencies, d)
			if d.Type == string(types.DepParentChild) && out.ParentID == "" {
				out.ParentID = d.DependsOnID
			}
		}
	}
	return out
}

func rawMetadataToMap(raw json.RawMessage) map[string]string {
	result := map[string]string{}
	if len(raw) == 0 {
		return result
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(raw, &values); err != nil {
		return result
	}
	for k, v := range values {
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			result[k] = s
			continue
		}
		result[k] = strings.TrimSpace(string(v))
	}
	return result
}

func IsNotFound(err error) bool {
	return errors.Is(err, storage.ErrNotFound)
}
