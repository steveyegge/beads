# Dolt Performance Research Report (bd-nzvk.1)

## Executive Summary

Dolt is a version-controlled SQL database that uses content-addressed Prolly trees for storage. It trades some performance for versioning capabilities. Current benchmarks show Dolt is approximately **10% slower overall than MySQL** - faster on writes, slower on reads. The primary bottlenecks are transactional throughput (40% of MySQL) and startup/bootstrapping overhead.

## Dolt Architecture Overview

### Storage Engine: Prolly Trees

Dolt uses a specialized data structure called **Prolly trees** (probabilistic B-trees):

- **Content-addressed**: Chunks are identified by their hash
- **B-tree seek performance**: O(log n) lookups
- **Fast diffing**: Content addressing enables efficient version comparison
- **Merkle DAG structure**: Enables Git-like versioning with shared storage

**Performance implication**: The content-addressing adds overhead compared to traditional row-based storage, but enables versioning features.

### Chunk-Based Storage

- Data stored as compressed chunks using Snappy compression
- Table file indexes loaded into memory (~1% of database size)
- Each chunk has variable size with content-based identity
- Chunks compressed on write, decompressed on read

### Connection Modes

| Mode | Use Case | Characteristics |
|------|----------|-----------------|
| **Embedded** | Single-process CLI | Single writer, LOCK file, bootstrapping on each open |
| **Server** (`sql-server`) | Multi-client | Better caching, amortized startup, concurrent reads |

**Key finding**: Server mode significantly outperforms embedded mode because:
1. Bootstrapping overhead is amortized across queries
2. In-memory caching is maintained across connections
3. Connection state persists

## Current Performance Benchmarks

### Latency vs MySQL (from DoltHub benchmarks)

| Operation | Dolt vs MySQL | Notes |
|-----------|---------------|-------|
| Covering index scan | **0.3x** (faster) | 0.56ms vs 1.86ms |
| Table scan | **0.64x** (faster) | 22.28ms vs 34.95ms |
| Delete/insert | **0.78x** (faster) | |
| Read-write mixed | 1.26x slower | 11.65ms vs 9.22ms |
| Writes overall | ~10% faster | |
| Reads overall | ~33% slower | |

### Transactional Throughput (TPC-C)

- Dolt achieves **~40% of MySQL throughput** (40 TPS vs 100 TPS)
- This is the primary weakness
- 2.56x higher latency in transactional scenarios

### Optimization via PGO

Profile-Guided Optimization (Go 1.21+) provides:
- ~5% decrease in read latency
- ~1.5% decrease in write latency
- 7.8% increased throughput on TPC-C

## Known Performance Issues and Gotchas

### 1. Per-Statement Disk Writes (Critical)

**Problem**: Dolt writes to disk after every UPDATE statement, unlike MySQL which batches.

**Impact**: ORM/SQLAlchemy patterns that issue individual UPDATEs are extremely slow.

**Workaround**:
- Use batch SQL operations instead of per-row updates
- Use `dolt table import -u` with JSON for bulk updates
- Avoid auto-commit mode for write-heavy operations

### 2. Auto-Increment Key Generation

**Problem**: 50x slower when using auto-generated keys vs explicit IDs.

**Workaround**: Pre-generate primary keys in application logic.

### 3. Bootstrapping Overhead

**Problem**: Embedded mode performs significant work on each process startup:
- Loading table file indexes into memory
- Validating storage format
- Initializing Prolly tree structures

**Workaround**: Use server mode for production workloads.

### 4. Memory Usage Patterns

- Table file indexes use ~1% of database size in RAM
- SELECT DISTINCT can use excessive memory (80GB+ reported)
- Memory leaks have been reported in some versions
- GC pressure from frequent small allocations

### 5. AUTOCOMMIT Impact

**Worst-case pattern**: Large imports with AUTOCOMMIT enabled commits ChunkStore after each statement.

**Workaround**: Disable AUTOCOMMIT, batch statements in transactions.

### 6. Import Resource Utilization

- Bulk loading has "a number of resource utilization problems"
- Each write materializes new Prolly tree for each index
- Statement and transaction batching both matter
- Frequent small allocations increase GC overhead

## Beads-Specific Findings

