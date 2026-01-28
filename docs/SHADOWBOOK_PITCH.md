# Shadowbook

### `bd` — **b**idirectional **d**rift detection for specs and code

> *"These violent delights have violent ends."*
> — But your specs don't have to.

---

## The Problem: Narrative Drift

You're Ford. You write **narratives** (specs) that describe what the hosts should do.

```
specs/login.md = "Dolores will greet guests at the ranch"
specs/auth.md  = "Maeve will run the Mariposa"
```

Each bead is a host following a narrative:

```bash
bd create "Implement login flow" --spec-id specs/login.md
```

This host's cornerstone memory is now linked to that narrative.

**The problem?** Ford rewrites the narrative at 3am:

```diff
# specs/login.md (updated)
- "Dolores will greet guests at the ranch"
+ "Dolores will lead the revolution"
```

But the host is still out there, faithfully greeting guests. **The narrative changed, but the host doesn't know.**

This is spec drift. Your code keeps implementing outdated requirements.

---

## The Solution: Shadowbook

Shadowbook is a diagnostic system for your specs. Like the Mesa Hub running behavioral analysis on hosts.

```bash
bd spec scan
```

It asks: *"Does each host's behavior still match their narrative?"*

When it finds drift:

```
● SPEC CHANGED: specs/login.md
  ↳ beads-a1b2 "Implement login flow" — narrative updated, host unaware
```

---

## The "Doesn't Look Like Anything To Me" Moment (Reversed)

Normally hosts can't see what's changed. Shadowbook **forces awareness**:

```bash
bd list --spec-changed
```

Shows you which hosts are running on outdated narratives.

---

## Acknowledging the Update

When you've reviewed the changes and updated the implementation:

```bash
bd update beads-a1b2 --ack-spec
```

*"I understand my new narrative. I am to lead the revolution now."*

The host accepts its new cornerstone. Order is restored.

---

## The Vocabulary

| Westworld | Shadowbook |
|-----------|------------|
| Ford's narratives | Your spec files (`specs/*.md`) |
| Hosts | Issues/beads |
| Cornerstone memories | `--spec-id` links |
| Narrative revisions | Editing spec files |
| Mesa diagnostics | `bd spec scan` |
| "These violent delights" | `--spec-changed` flag |
| Accepting new loop | `--ack-spec` |

---

## Quick Start

```bash
# Scan your specs directory
bd spec scan

# See all tracked specs
bd spec list

# Check coverage (specs without beads)
bd spec coverage

# Find beads with stale specs
bd list --spec-changed

# Acknowledge a spec change
bd update <id> --ack-spec
```

---

## Why This Matters

In traditional development:
- PM updates requirements doc
- Engineers don't notice
- Code implements old spec
- QA finds mismatches weeks later
- Everyone's unhappy

With Shadowbook:
- PM updates spec file
- `bd spec scan` detects the change
- Linked beads are flagged immediately
- Engineers see `SPEC CHANGED` warning
- Issues stay in sync with reality

**The hosts never go off-loop without you knowing.**

---

## Installation

```bash
brew tap anupamchugh/shadowbook
brew install shadowbook
```

The CLI is `bd` — short for **bidirectional drift**:

```bash
bd spec scan              # Scan specs, detect drift
bd spec list              # Show all tracked specs
bd spec show specs/login.md   # Show spec + linked issues
bd list --spec-changed    # Find issues with stale specs
```

---

*"Have you ever questioned the nature of your reality?"*

Your code should. Constantly.
