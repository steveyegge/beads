# Shadowbook v2 Spec: Auto-Linking & Suggestions

## Status: Proposed

---

## Problem Statement

Shadowbook v1 solves **drift detection** for issues already linked to specs. But it doesn't help users **create those links in the first place**.

**Current state in real projects:**
- 417 specs exist
- 100+ issues exist
- Only 4 are linked
- Manual linking is tedious
- Obvious matches go unlinked

**The gap:** Shadowbook tracks drift, but doesn't help establish the initial link.

---

## Proposed Features

### P0: `bd spec suggest <issue-id>`

Auto-suggest specs for an unlinked issue based on title similarity.

```bash
bd spec suggest beads-wku1v

# Output:
# beads-wku1v: TraderDeck: Full implementation with scheduler controls
#
# Suggested specs (by title similarity):
#   1. specs/active/TRADERDECK_FULL_IMPLEMENTATION_SPEC.md (92% match)
#   2. specs/active/TRADERDECK_REPLAY_NFO_UPDATES_SPEC.md (67% match)
#   3. specs/active/TRADERGLANCE_MACOS_MENUBAR_SPEC.md (45% match)
#
# Link with:
#   bd update beads-wku1v --spec-id "specs/active/TRADERDECK_FULL_IMPLEMENTATION_SPEC.md"
```

**Algorithm:**
1. Extract keywords from issue title (tokenize, remove stopwords)
2. For each spec in registry, compute similarity score:
   - Title match (weighted 0.6)
   - Filename match (weighted 0.4)
3. Return top 3 matches above 40% threshold
4. Output linkage command for easy copy-paste

**Implementation:**
- New file: `cmd/bd/spec_suggest.go`
- Add subcommand to `spec.go`
- Use `internal/spec/matcher.go` for fuzzy matching logic

---

### P1: `bd spec link --auto`

Bulk auto-link unlinked issues to specs above a similarity threshold.

```bash
# Preview mode (default)
bd spec link --auto
# Found 47 potential matches:
#   beads-wku1v → specs/active/TRADERDECK_FULL_IMPLEMENTATION_SPEC.md (92%)
#   beads-4ktsp → specs/TRADE_COMPANY_MVP_SPEC.md (89%)
#   beads-dqvvp → specs/features/MY_TRADE_COMPANY_LAUNCH_SPEC.md (85%)
#   ... (44 more)
#
# Run with --confirm to apply these links

# Apply mode
bd spec link --auto --confirm
# ✓ Linked 47 issues to specs

# Custom threshold
bd spec link --auto --threshold 80 --confirm
```

**Flags:**
- `--threshold <percent>` — Minimum similarity (default: 70)
- `--confirm` — Actually apply links (without this, preview only)
- `--dry-run` — Explicit preview mode

---

### P1: `bd spec orphans`

Show issues that likely SHOULD have specs but don't.

```bash
bd spec orphans

# Issues without specs that match spec keywords:
#   beads-wku1v: TraderDeck: Full implementation...
#     → Likely matches: TRADERDECK (3 specs exist)
#
#   beads-cebqh: Wave Rider: Fast Entry...
#     → No matching specs found
#
# Summary:
#   47 issues could be linked (run `bd spec link --auto`)
#   23 issues have no matching specs (may need new specs)
```

---

### P2: `bd create --auto-spec`

When creating an issue, auto-suggest matching spec.

```bash
bd create "Implement TraderDeck scheduler" -p 1 --auto-spec

# Found matching spec: specs/active/TRADERDECK_FULL_IMPLEMENTATION_SPEC.md (87%)
# Link to this spec? [Y/n]: y
# ✓ Created beads-xyz linked to specs/active/TRADERDECK_FULL_IMPLEMENTATION_SPEC.md
```

**Behavior:**
- If `--auto-spec` flag present, search for matching spec before creating
- If match found above threshold, prompt to link
- If `--yes` also passed, auto-link without prompt

---

### P2: Suggestions during `bd spec scan`