### Current Implementation

The beads Dolt storage layer (`internal/storage/dolt/`) has:

**Good practices**:
- Server mode support with auto-start
- Idle timeout to release locks
- Retry logic with exponential backoff
- Index definitions on frequently-queried columns
- Connection pooling (limited in embedded mode)

**Potential issues**:
- Embedded mode uses `SetMaxOpenConns(1)` (single writer)
- Schema initialization runs on every connection
- Read-only mode still opens full connection
- No query batching in storage operations

### Connection Pool Settings

| Mode | MaxOpenConns | MaxIdleConns | ConnMaxLifetime |
|------|--------------|--------------|-----------------|
| Embedded | 1 | 1 | unlimited |
| Read-only | 2 | 1 | unlimited |
| Server | 10 | 5 | 5 minutes |

### Existing Performance Tooling

- `bd doctor perf` command exists but only supports SQLite
- CPU profiling via `runtime/pprof`
- Basic operation timing

## Configuration Recommendations

### Memory Sizing

- **Baseline**: 10-20% of database disk size as RAM
- For 100GB database: allocate 10-20GB RAM
- Deep history databases may need less (smaller HEAD)

### Disk Considerations

- Bulk inserts minimize garbage
- Individual inserts generate up to 10x disk waste
- Run `dolt gc` periodically to reclaim space

### Server Mode Settings

```yaml
# Recommended for production
BEADS_DOLT_SERVER_MODE=1
```

### Query Pattern Best Practices

1. **Batch writes** in transactions
2. **Avoid per-row updates** from ORMs
3. **Pre-generate keys** instead of auto-increment
4. **Use server mode** for concurrent access
5. **Disable AUTOCOMMIT** for bulk operations

## Dolt-Specific vs MySQL-Compatible Optimizations

### Dolt-Specific

- Server mode for multi-client scenarios
- `dolt gc` for storage reclamation
- Chunk caching (internal)
- Prolly tree optimization (internal)

### MySQL-Compatible

- Standard indexes work as expected
- Query optimization follows MySQL patterns
- EXPLAIN works for query analysis
- Standard SQL optimization applies

## Recommendations for Next Steps

### Tooling (bd-nzvk.2)

1. Extend `bd doctor perf` to support Dolt backend
2. Add Dolt-specific metrics: chunk cache hits, Prolly tree depth
3. Add server mode status/health checking
4. Implement slow query logging (manual, since Dolt lacks built-in)

### Experiments (bd-nzvk.3)

1. **Baseline**: Measure current CPU/memory/latency in beads
2. **Server vs Embedded**: Compare performance in typical beads workflows
3. **Query patterns**: Measure batch vs individual operations
4. **Connection pooling**: Test different pool configurations in server mode
5. **Index impact**: Profile queries with/without specific indexes

### Synthesis (bd-nzvk.4)

Priority optimizations to investigate:
1. Enable server mode by default
2. Batch storage operations where possible
3. Consider query caching layer
4. Profile startup overhead

## Sources

- [Dolt Latency Benchmarks](https://docs.dolthub.com/sql-reference/benchmarks/latency)
- [Dolt Architecture Overview](https://docs.dolthub.com/architecture/architecture)
- [DoltHub Blog: Profile-Guided Optimization](https://www.dolthub.com/blog/2024-02-02-profile-guided-optimization/)
- [DoltHub Blog: Sizing Your Dolt Instance](https://www.dolthub.com/blog/2023-12-06-sizing-your-dolt-instance/)
- [DoltHub Blog: Storage Layer Memory Optimizations](https://www.dolthub.com/blog/2022-02-28-dolt-storage-layer-memory-optimizations/)
- [DoltHub Blog: Embedded Dolt](https://www.dolthub.com/blog/2022-07-25-embedded/)
- [GitHub Issue #2751: Slow updates via SQLAlchemy](https://github.com/dolthub/dolt/issues/2751)
- [GitHub Issue #4491: Import resource utilization](https://github.com/dolthub/dolt/issues/4491)
- [GitHub Issue #6536: Performance comparison](https://github.com/dolthub/dolt/issues/6536)
- [GitHub Issue #7717: Slow query log request](https://github.com/dolthub/dolt/issues/7717)
