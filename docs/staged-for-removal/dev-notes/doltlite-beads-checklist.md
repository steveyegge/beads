# Doltlite Beads Checklist

Purpose: track runtime findings while the Beads backend migration to `doltlite`
is in progress.

Rule: agents working on `bd`, `gc`, mail, routing, backup, or config behavior
must update this checklist with concrete findings before ending their session.

## Current Policy

- Backups must stay off during the doltlite migration.
- Do not enable auto-backup in project, city, or user config.
- Treat any `CALL DOLT_BACKUP(...)`, `gc dolt ...`, or similar server-era
  instructions as suspect until explicitly revalidated for doltlite.

## Operational Fragment Locations

Keep both operational-awareness doltlite templates updated when changing agent
runtime guidance:

- Source template embedded into future `gc` pack artifacts:
  `/data/projects/t3code/packages/gascity-config/config/packs/gastown/template-fragments/operational-awareness-doltlite.template.md`
- Current city runtime template used by this Gas Town instance:
  `/home/ubuntu/.local/state/t3code/gascity/current/city/packs/gastown/template-fragments/operational-awareness-doltlite.template.md`

The source template is the long-term source of truth. The runtime template is
what current agents see. Update both when changing doltlite operating rules.

The packaged source city config also selects this fragment:
`/data/projects/t3code/packages/gascity-config/config/city.toml`

## How To Check Backup Is Off

Run these commands and record any drift:

```bash
bd config get backup.enabled
bd config get backup.git-push
sed -n '1,80p' /data/projects/beads-doltlite/.beads/config.yaml
sed -n '1,80p' /home/ubuntu/.local/state/t3code/gascity/current/city/.beads/config.yaml
sed -n '1,80p' /home/ubuntu/.config/bd/config.yaml
```

Expected state:

- `backup.enabled: false` in project config
- `backup.enabled: false` in city config
- `backup.enabled: false` in user config
- `backup.git-push: false` anywhere it is set
- no agent should rely on backup side effects as part of validation

## Findings

- 2026-05-01: deacon startup after `gc prime` carried stale Dolt env overrides
  (`BEADS_DOLT_PORT=35819`, `BEADS_DOLT_SERVER_PORT=35819`, `GC_DOLT_PORT=35819`)
  even though `gc doctor` and `gc dolt health` showed the live doltlite server
  on `127.0.0.1:41465`; `bd list` failed until those vars were overridden to
  the live port, so agent startup can be blocked by stale runtime port state.
- 2026-05-01: `bd ready --json` worked in refinery workspace, which confirms
  core `bd` reads are functional under current doltlite state.
- 2026-05-01: `bd config get mail.delegate` returned `mail.delegate (not set)`
  but also emitted `Warning: auto-backup failed: register backup remote: add
  backup backup_export: near "CALL": syntax error`, which shows a stale Dolt
  backup codepath still runs in doltlite mode.
- 2026-05-01: direct doltlite storage probe succeeded for message creation,
  `replies-to` dependency insertion, message search, and ack-like close/update.
- 2026-05-01: `cmd/bd/backup_auto.go` now skips post-run auto-backup entirely
  when the opened store is `*doltlite.DoltliteStore`, preventing stale
  `CALL DOLT_BACKUP(...)` warnings from read-only commands in doltlite mode.
- 2026-05-01: `cmd/bd/backup_export.go` now rejects backup export early for
  doltlite with a migration-specific error instead of falling through to
  Dolt-only backup SQL.
- 2026-05-01: doltlite branch, commit, remote, history, diff, and as-of paths
  must use native `SELECT dolt_*` functions and per-table TVFs. Do not recreate
  branch-per-file databases or synthetic `doltlite_commits` / `doltlite_refs`
  tables in the Beads adapter.

## Next Checks

- Verify post-command auto-backup is skipped cleanly, not merely failing with a warning.
- Audit `BackupStore` / `versioncontrolops.Backup*` call sites for unconditional
  `CALL DOLT_BACKUP(...)` usage in doltlite mode.
- Add first-class doltlite messaging tests instead of relying on Dolt-backed
  `newTestStore(...)` helpers.
