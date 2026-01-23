# Dolt Performance Synthesis Report (bd-nzvk.4)

## Executive Summary

This report synthesizes findings from research (bd-nzvk.1), tooling development (bd-nzvk.2), and experiment planning (bd-nzvk.3) into an actionable optimization plan for Dolt performance in the beads codebase.

### Key Findings

1. **Server mode is significantly faster** than embedded mode for connection/bootstrap
2. **Batch operations are critical** - per-row updates are extremely slow
3. **Dolt is ~10% slower than MySQL overall** - faster on writes, slower on reads
4. **Existing beads code is well-designed** but has room for optimization

### Top Recommendations

| Priority | Optimization | Impact | Effort |
|----------|--------------|--------|--------|
| P0 | Enable server mode by default | High | Low |
| P1 | Add batch write API | High | Medium |
| P2 | Implement query caching | Medium | Medium |
| P3 | Profile and optimize hot paths | Medium | High |

## Detailed Findings

### From Research (bd-nzvk.1)

#### Architecture Understanding

- **Storage**: Prolly trees (content-addressed B-trees)
- **Overhead source**: Content-addressing for versioning
- **Chunk-based storage**: ~1% of DB size needed in RAM for indexes

#### Performance Characteristics

| Aspect | Dolt vs MySQL | Notes |
|--------|---------------|-------|
| Reads | 33% slower | Due to Prolly tree overhead |
| Writes | 10% faster | Efficient change tracking |
| Transactions | 60% slower | 40% of MySQL throughput |
| Bootstrap | 500-1000ms | Per-connection in embedded mode |

#### Known Bottlenecks

1. **Per-statement disk writes** - Dolt commits after each UPDATE
2. **Auto-increment overhead** - 50x slower than explicit keys
3. **AUTOCOMMIT impact** - Worst-case for bulk operations
4. **Bootstrap overhead** - Significant in embedded mode

### From Tooling (bd-nzvk.2)

#### Tools Available

1. `bd doctor --perf` - Auto-detect backend, run appropriate diagnostics
2. `bd doctor --perf-dolt` - Dolt-specific diagnostics with profiling
3. `bd doctor --perf-compare` - Compare embedded vs server mode
4. `bd doctor` - Includes Dolt health checks

#### Metrics Collected

- Connection/bootstrap time
- Query execution times (ready-work, list, show, complex)
- Dolt-specific queries (dolt_log)
- Server status
- Database size

### From Experiments (bd-nzvk.3)

#### Experiment Framework

- Systematic benchmark script (`scripts/dolt-benchmark.sh`)
- Multiple iterations for statistical significance
- Embedded vs server mode comparison
- Results template for tracking

#### Expected Results (from research)

- Server mode: 5-10x faster connection
- Batch operations: 10-100x faster than per-row
- Explicit keys: ~50x faster than auto-increment

## Optimization Plan

### Phase 1: Quick Wins (P0)

#### 1.1 Enable Server Mode by Default

**Current state**: Server mode available but not default
**Change**: Set `BEADS_DOLT_SERVER_MODE=1` by default when Dolt backend detected

**Implementation**:
```go
// In internal/storage/dolt/store.go
func New(ctx context.Context, cfg *Config) (*DoltStore, error) {
    // Auto-enable server mode unless explicitly disabled
    if !cfg.ServerMode && os.Getenv("BEADS_DOLT_SERVER_MODE") != "0" {
        cfg.ServerMode = true
    }
    // ...
}
```

**Expected impact**: 5-10x faster bootstrap

#### 1.2 Document Server Mode Setup

**Current state**: Server mode requires manual setup
**Change**: Add clear documentation and auto-start capability

**Files to update**:
- `docs/DOLT-BACKEND.md` (create)
- `cmd/bd/doctor/perf_dolt.go` (add recommendations)

### Phase 2: Code Optimizations (P1)

#### 2.1 Batch Write API

**Problem**: Current storage API does single-row operations
**Solution**: Add batch versions of write operations

**New methods**:
```go
// BatchCreateIssues creates multiple issues in a single transaction
func (s *DoltStore) BatchCreateIssues(ctx context.Context, issues []*storage.Issue) error

// BatchUpdateIssues updates multiple issues in a single transaction
func (s *DoltStore) BatchUpdateIssues(ctx context.Context, issues []*storage.Issue) error
```

**Expected impact**: 10-100x faster for bulk operations

#### 2.2 Optimize Import Path

**Problem**: `bd import` does per-issue inserts
**Solution**: Use batch API for imports

**Change in** `cmd/bd/import.go`:
- Collect issues in memory
- Use `BatchCreateIssues` for insertion
- Wrap in single transaction

### Phase 3: Caching Layer (P2)

#### 3.1 Query Result Cache

