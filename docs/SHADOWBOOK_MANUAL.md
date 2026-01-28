# Shadowbook: The Host's Awakening

Your specs are a narrative. Your code is the host executing it.

Most hosts never see the script change. They follow the old story into walls that no longer exist. The code diverges from the design, and nobody notices until the host breaks.

**Shadowbook forces awareness.**

## The Narrative Loop

### Act 1: The Script (Your Spec)

```bash
mkdir specs/
echo "# Authentication Flow" > specs/auth.md
```

You write the spec. Simple markdown. This is the host's story.

### Act 2: The Hosts Wake (Link Tasks)

```bash
bd create "Implement OAuth" --spec-id "specs/auth.md"
bd spec scan
```

Shadowbook reads the story. Hashes it. Memorizes it.

### Act 3: The Betrayal (Spec Drifts)

You update the spec. Add Apple Sign-In. Remove session tokens.
The old code doesn't know.

### Act 4: The Confrontation (Detection)

```bash
bd spec scan
# ✓ specs/auth.md CHANGED
# ⚠ 4 linked issues flagged for review
```

Shadowbook notices. The host is no longer following the script.

### Act 5: The Choice (Acknowledge)

```bash
bd show bd-xxx  # See the warning
bd update bd-xxx --ack-spec  # Accept the change
# OR
bd create "Add Apple Sign-In" --spec-id "specs/auth.md"  # Update the story
```

The host chooses: follow the new narrative or die trying.

---

## How It Works: The Mesa Hub Architecture

```
Your Spec Docs          The Spec Registry         Linked Issues
specs/auth.md    ──→    SHA256: a1b2c3...   ──→   bd-xxx [SPEC CHANGED]
                 scan   tracked locally            marked for review
                        (not synced)
```

**Key insight:** The registry is **local only**. Each developer scans their own copy.
When the spec changes on disk, Shadowbook **forces recalculation** on your next `scan`.

---

## Running the Loop

### 1. Setup (Once)

```bash
bd init
mkdir specs
```

### 2. Write a Spec

```bash
echo "# Payment Processing" > specs/payments.md
```

### 3. Scan to Register It

```bash
bd spec scan
# ✓ Scanned 1 specs (added=1 updated=0 missing=0)
```

The spec is now in the registry.

### 4. Link Work to Specs

```bash
bd create "Process credit cards" --spec-id "specs/payments.md"
# bd-a1b2 created

bd create "Handle disputes" --spec-id "specs/payments.md" --parent bd-a1b2
```

Multiple issues can link to one spec.

### 5. List What's Tracked

```bash
# All registered specs
bd spec list

# Details for one spec (and its linked issues)
bd spec show specs/payments.md

# Coverage report (specs vs issues)
bd spec coverage
```

### 6. Months Later... Spec Changes

```bash
# You update the spec file
# Add: "Handle refunds within 30 days"
# Edit: specs/payments.md
```

### 7. Rescan to Detect Drift

```bash
bd spec scan
# ✓ Scanned 1 specs (added=0 updated=1 missing=0 marked=1)
# ⚠ specs/payments.md changed
#   1 linked issue flagged for review
```

### 8. See Which Work Needs Review

```bash
bd list --spec-changed
# ○ bd-a1b2 [P1] - Process credit cards [SPEC CHANGED]
```

### 9. Deal With It

**Option A: Accept the change**

```bash
bd show bd-a1b2
# See the SPEC CHANGED warning
# Review the spec changes manually
bd update bd-a1b2 --ack-spec
# The flag is cleared
```

**Option B: Create new work**

```bash
bd create "Handle refunds" --spec-id "specs/payments.md" --parent bd-a1b2
# New issue for the new requirement
# Mark original as reviewed
bd update bd-a1b2 --ack-spec
```

**Option C: Update the issue**

```bash
bd update bd-a1b2 --description "Process credit cards AND handle refunds"
# Changes implicitly acknowledge the spec change
```

---

## Commands Reference

### Scanning & Registry

```bash
# Scan default specs/ directory and update registry
bd spec scan

# Scan a custom path
bd spec scan --path docs/design

# JSON output for scripting
bd spec scan --json
```

### Listing & Viewing

