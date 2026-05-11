package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/formula"
	"github.com/steveyegge/beads/internal/storage"
	"github.com/steveyegge/beads/internal/types"
)

// executeOnCompleteFanout runs the runtime fanout for a step that has an
// OnCompleteSpec persisted under metadata.on_complete (GH#3782). Reads the
// for_each path against the closed step's metadata, substitutes per-item
// placeholders ({item}, {item.field}, {index}) in the spec's Vars, and
// bonds the spec's Bond formula once per item onto the closed step.
//
// Hard-errors and refuses (returns error) when:
//   - the for_each path is missing in the issue's metadata
//   - the for_each value is not an array
//
// Per-item bond failures do not abort the fanout — they're accumulated and
// recorded into the closed issue's metadata.on_complete_failures so the
// agent/user can see what partially failed.
//
// Returns nil (no-op) when the issue has no on_complete spec.
func executeOnCompleteFanout(ctx context.Context, s storage.DoltStorage, closedIssueID, actor string) error {
	issue, err := s.GetIssue(ctx, closedIssueID)
	if err != nil {
		return fmt.Errorf("loading closed issue %s: %w", closedIssueID, err)
	}
	if len(issue.Metadata) == 0 {
		return nil
	}

	var meta map[string]interface{}
	if err := json.Unmarshal(issue.Metadata, &meta); err != nil {
		return nil
	}
	rawSpec, ok := meta["on_complete"]
	if !ok {
		return nil
	}

	specBytes, err := json.Marshal(rawSpec)
	if err != nil {
		return fmt.Errorf("on_complete spec for %s: malformed: %w", closedIssueID, err)
	}
	var spec formula.OnCompleteSpec
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		return fmt.Errorf("on_complete spec for %s: decode failed: %w", closedIssueID, err)
	}
	if spec.ForEach == "" || spec.Bond == "" {
		// Validator should have rejected this at cook time; defensive.
		return nil
	}

	items, err := readForEachItems(meta, spec.ForEach)
	if err != nil {
		return fmt.Errorf("on_complete on %s: %w", closedIssueID, err)
	}

	var spawnedIDs []string
	var bondFailures []string
	for i, item := range items {
		itemVars := substituteOnCompletePlaceholders(spec.Vars, item, i)
		spawnedID, err := bondOneForOnComplete(ctx, s, issue, spec.Bond, itemVars, actor)
		if err != nil {
			bondFailures = append(bondFailures, fmt.Sprintf("item[%d]: %v", i, err))
			continue
		}
		spawnedIDs = append(spawnedIDs, spawnedID)
	}

	// Sequential: thread blocks deps between consecutive spawned roots so
	// each waits on its predecessor. Default (parallel) leaves them
	// independent siblings.
	if spec.Sequential && len(spawnedIDs) > 1 {
		for i := 1; i < len(spawnedIDs); i++ {
			dep := &types.Dependency{
				IssueID:     spawnedIDs[i],
				DependsOnID: spawnedIDs[i-1],
				Type:        types.DepBlocks,
			}
			if err := s.AddDependency(ctx, dep, actor); err != nil {
				bondFailures = append(bondFailures, fmt.Sprintf("sequential link %s -> %s: %v", spawnedIDs[i], spawnedIDs[i-1], err))
			}
		}
	}

	if len(bondFailures) > 0 {
		recordOnCompleteFailures(ctx, s, closedIssueID, bondFailures, actor)
	}

	return nil
}

// readForEachItems resolves the for_each path (e.g. "output.items") against
// the issue's metadata map. Returns the iterable as a slice. Hard-errors
// when the path is missing or the value isn't an array.
func readForEachItems(meta map[string]interface{}, path string) ([]interface{}, error) {
	parts := strings.Split(path, ".")
	var cur interface{} = meta
	for _, p := range parts {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("for_each path %q: %q is not an object", path, p)
		}
		next, exists := m[p]
		if !exists {
			return nil, fmt.Errorf("for_each path %q: key %q not present in metadata; an on_complete step must populate output before closing", path, p)
		}
		cur = next
	}
	arr, ok := cur.([]interface{})
	if !ok {
		return nil, fmt.Errorf("for_each path %q resolved to %T, not an array", path, cur)
	}
	return arr, nil
}

// substituteOnCompletePlaceholders materializes a Vars map for one iteration:
// replaces {item}, {item.<field>}, and {index} in each value with the current
// item's contents. Values without placeholders pass through unchanged.
func substituteOnCompletePlaceholders(vars map[string]string, item interface{}, index int) map[string]string {
	out := make(map[string]string, len(vars))
	for k, v := range vars {
		out[k] = substituteOnePlaceholder(v, item, index)
	}
	return out
}

func substituteOnePlaceholder(value string, item interface{}, index int) string {
	value = strings.ReplaceAll(value, "{index}", fmt.Sprintf("%d", index))
	// {item.<field>} for object items.
	for {
		start := strings.Index(value, "{item.")
		if start < 0 {
			break
		}
		end := strings.Index(value[start:], "}")
		if end < 0 {
			break
		}
		end += start
		field := value[start+len("{item.") : end]
		var sub string
		if m, ok := item.(map[string]interface{}); ok {
			if v, exists := m[field]; exists {
				sub = fmt.Sprintf("%v", v)
			}
		}
		value = value[:start] + sub + value[end+1:]
	}
	// Bare {item} for primitives.
	value = strings.ReplaceAll(value, "{item}", fmt.Sprintf("%v", item))
	return value
}

// bondOneForOnComplete cooks the named formula and bonds it onto the closed
// step as a per-item child molecule. Returns the spawned root ID on success.
func bondOneForOnComplete(ctx context.Context, s storage.DoltStorage, closedIssue *types.Issue, bondName string, itemVars map[string]string, actor string) (string, error) {
	subgraph, _, err := resolveOrCookToSubgraph(ctx, s, bondName, itemVars)
	if err != nil {
		return "", fmt.Errorf("resolving bond %q: %w", bondName, err)
	}
	result, err := bondProtoMolWithSubgraph(
		ctx, s, subgraph, subgraph.Root, closedIssue,
		types.BondTypeParallel, // siblings under the closed step
		itemVars,
		"",    // childRef: random hash by default; future spec field could add naming
		actor,
		false, // ephemeral
		true,  // pour: children persist
	)
	if err != nil {
		return "", err
	}
	// bondProtoMolWithSubgraph attaches to closedIssue.ID; the new mol root is the
	// remapped subgraph root. Pull from IDMapping (old subgraph root ID → new ID).
	if newID, ok := result.IDMapping[subgraph.Root.ID]; ok {
		return newID, nil
	}
	return "", nil
}

// recordOnCompleteFailures appends the bond-failure messages to the closed
// issue's metadata.on_complete_failures so partial failures stay visible.
// Best-effort: a failure to record is itself ignored — losing the visibility
// trace is preferable to surfacing a cascading error from the close path.
func recordOnCompleteFailures(ctx context.Context, s storage.DoltStorage, issueID string, failures []string, actor string) {
	issue, err := s.GetIssue(ctx, issueID)
	if err != nil {
		return
	}
	var meta map[string]interface{}
	if len(issue.Metadata) > 0 {
		_ = json.Unmarshal(issue.Metadata, &meta)
	}
	if meta == nil {
		meta = make(map[string]interface{})
	}
	meta["on_complete_failures"] = failures
	merged, err := json.Marshal(meta)
	if err != nil {
		return
	}
	_ = s.UpdateIssue(ctx, issueID, map[string]interface{}{"metadata": json.RawMessage(merged)}, actor)
}
