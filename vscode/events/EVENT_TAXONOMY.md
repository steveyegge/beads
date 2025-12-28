# Beads Event Taxonomy

Complete reference for all lifecycle events in a Beads-First application.

## Log Format

Events are logged to `.beads/events.log` in pipe-delimited format:

```
TIMESTAMP|EVENT_CODE|ISSUE_ID|AGENT_ID|SESSION_ID|DETAILS
```

Example:
```
2024-12-11T15:04:02Z|sk.bootup.activated|none|steve|1733929442|
2024-12-11T15:04:03Z|bd.issue.create|bd-0001|claude-abc|1733929442|InitApp epic
```

---

## Event Categories

| Prefix | Category | Description |
|--------|----------|-------------|
| `ep.*` | Epoch | Application lifecycle boundaries |
| `ss.*` | Session | Agent session lifecycle |
| `sk.*` | Skill | Claude skill activations |
| `bd.*` | Beads | Issue tracker operations |
| `gt.*` | Git | Version control operations |
| `hk.*` | Hook | Git hook triggers |
| `gd.*` | Guard | Enforcement/constraint checks |
| `rc.*` | Recovery | Failure recovery operations |

---

## Epoch Events (ep.*)

Application-level lifecycle boundaries.

| Code | Description | When |
|------|-------------|------|
| `ep.init.start` | InitApp ceremony beginning | Ceremony start |
| `ep.init.structure` | InitApp issue structure created | After tasks created |
| `ep.init.complete` | Epoch established, InitApp closed | Ceremony end |
| `ep.version` | Epoch version tag created | After tagging |

---

## Session Events (ss.*)

Individual agent session lifecycle.

| Code | Description | When |
|------|-------------|------|
| `ss.start` | Session initiated | Session begin |
| `ss.bootup.start` | Bootup ritual begins | Bootup start |
| `ss.bootup.ground` | pwd executed | Bootup step 1 |
| `ss.bootup.sync` | git pull + bd sync executed | Bootup step 2 |
| `ss.bootup.orient` | bd ready queried | Bootup step 3 |
| `ss.bootup.select` | Issue selected for work | Bootup step 4 |
| `ss.bootup.verify` | Health check executed | Bootup step 5 |
| `ss.bootup.complete` | Bootup ritual finished | Bootup end |
| `ss.work.start` | Implementation work begins | Work start |
| `ss.work.progress` | Periodic progress checkpoint | During work |
| `ss.discovered` | New issue filed during work | Discovery |
| `ss.circuitbreaker.triggered` | Circuit breaker activated | After 3rd failed attempt |
| `ss.circuitbreaker.deferred` | Issue documented and filed | BLOCKED_N.md created |
| `ss.circuitbreaker.resume` | Resuming primary work | Circuit breaker complete |
| `ss.landing.start` | Landing ritual begins | Landing start |
| `ss.landing.test` | Quality gates executed | Landing step 2 |
| `ss.landing.update` | Beads state updated | Landing step 3 |
| `ss.landing.sync` | Final sync + push | Landing step 4 |
| `ss.landing.handoff` | Handoff document generated | Landing step 5 |
| `ss.landing.complete` | Landing ritual finished | Landing end |
| `ss.end` | Session terminated | Session end |

---

## Skill Events (sk.*)

Claude skill activations and violations.

| Code | Description | When |
|------|-------------|------|
| `sk.bootup.activated` | beads-bootup skill loaded | Skill load |
| `sk.bootup.blocked` | InitApp not complete, blocking | Guard triggered |
| `sk.landing.activated` | beads-landing skill loaded | Skill load |
| `sk.scope.activated` | beads-scope skill loaded | Skill load |
| `sk.scope.violation` | Worked outside selected issue | Violation |
| `sk.scope.discovery` | Properly filed discovered work | Good behavior |
| `sk.initapp.activated` | beads-init-app skill loaded | Skill load |

---

## Beads Events (bd.*)

Issue tracker operations.

| Code | Description | When |
|------|-------------|------|
| `bd.init` | bd init executed | Initialization |
| `bd.sync.start` | Sync operation begins | Sync start |
| `bd.sync.complete` | Sync operation finished | Sync end |
| `bd.sync.conflict` | Merge conflict detected | Sync failure |
| `bd.issue.create` | New issue created | Issue creation |
| `bd.issue.update` | Issue modified | Issue update |
| `bd.issue.close` | Issue closed | Issue closure |
| `bd.dep.add` | Dependency added | Dep management |
| `bd.dep.remove` | Dependency removed | Dep management |
| `bd.ready.query` | bd ready executed | Query |
| `bd.ready.empty` | No ready issues (all blocked) | Query result |
| `bd.doctor.pass` | bd doctor succeeded | Health check |
| `bd.doctor.fail` | bd doctor found problems | Health check |

---

