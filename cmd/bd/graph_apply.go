package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"

	"github.com/steveyegge/beads/internal/config"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// GraphApplyPlan describes a symbolic bead graph to create atomically.
type GraphApplyPlan struct {
	CommitMessage string           `json:"commit_message,omitempty"`
	Nodes         []GraphApplyNode `json:"nodes"`
	Edges         []GraphApplyEdge `json:"edges,omitempty"`
}

// GraphApplyNode describes a single bead to create.
type GraphApplyNode struct {
	Key               string            `json:"key"`
	Title             string            `json:"title"`
	Type              string            `json:"type,omitempty"`
	Description       string            `json:"description,omitempty"`
	Assignee          string            `json:"assignee,omitempty"`
	AssignAfterCreate bool              `json:"assign_after_create,omitempty"`
	Priority          *int              `json:"priority,omitempty"` // nil defaults to P2
	Labels            []string          `json:"labels,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	MetadataRefs      map[string]string `json:"metadata_refs,omitempty"`
	ParentKey         string            `json:"parent_key,omitempty"`
	ParentID          string            `json:"parent_id,omitempty"`
}

// GraphApplyEdge describes a dependency edge.
type GraphApplyEdge struct {
	FromKey string `json:"from_key,omitempty"`
	FromID  string `json:"from_id,omitempty"`
	ToKey   string `json:"to_key,omitempty"`
	ToID    string `json:"to_id,omitempty"`
	Type    string `json:"type,omitempty"`
}

// GraphApplyResult returns the concrete bead IDs assigned to each symbolic key.
type GraphApplyResult struct {
	IDs map[string]string `json:"ids"`
}

// GraphApplyDryRun describes the actions that would be taken by a graph plan,
// without performing any writes. Emitted by `bd create --graph --dry-run`.
type GraphApplyDryRun struct {
	DryRun     bool                  `json:"dry_run"`
	NodeCount  int                   `json:"node_count"`
	EdgeCount  int                   `json:"edge_count"`
	ParentDeps int                   `json:"parent_deps"`
	Nodes      []GraphApplyDryRunRow `json:"nodes"`
}

// GraphApplyDryRunRow describes a single planned node in the dry-run preview.
type GraphApplyDryRunRow struct {
	Key       string `json:"key"`
	Title     string `json:"title"`
	Type      string `json:"type"`
	Priority  int    `json:"priority"`
	ParentKey string `json:"parent_key,omitempty"`
	ParentID  string `json:"parent_id,omitempty"`
}

// knownGraphPlanFields lists the JSON keys recognized at the top level of a
// GraphApplyPlan. Any other top-level keys produce a warning so users can spot
// schema typos (e.g. when a plan uses a sibling tool's format) instead of
// having fields silently dropped by encoding/json. (GH#3367)
var knownGraphPlanFields = map[string]struct{}{
	"commit_message": {},
	"nodes":          {},
	"edges":          {},
}

// knownGraphNodeFields lists the JSON keys recognized on a GraphApplyNode.
// Kept in sync with the json tags on GraphApplyNode. (GH#3367)
var knownGraphNodeFields = map[string]struct{}{
	"key":                 {},
	"title":               {},
	"type":                {},
	"description":         {},
	"assignee":            {},
	"assign_after_create": {},
	"priority":            {},
	"labels":              {},
	"metadata":            {},
	"metadata_refs":       {},
	"parent_key":          {},
	"parent_id":           {},
}

// knownGraphEdgeFields lists the JSON keys recognized on a GraphApplyEdge.
// Kept in sync with the json tags on GraphApplyEdge. (GH#3367)
var knownGraphEdgeFields = map[string]struct{}{
	"from_key": {},
	"from_id":  {},
	"to_key":   {},
	"to_id":    {},
	"type":     {},
}

// graphFieldHints maps unknown-field names to a corrective hint pointing at
// the recognized schema field. Used by warnUnknownGraphFields to suggest the
// intended schema when a plan uses a common-but-wrong name (e.g. nodes carry
// a "parent" string instead of "parent_key", or "blocks" arrays instead of
// the top-level edges array). (GH#3367)
var graphFieldHints = map[string]string{
	"parent":   "use 'parent_key' (referencing another node's 'key') or 'parent_id' (an existing issue ID)",
	"blocks":   "use the top-level 'edges' array, e.g. {\"from_key\": \"a\", \"to_key\": \"b\", \"type\": \"blocks\"}",
	"depends":  "use the top-level 'edges' array with type 'blocks'",
	"children": "set 'parent_key' on each child instead of listing children on the parent",
}

// detectUnknownGraphFields scans the raw plan JSON and returns unknown field
// names grouped by their location in the plan. The returned map keys describe
// the location ("plan", "node[<key-or-index>]", "edge[<index>]") and values
// are sorted lists of unknown field names at that location. Returns an empty
// map when the plan is structurally invalid (callers should still attempt the
// strict parse so the operator gets a normal parse error rather than only the
// schema warning). (GH#3367)
func detectUnknownGraphFields(rawData []byte) map[string][]string {
	out := make(map[string][]string)

	var top map[string]json.RawMessage
	if err := json.Unmarshal(rawData, &top); err != nil {
		return out
	}

	if planUnknown := unknownKeys(top, knownGraphPlanFields); len(planUnknown) > 0 {
		out["plan"] = planUnknown
	}

	if nodesRaw, ok := top["nodes"]; ok {
		var rawNodes []json.RawMessage
		if err := json.Unmarshal(nodesRaw, &rawNodes); err == nil {
			for i, nodeRaw := range rawNodes {
				var nodeMap map[string]json.RawMessage
				if err := json.Unmarshal(nodeRaw, &nodeMap); err != nil {
					continue
				}
				if unknown := unknownKeys(nodeMap, knownGraphNodeFields); len(unknown) > 0 {
					label := fmt.Sprintf("node[%d]", i)
					if keyRaw, ok := nodeMap["key"]; ok {
						var keyStr string
						if err := json.Unmarshal(keyRaw, &keyStr); err == nil && keyStr != "" {
							label = fmt.Sprintf("node[%q]", keyStr)
						}
					}
					out[label] = unknown
				}
			}
		}
	}

	if edgesRaw, ok := top["edges"]; ok {
		var rawEdges []json.RawMessage
		if err := json.Unmarshal(edgesRaw, &rawEdges); err == nil {
			for i, edgeRaw := range rawEdges {
				var edgeMap map[string]json.RawMessage
				if err := json.Unmarshal(edgeRaw, &edgeMap); err != nil {
					continue
				}
				if unknown := unknownKeys(edgeMap, knownGraphEdgeFields); len(unknown) > 0 {
					out[fmt.Sprintf("edge[%d]", i)] = unknown
				}
			}
		}
	}

	return out
}

// unknownKeys returns the keys present in have that are not in known, sorted
// alphabetically for deterministic output.
func unknownKeys(have map[string]json.RawMessage, known map[string]struct{}) []string {
	var unknown []string
	for k := range have {
		if _, ok := known[k]; !ok {
			unknown = append(unknown, k)
		}
	}
	sort.Strings(unknown)
	return unknown
}

// warnUnknownGraphFields prints a single warning line per location in the
// plan with one or more unknown fields, plus a per-field hint when one is
// available. Output goes to w (typically os.Stderr). Returns the set of
// distinct unknown field names that were warned about, primarily for tests.
// (GH#3367)
func warnUnknownGraphFields(w io.Writer, unknown map[string][]string) []string {
	if len(unknown) == 0 {
		return nil
	}

	locations := make([]string, 0, len(unknown))
	for loc := range unknown {
		locations = append(locations, loc)
	}
	sort.Strings(locations)

	distinct := make(map[string]struct{})
	for _, loc := range locations {
		fields := append([]string(nil), unknown[loc]...)
		sort.Strings(fields)
		fmt.Fprintf(w, "warning: graph plan %s has unknown field(s): %v (silently dropped — see 'bd create --graph' schema)\n", loc, fields)
		for _, f := range fields {
			distinct[f] = struct{}{}
		}
	}

	hintFields := make([]string, 0, len(distinct))
	for f := range distinct {
		hintFields = append(hintFields, f)
	}
	sort.Strings(hintFields)
	for _, f := range hintFields {
		if hint, ok := graphFieldHints[f]; ok {
			fmt.Fprintf(w, "  hint: %q is not part of the schema; %s\n", f, hint)
		}
	}

	return hintFields
}

// createIssuesFromGraph handles `bd create --graph <plan-file>`.
// When dryRun is true, the plan is parsed and validated but no writes occur;
// a preview is emitted to stdout (JSON when jsonOutput is set, otherwise
// human-readable). Unknown plan/node/edge fields are reported to stderr in
// both modes so schema gaps are visible before any writes happen. (GH#3367)
func createIssuesFromGraph(planFile string, dryRun bool) {
	data, err := os.ReadFile(planFile) // #nosec G304 -- user-provided path is intentional
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

	if err := validateGraphApplyPlan(&plan); err != nil {
		FatalError("invalid graph plan: %v", err)
	}

	if dryRun {
		emitGraphApplyDryRun(&plan)
		return
	}

	result, err := executeGraphApply(rootCtx, &plan)
	if err != nil {
		FatalError("graph create: %v", err)
	}

	if jsonOutput {
		outputJSON(result)
	} else {
		fmt.Printf("Created %d issues\n", len(result.IDs))
		keys := make([]string, 0, len(result.IDs))
		for key := range result.IDs {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Printf("  %s -> %s\n", key, result.IDs[key])
		}
	}
}

// emitGraphApplyDryRun prints what `bd create --graph` would do without
// performing any writes. Mirrors the JSON-vs-human split of the live path.
// (GH#3367)
func emitGraphApplyDryRun(plan *GraphApplyPlan) {
	parentDeps := 0
	rows := make([]GraphApplyDryRunRow, 0, len(plan.Nodes))
	for _, node := range plan.Nodes {
		issueType := node.Type
		if issueType == "" {
			issueType = string(types.TypeTask)
		}
		priority := 2
		if node.Priority != nil {
			priority = *node.Priority
		}
		if node.ParentKey != "" || node.ParentID != "" {
			parentDeps++
		}
		rows = append(rows, GraphApplyDryRunRow{
			Key:       node.Key,
			Title:     node.Title,
			Type:      issueType,
			Priority:  priority,
			ParentKey: node.ParentKey,
			ParentID:  node.ParentID,
		})
	}

	preview := GraphApplyDryRun{
		DryRun:     true,
		NodeCount:  len(plan.Nodes),
		EdgeCount:  len(plan.Edges),
		ParentDeps: parentDeps,
		Nodes:      rows,
	}

	if jsonOutput {
		outputJSON(preview)
		return
	}

	fmt.Printf("Dry run: would create %d issue(s) and %d edge(s) (%d parent-child link(s))\n",
		preview.NodeCount, preview.EdgeCount, preview.ParentDeps)
	for _, row := range rows {
		parent := ""
		switch {
		case row.ParentKey != "":
			parent = fmt.Sprintf(" parent_key=%s", row.ParentKey)
		case row.ParentID != "":
			parent = fmt.Sprintf(" parent_id=%s", row.ParentID)
		}
		fmt.Printf("  %s [%s] P%d %q%s\n", row.Key, row.Type, row.Priority, row.Title, parent)
	}
}

// validateGraphApplyPlan checks the plan for structural errors before any writes.
func validateGraphApplyPlan(plan *GraphApplyPlan) error {
	if len(plan.Nodes) == 0 {
		return fmt.Errorf("plan has no nodes")
	}

	// Load custom types so user-configured types (spec, session, etc.) are accepted.
	var customTypes []string
	if store != nil {
		ct, _ := store.GetCustomTypes(rootCtx)
		customTypes = ct
	}
	if len(customTypes) == 0 {
		customTypes = config.GetCustomTypesFromYAML()
	}

	seenKeys := make(map[string]bool, len(plan.Nodes))
	for i, node := range plan.Nodes {
		if node.Key == "" {
			return fmt.Errorf("node %d has empty key", i)
		}
		if seenKeys[node.Key] {
			return fmt.Errorf("duplicate node key %q", node.Key)
		}
		seenKeys[node.Key] = true
		if node.Title == "" {
			return fmt.Errorf("node %q has empty title", node.Key)
		}
		if node.Type != "" {
			it := types.IssueType(node.Type)
			if !it.IsValidWithCustom(customTypes) {
				return fmt.Errorf("node %q: invalid type %q", node.Key, node.Type)
			}
		}
		// Validate MetadataRefs point to known keys.
		for metaKey, refKey := range node.MetadataRefs {
			if !seenKeys[refKey] {
				found := false
				for _, other := range plan.Nodes {
					if other.Key == refKey {
						found = true
						break
					}
				}
				if !found {
					return fmt.Errorf("node %q: metadata ref %q references unknown key %q", node.Key, metaKey, refKey)
				}
			}
		}
		if node.ParentKey != "" && !seenKeys[node.ParentKey] {
			found := false
			for _, other := range plan.Nodes {
				if other.Key == node.ParentKey {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("node %q: parent key %q not found in plan", node.Key, node.ParentKey)
			}
		}
	}

	for i, edge := range plan.Edges {
		if edge.FromKey != "" && !seenKeys[edge.FromKey] {
			return fmt.Errorf("edge %d: from key %q not found in plan", i, edge.FromKey)
		}
		if edge.ToKey != "" && !seenKeys[edge.ToKey] {
			return fmt.Errorf("edge %d: to key %q not found in plan", i, edge.ToKey)
		}
		if edge.FromKey == "" && edge.FromID == "" {
			return fmt.Errorf("edge %d: must specify from_key or from_id", i)
		}
		if edge.ToKey == "" && edge.ToID == "" {
			return fmt.Errorf("edge %d: must specify to_key or to_id", i)
		}
		if edge.Type != "" {
			dt := types.DependencyType(edge.Type)
			if !dt.IsValid() {
				return fmt.Errorf("edge %d: invalid dependency type %q", i, edge.Type)
			}
		}
	}

	return nil
}

func executeGraphApply(ctx context.Context, plan *GraphApplyPlan) (*GraphApplyResult, error) {
	keyToID := make(map[string]string, len(plan.Nodes))

	commitMsg := plan.CommitMessage
	if commitMsg == "" {
		commitMsg = fmt.Sprintf("bd: graph-apply %d nodes", len(plan.Nodes))
	}

	if err := store.RunInTransaction(ctx, commitMsg, func(tx storage.Transaction) error {
		issues := make([]*types.Issue, 0, len(plan.Nodes))
		pendingAssignees := make(map[int]string)

		for i, node := range plan.Nodes {
			issueType := types.IssueType(node.Type)
			if issueType == "" {
				issueType = types.TypeTask
			}

			var metadataJSON json.RawMessage
			if len(node.Metadata) > 0 {
				raw, err := json.Marshal(node.Metadata)
				if err != nil {
					return fmt.Errorf("node %q: marshaling metadata: %w", node.Key, err)
				}
				metadataJSON = raw
			}

			priority := 2 // Default P2
			if node.Priority != nil {
				priority = *node.Priority
			}

			issue := &types.Issue{
				Title:     node.Title,
				IssueType: issueType,
				Status:    types.StatusOpen,
				Priority:  priority,
				Labels:    node.Labels,
				Metadata:  metadataJSON,
			}
			if node.Description != "" {
				issue.Description = node.Description
			}
			if node.Assignee != "" {
				if node.AssignAfterCreate {
					pendingAssignees[i] = node.Assignee
				} else {
					issue.Assignee = node.Assignee
				}
			}

			issues = append(issues, issue)
		}

		if err := tx.CreateIssues(ctx, issues, actor); err != nil {
			return fmt.Errorf("batch create: %w", err)
		}

		for i, node := range plan.Nodes {
			keyToID[node.Key] = issues[i].ID
		}

		// Persist labels.
		for i, node := range plan.Nodes {
			for _, label := range node.Labels {
				if err := tx.AddLabel(ctx, issues[i].ID, label, actor); err != nil {
					return fmt.Errorf("node %q: adding label %q: %w", node.Key, label, err)
				}
			}
		}

		// Resolve MetadataRefs now that all IDs are known.
		for i, node := range plan.Nodes {
			if len(node.MetadataRefs) == 0 {
				continue
			}
			mergedMeta := make(map[string]string)
			if issues[i].Metadata != nil {
				if err := json.Unmarshal(issues[i].Metadata, &mergedMeta); err != nil {
					return fmt.Errorf("node %q: re-parsing metadata: %w", node.Key, err)
				}
			}
			for metaKey, refKey := range node.MetadataRefs {
				mergedMeta[metaKey] = keyToID[refKey]
			}
			metaJSON, err := json.Marshal(mergedMeta)
			if err != nil {
				return fmt.Errorf("node %q: marshaling updated metadata: %w", node.Key, err)
			}
			updates := map[string]interface{}{
				"metadata": json.RawMessage(metaJSON),
			}
			if err := tx.UpdateIssue(ctx, issues[i].ID, updates, actor); err != nil {
				return fmt.Errorf("node %q: updating metadata refs: %w", node.Key, err)
			}
		}

		// Add dependencies from edges.
		for _, edge := range plan.Edges {
			fromID := resolveEdgeRef(edge.FromKey, edge.FromID, keyToID)
			toID := resolveEdgeRef(edge.ToKey, edge.ToID, keyToID)
			depType := types.DependencyType(edge.Type)
			if depType == "" {
				depType = types.DepBlocks
			}
			dep := &types.Dependency{
				IssueID:     fromID,
				DependsOnID: toID,
				Type:        depType,
			}
			if err := tx.AddDependency(ctx, dep, actor); err != nil {
				return fmt.Errorf("adding edge %s->%s: %w", fromID, toID, err)
			}
		}

		// Add parent-child dependencies.
		for i, node := range plan.Nodes {
			parentID := node.ParentID
			if node.ParentKey != "" {
				parentID = keyToID[node.ParentKey]
			}
			if parentID != "" {
				dep := &types.Dependency{
					IssueID:     issues[i].ID,
					DependsOnID: parentID,
					Type:        types.DepParentChild,
				}
				if err := tx.AddDependency(ctx, dep, actor); err != nil {
					return fmt.Errorf("node %q: adding parent-child dep: %w", node.Key, err)
				}
			}
		}

		// Apply deferred assignees.
		for i, assignee := range pendingAssignees {
			updates := map[string]interface{}{
				"assignee": assignee,
			}
			if err := tx.UpdateIssue(ctx, issues[i].ID, updates, actor); err != nil {
				return fmt.Errorf("node %q: setting assignee: %w", plan.Nodes[i].Key, err)
			}
		}

		return nil
	}); err != nil {
		return nil, err
	}

	return &GraphApplyResult{IDs: keyToID}, nil
}

func resolveEdgeRef(key, id string, keyToID map[string]string) string {
	if id != "" {
		return id
	}
	if key != "" {
		return keyToID[key]
	}
	return ""
}
