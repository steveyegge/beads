# Session Handoff: Plugin-Based Issue Tracker Integration

**Date**: 2026-01-17
**Branch**: `feat/plugin-based-issue-tracker-integration`
**Commit**: `1b9dc633`

---

## What Was Completed

Implemented the full plugin-based architecture for issue tracker integrations per the plan in the previous session.

### Files Created (18 total, ~4100 lines)

**Core Framework** (`internal/tracker/`):
| File | Purpose |
|------|---------|
| `stats.go` | Shared types: SyncStats, Conflict, DependencyInfo, IssueConversion |
| `tracker.go` | IssueTracker interface (the plugin contract) |
| `mapper.go` | FieldMapper interface for field conversion |
| `config.go` | TrackerConfig wrapper for config/env access |
| `registry.go` | Plugin registry with auto-discovery via init() |
| `sync.go` | SyncEngine with unified sync/conflict resolution logic |
| `tracker_test.go` | Framework unit tests |
| `registry_test.go` | Registry unit tests |

**Linear Plugin** (`internal/tracker/linear/`):
| File | Purpose |
|------|---------|
| `linear.go` | IssueTracker implementation wrapping internal/linear |
| `mapper.go` | LinearMapper adapting existing MappingConfig |

**Jira Plugin** (`internal/tracker/jira/`):
| File | Purpose |
|------|---------|
| `types.go` | Jira API types (Issue, Status, Priority, etc.) |
| `client.go` | Go-native REST client (replaces Python scripts!) |
| `mapper.go` | JiraMapper with configurable mappings |
| `jira.go` | IssueTracker implementation |

**Azure DevOps Plugin** (`internal/tracker/azuredevops/`):
| File | Purpose |
|------|---------|
| `types.go` | Work item API types |
| `client.go` | REST client with WIQL query support |
| `mapper.go` | AzureDevOpsMapper for field conversion |
| `azuredevops.go` | IssueTracker implementation |

---

## Test Status

- ✅ All existing `internal/linear` tests pass (28 tests)
- ✅ New framework tests pass (5 tests)
- ✅ Go build succeeds for all tracker packages
- ⚠️ Full project build has pre-existing gozstd dependency issue (unrelated)

---

## Architecture Overview

```
internal/tracker/
├── tracker.go      # IssueTracker interface
├── mapper.go       # FieldMapper interface
├── sync.go         # SyncEngine (shared sync logic)
├── stats.go        # Shared types
├── config.go       # Config wrapper
├── registry.go     # Plugin registry
├── linear/         # Linear plugin
│   ├── linear.go   # IssueTracker impl
│   └── mapper.go   # LinearMapper
├── jira/           # Jira plugin (Go-native)
│   ├── jira.go     # IssueTracker impl
│   ├── client.go   # REST client
│   ├── mapper.go   # JiraMapper
│   └── types.go    # API types
└── azuredevops/    # Azure DevOps plugin
    ├── azuredevops.go
    ├── client.go
    ├── mapper.go
    └── types.go
```

---

## What's Next (Phase 2: CLI Integration)

The framework is complete. Next phase is updating the CLI commands to use SyncEngine.

### 1. Update `cmd/bd/linear.go`
```go
// Before: ~650 lines with inline sync logic
// After: ~200 lines delegating to SyncEngine

tracker := &linear.LinearTracker{}
tracker.Init(ctx, config)
engine := tracker.NewSyncEngine(tracker, config, store, actor)
result, err := engine.Sync(ctx, tracker.SyncOptions{...})
```

**Files to delete after refactor:**
- `cmd/bd/linear_sync.go` (383 lines → moves to SyncEngine)
- `cmd/bd/linear_conflict.go` (190 lines → moves to SyncEngine)

### 2. Update `cmd/bd/jira.go`
Same pattern. Keep Python scripts as `--use-python` fallback for transition.

### 3. Add `cmd/bd/azuredevops.go`
New command: `bd azuredevops sync --pull/--push`

---

## Key Code References

| Location | Description |
|----------|-------------|
| `internal/tracker/sync.go:44-180` | SyncEngine.Sync() main orchestration |
| `internal/tracker/sync.go:182-245` | DetectConflicts() implementation |
| `internal/tracker/sync.go:247-340` | doPull() - import logic |
| `internal/tracker/sync.go:342-440` | doPush() - export logic |
| `internal/tracker/linear/linear.go` | Example IssueTracker implementation |
| `cmd/bd/linear.go:140-361` | Current CLI to refactor (runLinearSync) |

---

## Notes

- The `bd sync --from-main` failed due to a pre-existing database migration issue, not related to this work
- All new code follows existing patterns from `internal/linear/` and `internal/storage/`
- Plugins auto-register via init() - just import the package to enable