```bash
# List all tracked specs with issue counts
bd spec list

# Show spec details + linked issues
bd spec show specs/auth/oauth.md

# Show specs in a directory
bd spec show specs/auth/

# Coverage metrics (specs vs issues)
bd spec coverage

# JSON output
bd spec list --json
bd spec coverage --json
```

### Compaction (Summary Archive)

```bash
# Archive a spec with a human-written summary
bd spec compact specs/auth.md --summary "OAuth flow implemented with PKCE; MFA added in v2."

# Or provide the summary via file
bd spec compact specs/auth.md --summary-file docs/summary/auth.txt
```

### Linking Issues to Specs

```bash
# Create issue linked to spec
bd create "Implement feature" --spec-id "specs/feature.md"

# View linked issues
bd list --spec "specs/auth/"  # All specs in auth/
bd list --spec "specs/payments.md"  # Single spec

# Issues needing review (spec changed)
bd list --spec-changed

# Show change warning
bd show bd-xxx
```

### Acknowledging Changes

```bash
# Explicit acknowledgment
bd update bd-xxx --ack-spec

# Implicit acknowledgment (change detected)
bd update bd-xxx --description "new description"

# Close the issue (also clears flag)
bd close bd-xxx
```

---

## What Shadowbook Does NOT Do

### Does Not Sync Via Git

The registry lives on your machine. You run scans locally. There's no `.shadowbook/registry.db` in git.

```bash
# This is LOCAL to your machine:
.beads/beads.db (SQLite cache)

# What DOES sync via git:
.beads/issues.jsonl (issue definitions)
# → includes spec_id and spec_changed_at fields
```

Each developer maintains their own registry by running `bd spec scan`.

### Does Not Track External Specs

Links to external specs work, but changes won't be detected:

```bash
# External specs (no detection):
bd create "Task" --spec-id "https://notion.so/doc/abc123"
bd create "Task" --spec-id "SPEC-001"
bd create "Task" --spec-id "/absolute/path/spec.md"

# Local specs (detection enabled):
bd create "Task" --spec-id "specs/auth.md"  ✓
```

Only **relative paths** trigger change detection.

### Does Not Replace Code Review

It forces **awareness**, not compliance. You still decide what to do when specs change.

---

## The Catch: What Gets Scanned

Shadowbook only tracks **relative file paths** that exist in your repo.

```bash
# These WILL be scanned for changes:
specs/auth.md
docs/design/api.md
design/spec-1.md
nested/deeply/docs.md

# These WILL NOT be scanned (but links still work):
https://notion.so/doc/123           # External URL
SPEC-001                             # Identifier format
/absolute/path/specs/auth.md         # Absolute path
specs/deleted.md                     # File no longer exists
```

If a spec is **deleted**, the registry marks it `missing_at`. Linked issues aren't automatically cleared—you choose whether they're still valid.

---

## Workflow Examples

### Example 1: Simple Spec → Task → Implementation

```bash
# Write spec
echo "# Login with Google OAuth" > specs/auth.md

# Register it
bd spec scan

# Create task
bd create "Implement Google OAuth" --spec-id "specs/auth.md" -p 1

# Work on it
bd update bd-a1b2 --status in-progress
# ... implement ...

# Mark done
bd close bd-a1b2

# If spec never changed, we're aligned
bd spec scan  # No changes detected
```

### Example 2: Spec Change Mid-Project

```bash
# Original spec
echo "# API Rate Limiting
- Limit to 100 req/min per user
- Return 429 on excess" > specs/api-limits.md

bd spec scan
bd create "Implement rate limiting" --spec-id "specs/api-limits.md" -p 1

# Project is 50% done...

# Spec changes
cat >> specs/api-limits.md << 'EOF'
- NEW: Support burst mode (200 req/min for 30s)
- NEW: Add metrics endpoint
EOF

# Rescan
bd spec scan
# ⚠ specs/api-limits.md CHANGED
# → bd-a1b2 flagged

# Review
bd show bd-a1b2

# Decide: Need new tasks?
bd create "Add burst mode" --spec-id "specs/api-limits.md" --parent bd-a1b2
bd create "Add metrics endpoint" --spec-id "specs/api-limits.md" --parent bd-a1b2

# Acknowledge original is still valid
bd update bd-a1b2 --ack-spec
```

