# shadowbook

**Keep your specs and code in sync.**

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Shadowbook detects when your specification documents change and alerts you when linked implementation work needs review. Built on [beads](https://github.com/steveyegge/beads).

## The Problem

You write a spec. You create tasks. You implement.

Then the spec changes. The tasks don't. Your code drifts from the design.

**Shadowbook catches this.**

```
$ shadowbook scan
✓ Scanned 42 specs (added=0 updated=3 missing=0)

⚠ 3 specs changed — 7 linked issues need review:
  specs/auth/login.md (changed)
    → bd-a1b2: Implement OAuth flow
    → bd-c3d4: Add session management
  specs/api/rate-limits.md (changed)
    → bd-e5f6: Add rate limiting middleware
```

## Quick Start

```bash
# Install
brew tap yourname/shadowbook
brew install shadowbook

# Initialize in your project (requires beads)
cd your-project
bd init
shadowbook init

# Scan your specs
shadowbook scan

# See what needs attention
shadowbook status
bd list --spec-changed
```

## How It Works

```
┌─────────────────────────────────────────┐
│  Your spec docs (specs/*.md)            │
│  - Design documents                     │
│  - API specifications                   │
│  - Feature requirements                 │
└─────────────────┬───────────────────────┘
                  │ shadowbook scan
                  ▼
┌─────────────────────────────────────────┐
│  Spec Registry (local SQLite)           │
│  - Tracks file paths                    │
│  - Stores content hashes                │
│  - Detects changes                      │
└─────────────────┬───────────────────────┘
                  │ hash changed?
                  ▼
┌─────────────────────────────────────────┐
│  Linked Issues (beads)                  │
│  - Flags issues for review              │
│  - Shows in bd list --spec-changed      │
│  - Visible in bd show                   │
└─────────────────────────────────────────┘
```

## Commands

### Scanning

```bash
# Scan default specs/ directory
shadowbook scan

# Scan custom path
shadowbook scan --path docs/specs

# JSON output for scripting
shadowbook scan --json
```

### Registry

```bash
# List all tracked specs
shadowbook list

# Show spec details + linked issues
shadowbook show specs/auth/login.md

# Check coverage (which specs have issues, which don't)
shadowbook coverage
```

### Integration with beads

```bash
# Create issue linked to spec
bd create "Implement feature X" --spec-id "specs/feature-x.md"

# Filter issues by spec
bd list --spec "specs/auth/"

# See issues needing review after spec changes
bd list --spec-changed

# View issue with spec change warning
bd show bd-xxx
# Output:
# ○ bd-xxx · Implement feature X   [P2 · OPEN]
# Spec: specs/feature-x.md
# ● [SPEC CHANGED] 2026-01-28 — review may be needed

# Acknowledge the change
bd update bd-xxx --ack-spec
```

## Workflow

### 1. Design Phase

Write your specs in markdown:

```markdown
# specs/auth/oauth.md

## Overview
Implement OAuth 2.0 authentication with Google and GitHub providers.

## Requirements
- Support authorization code flow
- Store tokens securely
- Implement token refresh
- Add logout endpoint
```

### 2. Planning Phase

Create linked issues:

```bash
bd create "Implement OAuth flow" --spec-id "specs/auth/oauth.md" --type epic
bd create "Add Google provider" --spec-id "specs/auth/oauth.md" --parent bd-xxx
bd create "Add GitHub provider" --spec-id "specs/auth/oauth.md" --parent bd-xxx
bd create "Implement token refresh" --spec-id "specs/auth/oauth.md" --parent bd-xxx
```

### 3. Implementation Phase

Work on issues normally. Shadowbook watches for spec drift.

### 4. Spec Changes

When requirements change, update the spec:

```markdown
# specs/auth/oauth.md (updated)

## Requirements
- Support authorization code flow
- Store tokens securely
- Implement token refresh
- Add logout endpoint
- **NEW: Add Apple Sign-In support**  ← change
```

### 5. Drift Detection

```bash
$ shadowbook scan
✓ Scanned 12 specs (updated=1)

⚠ specs/auth/oauth.md changed
  4 linked issues flagged for review

$ bd list --spec-changed
○ bd-a1b2 [P1] [epic] - Implement OAuth flow
○ bd-c3d4 [P2] [task] - Add Google provider
○ bd-e5f6 [P2] [task] - Add GitHub provider
○ bd-g7h8 [P2] [task] - Implement token refresh
```

### 6. Review & Update

Review each issue against the updated spec:

```bash
bd show bd-a1b2  # See the change warning
# Review the spec change
# Update the issue if needed
bd update bd-a1b2 --ack-spec  # Clear the flag

# Or create new work for the new requirement
bd create "Add Apple Sign-In" --spec-id "specs/auth/oauth.md" --parent bd-a1b2
```

## Configuration

Create `.shadowbook.yaml` in your project root:

```yaml
# Directories to scan for specs
paths:
  - specs/
  - docs/design/

# File patterns to include
patterns:
  - "*.md"
  - "*.txt"

# Exclude patterns
exclude:
  - "**/node_modules/**"
  - "**/.git/**"

# Auto-scan on git pull (optional)
hooks:
  post-merge: true
  post-checkout: true
```

## Spec ID Formats

Shadowbook recognizes different spec_id formats:

| Format | Example | Scanned? |
|--------|---------|----------|
| Repo-relative path | `specs/auth/login.md` | ✅ Yes |
| Nested path | `docs/design/api.md` | ✅ Yes |
| URL | `https://notion.so/spec/123` | ❌ No (external) |
| Identifier | `SPEC-001` | ❌ No (external) |
| Absolute path | `/Users/x/spec.md` | ❌ No (not portable) |

External spec_ids are valid for linking but won't trigger change detection.

## CI Integration

Add to your CI pipeline:

```yaml
# .github/workflows/spec-check.yml
name: Spec Drift Check
on: [push, pull_request]
jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Install shadowbook
        run: |
          brew install beads shadowbook
          bd init --if-missing
      - name: Check for spec drift
        run: |
          shadowbook scan
          if bd list --spec-changed --json | jq -e 'length > 0'; then
            echo "::warning::Specs changed but linked issues not reviewed"
            bd list --spec-changed
          fi
```

## FAQ

**Q: How is this different from beads?**

Beads tracks issues. Shadowbook tracks specs. They complement each other:
- Beads = "What work needs doing?"
- Shadowbook = "Is the work aligned with the spec?"

**Q: Do I need beads to use shadowbook?**

Yes, currently. Shadowbook extends beads with spec intelligence. Future versions may support other issue trackers.

**Q: Is the spec registry synced via git?**

No. The registry is local to each machine. Only the `spec_changed_at` flag on issues is synced. Each developer runs their own scans.

**Q: What if I use Notion/Confluence for specs?**

You can still link via URL: `bd create "Task" --spec-id "https://notion.so/spec/123"`. Change detection won't work (external URL), but the link is preserved.

**Q: How do I clear the spec-changed flag?**

Three ways:
1. `bd update <id> --ack-spec` — explicit acknowledgment
2. `bd update <id> --spec-id <new>` — changing the spec clears it
3. `bd close <id>` — closing the issue clears it

## Comparison

| Feature | beads | shadowbook | gastown |
|---------|-------|------------|---------|
| Issue tracking | ✅ | - | - |
| Spec linking | ✅ (`spec_id`) | ✅ | - |
| Spec registry | - | ✅ | - |
| Change detection | - | ✅ | - |
| Coverage reports | - | ✅ | - |
| Multi-agent orchestration | - | - | ✅ |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

MIT License. See [LICENSE](LICENSE).

---

**Built with [beads](https://github.com/steveyegge/beads)** — the git-backed issue tracker for AI agents.