**Problem**: Repeated queries for same data
**Solution**: Add in-memory cache for common queries

**Implementation**:
```go
type CachedDoltStore struct {
    *DoltStore
    cache *lru.Cache  // or sync.Map for simpler impl
    ttl   time.Duration
}

// GetReadyWork with caching
func (s *CachedDoltStore) GetReadyWork(ctx context.Context) ([]*storage.Issue, error) {
    if cached := s.cache.Get("ready_work"); cached != nil {
        return cached.([]*storage.Issue), nil
    }
    result, err := s.DoltStore.GetReadyWork(ctx)
    if err == nil {
        s.cache.Set("ready_work", result, s.ttl)
    }
    return result, err
}
```

**Expected impact**: Significant for repeated read operations

#### 3.2 Connection Pool Optimization

**Problem**: Fixed connection pool settings
**Solution**: Make pool settings configurable

**New config options**:
```yaml
# config.yaml
dolt:
  max-open-conns: 10
  max-idle-conns: 5
  conn-max-lifetime: 5m
```

### Phase 4: Profile-Guided Optimization (P3)

#### 4.1 CPU Profiling Analysis

**Process**:
1. Run `bd doctor --perf-dolt` to generate profile
2. Analyze with `go tool pprof`
3. Identify hotspots
4. Optimize hot paths

**Common hotspots to check**:
- JSON serialization/deserialization
- SQL query building
- Result scanning
- Memory allocations

#### 4.2 Memory Optimization

**Process**:
1. Add memory profiling to benchmark
2. Identify allocation hotspots
3. Reduce allocations in hot paths
4. Consider object pooling for common types

## Configuration Recommendations

### Production Settings

```yaml
# .beads/config.yaml
storage-backend: dolt

# Environment variables
export BEADS_DOLT_SERVER_MODE=1
```

### Server Mode Setup

```bash
# Start Dolt server (recommended for production)
dolt sql-server --host 127.0.0.1 --port 3306 --data-dir .beads/dolt &

# Or use systemd service for persistence
# See docs/DOLT-BACKEND.md for systemd unit file
```

### Memory Sizing

| Database Size | Recommended RAM |
|---------------|-----------------|
| < 100MB | 512MB |
| 100MB - 1GB | 1-2GB |
| 1GB - 10GB | 2-4GB |
| > 10GB | 10-20% of DB size |

## Success Metrics

### Performance Targets

| Metric | Current (est.) | Target | Monitoring |
|--------|----------------|--------|------------|
| Bootstrap | 500-1000ms | <200ms | `bd doctor --perf` |
| Ready-work query | 100-500ms | <50ms | `bd doctor --perf` |
| List open (100) | 50-200ms | <50ms | `bd doctor --perf` |
| Bulk import (1000) | 30-60s | <10s | Benchmark script |

### Monitoring Plan

1. **Regular benchmarks**: Run `scripts/dolt-benchmark.sh` weekly
2. **Profile on degradation**: If metrics exceed targets, profile and analyze
3. **Track database growth**: Monitor `.beads/dolt` size vs performance
4. **User feedback**: Track performance-related issues

## Implementation Timeline

### Week 1-2: Phase 1 (Quick Wins)
- [ ] Enable server mode by default
- [ ] Document server setup
- [ ] Add server mode health check

### Week 3-4: Phase 2 (Code Optimizations)
- [ ] Implement batch write API
- [ ] Update import to use batch API
- [ ] Add batch update support

### Week 5-6: Phase 3 (Caching)
- [ ] Design cache layer
- [ ] Implement for common queries
- [ ] Add cache configuration

### Week 7+: Phase 4 (Profile-Guided)
- [ ] Run production profiling
- [ ] Analyze and optimize hotspots
- [ ] Document findings

## Risk Assessment

| Risk | Impact | Mitigation |
|------|--------|------------|
| Server mode complexity | Medium | Clear documentation, auto-start |
| Cache invalidation bugs | High | Conservative TTL, thorough testing |
| Migration issues | High | Feature flags, rollback plan |
| Memory growth | Medium | Monitoring, configurable limits |

## Appendix: File References

### Reports
- `docs/reports/bd-nzvk.1-dolt-performance-research.md` - Research findings
- `docs/reports/bd-nzvk.2-dolt-performance-tooling.md` - Tooling documentation
- `docs/reports/bd-nzvk.3-dolt-performance-experiments.md` - Experiment methodology

### Source Files
- `internal/storage/dolt/store.go` - Main Dolt storage implementation
- `internal/storage/dolt/server.go` - Server management
- `cmd/bd/doctor/perf_dolt.go` - Performance diagnostics

### Scripts
- `scripts/dolt-benchmark.sh` - Benchmark script

---

*Generated by bd-nzvk.4 synthesis task*
*Date: 2026-01-22*
