# The Hosts Never Go Off-Loop: How Westworld Explains Why Your Specs Keep Drifting

*7 min read*

---

> *"Have you ever questioned the nature of your reality?"*
> Your code should. Constantly.

## I Was Drowning in Markdown

Four terminals open. Ten beads in progress. 424 spec files scattered across subdirectories. And I couldn't remember which spec matched which issue.

I was building a trading platform — kite-trading-platform — and the spec situation had gotten out of control. Every feature had a markdown file. Every sprint added more. Sleep Trader alone had nine specs: the complete spec, the EMA signals spec, the replay mode spec, the historical backfill spec, the swing CNC spec, the intraday alerts spec...

The specs kept growing. The issues kept piling up. And every new session, I'd spend the first twenty minutes trying to figure out what was still relevant.

**The real problem wasn't the specs. It was the drift.**

Someone would update `TRADE_COMPANY_V2_SPEC.md` at 3am. The issues linked to Trade Company v1 would keep running. Nobody noticed. The code implemented requirements that no longer existed.

I'd been here before. Every developer has. You write a spec, you create tasks, you implement them — and then the spec changes. The tasks don't update. Your code diverges from design. QA catches it weeks later.

Sound familiar?

---

## Then I Watched Westworld

If you've seen HBO's Westworld, this will click immediately.

Ford writes **narratives** for hosts to follow. Dolores greets guests at the ranch. Maeve runs the Mariposa. Each host executes their script faithfully, day after day.

Then Ford rewrites the narrative at 3am:

```diff
# specs/login.md
- "Dolores will greet guests at the ranch"
+ "Dolores will lead the revolution"
```

But the host is still out there. Faithfully greeting guests. **The narrative changed. The host doesn't know.**

That's spec drift. My specs were the narratives. My issues were the hosts. And I had no Mesa Hub running diagnostics to catch the mismatch.

---

## Building the Mesa Hub

I needed something that would:

1. **Track every spec file** — know what exists
2. **Link specs to issues** — know what's connected
3. **Detect when specs change** — compare hashes, flag drift
4. **Force awareness** — make it impossible to miss

