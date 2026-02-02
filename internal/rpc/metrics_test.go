package rpc

import (
	"testing"
	"time"
)

func TestMetricsRecording(t *testing.T) {
	m := NewMetrics()

	t.Run("record request", func(t *testing.T) {
		m.RecordRequest("create", 10*time.Millisecond)
		m.RecordRequest("create", 20*time.Millisecond)

		m.mu.RLock()
		count := m.requestCounts["create"]
		m.mu.RUnlock()

		if count != 2 {
			t.Errorf("Expected 2 requests, got %d", count)
		}
	})

	t.Run("record error", func(t *testing.T) {
		m.RecordError("create")

		m.mu.RLock()
		errors := m.requestErrors["create"]
		m.mu.RUnlock()

		if errors != 1 {
			t.Errorf("Expected 1 error, got %d", errors)
		}
	})

	t.Run("record connection", func(t *testing.T) {
		before := m.totalConns
		m.RecordConnection()
		after := m.totalConns

		if after != before+1 {
			t.Errorf("Expected connection count to increase by 1, got %d -> %d", before, after)
		}
	})

	t.Run("record rejected connection", func(t *testing.T) {
		before := m.rejectedConns
		m.RecordRejectedConnection()
		after := m.rejectedConns

		if after != before+1 {
			t.Errorf("Expected rejected count to increase by 1, got %d -> %d", before, after)
		}
	})
}

func TestMetricsSnapshot(t *testing.T) {
	m := NewMetrics()

	// Record some operations
	m.RecordRequest("create", 10*time.Millisecond)
	m.RecordRequest("create", 20*time.Millisecond)
	m.RecordRequest("update", 5*time.Millisecond)
	m.RecordError("create")
	m.RecordConnection()
	m.RecordRejectedConnection()

	// Take snapshot
	snapshot := m.Snapshot(3)

	t.Run("basic metrics", func(t *testing.T) {
		if snapshot.TotalConns < 1 {
			t.Error("Expected at least 1 total connection")
		}
		if snapshot.RejectedConns < 1 {
			t.Error("Expected at least 1 rejected connection")
		}
		if snapshot.ActiveConns != 3 {
			t.Errorf("Expected 3 active connections, got %d", snapshot.ActiveConns)
		}
	})

	t.Run("operation metrics", func(t *testing.T) {
		if len(snapshot.Operations) != 2 {
			t.Errorf("Expected 2 operations, got %d", len(snapshot.Operations))
		}

		// Find create operation
		var createOp *OperationMetrics
		for i := range snapshot.Operations {
			if snapshot.Operations[i].Operation == "create" {
				createOp = &snapshot.Operations[i]
				break
			}
		}

		if createOp == nil {
			t.Fatal("Expected to find 'create' operation")
		}

		if createOp.TotalCount != 2 {
			t.Errorf("Expected 2 total creates, got %d", createOp.TotalCount)
		}
		if createOp.ErrorCount != 1 {
			t.Errorf("Expected 1 error, got %d", createOp.ErrorCount)
		}
		if createOp.SuccessCount != 1 {
			t.Errorf("Expected 1 success, got %d", createOp.SuccessCount)
		}
	})

	t.Run("latency stats", func(t *testing.T) {
		var createOp *OperationMetrics
		for i := range snapshot.Operations {
			if snapshot.Operations[i].Operation == "create" {
				createOp = &snapshot.Operations[i]
				break
			}
		}

		if createOp == nil {
			t.Fatal("Expected to find 'create' operation")
		}

		// Should have latency stats
		if createOp.Latency.MinMS <= 0 {
			t.Error("Expected non-zero min latency")
		}
		if createOp.Latency.MaxMS <= 0 {
			t.Error("Expected non-zero max latency")
		}
		if createOp.Latency.AvgMS <= 0 {
			t.Error("Expected non-zero avg latency")
		}
	})

	t.Run("uptime", func(t *testing.T) {
		// The uptime calculation uses math.Ceil and ensures minimum 1 second
		// if any time has passed. This should always be >= 1.
		if snapshot.UptimeSeconds < 1 {
			t.Errorf("Expected uptime >= 1, got %f", snapshot.UptimeSeconds)
		}
	})

	t.Run("memory stats", func(t *testing.T) {
		// Memory stats can be 0 on some systems/timing, especially in CI
		// Just verify the fields are populated (even if zero)
		if snapshot.GoroutineCount <= 0 {
			t.Error("Expected positive goroutine count")
		}
		// MemoryAllocMB can legitimately be 0 due to GC timing, so don't fail on it
	})
}