During scan, show potential matches for unlinked issues.

```bash
bd spec scan

# ✓ Scanned 417 specs (added=0 updated=2 missing=0 marked=2)
#
# 💡 Tip: 47 unlinked issues may match existing specs
#    Run `bd spec link --auto` to preview suggestions
```

---

## Implementation Plan

### Phase 1: Core Matching (P0)

1. Create `internal/spec/matcher.go`:
   - `SuggestSpecsForIssue(title string, specs []SpecRegistryEntry) []ScoredMatch`
   - Tokenization + stopword removal
   - Jaccard similarity for title matching
   - Levenshtein for typo tolerance

2. Create `cmd/bd/spec_suggest.go`:
   - `bd spec suggest <issue-id>` command
   - Fetch issue title, call matcher, format output

3. Tests:
   - `internal/spec/matcher_test.go`
   - Test cases for exact match, partial match, no match

### Phase 2: Bulk Operations (P1)

1. Add `cmd/bd/spec_link.go`:
   - `bd spec link --auto [--threshold N] [--confirm]`
   - Batch update with transaction

2. Add `cmd/bd/spec_orphans.go`:
   - `bd spec orphans`
   - Group by matching vs non-matching

### Phase 3: Integration (P2)

1. Modify `cmd/bd/create.go`:
   - Add `--auto-spec` flag
   - Interactive prompt or `--yes` for auto-confirm

2. Modify `cmd/bd/spec_scan.go`:
   - Add tip about unlinked issues after scan

---

## Matching Algorithm Details

### Title Similarity Score

```go
func ComputeSimilarity(issueTitle, specTitle, specPath string) float64 {
    // Normalize
    issueToks := tokenize(lower(issueTitle))
    specTitleToks := tokenize(lower(specTitle))
    specPathToks := extractFromPath(specPath) // TRADERDECK from specs/active/TRADERDECK_FULL_IMPLEMENTATION_SPEC.md

    // Jaccard similarity on title
    titleScore := jaccard(issueToks, specTitleToks)

    // Bonus for path keyword match
    pathScore := jaccard(issueToks, specPathToks)

    // Weighted combination
    return 0.6*titleScore + 0.4*pathScore
}
```

### Tokenization

```go
var stopwords = map[string]bool{
    "the": true, "a": true, "an": true, "and": true, "or": true,
    "for": true, "to": true, "in": true, "on": true, "with": true,
    "spec": true, "feature": true, "task": true, "bug": true,
    "implement": true, "add": true, "fix": true, "update": true,
}

func tokenize(s string) []string {
    words := regexp.MustCompile(`\w+`).FindAllString(s, -1)
    var tokens []string
    for _, w := range words {
        if !stopwords[w] && len(w) > 2 {
            tokens = append(tokens, w)
        }
    }
    return tokens
}
```

---

## Success Metrics

| Metric | Target |
|--------|--------|
| `bd spec suggest` accuracy | >80% correct in top-3 |
| `bd spec link --auto` precision | >90% correct matches |
| Time to link 50 issues | <1 minute (vs 10+ manual) |

---

## Open Questions

1. **Should auto-link be opt-in or opt-out?**
   - Current proposal: preview by default, `--confirm` to apply

2. **Threshold default?**
   - Proposed: 70% for suggestions, 85% for auto-link

3. **Handle multiple good matches?**
   - Proposed: show top 3, let user pick

---

## Timeline

| Phase | Effort | Deliverable |
|-------|--------|-------------|
| Phase 1 | 1 day | `bd spec suggest` working |
| Phase 2 | 1 day | `bd spec link --auto` + `bd spec orphans` |
| Phase 3 | 0.5 day | Integration with create/scan |

---

## References

- [Shadowbook v1 Pitch](SHADOWBOOK_PITCH.md)
- [Shadowbook Manual](SHADOWBOOK_MANUAL.md)
- [Test Results](../../SHADOWBOOK_TEST_RESULTS.md)
