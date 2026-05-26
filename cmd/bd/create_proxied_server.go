package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/steveyegge/beads/internal/storage/domain"
	"github.com/steveyegge/beads/internal/storage/uow"
	"github.com/steveyegge/beads/internal/types"
	"github.com/steveyegge/beads/internal/ui"
	"github.com/steveyegge/beads/internal/validation"
)

func runCreateProxiedServer(cmd *cobra.Command, ctx context.Context, in createInput) {
	if in.repoOverrideSet {
		FatalError("--repo is not supported with --proxied-server")
	}
	switch {
	case in.graphFile != "":
		runCreateProxiedGraph(cmd, ctx, in)
	case in.markdownFile != "":
		runCreateProxiedMarkdown(cmd, ctx, in)
	default:
		runCreateProxiedSingle(cmd, ctx, in)
	}
}

func proxiedOpenUOW(ctx context.Context) (uow.UnitOfWork, domain.CreateContext) {
	if uowProvider == nil {
		FatalError("proxied-server UOW provider not initialized")
	}
	uw, err := uowProvider.NewUOW(ctx)
	if err != nil {
		FatalError("open unit of work: %v", err)
	}
	cctx, err := uw.ConfigUseCase().LoadCreateContext(ctx)
	if err != nil {
		uw.Close(ctx)
		FatalError("load create context: %v", err)
	}
	return uw, cctx
}

func runCreateProxiedSingle(_ *cobra.Command, ctx context.Context, in createInput) {
	runCreateLintIssue(in)
	if in.explicitID != "" {
		if _, err := validation.ValidateIDFormat(in.explicitID); err != nil {
			FatalError("%v", err)
		}
	}
	deps, err := parseDepSpecs(in.deps)
	if err != nil {
		FatalError("%v", err)
	}
	waitsFor, err := buildWaitsFor(in.waitsFor, in.waitsForGate)
	if err != nil {
		FatalError("%v", err)
	}

	// Dry-run: render preview from input; no DB.
	if in.dryRun {
		previewIssue := buildCreateIssueFromInput(in)
		if in.jsonOutput {
			outputJSON(previewIssue)
		} else {
			renderCreateDryRunPreview(previewIssue, in.labels, in.deps)
		}
		return
	}

	// --- DB-dependent work ---
	uw, cctx := proxiedOpenUOW(ctx)
	defer uw.Close(ctx)

	if in.issueType != "" {
		it := types.IssueType(in.issueType).Normalize()
		if !it.IsValidWithCustom(cctx.CustomTypes) {
			FatalError("invalid type %q (allowed: built-ins plus configured custom types)", in.issueType)
		}
	}
	if in.explicitID != "" {
		effectivePrefix := overlayYAMLPrefix(cctx.IssuePrefix)
		if err := validation.ValidateIDPrefixAllowed(in.explicitID, effectivePrefix, cctx.AllowedPrefixes, in.force); err != nil {
			FatalError("%v", err)
		}
	}

	issue := buildCreateIssueFromInput(in)
	params := domain.CreateIssueParams{
		Issue:                   issue,
		ExplicitID:              in.explicitID,
		ParentID:                in.parentID,
		Labels:                  in.labels,
		InheritLabelsFromParent: !in.noInheritLabels && in.parentID != "",
		Dependencies:            deps,
		WaitsFor:                waitsFor,
		DiscoveredFromParent:    discoveredFromParent(in.deps),
		ForcePrefix:             in.force,
	}

	var result domain.CreateIssueResult
	if issue.Ephemeral {
		result, err = uw.IssueUseCase().CreateWisp(ctx, params, in.createdBy)
	} else {
		result, err = uw.IssueUseCase().CreateIssue(ctx, params, in.createdBy)
	}
	if err != nil {
		FatalError("%v", err)
	}

	if err := uw.Commit(ctx, fmt.Sprintf("bd: create %s", result.Issue.ID)); err != nil && !isDoltNothingToCommit(err) {
		FatalError("commit: %v", err)
	}

	switch {
	case in.jsonOutput:
		outputJSON(result.Issue)
	case in.silent:
		fmt.Println(result.Issue.ID)
	default:
		fmt.Printf("%s Created issue: %s\n", ui.RenderPass("✓"), formatFeedbackID(result.Issue.ID, result.Issue.Title))
		fmt.Printf("  Priority: P%d\n", result.Issue.Priority)
		fmt.Printf("  Status: %s\n", result.Issue.Status)
		// Tips intentionally skipped on the proxied path.
	}

	SetLastTouchedID(result.Issue.ID)
}