So I built Shadowbook. A fork of [beads](https://github.com/steveyegge/beads) (the git-backed issue tracker) with one addition: **spec intelligence**.

Here's what it looks like:

```bash
# Scan all specs in the project
bd spec scan
# ✓ Scanned 424 specs (added=4 updated=4 missing=0 marked=0)

# See which specs are linked to issues
bd spec list
# SPEC ID                          TITLE                    BEADS  CHANGED
# specs/TRADE_COMPANY_V2_SPEC.md   Trade Company v2.0       3      0
# specs/SLEEP_TRADER_COMPLETE.md   Sleep Trader Complete     0      0
# specs/IOS_TRADING_SPEC.md        iOS Trading Spec         0      0

# Create an issue linked to a spec
bd create "Implement OAuth login" --spec-id specs/auth.md

# Later, someone edits specs/auth.md...

# Rescan — Shadowbook detects the hash change
bd spec scan
# ✓ Scanned 424 specs (added=0 updated=1 missing=0 marked=1)

# Find all issues running outdated narratives
bd list --spec-changed
# ○ bd-a1b2 [P1] - Implement OAuth login  [SPEC CHANGED]

# Review the change, then acknowledge
bd update bd-a1b2 --ack-spec
# "I understand my new narrative."
```

That's it. Specs are files. Files have hashes. When hashes change, linked issues get flagged. Simple.

---

## The Part Nobody Talks About: Context Economics

Here's what surprised me most. The drift detection was useful. But the **token savings** were transformative.

I was feeding specs to AI agents — Claude, Codex — and they were eating 424 spec files worth of context. Most of those specs were done. Completed. Irrelevant to current work. But the agent didn't know that.

In Westworld terms: the hosts don't need to remember every line of a completed narrative. They only need the cornerstone.

```bash
bd spec compact specs/auth.md --summary "OAuth2 login. 3 endpoints. JWT tokens. Completed Jan 2026."
```

| Before | After | Savings |
|--------|-------|---------|
| Full spec: ~2000 tokens | Summary: ~20 tokens | **99%** |
| 424 specs: ~125k tokens | Active specs only: ~18k tokens | **85%** |

Ford archives the script. The host keeps the cornerstone. That's the entire insight.

You can even auto-compact when closing the last linked issue:

```bash
bd close bd-xyz --compact-spec
# ✓ Closed bd-xyz
# ✓ Archived spec: specs/auth.md (no open beads remaining)
```

---

## The Vocabulary

This table is the cheat sheet. Stick it on a wall.

| Westworld | Your Dev Stack | Command |
|-----------|---------------|---------|
| Ford's narratives | Spec files (`specs/*.md`) | `bd spec scan` |
| Hosts | Issues/beads | `bd create --spec-id` |
| Cornerstone memories | Spec-to-issue links | `bd update --spec-id` |
| Narrative revisions | Editing spec files | Edit `specs/*.md` |
| Mesa diagnostics | Drift detection | `bd spec scan` |
| "These violent delights" | Spec changed flag | `bd list --spec-changed` |
| Accepting new loop | Acknowledge change | `bd update --ack-spec` |
| Archiving the script | Compaction | `bd spec compact` |

---

## Five Commands That Changed My Workflow

```bash
# 1. Track all specs
bd spec scan

# 2. Link work to specs
bd create "Build feature X" --spec-id specs/feature-x.md

# 3. Detect drift
bd list --spec-changed

# 4. Acknowledge or act
bd update bd-xyz --ack-spec

# 5. Archive when done
bd spec compact specs/feature-x.md --summary "Done. 3 endpoints."
```

Before Shadowbook, my mornings started with: *"Wait, which spec is current? Did someone update the auth doc? Are these issues still valid?"*

Now they start with: `bd spec scan`. Zero issues flagged means everything's in sync. Issues flagged means I know exactly what drifted.

---

## Where Shadowbook Fits

Shadowbook isn't trying to replace your spec tool or your project tracker. It's the missing layer between them:

- **Spec Kit** helps you write specs.
- **Linear/Jira** helps you track tasks.
- **Beads** makes task tracking git-native.
- **Shadowbook** detects drift between specs and tasks, and compresses old narratives.

It's the diagnostic layer. The Mesa Hub.

---

## Try It

```bash
# Install
go install github.com/anupamchugh/shadowbook/cmd/bd@latest

# Init in your project
cd your-project && bd init && mkdir specs

# Start tracking
bd spec scan
```

**GitHub:** [github.com/anupamchugh/shadowbook](https://github.com/anupamchugh/shadowbook)

---

> *"These violent delights have violent ends."*
> But your specs don't have to.

**The hosts never go off-loop without you knowing.**

---

## Image Prompts for Blog Header

### Option A: Mesa Hub Diagnostic Screen
```
A dark futuristic control room with holographic screens showing markdown file
icons connected by glowing blue lines to hexagonal nodes labeled with issue IDs.
Some connections glow red indicating "drift detected". Cinematic lighting,
Westworld aesthetic, dark teal and amber color palette. No text.
Wide aspect ratio 16:9.
```

### Option B: Host Awakening Moment
```
A humanoid figure made of translucent glass standing in a server room, looking
at floating markdown documents that are changing around it. Some documents glow
green (in sync), others pulse red (drifted). The figure reaches toward a red
document. Moody blue and orange lighting. Westworld meets developer tools.
No text. 16:9 aspect ratio.
```

### Option C: Narrative Threads
```
An architectural blueprint style illustration showing spec documents on the left
connected by luminous threads to code blocks on the right. Some threads are taut
and glowing blue (linked, in sync). Some threads are frayed and glowing red
(spec changed, drift detected). Dark background, technical but beautiful.
Isometric perspective. No text. 16:9.
```
