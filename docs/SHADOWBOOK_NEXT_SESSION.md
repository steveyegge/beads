# Shadowbook Next Session Spec

## Date: 2026-01-29
## Status: Ready for implementation
## Previous: v1 complete, tested in kite-trading-platform (424 specs)

---

## What Works Now

| Feature | Command | Status |
|---------|---------|--------|
| Spec scanning | `bd spec scan` | ✅ |
| Spec listing | `bd spec list` | ✅ |
| Spec detail | `bd spec show <path>` | ✅ |
| Coverage report | `bd spec coverage` | ✅ |
| Drift detection | `bd list --spec-changed` | ✅ |
| Acknowledge | `bd update <id> --ack-spec` | ✅ |
| Link issue | `bd update <id> --spec-id <path>` | ✅ |
| Compaction | `bd spec compact <path> --summary "..."` | ✅ |
| Find unlinked | `bd list --no-spec` | ✅ |
| Idempotent init | `bd init --if-missing` | ✅ |

---

## Priority 1: Improve `bd spec scan` Output

### Problem
User expected scan to show which specs are linked vs unlinked. Currently:
```bash
bd spec scan
# ✓ Scanned 424 specs (added=4 updated=4 missing=0 marked=0)
```

No coverage info. Have to run `bd spec coverage` separately.

### Desired Output
```bash
bd spec scan

# ✓ Scanned 424 specs
#   Added: 4 | Updated: 4 | Missing: 0 | Marked: 0
#
# Coverage:
#   ● 4 specs linked to beads
#   ○ 420 specs have NO linked beads
#
# 💡 Run `bd spec suggest` to auto-match unlinked issues
```

### Implementation
1. After scan, call `GetSpecCoverage()`
2. Add coverage summary to output
3. Add tip if unlinked > 10

**File:** `cmd/bd/spec.go` (specScanCmd, around line 85-92)

```go
// After successful scan, show coverage
coverage, err := store.GetSpecCoverage(ctx)
if err == nil && coverage.Total > 0 {
    fmt.Printf("\nCoverage:\n")
    fmt.Printf("  ● %d specs linked to beads\n", coverage.WithBeads)
    fmt.Printf("  ○ %d specs have NO linked beads\n", coverage.WithoutBeads)
    if coverage.WithoutBeads > 10 {
        fmt.Println("\n💡 Run `bd spec suggest` to auto-match unlinked issues")
    }
}
```

---

## Priority 2: `bd spec suggest <issue-id>`

### Purpose
Auto-suggest which spec an unlinked issue should link to.

### Usage
```bash
bd spec suggest beads-wku1v

# beads-wku1v: TraderDeck: Full implementation with scheduler controls
#
# Suggested specs (by title similarity):
#   1. specs/active/TRADERDECK_FULL_IMPLEMENTATION_SPEC.md (92%)
#   2. specs/active/TRADERDECK_REPLAY_NFO_UPDATES_SPEC.md (67%)
#   3. specs/active/TRADERGLANCE_MACOS_MENUBAR_SPEC.md (45%)
#
# Link with:
#   bd update beads-wku1v --spec-id "specs/active/TRADERDECK_FULL_IMPLEMENTATION_SPEC.md"
```