func runCreateLintIssue(in createInput) {
	if in.validationMode != "error" && in.validationMode != "warn" {
		return
	}
	lintIssue := &types.Issue{
		IssueType:          types.IssueType(in.issueType).Normalize(),
		Description:        in.description,
		AcceptanceCriteria: in.acceptanceCriteria,
	}
	if err := validation.LintIssue(lintIssue); err != nil {
		if in.validationMode == "error" {
			FatalError("%v", err)
		}
		fmt.Fprintf(os.Stderr, "%s %v\n", ui.RenderWarn("⚠"), err)
	}
}

// buildCreateIssueFromInput adapts createInput into a *types.Issue via the
// existing buildCreateIssue helper. Both modes (dry-run and live) use this
// so the issue shape stays in lockstep with the embedded path.
func buildCreateIssueFromInput(in createInput) *types.Issue {
	return buildCreateIssue(createIssueParams{
		ID:                 in.explicitID,
		Title:              in.title,
		Description:        in.description,
		Design:             in.design,
		AcceptanceCriteria: in.acceptanceCriteria,
		Notes:              in.notes,
		SpecID:             in.specID,
		Priority:           in.priority,
		IssueType:          types.IssueType(in.issueType).Normalize(),
		Assignee:           in.assignee,
		ExternalRef:        in.externalRef,
		EstimatedMinutes:   in.estimatedMinutes,
		Ephemeral:          in.ephemeral,
		NoHistory:          in.noHistory,
		CreatedBy:          in.createdBy,
		Owner:              in.owner,
		MolType:            in.molType,
		WispType:           in.wispType,
		EventKind:          in.eventCategory,
		Actor:              in.eventActor,
		Target:             in.eventTarget,
		Payload:            in.eventPayload,
		DueAt:              in.dueAt,
		DeferUntil:         in.deferUntil,
		Metadata:           in.metadata,
	})
}

// runCreateProxiedMarkdown parses a markdown file and creates each template
// as an issue inside one UOW transaction. Mirrors the embedded path's
// IssueTemplate → Issue mapping; uses IssueUseCase.CreateIssues (or
// CreateWisps when --ephemeral) so the whole batch lands atomically.
func runCreateProxiedMarkdown(_ *cobra.Command, ctx context.Context, in createInput) {
	templates, err := parseMarkdownFile(in.markdownFile)
	if err != nil {
		FatalError("parsing markdown file: %v", err)
	}
	if len(templates) == 0 {
		FatalError("no issues found in markdown file")
	}

	// Per-template template validation (no DB).
	if in.validationMode == "error" || in.validationMode == "warn" {
		for _, t := range templates {
			lintIssue := &types.Issue{
				IssueType:          t.IssueType,
				Description:        t.Description,
				AcceptanceCriteria: t.AcceptanceCriteria,
			}
			if err := validation.LintIssue(lintIssue); err != nil {
				if in.validationMode == "error" {
					FatalError("template %q: %v", t.Title, err)
				}
				fmt.Fprintf(os.Stderr, "%s template %q: %v\n", ui.RenderWarn("⚠"), t.Title, err)
			}
		}
	}

	// Parse per-template deps (pure) before opening the UOW so we fail fast.
	type templateBuild struct {
		template *IssueTemplate
		deps     []domain.DependencySpec
	}
	builds := make([]templateBuild, 0, len(templates))
	for _, t := range templates {
		deps, err := parseMarkdownDepSpecs(t.Dependencies, t.Title)
		if err != nil {
			FatalError("%v", err)
		}
		builds = append(builds, templateBuild{template: t, deps: deps})
	}

	uw, cctx := proxiedOpenUOW(ctx)
	defer uw.Close(ctx)

	for _, b := range builds {
		if b.template.IssueType == "" {
			continue
		}
		if !b.template.IssueType.IsValidWithCustom(cctx.CustomTypes) {
			FatalError("template %q: invalid type %q", b.template.Title, b.template.IssueType)
		}
	}

	paramsList := make([]domain.CreateIssueParams, 0, len(builds))
	for _, b := range builds {
		t := b.template
		paramsList = append(paramsList, domain.CreateIssueParams{
			Issue: &types.Issue{
				Title:              t.Title,
				Description:        t.Description,
				Design:             t.Design,
				AcceptanceCriteria: t.AcceptanceCriteria,
				Status:             types.StatusOpen,
				Priority:           t.Priority,
				IssueType:          t.IssueType,
				Assignee:           t.Assignee,
				Ephemeral:          in.ephemeral,
				NoHistory:          in.noHistory,
				MolType:            in.molType,
				CreatedBy:          in.createdBy,
				Owner:              in.owner,
			},
			Labels:       t.Labels,
			Dependencies: b.deps,
		})
	}

	var result domain.CreateIssuesResult
	if in.ephemeral {
		result, err = uw.IssueUseCase().CreateWisps(ctx, paramsList, in.createdBy, domain.CreateIssuesOpts{})
	} else {
		result, err = uw.IssueUseCase().CreateIssues(ctx, paramsList, in.createdBy, domain.CreateIssuesOpts{})
	}
	if err != nil {
		FatalError("creating issues from markdown: %v", err)
	}

	commitMsg := fmt.Sprintf("bd: create %d issue(s) from %s", len(result.Issues), in.markdownFile)
	if err := uw.Commit(ctx, commitMsg); err != nil && !isDoltNothingToCommit(err) {
		FatalError("commit: %v", err)
	}

	if in.jsonOutput {
		outputJSON(result.Issues)
		return
	}
	fmt.Printf("%s Created %d issues from %s:\n", ui.RenderPass("✓"), len(result.Issues), in.markdownFile)
	for _, issue := range result.Issues {
		fmt.Printf("  %s: %s [P%d, %s]\n", issue.ID, issue.Title, issue.Priority, issue.IssueType)
	}
}

