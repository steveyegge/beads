# Dolt Performance Experiments (bd-nzvk.3)

## Experiment Status

**Current backend**: SQLite (experiments require Dolt backend)

To run these experiments, first configure Dolt backend:
```bash
# Initialize with Dolt backend (new repo)
bd init --backend dolt

# Or convert existing repo
# (requires migration - see Dolt migration docs)
```

## Experiment Plan

### Experiment 1: Baseline Measurements

**Goal**: Establish current performance characteristics.

**Methodology**:
```bash
# Run with profiling enabled
bd doctor --perf-dolt

# Capture metrics:
# - Connection/bootstrap time
# - Query execution times
# - Memory usage (from profile)
# - CPU hotspots (from profile)
```

**Key Metrics**:
| Metric | Target | Acceptable |
|--------|--------|------------|
| Bootstrap time | <200ms | <500ms |
| Ready-work query | <50ms | <200ms |
| List open (100) | <50ms | <200ms |
| Show single issue | <20ms | <100ms |
| Complex filter | <100ms | <500ms |

### Experiment 2: Server vs Embedded Mode

**Goal**: Quantify server mode benefits.

**Methodology**:
```bash
# Start Dolt server
dolt sql-server --data-dir .beads/dolt &

# Run comparison
bd doctor --perf-compare

# Stop server
pkill -f "dolt sql-server"
```

**Expected Results** (based on research):
- Connection time: 5-10x faster in server mode
- Query time: Similar (may be slightly faster due to caching)
- Concurrent access: Only possible in server mode

### Experiment 3: Index Impact

**Goal**: Measure impact of indexes on common queries.

**Methodology**:
1. Record baseline query times
2. Add/remove indexes one at a time
3. Measure query time changes

**Indexes to test**:
- `idx_issues_status`
- `idx_issues_priority`
- `idx_issues_assignee`
- `idx_issues_created_at`

### Experiment 4: Query Pattern Comparison

**Goal**: Identify optimal query patterns.

**Tests**:
1. **Single row updates** vs **batch updates**
   - Expected: Batch updates significantly faster
   - Reason: Dolt commits per statement in single-update mode

2. **Auto-increment keys** vs **explicit keys**
   - Expected: Explicit keys much faster
   - Reason: Auto-increment has 50x overhead

3. **AUTOCOMMIT on** vs **AUTOCOMMIT off**
   - Expected: AUTOCOMMIT off faster for bulk operations
   - Reason: Avoids per-statement ChunkStore commits

### Experiment 5: Configuration Tuning

**Goal**: Find optimal configuration settings.

**Parameters to test**:
| Setting | Values to Test | Expected Impact |
|---------|----------------|-----------------|
| MaxOpenConns (server) | 1, 5, 10, 20 | Concurrency |
| MaxIdleConns | 1, 2, 5 | Connection reuse |
| ConnMaxLifetime | 0, 5min, 30min | Memory/reconnects |
| IdleTimeout | 0, 30s, 5min | Lock release |

### Experiment 6: Workload Simulation

**Goal**: Measure realistic beads usage patterns.

**Workloads**:
1. **CLI-heavy**: Many short-lived connections (typical `bd` usage)
2. **Daemon-heavy**: Long-lived connection with periodic queries
3. **Mixed**: Combination of daemon + CLI access

## Benchmark Script

Created a benchmark script for systematic testing:

```bash
#!/bin/bash
# scripts/dolt-benchmark.sh

# Check if Dolt backend
if ! bd doctor 2>&1 | grep -q "Dolt"; then
    echo "Error: Not a Dolt backend"
    exit 1
fi

echo "=== Dolt Performance Benchmark ==="
echo "Date: $(date -Iseconds)"
echo ""

# Run 5 iterations for statistical significance
for i in {1..5}; do
    echo "--- Iteration $i ---"
    bd doctor --perf-dolt 2>&1 | grep -E "(Connection|Ready|List|Show|Complex|dolt_log)"
    echo ""
done

# Run mode comparison if server available
if nc -z localhost 3306 2>/dev/null; then
    echo "--- Mode Comparison ---"
    bd doctor --perf-compare
fi
```

## Results Template

### Environment
- **OS**: [e.g., Linux 6.x]
- **CPU**: [e.g., AMD EPYC 7002 @ 2.25GHz]
- **RAM**: [e.g., 16GB]
- **Dolt Version**: [e.g., 1.x.x]
- **Go Version**: [e.g., 1.21.x]
- **Database Size**: [e.g., 10MB, 500 issues]

### Baseline Results (Embedded Mode)
| Metric | Run 1 | Run 2 | Run 3 | Run 4 | Run 5 | Avg |
|--------|-------|-------|-------|-------|-------|-----|
| Connection (ms) | | | | | | |
| Ready-work (ms) | | | | | | |
| List open (ms) | | | | | | |
| Show issue (ms) | | | | | | |
| Complex query (ms) | | | | | | |

### Server Mode Results
| Metric | Run 1 | Run 2 | Run 3 | Run 4 | Run 5 | Avg |
|--------|-------|-------|-------|-------|-------|-----|
| Connection (ms) | | | | | | |
| Ready-work (ms) | | | | | | |
| List open (ms) | | | | | | |
| Show issue (ms) | | | | | | |
| Complex query (ms) | | | | | | |

### Comparison Summary
| Metric | Embedded | Server | Speedup |
|--------|----------|--------|---------|
| Connection | | | x |
| Ready-work | | | x |
| List open | | | x |
| Show issue | | | x |
| Complex query | | | x |

## Preliminary Findings

Based on the research phase (bd-nzvk.1), expected findings:

1. **Server mode provides significant bootstrap improvement**
   - Embedded mode: 500-1000ms bootstrap per connection
   - Server mode: <50ms connection time
   - Reason: Amortized initialization, in-memory caching

2. **Query times similar between modes**
   - Once connected, query execution is comparable
   - Server mode may be slightly faster due to warm caches

3. **Write patterns matter significantly**
   - Batch writes: ~10-100x faster than per-row updates
   - Explicit keys: ~50x faster than auto-increment
   - Transaction batching: Major impact on throughput

4. **Memory usage scales with database size**
   - Table file indexes: ~1% of database size in RAM
   - Larger databases need proportionally more memory

## Recommendations for Running Experiments

1. **Isolate the test environment**
   - Stop other beads-related processes
   - Use a dedicated database clone

2. **Warm up before measuring**
   - Run a few queries first to populate caches
   - Measure subsequent runs for steady-state performance

3. **Control variables**
   - Same data set across tests
   - Document exact configuration
   - Note any concurrent activity

4. **Statistical significance**
   - Run at least 5 iterations
   - Report mean and standard deviation
   - Watch for outliers (GC pauses, etc.)

5. **Profile first, optimize second**
   - Use `go tool pprof` on generated profiles
   - Identify actual bottlenecks before optimizing
   - Focus on the top hotspots

## Next Steps

1. Configure a Dolt backend for testing
2. Run baseline experiments
3. Compare server vs embedded modes
4. Profile CPU and memory usage
5. Document findings in synthesis report (bd-nzvk.4)