## Git Events (gt.*)

Version control operations.

| Code | Description | When |
|------|-------------|------|
| `gt.pull.start` | git pull begins | Pull start |
| `gt.pull.complete` | git pull finished | Pull end |
| `gt.pull.conflict` | Merge conflict detected | Pull failure |
| `gt.commit.start` | git commit begins | Commit start |
| `gt.commit.complete` | git commit finished | Commit end |
| `gt.push.start` | git push begins | Push start |
| `gt.push.complete` | git push finished | Push end |
| `gt.push.reject` | Push rejected (needs pull) | Push failure |
| `gt.tag.create` | Tag created | Tagging |
| `gt.stash.create` | Stash created | Recovery |
| `gt.stash.apply` | Stash applied | Recovery |
| `gt.reset.hard` | Hard reset executed | Recovery |

---

## Hook Events (hk.*)

Git hook triggers.

| Code | Description | When |
|------|-------------|------|
| `hk.pre-commit.start` | pre-commit hook triggered | Hook start |
| `hk.pre-commit.pass` | pre-commit checks passed | Hook pass |
| `hk.pre-commit.fail` | pre-commit checks failed | Hook fail |
| `hk.post-commit` | post-commit hook triggered | Hook run |
| `hk.pre-push.start` | pre-push hook triggered | Hook start |
| `hk.pre-push.pass` | pre-push checks passed | Hook pass |
| `hk.pre-push.fail` | pre-push checks failed (blocked) | Hook fail |
| `hk.post-merge` | post-merge hook triggered | Hook run |

---

## Guard Events (gd.*)

Enforcement and constraint checks.

| Code | Description | When |
|------|-------------|------|
| `gd.initapp.check` | Checking if InitApp is complete | Guard check |
| `gd.initapp.blocked` | Work blocked - InitApp incomplete | Guard fail |
| `gd.initapp.passed` | InitApp complete, work allowed | Guard pass |
| `gd.scope.check` | Checking scope constraint | Guard check |
| `gd.scope.violation` | Working outside selected issue | Guard fail |
| `gd.landing.required` | Landing not complete, push blocked | Guard fail |

---

## Recovery Events (rc.*)

Failure recovery operations performed by the beads-recovery skill.

| Code | Description | When |
|------|-------------|------|
| `rc.detect.complete` | Failure state detection finished | Skill activation |
| `rc.recover.sync` | bd sync during recovery | Recovery step |
| `rc.recover.commit` | Recovery commit created | Recovery step |
| `rc.recover.push` | Changes pushed during recovery | Recovery step |
| `rc.recover.revert` | git revert executed | Destructive recovery |
| `rc.recover.reset` | git reset executed | Destructive recovery |
| `rc.recover.import` | bd import executed | State restoration |
| `rc.recover.conflict` | Merge conflict resolved | Conflict recovery |
| `rc.recover.reopen` | Issue reopened | Issue recovery |
| `rc.recover.update` | Issue updated during recovery | Issue recovery |
| `rc.recover.scope` | Scope violation resolved | Scope recovery |
| `rc.recover.stash` | Changes stashed | Session abandonment |
| `rc.recover.abandon` | Session abandoned, issues reset | Session abandonment |
| `rc.recover.assess` | State assessment completed | Context exhaustion |
| `rc.recover.checkpoint` | Checkpoint saved | Context exhaustion |
| `rc.recover.complete` | Recovery finished | Recovery end |
| `rc.recover.cancelled` | Recovery cancelled by user | Recovery abort |

---

## Querying Events

### View all events
```bash
cat .beads/events.log
```

### Filter by category
```bash
grep "|sk\." .beads/events.log  # Skill events
grep "|hk\." .beads/events.log  # Hook events
grep "|bd\." .beads/events.log  # Beads events
```

### Filter by issue
```bash
grep "|bd-0001|" .beads/events.log
```

### Filter by session
```bash
grep "|1733929442|" .beads/events.log
```

### Count events by type
```bash
cut -d'|' -f2 .beads/events.log | sort | uniq -c | sort -rn
```

---

## Adding Custom Events

For project-specific events, use a custom prefix:

```bash
./scripts/beads-log-event.sh "proj.build.start" "none" "webpack build"
./scripts/beads-log-event.sh "proj.deploy.complete" "none" "deployed to staging"
```

Suggested custom prefixes:
- `proj.*` - Project-specific events
- `ci.*` - CI/CD events
- `test.*` - Test execution events
- `review.*` - Code review events

---

## Event Retention

Events are append-only. For long-running projects, consider:

1. **Archival:** Move old events to `.beads/events-archive/YYYY-MM.log`
2. **Rotation:** Keep only last N days in active log
3. **Compression:** `gzip .beads/events-YYYY-MM.log`

Future: Beads may provide built-in event compaction similar to issue compaction.