func TestCalculateLatencyStats(t *testing.T) {
	t.Run("empty samples", func(t *testing.T) {
		stats := calculateLatencyStats([]time.Duration{})
		if stats.MinMS != 0 || stats.MaxMS != 0 {
			t.Error("Expected zero stats for empty samples")
		}
	})

	t.Run("single sample", func(t *testing.T) {
		samples := []time.Duration{10 * time.Millisecond}
		stats := calculateLatencyStats(samples)

		if stats.MinMS != 10.0 {
			t.Errorf("Expected min 10ms, got %f", stats.MinMS)
		}
		if stats.MaxMS != 10.0 {
			t.Errorf("Expected max 10ms, got %f", stats.MaxMS)
		}
		if stats.AvgMS != 10.0 {
			t.Errorf("Expected avg 10ms, got %f", stats.AvgMS)
		}
	})

	t.Run("multiple samples", func(t *testing.T) {
		samples := []time.Duration{
			5 * time.Millisecond,
			10 * time.Millisecond,
			15 * time.Millisecond,
			20 * time.Millisecond,
			100 * time.Millisecond,
		}
		stats := calculateLatencyStats(samples)

		if stats.MinMS != 5.0 {
			t.Errorf("Expected min 5ms, got %f", stats.MinMS)
		}
		if stats.MaxMS != 100.0 {
			t.Errorf("Expected max 100ms, got %f", stats.MaxMS)
		}
		if stats.AvgMS != 30.0 {
			t.Errorf("Expected avg 30ms, got %f", stats.AvgMS)
		}
		// P50 should be around 15ms (middle value)
		if stats.P50MS < 10.0 || stats.P50MS > 20.0 {
			t.Errorf("Expected P50 around 15ms, got %f", stats.P50MS)
		}
	})
}

func TestLatencySampleBounding(t *testing.T) {
	m := NewMetrics()
	m.maxSamples = 10 // Small size for testing

	// Add more samples than max
	for i := 0; i < 20; i++ {
		m.RecordRequest("test", time.Duration(i)*time.Millisecond)
	}

	m.mu.RLock()
	samples := m.requestLatency["test"]
	m.mu.RUnlock()

	if len(samples) != 10 {
		t.Errorf("Expected 10 samples (bounded), got %d", len(samples))
	}

	// Verify oldest samples were dropped (should have newest 10)
	expectedMin := 10 * time.Millisecond
	if samples[0] != expectedMin {
		t.Errorf("Expected oldest sample to be %v, got %v", expectedMin, samples[0])
	}
}

func TestMinHelper(t *testing.T) {
	if min(5, 10) != 5 {
		t.Error("min(5, 10) should be 5")
	}
	if min(10, 5) != 5 {
		t.Error("min(10, 5) should be 5")
	}
	if min(7, 7) != 7 {
		t.Error("min(7, 7) should be 7")
	}
}