// parseMarkdownDepSpecs mirrors the embedded markdown path's dep parsing:
// "type:id" → typed edge, bare "id" → blocks edge. Does NOT support the
// alias / swap-direction operators that --deps accepts (depends-on:,
// blocked-by:, blocks: with swap). Markdown templates are an authoring
// surface, not a CLI flag — keeping the syntax minimal matches the
// embedded behavior so users see identical results across modes.
func parseMarkdownDepSpecs(deps []string, templateTitle string) ([]domain.DependencySpec, error) {
	var out []domain.DependencySpec
	for _, raw := range deps {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var depType types.DependencyType
		var target string
		if strings.Contains(raw, ":") {
			parts := strings.SplitN(raw, ":", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid dependency format %q for issue %q", raw, templateTitle)
			}
			depType = types.DependencyType(strings.TrimSpace(parts[0]))
			target = strings.TrimSpace(parts[1])
		} else {
			depType = types.DepBlocks
			target = raw
		}

		if !depType.IsValid() {
			return nil, fmt.Errorf("invalid dependency type %q for issue %q", depType, templateTitle)
		}
		out = append(out, domain.DependencySpec{
			Type:     depType,
			TargetID: target,
		})
	}
	return out, nil
}

// runCreateProxiedGraph reads a graph plan and applies it atomically against
// the proxied UOW. Mirrors createIssuesFromGraph's shape: parse file,
// detect unknown fields, validate plan, dry-run preview OR execute via
// IssueUseCase.ApplyIssueGraph (CreateWisps variant when --ephemeral),
// then resolve MetadataRefs as post-create UpdateIssue calls.
func runCreateProxiedGraph(_ *cobra.Command, ctx context.Context, in createInput) {
	data, err := os.ReadFile(in.graphFile) // #nosec G304 -- user-provided path is intentional
	if err != nil {
		FatalError("reading graph plan: %v", err)
	}
	if unknown := detectUnknownGraphFields(data); len(unknown) > 0 {
		warnUnknownGraphFields(os.Stderr, unknown)
	}

	var plan GraphApplyPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		FatalError("parsing graph plan: %v", err)
	}

	// Dry-run path: no UOW. Falls back to YAML-only custom types in proxied
	// mode (store is nil), which is the same fidelity the embedded path
	// offers when called outside an open store.
	if in.dryRun {
		if err := validateGraphApplyPlan(&plan, loadEmbeddedCustomTypes()); err != nil {
			FatalError("invalid graph plan: %v", err)
		}
		emitGraphApplyDryRun(&plan)
		return
	}

	uw, cctx := proxiedOpenUOW(ctx)
	defer uw.Close(ctx)

	if err := validateGraphApplyPlan(&plan, cctx.CustomTypes); err != nil {
		FatalError("invalid graph plan: %v", err)
	}

	domainPlan := buildDomainGraphPlan(plan, in)

	var result domain.GraphApplyResult
	if in.ephemeral {
		result, err = uw.IssueUseCase().ApplyWispGraph(ctx, domainPlan, in.createdBy)
	} else {
		result, err = uw.IssueUseCase().ApplyIssueGraph(ctx, domainPlan, in.createdBy)
	}
	if err != nil {
		FatalError("graph create: %v", err)
	}

	// Resolve MetadataRefs now that all IDs are known. IssueUseCase
	// doesn't perform this step itself — each node with refs gets a
	// post-create UpdateIssue carrying the merged metadata.
	for _, node := range plan.Nodes {
		if len(node.MetadataRefs) == 0 {
			continue
		}
		merged := make(map[string]string, len(node.Metadata)+len(node.MetadataRefs))
		for k, v := range node.Metadata {
			merged[k] = v
		}
		for metaKey, refKey := range node.MetadataRefs {
			resolvedID, ok := result.IDs[refKey]
			if !ok {
				FatalError("node %q: metadata_ref %q references unknown key %q", node.Key, metaKey, refKey)
			}
			merged[metaKey] = resolvedID
		}
		metaJSON, err := json.Marshal(merged)
		if err != nil {
			FatalError("node %q: marshaling merged metadata: %v", node.Key, err)
		}
		updates := map[string]any{"metadata": json.RawMessage(metaJSON)}
		if in.ephemeral {
			err = uw.IssueUseCase().UpdateWisp(ctx, result.IDs[node.Key], updates, in.createdBy)
		} else {
			err = uw.IssueUseCase().UpdateIssue(ctx, result.IDs[node.Key], updates, in.createdBy)
		}
		if err != nil {
			FatalError("node %q: updating metadata refs: %v", node.Key, err)
		}
	}

	commitMsg := plan.CommitMessage
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("bd: graph-apply %d nodes", len(plan.Nodes))
	}
	if err := uw.Commit(ctx, commitMsg); err != nil && !isDoltNothingToCommit(err) {
		FatalError("commit: %v", err)
	}

	if in.jsonOutput {
		outputJSON(GraphApplyResult{IDs: result.IDs})
		return
	}
	fmt.Printf("Created %d issues\n", len(result.IDs))
	keys := make([]string, 0, len(result.IDs))
	for k := range result.IDs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("  %s -> %s\n", k, result.IDs[k])
	}
}

