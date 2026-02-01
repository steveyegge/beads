package rpc

import (
	"fmt"
	"math"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// SlowQueryCallback is called when a slow query is detected.
// Parameters: operation name, latency duration, timestamp
type SlowQueryCallback func(operation string, latency time.Duration, timestamp time.Time)

// SlowQueryRecord captures details of a slow query for analysis
type SlowQueryRecord struct {
	Operation string        `json:"operation"`
	LatencyMS float64       `json:"latency_ms"`
	Timestamp time.Time     `json:"timestamp"`
}

// Metrics holds all telemetry data for the daemon
type Metrics struct {
	mu sync.RWMutex

	// Request metrics
	requestCounts  map[string]int64           // operation -> count
	requestErrors  map[string]int64           // operation -> error count
	requestLatency map[string][]time.Duration // operation -> latency samples (bounded slice)
	maxSamples     int

	// Connection metrics
	totalConns    int64
	rejectedConns int64

	// Slow query profiling
	slowQueryThreshold time.Duration           // queries exceeding this are logged (0 = disabled)
	slowQueryCounts    map[string]int64        // operation -> slow query count
	recentSlowQueries  []SlowQueryRecord       // bounded list of recent slow queries
	maxSlowQueries     int                     // max recent slow queries to track
	slowQueryCallback  SlowQueryCallback       // optional callback for slow query logging

	// System start time (for uptime calculation)
	startTime time.Time
}

// DefaultSlowQueryThreshold is the default threshold for slow query detection (100ms)
const DefaultSlowQueryThreshold = 100 * time.Millisecond

// NewMetrics creates a new metrics collector
func NewMetrics() *Metrics {
	return &Metrics{
		requestCounts:      make(map[string]int64),
		requestErrors:      make(map[string]int64),
		requestLatency:     make(map[string][]time.Duration),
		maxSamples:         1000, // Keep last 1000 samples per operation
		slowQueryCounts:    make(map[string]int64),
		recentSlowQueries:  make([]SlowQueryRecord, 0),
		maxSlowQueries:     100, // Keep last 100 slow queries
		slowQueryThreshold: DefaultSlowQueryThreshold,
		startTime:          time.Now(),
	}
}

// SetSlowQueryThreshold sets the threshold for slow query detection.
// Queries exceeding this duration will be counted and logged.
// Set to 0 to disable slow query detection.
func (m *Metrics) SetSlowQueryThreshold(threshold time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slowQueryThreshold = threshold
}

// SetSlowQueryCallback sets the callback function for slow query logging.
// The callback is invoked (outside the lock) when a slow query is detected.
func (m *Metrics) SetSlowQueryCallback(cb SlowQueryCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.slowQueryCallback = cb
}

// RecordRequest records a request (successful or failed)
func (m *Metrics) RecordRequest(operation string, latency time.Duration) {
	now := time.Now()
	var callback SlowQueryCallback
	var isSlow bool

	m.mu.Lock()

	m.requestCounts[operation]++

	// Add latency sample to bounded slice
	samples := m.requestLatency[operation]
	if len(samples) >= m.maxSamples {
		// Drop oldest sample to maintain max size
		samples = samples[1:]
	}
	samples = append(samples, latency)
	m.requestLatency[operation] = samples

	// Check for slow query
	if m.slowQueryThreshold > 0 && latency >= m.slowQueryThreshold {
		isSlow = true
		m.slowQueryCounts[operation]++

		// Add to recent slow queries (bounded)
		record := SlowQueryRecord{
			Operation: operation,
			LatencyMS: float64(latency) / float64(time.Millisecond),
			Timestamp: now,
		}
		if len(m.recentSlowQueries) >= m.maxSlowQueries {
			// Drop oldest record
			m.recentSlowQueries = m.recentSlowQueries[1:]
		}
		m.recentSlowQueries = append(m.recentSlowQueries, record)

		// Copy callback reference for use outside lock
		callback = m.slowQueryCallback
	}

	m.mu.Unlock()

	// Invoke callback outside the lock to avoid deadlocks
	if isSlow && callback != nil {
		callback(operation, latency, now)
	}
}

// RecordError records a failed request
func (m *Metrics) RecordError(operation string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.requestErrors[operation]++
}

// RecordConnection records a new connection
func (m *Metrics) RecordConnection() {
	atomic.AddInt64(&m.totalConns, 1)
}

// RecordRejectedConnection records a rejected connection (max conns reached)
func (m *Metrics) RecordRejectedConnection() {
	atomic.AddInt64(&m.rejectedConns, 1)
}

// Snapshot returns a point-in-time snapshot of all metrics
func (m *Metrics) Snapshot(activeConns int) MetricsSnapshot {
	// Copy data under a short critical section
	m.mu.RLock()

	// Build union of all operations (from both counts and errors)
	opsSet := make(map[string]struct{})
	for op := range m.requestCounts {
		opsSet[op] = struct{}{}
	}
	for op := range m.requestErrors {
		opsSet[op] = struct{}{}
	}

	// Copy counts, errors, and latency slices
	countsCopy := make(map[string]int64, len(opsSet))
	errorsCopy := make(map[string]int64, len(opsSet))
	latCopy := make(map[string][]time.Duration, len(opsSet))
	slowCountsCopy := make(map[string]int64, len(m.slowQueryCounts))

	for op := range opsSet {
		countsCopy[op] = m.requestCounts[op]
		errorsCopy[op] = m.requestErrors[op]
		// Deep copy the latency slice
		if samples := m.requestLatency[op]; len(samples) > 0 {
			latCopy[op] = append([]time.Duration(nil), samples...)
		}
	}

	// Copy slow query data
	for op, count := range m.slowQueryCounts {
		slowCountsCopy[op] = count
	}
	slowThreshold := m.slowQueryThreshold
	recentSlowCopy := make([]SlowQueryRecord, len(m.recentSlowQueries))
	copy(recentSlowCopy, m.recentSlowQueries)

	m.mu.RUnlock()

	// Compute statistics outside the lock
	uptime := time.Since(m.startTime)
	// Round up uptime and enforce minimum of 1 second
	// This prevents flaky tests on fast systems (especially Windows VMs)
	uptimeSeconds := math.Ceil(uptime.Seconds())
	if uptimeSeconds == 0 {
		uptimeSeconds = 1
	}

	// Calculate per-operation stats
	operations := make([]OperationMetrics, 0, len(opsSet))
	var totalSlowQueries int64
	for op := range opsSet {
		count := countsCopy[op]
		errors := errorsCopy[op]
		samples := latCopy[op]
		slowCount := slowCountsCopy[op]
		totalSlowQueries += slowCount

		// Ensure success count is never negative
		successCount := count - errors
		if successCount < 0 {
			successCount = 0
		}

		opMetrics := OperationMetrics{
			Operation:      op,
			TotalCount:    count,
			ErrorCount:    errors,
			SuccessCount:  successCount,
			SlowQueryCount: slowCount,
		}

		// Calculate latency percentiles if we have samples
		if len(samples) > 0 {
			opMetrics.Latency = calculateLatencyStats(samples)
		}

		operations = append(operations, opMetrics)
	}

	// Sort by total count (most frequent first)
	sort.Slice(operations, func(i, j int) bool {
		return operations[i].TotalCount > operations[j].TotalCount
	})

	// Get memory stats
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	return MetricsSnapshot{
		Timestamp:            time.Now(),
		UptimeSeconds:        uptimeSeconds,
		Operations:           operations,
		TotalConns:           atomic.LoadInt64(&m.totalConns),
		ActiveConns:          activeConns,
		RejectedConns:        atomic.LoadInt64(&m.rejectedConns),
		MemoryAllocMB:        memStats.Alloc / 1024 / 1024,
		MemorySysMB:          memStats.Sys / 1024 / 1024,
		GoroutineCount:       runtime.NumGoroutine(),
		SlowQueryThresholdMS: float64(slowThreshold) / float64(time.Millisecond),
		TotalSlowQueries:     totalSlowQueries,
		RecentSlowQueries:    recentSlowCopy,
	}
}

// MetricsSnapshot is a point-in-time view of all metrics
type MetricsSnapshot struct {
	Timestamp      time.Time          `json:"timestamp"`
	UptimeSeconds  float64            `json:"uptime_seconds"`
	Operations     []OperationMetrics `json:"operations"`
	TotalConns     int64              `json:"total_connections"`
	ActiveConns    int                `json:"active_connections"`
	RejectedConns  int64              `json:"rejected_connections"`
	MemoryAllocMB  uint64             `json:"memory_alloc_mb"`
	MemorySysMB    uint64             `json:"memory_sys_mb"`
	GoroutineCount int                `json:"goroutine_count"`
	Cache          *CacheStats        `json:"cache,omitempty"`
	// Slow query profiling stats
	SlowQueryThresholdMS float64            `json:"slow_query_threshold_ms"`
	TotalSlowQueries     int64              `json:"total_slow_queries"`
	RecentSlowQueries    []SlowQueryRecord  `json:"recent_slow_queries,omitempty"`
}

// OperationMetrics holds metrics for a single operation type
type OperationMetrics struct {
	Operation      string       `json:"operation"`
	TotalCount     int64        `json:"total_count"`
	SuccessCount   int64        `json:"success_count"`
	ErrorCount     int64        `json:"error_count"`
	SlowQueryCount int64        `json:"slow_query_count,omitempty"`
	Latency        LatencyStats `json:"latency,omitempty"`
}

// LatencyStats holds latency percentile data in milliseconds
type LatencyStats struct {
	MinMS float64 `json:"min_ms"`
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	P99MS float64 `json:"p99_ms"`
	MaxMS float64 `json:"max_ms"`
	AvgMS float64 `json:"avg_ms"`
}

// calculateLatencyStats computes percentiles from latency samples and returns milliseconds
func calculateLatencyStats(samples []time.Duration) LatencyStats {
	if len(samples) == 0 {
		return LatencyStats{}
	}

	// Sort samples
	sorted := make([]time.Duration, len(samples))
	copy(sorted, samples)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	n := len(sorted)
	// Calculate percentiles with defensive clamping
	p50Idx := minInt(n-1, n*50/100)
	p95Idx := minInt(n-1, n*95/100)
	p99Idx := minInt(n-1, n*99/100)

	// Calculate average
	var sum time.Duration
	for _, d := range sorted {
		sum += d
	}
	avg := sum / time.Duration(n)

	// Convert to milliseconds
	toMS := func(d time.Duration) float64 {
		return float64(d) / float64(time.Millisecond)
	}

	return LatencyStats{
		MinMS: toMS(sorted[0]),
		P50MS: toMS(sorted[p50Idx]),
		P95MS: toMS(sorted[p95Idx]),
		P99MS: toMS(sorted[p99Idx]),
		MaxMS: toMS(sorted[n-1]),
		AvgMS: toMS(avg),
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TopSlowQueries returns the N slowest queries from recent history, sorted by latency (slowest first)
func (m *Metrics) TopSlowQueries(n int) []SlowQueryRecord {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.recentSlowQueries) == 0 {
		return nil
	}

	// Copy and sort by latency descending
	result := make([]SlowQueryRecord, len(m.recentSlowQueries))
	copy(result, m.recentSlowQueries)
	sort.Slice(result, func(i, j int) bool {
		return result[i].LatencyMS > result[j].LatencyMS
	})

	// Return top N
	if n > 0 && n < len(result) {
		return result[:n]
	}
	return result
}

// TopFrequentOperations returns the N most frequent operations, sorted by count (most frequent first)
func (m *Metrics) TopFrequentOperations(n int) []OperationMetrics {
	snapshot := m.Snapshot(0)
	ops := snapshot.Operations // already sorted by frequency

	if n > 0 && n < len(ops) {
		return ops[:n]
	}
	return ops
}

// SlowQuerySummary returns summary stats about slow queries
type SlowQuerySummary struct {
	ThresholdMS      float64          `json:"threshold_ms"`
	TotalSlowQueries int64            `json:"total_slow_queries"`
	SlowByOperation  map[string]int64 `json:"slow_by_operation"`
}

// GetSlowQuerySummary returns a summary of slow query statistics
func (m *Metrics) GetSlowQuerySummary() SlowQuerySummary {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var total int64
	byOp := make(map[string]int64, len(m.slowQueryCounts))
	for op, count := range m.slowQueryCounts {
		byOp[op] = count
		total += count
	}

	return SlowQuerySummary{
		ThresholdMS:      float64(m.slowQueryThreshold) / float64(time.Millisecond),
		TotalSlowQueries: total,
		SlowByOperation:  byOp,
	}
}

// PeriodicStatsSummary returns a human-readable summary suitable for periodic logging
func (m *Metrics) PeriodicStatsSummary(activeConns int) string {
	snapshot := m.Snapshot(activeConns)

	// Calculate total requests and overall avg latency
	var totalRequests int64
	var totalLatencySum float64
	var opCount int
	for _, op := range snapshot.Operations {
		totalRequests += op.TotalCount
		if op.Latency.AvgMS > 0 {
			totalLatencySum += op.Latency.AvgMS
			opCount++
		}
	}

	avgLatency := float64(0)
	if opCount > 0 {
		avgLatency = totalLatencySum / float64(opCount)
	}

	// Calculate requests/second
	requestsPerSec := float64(0)
	if snapshot.UptimeSeconds > 0 {
		requestsPerSec = float64(totalRequests) / snapshot.UptimeSeconds
	}

	return fmt.Sprintf("STATS: requests=%d rate=%.2f/s avg_latency=%.2fms slow_queries=%d threshold=%.0fms conns=%d/%d mem=%dMB",
		totalRequests,
		requestsPerSec,
		avgLatency,
		snapshot.TotalSlowQueries,
		snapshot.SlowQueryThresholdMS,
		activeConns,
		snapshot.TotalConns,
		snapshot.MemoryAllocMB,
	)
}