func TestSlowQueryProfiling(t *testing.T) {
	t.Run("slow query detection", func(t *testing.T) {
		m := NewMetrics()
		m.SetSlowQueryThreshold(50 * time.Millisecond)

		// Fast query - should not be counted
		m.RecordRequest("fast_op", 10*time.Millisecond)

		// Slow query - should be counted
		m.RecordRequest("slow_op", 100*time.Millisecond)

		m.mu.RLock()
		slowCount := m.slowQueryCounts["slow_op"]
		fastSlowCount := m.slowQueryCounts["fast_op"]
		m.mu.RUnlock()

		if slowCount != 1 {
			t.Errorf("Expected 1 slow query for slow_op, got %d", slowCount)
		}
		if fastSlowCount != 0 {
			t.Errorf("Expected 0 slow queries for fast_op, got %d", fastSlowCount)
		}
	})

	t.Run("slow query callback", func(t *testing.T) {
		m := NewMetrics()
		m.SetSlowQueryThreshold(50 * time.Millisecond)

		var callbackCalled bool
		var callbackOp string
		var callbackLatency time.Duration

		m.SetSlowQueryCallback(func(op string, latency time.Duration, _ time.Time) {
			callbackCalled = true
			callbackOp = op
			callbackLatency = latency
		})

		// Trigger slow query
		m.RecordRequest("callback_test", 100*time.Millisecond)

		if !callbackCalled {
			t.Error("Expected slow query callback to be called")
		}
		if callbackOp != "callback_test" {
			t.Errorf("Expected callback operation 'callback_test', got '%s'", callbackOp)
		}
		if callbackLatency != 100*time.Millisecond {
			t.Errorf("Expected callback latency 100ms, got %v", callbackLatency)
		}
	})

	t.Run("recent slow queries tracking", func(t *testing.T) {
		m := NewMetrics()
		m.SetSlowQueryThreshold(50 * time.Millisecond)
		m.maxSlowQueries = 5 // Small size for testing

		// Add more slow queries than max
		for i := 0; i < 10; i++ {
			m.RecordRequest("test_op", time.Duration(100+i)*time.Millisecond)
		}

		m.mu.RLock()
		recentCount := len(m.recentSlowQueries)
		m.mu.RUnlock()

		if recentCount != 5 {
			t.Errorf("Expected 5 recent slow queries (bounded), got %d", recentCount)
		}
	})

	t.Run("slow query snapshot", func(t *testing.T) {
		m := NewMetrics()
		m.SetSlowQueryThreshold(50 * time.Millisecond)

		// Add some slow queries
		m.RecordRequest("op1", 100*time.Millisecond)
		m.RecordRequest("op2", 200*time.Millisecond)
		m.RecordRequest("op1", 150*time.Millisecond)

		snapshot := m.Snapshot(0)

		if snapshot.SlowQueryThresholdMS != 50.0 {
			t.Errorf("Expected threshold 50ms, got %f", snapshot.SlowQueryThresholdMS)
		}
		if snapshot.TotalSlowQueries != 3 {
			t.Errorf("Expected 3 total slow queries, got %d", snapshot.TotalSlowQueries)
		}
		if len(snapshot.RecentSlowQueries) != 3 {
			t.Errorf("Expected 3 recent slow queries, got %d", len(snapshot.RecentSlowQueries))
		}
	})

	t.Run("disabled slow query tracking", func(t *testing.T) {
		m := NewMetrics()
		m.SetSlowQueryThreshold(0) // Disable

		m.RecordRequest("test_op", 1*time.Second)

		m.mu.RLock()
		slowCount := m.slowQueryCounts["test_op"]
		m.mu.RUnlock()

		if slowCount != 0 {
			t.Errorf("Expected 0 slow queries when disabled, got %d", slowCount)
		}
	})
}

func TestTopSlowQueries(t *testing.T) {
	m := NewMetrics()
	m.SetSlowQueryThreshold(50 * time.Millisecond)

	// Add slow queries with different latencies
	m.RecordRequest("slow", 100*time.Millisecond)
	m.RecordRequest("slower", 200*time.Millisecond)
	m.RecordRequest("slowest", 300*time.Millisecond)

	top := m.TopSlowQueries(2)

	if len(top) != 2 {
		t.Errorf("Expected 2 top slow queries, got %d", len(top))
	}

	// Should be sorted by latency descending
	if top[0].LatencyMS != 300.0 {
		t.Errorf("Expected top query to have 300ms latency, got %f", top[0].LatencyMS)
	}
	if top[1].LatencyMS != 200.0 {
		t.Errorf("Expected second query to have 200ms latency, got %f", top[1].LatencyMS)
	}
}