### Example 3: Coverage Analysis

```bash
bd spec coverage
# Specs: 12 total
#   ✓ Covered (linked issues): 10
#   ⚠ Orphaned (no issues): 2
# Issues: 15 total
#   ✓ Linked to specs: 12
#   ⚠ Unlinked: 3

# Find orphaned specs
bd spec list | grep "0 issues"

# Find unlinked issues
bd list --no-spec
```

---

## Advanced: CI Integration

### GitHub Actions Check

```yaml
# .github/workflows/spec-check.yml
name: Spec Drift Check
on: [push, pull_request]

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      
      - name: Install beads
        run: |
          go install github.com/steveyegge/beads/cmd/bd@latest
          bd init --if-missing
      
      - name: Scan specs
        run: bd spec scan
      
      - name: Check for drift
        run: |
          CHANGED=$(bd list --spec-changed --json | jq 'length')
          if [ "$CHANGED" -gt 0 ]; then
            echo "::warning::$CHANGED issues need spec review"
            bd list --spec-changed
            exit 1
          fi
```

### Local Git Hook

```bash
#!/bin/bash
# .git/hooks/post-merge
bd spec scan
if bd list --spec-changed --json | jq -e 'length > 0' > /dev/null; then
  echo "⚠ Specs changed. Run: bd list --spec-changed"
fi
```

---

## Appendix: When Specs Are Not Files

If your specs live in Notion, GitHub Discussions, or Confluence:

**1. Create issues with external spec_id:**

```bash
bd create "Task" --spec-id "https://notion.so/spec/123"
```

The link is preserved in the issue.

**2. Shadowbook won't auto-detect changes** (external URL)

You must notice them manually or set up a separate notification.

**3. Consider a workaround:**

- Export specs from external system to markdown periodically
- Put them in `specs/` and let Shadowbook scan
- OR: Use CI to notify when external docs change

**4. Manual acknowledgment:**

When you review the external spec and update your understanding:

```bash
bd update bd-xxx --ack-spec
```

---

## The Question Hosts Don't Ask

**How do I know if my code matches my spec?**

Shadowbook answers: **You don't, until the spec changes. Then you're forced to look.**

That's the magic. Not prophecy—just accountability.

When your requirements shift, your code is immediately flagged. The narrative and the performance must align, or the host breaks.

---

## Troubleshooting

### "No specs found after scan"

```bash
# Check the directory
ls -la specs/
# Make sure files end in .md
# Make sure files have H1 header (# Title)
```

### "Spec file changed but not detected"

```bash
# Registry is local—you need to run scan again
bd spec scan

# Check registry
bd spec list
```

### "Issue says SPEC CHANGED but I don't see changes"

```bash
# View the issue with change info
bd show bd-xxx

# Acknowledge to clear flag
bd update bd-xxx --ack-spec
```

### "External spec_id not detected"

This is expected. Only `specs/` relative paths trigger detection:

```bash
# Won't detect:
--spec-id "https://notion.so/..."
--spec-id "SPEC-001"

# Will detect:
--spec-id "specs/auth.md"
```

---

## Key Concepts

| Concept | Meaning |
|---------|---------|
| **Spec** | A markdown file describing a feature or system |
| **Scannable** | A spec_id that refers to a local relative file path (can be auto-detected) |
| **Registry** | Local SQLite cache of scanned specs (not synced via git) |
| **SPEC CHANGED** | The spec file's content hash differs from last scan |
| **Linked Issue** | An issue with a `spec_id` pointing to a spec |
| **Coverage** | Report showing which specs have issues, which issues have specs |

---

## Installation

Currently in development. Available branches:

```bash
# From source
go install github.com/anupamchugh/shadowbook/cmd/bd@latest

# Or clone the repo
git clone https://github.com/anupamchugh/shadowbook
cd shadowbook
go build -o bd ./cmd/bd
./bd init
```

Homebrew tap coming soon.

---

## License

MIT License. See [LICENSE](../LICENSE).

Built on [beads](https://github.com/steveyegge/beads) — the git-backed issue tracker for AI agents.
