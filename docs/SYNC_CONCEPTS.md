# Cross-Machine Sync: How It Works

> **One-sentence model:** Issues live in a local Dolt database under
> `.beads/dolt/` (server mode) or `.beads/embeddeddolt/` (embedded mode);
> cross-machine sync uses Dolt's native push/pull, which stores the issue
> history under `refs/dolt/data` on your git remote — separate from
> `refs/heads/*` where your code lives. `.beads/issues.jsonl` is a passive
> export for portability, not the wire protocol.

This page is the entry point. Read it first, then follow the deeper-doc
links below for the layer you need.

## Mental Model

```
┌──────────────────────┐                    ┌──────────────────────┐
│   Machine A          │                    │   Machine B          │
│                      │                    │                      │
│   bd create / list   │                    │   bd create / list   │
│         ↕            │                    │         ↕            │
│   Local Dolt DB      │                    │   Local Dolt DB      │
│   .beads/dolt/       │                    │   .beads/dolt/       │
│   (source of truth)  │                    │   (source of truth)  │
└──────────┬───────────┘                    └──────────┬───────────┘
           │                                           │
           │  bd dolt push                bd dolt pull │
           │  (Dolt commit graph)                      │
           v                                           v
        ┌────────────────────────────────────────────────┐
        │   git remote                                   │
        │   refs/heads/*    ← your code                  │
        │   refs/dolt/data  ← issues (Dolt history)      │
        └────────────────────────────────────────────────┘
```

When you run a `bd` command, it reads or writes the **local Dolt
database** — `.beads/dolt/` in server mode or `.beads/embeddeddolt/` in
embedded mode (see [DOLT.md](DOLT.md) for the difference). Every write
auto-commits to Dolt history. To share with another machine you run
`bd dolt push`, which pushes the Dolt commit graph to your git remote
under `refs/dolt/data` — a separate ref namespace from `refs/heads/*`,
so it does not collide with your code's branches. The other machine
runs `bd dolt pull` to fetch and merge those refs into its own local
Dolt DB.

`.beads/issues.jsonl` (when present) is an export view: a human-readable
snapshot produced by `bd export` or by hooks. It exists for portability,
git-diff review, and bootstrap — it is not the channel that `bd dolt
push` / `bd dolt pull` use.

## Where to read deeper

After this overview, follow the doc that matches what you need to do:

| Doc | When to read it |
|-----|-----------------|
| [ARCHITECTURE.md](ARCHITECTURE.md) | The full data-model picture: two-layer design, write path, read path, federation. |
| [SYNC_SETUP.md](SYNC_SETUP.md) | Step-by-step: set up Dolt sync on a new machine and add a remote. |
| [GIT_INTEGRATION.md](GIT_INTEGRATION.md) | Working with git worktrees, protected branches, or the `bd merge` driver for `.beads/issues.jsonl`. |
| [DOLT.md](DOLT.md) | Configuring Dolt remotes, embedded vs server mode, backup, and advanced Dolt CLI use. |
| [DOLT-BACKEND.md](DOLT-BACKEND.md) | Remote types (Dolt remotes, S3, GCS, filesystem) and configuration details. |

## Anti-patterns

These are footguns that look reasonable but break the sync model.
Newcomers — humans and agents alike — keep rediscovering them.

### Don't treat `.beads/issues.jsonl` as the source of truth

The local Dolt database is the source of truth. `.beads/issues.jsonl`
is an export — useful for human diff review, for portability, and as a
bootstrap input for `bd init --from-jsonl`. Editing JSONL directly will
be overwritten by the next export, and `bd dolt push` / `bd dolt pull`
don't move data through it. Drive all changes through `bd` commands so
they land in Dolt; let JSONL be a downstream view.

### When JSONL-via-git IS acceptable as a sync channel

If you can't use `bd dolt push` (e.g. policy forbids extra refs on the
remote), it's fine to commit `.beads/issues.jsonl` to git and have peers
`bd init --from-jsonl` or re-import on pull. The Dolt DB on each machine
still remains the source of truth — JSONL is just the wire format
between them.

### Don't use `bd import` as part of normal operation

`bd import` is a bootstrap path: load a JSONL snapshot into a fresh or
re-initialised database. If you find yourself running it routinely to
get a peer's latest issues, you've drifted off the Dolt-native sync
channel — fix the underlying remote / credential / network issue
instead of rebuilding the local database from a JSONL snapshot each
time. Reaching for `bd import` regularly is a smell, not a workflow.

### Don't propose a third-party Dolt hosting workaround

Dolt sync uses *your existing git remote*. The Dolt commit graph lives
under `refs/dolt/data` on the same GitHub / GitLab / SSH remote that
hosts your code — no separate Dolt server, no DoltHub account, no
side-channel sync service required. Reach for a hosted Dolt service
(or a self-run `dolt sql-server`) only when you have a concrete
requirement that the default doesn't cover (e.g. multiple agents
writing simultaneously from different processes or machines that can't
all reach the same git remote). Start with the default; only add
infrastructure when something measurable forces you to.

### Don't commit `.beads/dolt/` to git

The Dolt database directory lives next to your code, but it does not
travel through git's commit flow. `bd init` adds `.beads/dolt/` and
`.beads/embeddeddolt/` to `.gitignore` automatically. Committing those
directories would bloat the repo with binary chunk files and break
Dolt's expectations about exclusive on-disk ownership — sync moves
through `bd dolt push` / `bd dolt pull` (refs under `refs/dolt/data`)
instead. See [GIT_INTEGRATION.md](GIT_INTEGRATION.md) for the full
boundary between git-tracked and Dolt-tracked state.

---

**See also:** [FAQ.md](FAQ.md) for general questions,
[TROUBLESHOOTING.md](TROUBLESHOOTING.md) when sync misbehaves.