func TestSlowQuerySummary(t *testing.T) {
	m := NewMetrics()
	m.SetSlowQueryThreshold(50 * time.Millisecond)

	m.RecordRequest("op1", 100*time.Millisecond)
	m.RecordRequest("op1", 150*time.Millisecond)
	m.RecordRequest("op2", 200*time.Millisecond)

	summary := m.GetSlowQuerySummary()

	if summary.ThresholdMS != 50.0 {
		t.Errorf("Expected threshold 50ms, got %f", summary.ThresholdMS)
	}
	if summary.TotalSlowQueries != 3 {
		t.Errorf("Expected 3 total slow queries, got %d", summary.TotalSlowQueries)
	}
	if summary.SlowByOperation["op1"] != 2 {
		t.Errorf("Expected 2 slow queries for op1, got %d", summary.SlowByOperation["op1"])
	}
	if summary.SlowByOperation["op2"] != 1 {
		t.Errorf("Expected 1 slow query for op2, got %d", summary.SlowByOperation["op2"])
	}
}

func TestPeriodicStatsSummary(t *testing.T) {
	m := NewMetrics()
	m.SetSlowQueryThreshold(50 * time.Millisecond)

	// Add some requests
	m.RecordRequest("create", 10*time.Millisecond)
	m.RecordRequest("update", 100*time.Millisecond) // slow
	m.RecordConnection()

	summary := m.PeriodicStatsSummary(1)

	// Check that the summary contains expected substrings
	if summary == "" {
		t.Error("Expected non-empty summary")
	}
	if !contains(summary, "STATS:") {
		t.Error("Expected summary to contain 'STATS:'")
	}
	if !contains(summary, "requests=") {
		t.Error("Expected summary to contain 'requests='")
	}
	if !contains(summary, "slow_queries=") {
		t.Error("Expected summary to contain 'slow_queries='")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (s == substr || len(s) > len(substr) && (s[:len(substr)] == substr || contains(s[1:], substr)))
}

func TestOperationMetricsSlowQueryCount(t *testing.T) {
	m := NewMetrics()
	m.SetSlowQueryThreshold(50 * time.Millisecond)

	// Mix of fast and slow queries for same operation
	m.RecordRequest("mixed_op", 10*time.Millisecond)  // fast
	m.RecordRequest("mixed_op", 100*time.Millisecond) // slow
	m.RecordRequest("mixed_op", 20*time.Millisecond)  // fast
	m.RecordRequest("mixed_op", 150*time.Millisecond) // slow

	snapshot := m.Snapshot(0)

	var mixedOp *OperationMetrics
	for i := range snapshot.Operations {
		if snapshot.Operations[i].Operation == "mixed_op" {
			mixedOp = &snapshot.Operations[i]
			break
		}
	}

	if mixedOp == nil {
		t.Fatal("Expected to find 'mixed_op' operation")
	}

	if mixedOp.TotalCount != 4 {
		t.Errorf("Expected 4 total requests, got %d", mixedOp.TotalCount)
	}
	if mixedOp.SlowQueryCount != 2 {
		t.Errorf("Expected 2 slow queries, got %d", mixedOp.SlowQueryCount)
	}
}

func TestDedupMetrics(t *testing.T) {
	t.Run("record dedup hit", func(t *testing.T) {
		m := NewMetrics()

		m.RecordDedup("list", true) // hit
		m.RecordDedup("list", true) // hit

		m.mu.RLock()
		hits := m.dedupHits["list"]
		misses := m.dedupMisses["list"]
		m.mu.RUnlock()

		if hits != 2 {
			t.Errorf("Expected 2 dedup hits, got %d", hits)
		}
		if misses != 0 {
			t.Errorf("Expected 0 dedup misses, got %d", misses)
		}
	})

	t.Run("record dedup miss", func(t *testing.T) {
		m := NewMetrics()

		m.RecordDedup("list", false) // miss

		m.mu.RLock()
		hits := m.dedupHits["list"]
		misses := m.dedupMisses["list"]
		m.mu.RUnlock()

		if hits != 0 {
			t.Errorf("Expected 0 dedup hits, got %d", hits)
		}
		if misses != 1 {
			t.Errorf("Expected 1 dedup miss, got %d", misses)
		}
	})

	t.Run("mixed hits and misses", func(t *testing.T) {
		m := NewMetrics()

		m.RecordDedup("show", false) // miss (first query executes)
		m.RecordDedup("show", true)  // hit (second waits for result)
		m.RecordDedup("show", true)  // hit (third waits for result)
		m.RecordDedup("show", false) // miss (new query after first completed)

		m.mu.RLock()
		hits := m.dedupHits["show"]
		misses := m.dedupMisses["show"]
		m.mu.RUnlock()

		if hits != 2 {
			t.Errorf("Expected 2 dedup hits, got %d", hits)
		}
		if misses != 2 {
			t.Errorf("Expected 2 dedup misses, got %d", misses)
		}
	})
}

func TestDedupMetricsSnapshot(t *testing.T) {
	m := NewMetrics()

	// Record some request and dedup data
	m.RecordRequest("list", 10*time.Millisecond)
	m.RecordRequest("list", 5*time.Millisecond)
	m.RecordRequest("show", 15*time.Millisecond)

	// Record dedup events
	m.RecordDedup("list", false) // miss
	m.RecordDedup("list", true)  // hit
	m.RecordDedup("list", true)  // hit
	m.RecordDedup("show", false) // miss

	snapshot := m.Snapshot(0)

	t.Run("global dedup stats", func(t *testing.T) {
		if snapshot.TotalDedupHits != 2 {
			t.Errorf("Expected 2 total dedup hits, got %d", snapshot.TotalDedupHits)
		}
		if snapshot.TotalDedupMisses != 2 {
			t.Errorf("Expected 2 total dedup misses, got %d", snapshot.TotalDedupMisses)
		}
		// Hit ratio: 2 / (2+2) = 0.5
		if snapshot.DedupHitRatio != 0.5 {
			t.Errorf("Expected dedup hit ratio 0.5, got %f", snapshot.DedupHitRatio)
		}
		if snapshot.SavedQueries != 2 {
			t.Errorf("Expected 2 saved queries, got %d", snapshot.SavedQueries)
		}
	})

	t.Run("per-operation dedup stats", func(t *testing.T) {
		var listOp, showOp *OperationMetrics
		for i := range snapshot.Operations {
			if snapshot.Operations[i].Operation == "list" {
				listOp = &snapshot.Operations[i]
			}
			if snapshot.Operations[i].Operation == "show" {
				showOp = &snapshot.Operations[i]
			}
		}

		if listOp == nil {
			t.Fatal("Expected to find 'list' operation")
		}
		if showOp == nil {
			t.Fatal("Expected to find 'show' operation")
		}

		if listOp.DedupHits != 2 {
			t.Errorf("Expected list to have 2 dedup hits, got %d", listOp.DedupHits)
		}
		if listOp.DedupMisses != 1 {
			t.Errorf("Expected list to have 1 dedup miss, got %d", listOp.DedupMisses)
		}
		if showOp.DedupHits != 0 {
			t.Errorf("Expected show to have 0 dedup hits, got %d", showOp.DedupHits)
		}
		if showOp.DedupMisses != 1 {
			t.Errorf("Expected show to have 1 dedup miss, got %d", showOp.DedupMisses)
		}
	})
}

func TestDedupHitRatioEdgeCases(t *testing.T) {
	t.Run("no dedup data returns zero ratio", func(t *testing.T) {
		m := NewMetrics()
		m.RecordRequest("create", 10*time.Millisecond) // non-dedupable op

		snapshot := m.Snapshot(0)

		if snapshot.DedupHitRatio != 0 {
			t.Errorf("Expected dedup hit ratio 0 when no data, got %f", snapshot.DedupHitRatio)
		}
	})

	t.Run("all hits", func(t *testing.T) {
		m := NewMetrics()
		m.RecordDedup("list", true)
		m.RecordDedup("list", true)
		m.RecordDedup("list", true)

		snapshot := m.Snapshot(0)

		if snapshot.DedupHitRatio != 1.0 {
			t.Errorf("Expected dedup hit ratio 1.0 when all hits, got %f", snapshot.DedupHitRatio)
		}
	})

	t.Run("all misses", func(t *testing.T) {
		m := NewMetrics()
		m.RecordDedup("list", false)
		m.RecordDedup("list", false)

		snapshot := m.Snapshot(0)

		if snapshot.DedupHitRatio != 0 {
			t.Errorf("Expected dedup hit ratio 0 when all misses, got %f", snapshot.DedupHitRatio)
		}
	})
}
