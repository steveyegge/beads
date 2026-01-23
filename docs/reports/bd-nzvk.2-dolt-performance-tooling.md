# Dolt Performance Tooling Report (bd-nzvk.2)

## Summary

Built diagnostic tools to measure and profile Dolt performance in the beads codebase.

## Tools Created

### 1. `bd doctor --perf` (Auto-detect Backend)

The existing `--perf` flag now automatically detects whether the backend is SQLite or Dolt and runs the appropriate diagnostics.

```bash
bd doctor --perf
```

### 2. `bd doctor --perf-dolt` (Dolt-Specific)

Dedicated Dolt performance diagnostics with CPU profiling:

```bash
bd doctor --perf-dolt
```

**Output includes:**
- Backend mode (embedded vs server)
- Server status (running/not running)
- Platform info (OS, arch, Go version, Dolt version)
- Database statistics (issue counts, database size)
- Operation timings:
  - Connection/bootstrap time
  - Ready-work query time
  - List open issues time
  - Show single issue time
  - Complex filter query time
  - Dolt log query time
- Performance assessment with recommendations
- CPU profile file for flamegraph analysis

### 3. `bd doctor --perf-compare` (Mode Comparison)

Compares embedded vs server mode performance side-by-side:

```bash
bd doctor --perf-compare
```

**Output includes:**
- Embedded mode metrics
- Server mode metrics (if server is running)
- Speedup comparison between modes

### 4. Regular Doctor Checks

Added Dolt-specific checks to the regular `bd doctor` command:

- **Dolt Connection**: Verifies connectivity to Dolt database
- **Dolt Schema**: Checks all required tables are present
- **Dolt-JSONL Sync**: Compares issue count between Dolt and JSONL
- **Dolt Status**: Reports uncommitted Dolt changes
- **Dolt Performance**: Quick performance check with recommendations

## Files Created/Modified

### New Files
- `cmd/bd/doctor/perf_dolt.go` - Dolt performance diagnostics implementation

### Modified Files
- `cmd/bd/doctor.go` - Added flags and integration
- `cmd/bd/doctor/types.go` - Added `CategoryPerformance`

## Implementation Details

### DoltPerfMetrics Structure

```go
type DoltPerfMetrics struct {
    Backend          string        // "dolt-embedded" or "dolt-server"
    ServerMode       bool          // Connected via sql-server
    ServerStatus     string        // "running" or "not running"
    Platform         string        // OS/arch
    GoVersion        string        // Go runtime version
    DoltVersion      string        // Dolt version
    TotalIssues      int
    OpenIssues       int
    ClosedIssues     int
    Dependencies     int
    DatabaseSize     string

    // Timing metrics (milliseconds)
    ConnectionTime   int64         // Time to establish connection
    ReadyWorkTime    int64         // GetReadyWork equivalent
    ListOpenTime     int64         // List open issues
    ShowIssueTime    int64         // Get single issue
    ComplexQueryTime int64         // Complex filter query
    CommitLogTime    int64         // dolt_log query

    ProfilePath      string        // CPU profile file path
}
```

### Key Functions

- `RunDoltPerformanceDiagnostics(path string, enableProfiling bool)` - Main entry point
- `runDoltServerDiagnostics(metrics, host, port)` - Server mode diagnostics
- `runDoltEmbeddedDiagnostics(metrics, doltDir)` - Embedded mode diagnostics
- `PrintDoltPerfReport(metrics)` - Formatted output
- `CompareDoltModes(path)` - Side-by-side comparison
- `CheckDoltPerformance(path)` - Quick health check

### Performance Assessment Logic

The tool provides recommendations based on measured metrics:

1. **High bootstrap time (>500ms)** in embedded mode → Suggest server mode
2. **Server running but not used** → Suggest enabling server mode
3. **Slow ready-work query (>200ms)** → Check indexes
4. **Slow complex query (>500ms)** → Review query patterns
5. **Many closed issues (>4000)** → Suggest cleanup

## Usage Examples

```bash
# Quick performance check as part of regular doctor
bd doctor

# Detailed Dolt performance diagnostics with profiling
bd doctor --perf-dolt

# Auto-detect backend and run appropriate perf diagnostics
bd doctor --perf

# Compare embedded vs server mode
bd doctor --perf-compare

# View CPU profile flamegraph
go tool pprof -http=:8080 beads-dolt-perf-*.prof
```

## Next Steps

The tooling is now ready for use in the experiments phase (bd-nzvk.3).

Recommended experiments:
1. Baseline measurements using `bd doctor --perf-dolt`
2. Server vs embedded comparison using `bd doctor --perf-compare`
3. Index impact analysis by reviewing query times
4. Connection pool tuning in server mode
