package formula

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// CloneSubgraph creates new issues from the template with variable substitution.
// Uses CloneOptions to control all spawn/bond behavior including dynamic bonding.
func CloneSubgraph(ctx context.Context, s storage.Storage, subgraph *TemplateSubgraph, opts CloneOptions) (*InstantiateResult, error) {
	if s == nil {
		return nil, fmt.Errorf("no database connection")
	}

	// Generate new IDs and create mapping
	idMapping := make(map[string]string)

	// Use transaction for atomicity
	err := s.RunInTransaction(ctx, func(tx storage.Transaction) error {
		// First pass: create all issues with new IDs
		for _, oldIssue := range subgraph.Issues {
			// Determine assignee: use override for root epic, otherwise keep template's
			issueAssignee := oldIssue.Assignee
			if oldIssue.ID == subgraph.Root.ID && opts.Assignee != "" {
				issueAssignee = opts.Assignee
			}

			// Determine issue type: wisps (ephemeral) get their own type to avoid cluttering epic listings
			issueType := oldIssue.IssueType
			if opts.Ephemeral && oldIssue.IssueType == types.TypeEpic {
				issueType = types.IssueType("wisp")
			}

			newIssue := &types.Issue{
				// ID will be set below based on bonding options
				Title:              SubstituteVariables(oldIssue.Title, opts.Vars),
				Description:        SubstituteVariables(oldIssue.Description, opts.Vars),
				Design:             SubstituteVariables(oldIssue.Design, opts.Vars),
				AcceptanceCriteria: SubstituteVariables(oldIssue.AcceptanceCriteria, opts.Vars),
				Notes:              SubstituteVariables(oldIssue.Notes, opts.Vars),
				Status:             types.StatusOpen, // Always start fresh
				Priority:           oldIssue.Priority,
				IssueType:          issueType,
				Assignee:           issueAssignee,
				EstimatedMinutes:   oldIssue.EstimatedMinutes,
				Ephemeral:          opts.Ephemeral, // mark for cleanup when closed
				IDPrefix:           opts.Prefix,    // distinct prefixes for mols/wisps
				// Gate fields (for async coordination)
				AwaitType: oldIssue.AwaitType,
				AwaitID:   SubstituteVariables(oldIssue.AwaitID, opts.Vars),
				Timeout:   oldIssue.Timeout,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}

			// Generate custom ID for dynamic bonding if ParentID is set
			if opts.ParentID != "" {
				bondedID, err := GenerateBondedID(oldIssue.ID, subgraph.Root.ID, opts)
				if err != nil {
					return fmt.Errorf("failed to generate bonded ID for %s: %w", oldIssue.ID, err)
				}
				newIssue.ID = bondedID
			}

			if err := tx.CreateIssue(ctx, newIssue, opts.Actor); err != nil {
				return fmt.Errorf("failed to create issue from %s: %w", oldIssue.ID, err)
			}

			idMapping[oldIssue.ID] = newIssue.ID
		}

		// Second pass: recreate dependencies with new IDs
		for _, dep := range subgraph.Dependencies {
			newFromID, ok1 := idMapping[dep.IssueID]
			newToID, ok2 := idMapping[dep.DependsOnID]
			if !ok1 || !ok2 {
				continue // Skip if either end is outside the subgraph
			}

			newDep := &types.Dependency{
				IssueID:     newFromID,
				DependsOnID: newToID,
				Type:        dep.Type,
			}
			if err := tx.AddDependency(ctx, newDep, opts.Actor); err != nil {
				return fmt.Errorf("failed to create dependency: %w", err)
			}
		}

		// Third pass: add requires-skill dependencies for all new issues
		// This enables skill-based work routing via bd ready --with-skills
		if len(subgraph.RequiredSkills) > 0 {
			for _, skillID := range subgraph.RequiredSkills {
				// Normalize skill ID (add skill- prefix if needed)
				normalizedSkillID := skillID
				if !strings.HasPrefix(skillID, "skill-") {
					normalizedSkillID = "skill-" + skillID
				}

				// Add requires-skill dependency for each new issue
				for _, newID := range idMapping {
					skillDep := &types.Dependency{
						IssueID:     newID,
						DependsOnID: normalizedSkillID,
						Type:        types.DepRequiresSkill,
					}
					// Ignore errors - skill may not exist yet
					_ = tx.AddDependency(ctx, skillDep, opts.Actor)
				}
			}
		}

		// Fourth pass: add formula->runbook dependency edges (od-dv0.6)
		// Links the root issue to each referenced runbook bead
		if len(subgraph.Runbooks) > 0 {
			rootNewID := idMapping[subgraph.Root.ID]
			for _, rbRef := range subgraph.Runbooks {
				rbDep := &types.Dependency{
					IssueID:     rootNewID,
					DependsOnID: rbRef,
					Type:        types.DepRelated,
					Metadata:    `{"source":"formula-runbook"}`,
				}
				// Ignore errors - runbook bead may not exist in this DB yet
				_ = tx.AddDependency(ctx, rbDep, opts.Actor)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &InstantiateResult{
		NewEpicID: idMapping[subgraph.Root.ID],
		IDMapping: idMapping,
		Created:   len(subgraph.Issues),
	}, nil
}

// SpawnMolecule creates new issues from the proto with variable substitution.
// This instantiates a proto (template) into a molecule (real issues).
// If ephemeral is true, spawned issues are marked for bulk deletion when closed.
// The prefix parameter overrides the default issue prefix.
func SpawnMolecule(ctx context.Context, s storage.Storage, subgraph *TemplateSubgraph, vars map[string]string, assignee string, actorName string, ephemeral bool, prefix string) (*InstantiateResult, error) {
	opts := CloneOptions{
		Vars:      vars,
		Assignee:  assignee,
		Actor:     actorName,
		Ephemeral: ephemeral,
		Prefix:    prefix,
	}
	return CloneSubgraph(ctx, s, subgraph, opts)
}

// SpawnMoleculeWithOptions creates new issues from the proto using CloneOptions.
// This allows full control over dynamic bonding, variable substitution, and wisp phase.
func SpawnMoleculeWithOptions(ctx context.Context, s storage.Storage, subgraph *TemplateSubgraph, opts CloneOptions) (*InstantiateResult, error) {
	return CloneSubgraph(ctx, s, subgraph, opts)
}
