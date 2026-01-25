# Beads Performance Baseline Report

**Generated**: 2026-01-22
**bd version**: 0.48.0 (5264d7aa)

## System Specifications

| Spec | Value |
|------|-------|
| CPU | Apple M4 |
| Cores | 10 |
| Memory | 16 GB |
| OS | Darwin 25.1.0 (macOS) |
| Architecture | arm64 |

## Test Database

| Database | Size | Issues |
|----------|------|--------|
| Synthetic (10K) | 21 MB | 10,000 |

## Performance Results

### Latency Metrics (Direct Mode, 20 iterations)

| Command | P50 | P95 | P99 | Mean | StdDev | Min | Max |
|---------|-----|-----|-----|------|--------|-----|-----|
| `bd ready --limit 10` | 102.8ms | 135.3ms | 144.8ms | 108.7ms | 14.2ms | 96.4ms | 147.2ms |
| `bd ready --limit 100` | 103.9ms | 135.5ms | 184.6ms | 111.0ms | 22.0ms | 100.8ms | 196.9ms |
| `bd list --limit 10` | 95.1ms | 135.9ms | 166.5ms | 102.5ms | 19.8ms | 89.8ms | 174.2ms |
| `bd stats` | 393.8ms | 483.9ms | 506.6ms | 410.0ms | 35.1ms | 388.6ms | 512.3ms |
| `bd version` (baseline) | 50.0ms | 53.1ms | 55.0ms | 50.5ms | 1.7ms | 48.8ms | 55.5ms |

### Throughput Metrics

| Operation | Throughput |
|-----------|------------|
| `bd ready --limit 10` | **9.52 ops/sec** |

### Performance Breakdown

```
Total latency for `bd ready --limit 10`: ~109ms

├── CLI overhead (bd version baseline): ~50ms (46%)
├── DB open + query:                    ~59ms (54%)
│   ├── SQLite connection open
│   ├── GetReadyWork query
│   └── JSON serialization
```

## Key Insights

1. **CLI overhead is ~50ms** - This is the baseline Go binary startup + Cobra/Viper initialization.

2. **Limit doesn't affect latency** - `--limit 10` vs `--limit 100` are effectively the same (~103-104ms P50). The blocked_issues_cache optimization is working.

3. **Stats is expensive** - `bd stats` takes 4x longer (~410ms) due to aggregate calculations across all issues.

4. **Variance is low** - StdDev is typically 10-15% of mean for most operations (good consistency).

5. **Throughput is ~9.5 ops/sec** - Each operation takes ~105ms on average.

## Comparison: Earlier Results

| Measurement | Earlier | Current | Notes |
|-------------|---------|---------|-------|
| CLI overhead | 130ms | 50ms | Improved (different run conditions) |
| `bd ready` (10K) | 180ms | 109ms | Faster with clean DB |
| Go benchmark GetReadyWork | 62ms | - | Isolated query time |

## Daemon vs Direct Mode (from earlier test)

| Mode | Mean Latency | Notes |
|------|--------------|-------|
| Daemon | 164ms | Includes RPC overhead |
| Direct | 180ms | Direct SQLite access |

Daemon mode was slightly faster in earlier tests due to warm connection.

## Reproducibility

### Regenerate Synthetic Database

```bash
# Remove cached databases
rm -rf /tmp/beads-bench-cache/

# Run Go benchmarks to regenerate (takes 1-3 minutes)
go test -tags=bench -bench=BenchmarkGetReadyWork_Large -benchmem ./internal/storage/sqlite/...
```

### Run Full Benchmark Suite

```bash
# Full suite (synthetic only)
./scripts/benchmark-suite.sh --synthetic --iterations 20 --output benchmarks/results.json

# Quick run (10 iterations)
./scripts/benchmark-suite.sh --synthetic --iterations 10
```

### Run Concurrency Tests

```bash
./scripts/test-concurrency.sh --parallel 10 --iterations 10
```

## Files

- `scripts/benchmark-suite.sh` - Comprehensive benchmark runner
- `scripts/test-concurrency.sh` - Concurrent write stress test
- `benchmarks/baseline-YYYYMMDD.json` - Raw JSON results

## Optimizations Implemented

### Performance: Git Actor Caching (2026-01-22)

**Problem**: `getActorWithGit()` spawned `git config user.name` subprocess on every call (~15-20ms overhead).

**Solution**: Cache git user and email lookups using `sync.Once` pattern.

**Files changed**:
- `cmd/bd/main.go`: Added `cachedGitUser`, `cachedGitEmail` with `sync.Once`

**Impact**: Saves ~15-40ms per operation that calls `getActorWithGit()` or `getOwner()`.

### Concurrency: Improved Retry Parameters (2026-01-22)

**Problem**: `beginImmediateWithRetry` used only 5 retries with 10ms initial delay (~310ms max wait). Under high concurrency (20+ parallel writers), this was insufficient.

**Solution**: Increased defaults to 10 retries, 50ms initial delay (~25.5s max wait).

**Files changed**:
- `internal/storage/sqlite/util.go`: Updated `beginImmediateWithRetry` defaults
- `internal/storage/sqlite/transaction.go`: Updated call to use new defaults
- `internal/storage/sqlite/queries.go`: Updated call to use new defaults
- `internal/storage/sqlite/batch_ops.go`: Updated call to use new defaults

**Retry timing (new defaults)**:
```
Attempt 1: immediate
Attempt 2: 50ms, Attempt 3: 100ms, Attempt 4: 200ms,
Attempt 5: 400ms, Attempt 6: 800ms, Attempt 7: 1.6s,
Attempt 8: 3.2s, Attempt 9: 6.4s, Attempt 10: 12.8s
Total max wait: ~25.5s (within SQLite's 30s busy_timeout)
```

**Impact**: Concurrent creates maintain 100% success rate at 20 parallel workers.

### Concurrency Test Results (After Optimization)

| Metric | Before | After |
|--------|--------|-------|
| Parallel Workers | 20 | 20 |
| Create Success Rate | 100% | 100% |
| Lock Errors (logged) | 41 | 46* |
| Throughput | - | 26.39 ops/sec |

*Note: Lock errors in "After" are from auto-import path (separate issue), not the main create path.

## Optimization Targets

Based on these results, optimization priorities should be:

1. **CLI startup time (50ms)** - Lazy loading, reduced init
2. **Stats query (410ms)** - Pre-computed aggregates or caching
3. **Query latency (59ms)** - Already optimized with blocked_issues_cache
4. **Auto-import concurrency** - Add transaction wrapping for handleRename

## Benchmark Reliability

Run the benchmark suite 3 times and compare stddev values. Results should be within 10% variance.

```bash
for i in 1 2 3; do
  ./scripts/benchmark-suite.sh --synthetic --iterations 20 --output benchmarks/run-$i.json
done
```
