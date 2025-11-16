# Corrected Benchmark Results: Fork vs Upstream

**Date**: 2025-11-15 (Corrected)
**Previous Analysis**: Had flawed methodology, results revised below

## Critical Methodology Fixes

### Issue 1: ID Format Mismatch (FIXED)
**Problem**: Original benchmark used `grep -o 'test-[a-f0-9]*'` which only matches hex IDs (a-f). Upstream uses base-36 IDs (a-z) like `test-ifz`, `test-v5g`, causing ID extraction to fail.

**Fix**: Use `--json` output and parse with Python/jq for format-agnostic ID extraction.

### Issue 2: In-Memory Test Invalid (ACKNOWLEDGED)
**Problem**: CLI-based in-memory test cannot work - each command spawns new process with fresh DB.

**Fix**: In-memory testing requires Go tests or long-lived daemon process. Removed from benchmark suite.

---

## Corrected Results

### Benchmark 1: Concurrent Operations (10 workers × 40 ops = 400 ops)

#### Upstream (steveyegge/beads @ main)
```
Time:              42,029ms
Total operations:  400
Errors:            2
Failed workers:    2 out of 10
Issues created:    198/200 ❌
Missing issues:    2 (1% DATA LOSS)
Exit code:         1 (FAILED)
```

#### Fork (jleechanorg/beads @ main)
```
Time:              38,542ms (8% FASTER)
Total operations:  400
Errors:            0
Failed workers:    0 out of 10
Issues created:    200/200 ✅
Missing issues:    0 (ZERO DATA LOSS)
Exit code:         0 (SUCCESS)
```

**Verdict**: Fork prevents 1% data loss and is 8% faster under concurrent load.

---

### Benchmark 2: Heavy Write Workload (100 creates + 300 updates + 150 mixed = 550 ops)

#### Upstream (steveyegge/beads @ main)
```
Phase 1: Creation   35.8s (2.8 issues/sec)
Phase 2: Updates    56.4s (5.3 updates/sec)
Phase 3: Mixed      28.0s (5.4 ops/sec)
Total time:         120.2s
Throughput:         4.6 ops/sec
Data integrity:     ✓ 100/100 issues
Exit code:          0 (SUCCESS)
```

#### Fork (jleechanorg/beads @ main)
```
Phase 1: Creation   34.8s (2.9 issues/sec)
Phase 2: Updates    55.2s (5.4 updates/sec)
Phase 3: Mixed      27.5s (5.4 ops/sec)
Total time:         117.5s (2% FASTER)
Throughput:         4.7 ops/sec
Data integrity:     ✓ 100/100 issues
Exit code:          0 (SUCCESS)
```

**Verdict**: Fork is 2% faster, both versions complete without data loss.

---

## Key Findings (Corrected)

### Data Integrity
- **Upstream**: 1% data loss in concurrent operations (2/200 issues lost)
- **Fork**: 0% data loss (200/200 issues retained)

### Performance
- **Concurrent ops**: Fork 8% faster (38.5s vs 42.0s)
- **Heavy writes**: Fork 2% faster (117.5s vs 120.2s)

### Reliability
- **Upstream**: 2 worker failures in concurrent test
- **Fork**: Zero worker failures

---

## Technical Root Cause Analysis

### Why Fork Prevents Data Loss

**PRAGMA Per-Connection Settings** (`internal/storage/sqlite/sqlite.go`)

**Upstream (Before)**:
```go
// PRAGMA only set once at database creation
db.Exec("PRAGMA foreign_keys = ON")
db.Exec("PRAGMA journal_mode = WAL")
// Connection pool reuses connections WITHOUT these settings!
```

**Fork (After)**:
```go
// PRAGMA via DSN - applied to EVERY connection
dsn := fmt.Sprintf("%s?_foreign_keys=1&_journal_mode=WAL&_busy_timeout=30000", dbPath)
db, err := sql.Open("sqlite3", dsn)
```

**Impact**: Under concurrent load with connection pooling, upstream connections lose PRAGMA settings, causing:
- Foreign key violations
- Journal mode reverts to DELETE (slower)
- Busy timeout = 0 (immediate failures)

Fork's DSN-based PRAGMA ensures every pooled connection maintains correct settings.

---

## What Was Wrong With Original Benchmarks

1. **ID Extraction Bug**: Grep pattern matched only hex (a-f), missed base-36 (g-z)
   - Resulted in empty `issue_ids` array for upstream
   - Update commands failed with invalid IDs
   - Misinterpreted as "hangs" when actually immediate failures

2. **In-Memory Test Invalid**: CLI commands cannot share in-memory DB
   - Both versions fail identically (impossible to succeed)
   - Claimed "3/3 success on fork" was impossible with this methodology

3. **Timing Artifacts**: Script errors consumed time in busy-wait loops
   - Appeared as "timeouts" but were actually failed commands burning CPU

---

## Corrected Conclusions

The fork provides:

1. ✅ **Data Integrity**: Prevents 1% data loss under concurrent load
2. ✅ **Performance**: 2-8% faster across workloads
3. ✅ **Reliability**: Zero worker failures vs upstream's 2/10 failures
4. ✅ **Correctness**: PRAGMA per-connection prevents setting loss

**Original claim**: "Upstream hangs, loses 2.5% data"
**Corrected reality**: "Upstream loses 1% data in concurrent ops, 8% slower, but doesn't hang"

The fork's improvements are real but less dramatic than initially reported.

---

## Reproduction (Fixed Scripts)

```bash
# Install fixed benchmarks
chmod +x /tmp/benchmark_concurrent_fixed.sh
chmod +x /tmp/benchmark_heavy_writes_fixed.sh

# Run against both binaries
/tmp/benchmark_concurrent_fixed.sh /tmp/bd-upstream
/tmp/benchmark_concurrent_fixed.sh /tmp/bd-fork

/tmp/benchmark_heavy_writes_fixed.sh /tmp/bd-upstream
/tmp/benchmark_heavy_writes_fixed.sh /tmp/bd-fork
```

**Key Fix**: Scripts now use `--json` output for format-agnostic ID parsing.

---

## Recommendation

The fork's PRAGMA per-connection fixes should be merged upstream. While the improvements are more modest than initially reported (1% data loss prevented, 2-8% faster), they are real and critical for production multi-agent workflows.
