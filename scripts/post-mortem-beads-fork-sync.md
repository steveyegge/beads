# Post-mortem: Fork sync athosmartins/beads ← steveyegge/beads

**Bead:** dc-4sks
**Operator:** crew/batista (whatsapp_automation rig)
**Started:** 2026-05-06
**Source clone:** /tmp/beads-sync (worktree of ~/gt/beads)
**Strategy:** fast-forward (zero local divergence)

## Pre-sync state

- `fork/main` @ `e823b524` — Merge PR #2437: Fix config package shadow in compact command
- 0 commits ahead of `origin/main`
- 1276 commits behind `origin/main`
- bd binary version: 0.59.0
- Upstream version: 1.0.3 (`6a642174`)

## Why FF and not merge

Unlike dc-bsza (gastown sync), the beads fork had **zero local-only commits**.
`git rev-list origin/main..fork/main --count` returned 0. Every commit on
fork/main was already an ancestor of origin/main. This made the sync a pure
fast-forward — no merge commit, no conflicts, no audit of preserved changes.

Verified before push:
```
$ git merge-base --is-ancestor fork/main origin/main && echo OK
OK
```

## Backup

- Local branch: implicit (worktree was at fork/main pre-push, ~/gt/beads still
  has fork/main remote-tracking ref)
- Fork remote: `fork/backup-fork-main-pre-sync-2026-05-06` @ `e823b524`

Recovery if needed:
```bash
git fetch fork backup-fork-main-pre-sync-2026-05-06
git push fork backup-fork-main-pre-sync-2026-05-06:main --force-with-lease
```

## Worktree isolation

The canonical clone at `~/gt/beads` had ~94 uncommitted files on branch
`fix/dolt-unix-socket-timewait` (chrome's WIP). To avoid disturbing that
work, all sync operations ran in a temporary worktree at `/tmp/beads-sync`.

Cleanup is straightforward — `git worktree remove /tmp/beads-sync` after
the sync lands.

## Verification

- Build: `make build` (with `-tags=gms_pure_go`) clean, `bd version 1.0.3 (6a642174)`
- Tests: `go test -tags=gms_pure_go -count=1 -short ./...` passes except for
  one flaky concurrency test (`TestDoltServer_ConcurrentStart_SameRootDir_OneWins`)
  that passes in isolation — environmental, unrelated to sync.
- Install: `make install` → bd installed to `~/.local/bin/bd`
- Smoke tests after triggering metadata recreation:
  - `bd list` works
  - `bd doctor` 62 passed / 12 warnings / 1 error (the error is repo
    fingerprint mismatch in WA's local clone — unrelated to sync, per-repo
    metadata that needs `bd migrate --update-repo-id`)
  - `gt mq list whatsapp_automation` works (returns empty queue, expected
    after dc-kwgc drain)

## Schema migration notes

bd 1.0+ persists `issue_prefix` via `bd init --prefix` directly (the upstream
`gt install` change in PR #3829). The `local_metadata` table lives in the
working set as a dolt_ignore'd table and gets recreated on first bd command
after a server session boundary — this triggered automatically when we ran
`bd list` post-install.

No manual schema migration commands were needed. Future bd version bumps
inside 1.x.x line should likewise migrate transparently.

## Post-merge cleanup

After Mayor verifies the sync:
- [ ] Delete `fork/backup-fork-main-pre-sync-2026-05-06` after a carência
      period (recovery branch, low cost to keep — recommend keeping
      indefinitely like the gastown backup)
- [ ] Remove worktree at `/tmp/beads-sync` (`git worktree remove`)
- [ ] WIP author (chrome on `fix/dolt-unix-socket-timewait`) needs to rebase
      onto new fork/main at some point — that's their work, not part of
      this sync.

## Recurrence prevention

- Mayor noted in dc-4sks bead: "Mayor falhou em monitorar isso preventivamente
  — agora tem cross-fork check na rotina."
- For an additional layer, consider a monthly automated check that compares
  `fork/main` to `origin/main` for any registered fork in the gt town and
  escalates to Mayor when divergence exceeds a threshold (e.g., 50 commits
  behind).

## Refs

- dc-bsza (gastown sync — different pattern: 45 ahead + 203 behind, merge
  with conflicts)
- dc-xlcj (auto-convoy work, blocked by this — should now merge cleanly)
- Discovery: digo via dc-wisp-9o2 mail (2026-05-06 ~20:42)
