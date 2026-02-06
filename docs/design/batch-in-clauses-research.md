# Research: Batch IN Clauses in Daemon Label Queries

**Issue:** fhc-17yk.1 (parent: fhc-17yk - Dolt pod CPU-saturated)
**Date:** 2026-02-06
**Author:** polecat/obsidian

## Executive Summary

The bd-daemon sends `SELECT issue_id, label FROM labels WHERE issue_id IN (...)`
with 70-121+ IDs per query. On Dolt's prolly-tree storage engine, these large IN
clauses hang for 25-47 minutes, saturating the 2-core CPU limit. 18 stuck copies
of the query were found running simultaneously due to retry amplification and
lack of export-level mutual exclusion.

## 1. Dynamic IN Clause Query Sites (Dolt Backend)

### CRITICAL - Unbounded, called during every sync export

| Function | File:Line | Query | Called From |
|----------|-----------|-------|-------------|
| GetLabelsForIssues | labels.go:50 | `SELECT issue_id, label FROM labels WHERE issue_id IN (?)` | server_sync.go:257 (ALL issues) |
| GetCommentsForIssues | events.go:192 | `SELECT id, issue_id, author, text, created_at FROM comments WHERE issue_id IN (?)` | server_sync.go:274 (ALL issues) |
| ClearDirtyIssuesByID | dirty.go:46 | `DELETE FROM dirty_issues WHERE issue_id IN (?)` | server_sync.go:320 (ALL exported issues) |

### HIGH - Unbounded, called during list operations

| Function | File:Line | Query | Called From |
|----------|-----------|-------|-------------|
| GetDependencyCounts | dependencies.go:267 | TWO queries: `SELECT issue_id, COUNT(*) ... WHERE issue_id IN (?)` and `WHERE depends_on_id IN (?)` | server_issues_epics.go:1952 (all search results) |
| GetDependencyRecordsForIssues | dependencies.go:225 | `SELECT ... FROM dependencies WHERE issue_id IN (?)` | server_sync.go export path |

### MEDIUM - Subset of issues or variable size

| Function | File:Line | Query | Called From |
|----------|-----------|-------|-------------|
| GetEpicProgress | queries.go:445 | CTE with `WHERE d.depends_on_id IN (?)` | server_issues_epics.go:1961 (epic subset) |
| GetIssuesByIDs | dependencies.go:540 | `SELECT ... FROM issues WHERE id IN (?)` | GetBlockedIssues (variable size) |

**None of these functions have any batching or size limits.**

## 2. Safe IN Clause Size for Dolt

**Recommendation: 20 IDs per batch (conservative). Test up to 50.**

Rationale:
- Dolt's prolly-tree performs `log_k(n)` traversal per lookup. 121 IDs = 121
  separate tree traversals, each reading multiple content-addressed chunks.
- MySQL guidance recommends alternatives above 100 items; Dolt's overhead makes
  a lower threshold prudent.
- No Dolt-specific documentation on IN clause limits exists.
- Empirically: 70-121 IDs causes 25-47 minute hangs. Batches of 20 reduce the
  per-query work by 6x and keep each query well under the danger zone.

## 3. Batching Strategy

### Recommended approach: Sequential batch execution

```
Input: 121 issue IDs
Batch size: 20
→ 7 sequential queries (6 × 20 + 1 × 1)
→ Results merged client-side
```

**Why sequential, not concurrent:**
- 2-core Dolt pod is already CPU-saturated
- Concurrent queries would contend for CPU and prolly-tree chunk cache
- Sequential batches are predictable and debuggable

### Implementation: Generic helper

```go
// internal/storage/dolt/batch.go
func BatchQuery[T any](ctx context.Context, ids []string, batchSize int,
    fn func(ctx context.Context, batch []string) ([]T, error)) ([]T, error)
```

Apply to all 7 functions identified above.

## 4. Query Timeout Analysis