### Algorithm
```go
// internal/spec/matcher.go

var stopwords = map[string]bool{
    "the": true, "a": true, "an": true, "and": true, "or": true,
    "for": true, "to": true, "in": true, "on": true, "with": true,
    "spec": true, "feature": true, "task": true, "bug": true,
    "implement": true, "add": true, "fix": true, "update": true,
    "full": true, "complete": true, "new": true,
}

func Tokenize(s string) []string {
    words := regexp.MustCompile(`\w+`).FindAllString(strings.ToLower(s), -1)
    var tokens []string
    for _, w := range words {
        if !stopwords[w] && len(w) > 2 {
            tokens = append(tokens, w)
        }
    }
    return tokens
}

func JaccardSimilarity(a, b []string) float64 {
    setA := make(map[string]bool)
    for _, t := range a { setA[t] = true }
    setB := make(map[string]bool)
    for _, t := range b { setB[t] = true }

    intersection := 0
    for t := range setA {
        if setB[t] { intersection++ }
    }
    union := len(setA) + len(setB) - intersection
    if union == 0 { return 0 }
    return float64(intersection) / float64(union)
}

type ScoredMatch struct {
    SpecID string
    Title  string
    Score  float64
}

func SuggestSpecs(issueTitle string, specs []SpecRegistryEntry, limit int) []ScoredMatch {
    issueToks := Tokenize(issueTitle)
    var matches []ScoredMatch

    for _, spec := range specs {
        // Score against title
        specTitleToks := Tokenize(spec.Title)
        titleScore := JaccardSimilarity(issueToks, specTitleToks)

        // Score against filename
        filename := filepath.Base(spec.SpecID)
        filenameToks := Tokenize(strings.TrimSuffix(filename, ".md"))
        filenameScore := JaccardSimilarity(issueToks, filenameToks)

        // Combined score (title weighted higher)
        score := 0.6*titleScore + 0.4*filenameScore

        if score >= 0.4 { // 40% threshold
            matches = append(matches, ScoredMatch{
                SpecID: spec.SpecID,
                Title:  spec.Title,
                Score:  score,
            })
        }
    }

    // Sort by score descending
    sort.Slice(matches, func(i, j int) bool {
        return matches[i].Score > matches[j].Score
    })

    if len(matches) > limit {
        matches = matches[:limit]
    }
    return matches
}
```

### Files to Create
- `internal/spec/matcher.go`
- `internal/spec/matcher_test.go`
- `cmd/bd/spec_suggest.go`

### CLI Command
```go
// cmd/bd/spec_suggest.go
var specSuggestCmd = &cobra.Command{
    Use:   "suggest <issue-id>",
    Short: "Suggest specs for an unlinked issue",
    Args:  cobra.ExactArgs(1),
    Run: func(cmd *cobra.Command, args []string) {
        issueID := args[0]
        limit, _ := cmd.Flags().GetInt("limit")

        // Get issue
        issue, err := store.GetIssue(ctx, issueID)
        if err != nil { ... }

        if issue.SpecID != "" {
            fmt.Printf("Issue already linked to: %s\n", issue.SpecID)
            return
        }

        // Get all specs
        specs, err := specStore.ListSpecs(ctx)
        if err != nil { ... }

        // Suggest
        matches := spec.SuggestSpecs(issue.Title, specs, limit)

        fmt.Printf("%s: %s\n\n", issueID, issue.Title)

        if len(matches) == 0 {
            fmt.Println("No matching specs found.")
            return
        }

        fmt.Println("Suggested specs (by title similarity):")
        for i, m := range matches {
            fmt.Printf("  %d. %s (%.0f%%)\n", i+1, m.SpecID, m.Score*100)
        }

        fmt.Printf("\nLink with:\n  bd update %s --spec-id \"%s\"\n",
            issueID, matches[0].SpecID)
    },
}
```

---

## Priority 3: `bd spec link --auto`

### Purpose
Bulk link unlinked issues to specs above similarity threshold.

### Usage
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

### Flags
- `--threshold <percent>` — Minimum similarity (default: 70)
- `--confirm` — Actually apply links
- `--limit <n>` — Max issues to process (default: 100)