// buildDomainGraphPlan converts the CLI-facing GraphApplyPlan into the
// domain GraphPlan accepted by IssueUseCase.ApplyIssueGraph. Per-node
// materialization happens in materializeGraphNodeIssue so the --ephemeral /
// --no-history opts flow through.
func buildDomainGraphPlan(plan GraphApplyPlan, in createInput) domain.GraphPlan {
	nodes := make([]domain.GraphNode, 0, len(plan.Nodes))
	for _, n := range plan.Nodes {
		nodes = append(nodes, domain.GraphNode{
			Key:               n.Key,
			Issue:             materializeGraphNodeIssue(n, in),
			ParentKey:         n.ParentKey,
			ParentID:          n.ParentID,
			Assignee:          n.Assignee,
			AssignAfterCreate: n.AssignAfterCreate,
			MetadataRefs:      n.MetadataRefs,
			Labels:            n.Labels,
		})
	}
	edges := make([]domain.GraphEdge, 0, len(plan.Edges))
	for _, e := range plan.Edges {
		edges = append(edges, domain.GraphEdge{
			FromKey: e.FromKey,
			FromID:  e.FromID,
			ToKey:   e.ToKey,
			ToID:    e.ToID,
			Type:    graphApplyDependencyType(e.Type),
		})
	}
	return domain.GraphPlan{Nodes: nodes, Edges: edges}
}

// materializeGraphNodeIssue builds the *types.Issue that the use case
// stores for a single graph node. Applies plan-wide options (--ephemeral,
// --no-history) and identity (createdBy/owner). The use case handles the
// AssignAfterCreate deferred-assignee dance, so the Assignee field is left
// for the domain layer to populate via node.Assignee.
func materializeGraphNodeIssue(n GraphApplyNode, in createInput) *types.Issue {
	issueType := types.IssueType(n.Type)
	if issueType == "" {
		issueType = types.TypeTask
	}
	priority := 2
	if n.Priority != nil {
		priority = *n.Priority
	}
	var metadataJSON json.RawMessage
	if len(n.Metadata) > 0 {
		raw, err := json.Marshal(n.Metadata)
		if err != nil {
			FatalError("node %q: marshaling metadata: %v", n.Key, err)
		}
		metadataJSON = raw
	}
	return &types.Issue{
		Title:       n.Title,
		Description: n.Description,
		IssueType:   issueType,
		Status:      types.StatusOpen,
		Priority:    priority,
		Labels:      n.Labels,
		Metadata:    metadataJSON,
		Ephemeral:   in.ephemeral,
		NoHistory:   in.noHistory,
		CreatedBy:   in.createdBy,
		Owner:       in.owner,
	}
}