### Current state (insufficient)

| Layer | Timeout | Kills query? |
|-------|---------|-------------|
| RPC request context | 60s | NO - client-side only |
| MySQL readTimeout DSN | 30s | NO - I/O timeout, not query timeout |
| MySQL writeTimeout DSN | 30s | NO - I/O timeout, not query timeout |
| Dolt max_execution_time | NOT SUPPORTED | N/A |
| Database query level | NONE | N/A |

### Critical finding: go-sql-driver/mysql context cancellation is broken

When `context.WithTimeout` expires, go-sql-driver/mysql closes the client
connection but does **NOT** send `KILL QUERY` to the server. The query continues
executing on Dolt, consuming CPU until it finishes. This means:

- The 60s RPC timeout gives the *illusion* of protection
- The actual query runs for 25-47 minutes on the server
- Each retry creates another stuck query (3 retries × failed queries = pile-up)

Reference: github.com/go-sql-driver/mysql issues #731, #863, #1171

### Fix: KILL QUERY watchdog pattern

```go
connID := getConnectionID(db)
ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
defer cancel()
go func() {
    <-ctx.Done()
    db.ExecContext(background, "KILL QUERY ?", connID)
}()
rows, err := db.QueryContext(ctx, query, args...)
```

## 5. PRAGMA Bug

**Location:** cmd/bd/daemon_event_loop.go:247

The daemon health check (fires every 60s) sends `PRAGMA quick_check(1)` to the
database. PRAGMA is SQLite-only syntax - invalid on Dolt (MySQL-compatible).

The code has a fallback to `SELECT 1`, but:
1. The failed PRAGMA generates error spam every 60s
2. Wastes a query slot and adds latency
3. Backend type is known at startup - no need for try-then-fallback

**Fix:** Check backend type and use `SELECT 1` directly for Dolt.

## 6. Why 18 Stuck Queries Appear

The multiplication effect:

1. **No export mutual exclusion:** Unlike imports (which use `atomic.Bool` guard
   at server_export_import_auto.go:298), exports can run concurrently.
2. **FetchWithPolicy retries:** Each query retries 3 times (export/policy.go:39)
   with 100ms/200ms/400ms backoff.
3. **3 sequential queries per export:** labels, comments, dirty-clear.
4. **Math:** 2 concurrent exports × 3 queries × 3 retries = **18 stuck queries**

Each retry of a stuck query makes the pile-up worse since the original is still
running server-side (see context cancellation issue above).

## 7. Filed Subtasks

| ID | Type | Priority | Title |
|----|------|----------|-------|
| lo-tsk-batch_large_clauses_getlabelsforissues | task | P1 | Batch large IN clauses in GetLabelsForIssues, GetCommentsForIssues, ClearDirtyIssuesByID |
| lo-tsk-add_client_side_query_timeout_kill | task | P1 | Add client-side query timeout with KILL QUERY watchdog |
| lo-bug-fix_pragma_quick_check_sent_dolt_every | bug | P1 | Fix PRAGMA quick_check sent to Dolt every 60s in daemon health check |
| lo-tsk-add_export_level_mutual_exclusion | task | P1 | Add export-level mutual exclusion to prevent concurrent IN clause queries |
| lo-tsk-batch_getdependencycounts | task | P2 | Batch GetDependencyCounts and GetEpicProgress IN clauses |
| lo-tsk-fix_dolt_embedded_mode_connection_pool | task | P2 | Fix Dolt embedded mode connection pool: set ConnMaxLifetime |

## 8. Recommended Fix Order

1. **Export mutual exclusion** (quick win, stops pile-up immediately)
2. **PRAGMA bug fix** (quick win, removes noise)
3. **Batch IN clauses** (primary fix for the core problem)
4. **KILL QUERY watchdog** (defense in depth)
5. **Connection pool fix** (hardening)
6. **Batch remaining IN clauses** (completeness)