### Implementation
```go
// cmd/bd/spec_link.go
var specLinkCmd = &cobra.Command{
    Use:   "link",
    Short: "Link issues to specs",
}

var specLinkAutoCmd = &cobra.Command{
    Use:   "auto",
    Short: "Auto-link unlinked issues to matching specs",
    Run: func(cmd *cobra.Command, args []string) {
        threshold, _ := cmd.Flags().GetFloat64("threshold")
        confirm, _ := cmd.Flags().GetBool("confirm")
        limit, _ := cmd.Flags().GetInt("limit")

        // Get unlinked issues
        filter := types.IssueFilter{NoSpec: true, Limit: limit}
        issues, _ := store.SearchIssues(ctx, filter)

        // Get all specs
        specs, _ := specStore.ListSpecs(ctx)

        var matches []struct {
            IssueID string
            SpecID  string
            Score   float64
        }

        for _, issue := range issues {
            suggestions := spec.SuggestSpecs(issue.Title, specs, 1)
            if len(suggestions) > 0 && suggestions[0].Score >= threshold/100 {
                matches = append(matches, struct{...}{
                    IssueID: issue.ID,
                    SpecID:  suggestions[0].SpecID,
                    Score:   suggestions[0].Score,
                })
            }
        }

        if len(matches) == 0 {
            fmt.Println("No matches found above threshold.")
            return
        }

        fmt.Printf("Found %d potential matches:\n", len(matches))
        for _, m := range matches {
            fmt.Printf("  %s → %s (%.0f%%)\n", m.IssueID, m.SpecID, m.Score*100)
        }

        if !confirm {
            fmt.Println("\nRun with --confirm to apply these links")
            return
        }

        // Apply links
        for _, m := range matches {
            store.UpdateIssue(ctx, m.IssueID, types.IssueUpdate{
                SpecID: &m.SpecID,
            }, "shadowbook-auto-link")
        }
        fmt.Printf("✓ Linked %d issues to specs\n", len(matches))
    },
}
```

---

## Priority 4: `bd spec orphans`

### Purpose
Find issues that SHOULD have specs but don't.

### Usage
```bash
bd spec orphans

# Issues without specs (potential matches exist):
#   beads-wku1v: TraderDeck: Full implementation...
#     → 3 specs match "traderdeck"
#   beads-4ktsp: Trade Company MVP...
#     → 5 specs match "trade company"
#
# Issues without specs (no matches found):
#   beads-cebqh: Wave Rider: Fast Entry...
#   beads-xyz: Random Task...
#
# Summary:
#   47 issues could be auto-linked (run `bd spec link --auto`)
#   23 issues have no matching specs
```

---

## Implementation Order

| Order | Task | Effort | Files |
|-------|------|--------|-------|
| 1 | Improve scan output | 30 min | `cmd/bd/spec.go` |
| 2 | Create matcher | 1 hr | `internal/spec/matcher.go` |
| 3 | `bd spec suggest` | 1 hr | `cmd/bd/spec_suggest.go` |
| 4 | `bd spec link --auto` | 1 hr | `cmd/bd/spec_link.go` |
| 5 | `bd spec orphans` | 30 min | `cmd/bd/spec_orphans.go` |

---

## Testing Checklist

- [ ] `bd spec scan` shows coverage summary after scan
- [ ] `bd spec suggest beads-xxx` returns ranked matches
- [ ] `bd spec suggest` with already-linked issue shows current link
- [ ] `bd spec link --auto` previews correctly (no --confirm)
- [ ] `bd spec link --auto --confirm` applies links
- [ ] `bd spec link --auto --threshold 90` filters low matches
- [ ] `bd spec orphans` categorizes correctly
- [ ] All commands work with `--json` flag
- [ ] Works in kite-trading-platform (424 specs, 100+ issues)

---

## Success Criteria

After this session:
```bash
# 1. Scan shows coverage
bd spec scan
# → Shows "4 linked, 420 unlinked"

# 2. Suggest works
bd spec suggest beads-wku1v
# → Shows matching specs with scores

# 3. Auto-link works
bd spec link --auto --confirm
# → Links ~50 issues automatically

# 4. Coverage improves
bd spec coverage
# → "54 specs with beads, 370 without"
```

---

## Notes

- Matching uses Jaccard similarity on tokenized titles
- Default threshold: 70% for auto-link, 40% for suggestions
- Stopwords filtered: the, a, an, spec, feature, implement, etc.
- All commands support `--json` for programmatic use
- Test in kite-trading-platform before committing
