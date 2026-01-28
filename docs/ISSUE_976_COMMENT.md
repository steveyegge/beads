# Comment for Issue #976

**Post this comment to:** https://github.com/steveyegge/beads/issues/976

---

I've been working on exactly this problem and have a PR ready that adds first-class spec linking to beads.

## The feature

Instead of embedding doc references in `--desc`, you get a dedicated `spec_id` field:

```bash
# Link issue to spec document
bd create "Implement auth flow" --spec-id "docs/plans/auth-design.md"

# Filter by spec (or prefix)
bd list --spec "docs/plans/"
bd list --spec "docs/plans/auth/"  # all auth-related

# Show displays the linked spec
bd show bd-xxx
# Output includes: Spec: docs/plans/auth-design.md
```

## Why this helps

Building on @steveyegge's recommended pattern:

| Before (manual) | After (structured) |
|-----------------|-------------------|
| `--desc "See docs/plans/auth.md"` | `--spec-id "docs/plans/auth.md"` |
| Can't filter by spec | `bd list --spec "docs/plans/"` |
| Easy to forget/lose | First-class field, always visible |

The philosophy stays the same — beads track *what to do*, docs capture *how* — but now the link is explicit and queryable.

## What's in the PR

- `spec_id` field on issues
- `--spec-id` flag on create/update
- `--spec` filter on list (with prefix matching)
- Spec displayed in `bd show`
- Migration for existing databases
- Works across SQLite, Dolt, and memory backends

Intentionally minimal — just the linking, no heavy automation. Fits the beads philosophy of lightweight issues with rich context elsewhere.

Happy to submit the PR if there's interest. Also working on a separate tool called "shadowbook" that builds on this foundation for spec change detection (detects when your spec docs change and flags linked issues for review), but that's a separate thing and not part of the beads PR.

---

**Copy the above and post to the issue.**
