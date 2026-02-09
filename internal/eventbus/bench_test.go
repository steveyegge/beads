package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

// BenchmarkDispatchNoHandlers measures raw dispatch overhead with no handlers.
func BenchmarkDispatchNoHandlers(b *testing.B) {
	bus := New()
	event := &Event{
		Type:      EventSessionStart,
		SessionID: "bench-session",
		CWD:       "/tmp",
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Dispatch(ctx, event)
	}
}

// BenchmarkDispatchWithDefaultHandlers measures dispatch with all default handlers.
func BenchmarkDispatchWithDefaultHandlers(b *testing.B) {
	bus := New()
	for _, h := range DefaultHandlers() {
		bus.Register(h)
	}
	event := &Event{
		Type:      EventSessionStart,
		SessionID: "bench-session",
		CWD:       "/tmp",
		Raw:       json.RawMessage(`{"session_id":"bench-session","cwd":"/tmp","hook_event_name":"SessionStart"}`),
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Dispatch(ctx, event)
	}
}

// BenchmarkDispatchOjEvent measures dispatch of an OJ event with no handlers registered.
func BenchmarkDispatchOjEvent(b *testing.B) {
	bus := New()
	payload := OjJobEventPayload{
		JobID:   "job-123",
		JobName: "Build: feature X",
		Worker:  "epic",
		BeadID:  "gt-abc",
	}
	payloadJSON, _ := json.Marshal(payload)
	event := &Event{
		Type:      EventOjJobCreated,
		SessionID: "oj-bench",
		Raw:       payloadJSON,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Dispatch(ctx, event)
	}
}

// BenchmarkDispatchWithJetStream measures dispatch including JetStream publish.
func BenchmarkDispatchWithJetStream(b *testing.B) {
	_, js, cleanup := startTestNATS(b)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)

	event := &Event{
		Type:      EventSessionStart,
		SessionID: "bench-js",
		CWD:       "/tmp",
		Raw:       json.RawMessage(`{"session_id":"bench-js","cwd":"/tmp","hook_event_name":"SessionStart"}`),
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Dispatch(ctx, event)
	}
}

// BenchmarkDispatchOjEventWithJetStream measures OJ event dispatch + JetStream publish.
func BenchmarkDispatchOjEventWithJetStream(b *testing.B) {
	_, js, cleanup := startTestNATS(b)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)

	payload := OjJobEventPayload{
		JobID:   "job-bench",
		JobName: "Build: bench test",
		Worker:  "epic",
		BeadID:  "gt-bench",
	}
	payloadJSON, _ := json.Marshal(payload)
	event := &Event{
		Type:      EventOjJobCreated,
		SessionID: "oj-bench",
		Raw:       payloadJSON,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bus.Dispatch(ctx, event)
	}
}

// BenchmarkDispatchConcurrent5 measures dispatch latency with 5 concurrent emitters.
// This simulates the scenario described in bd-4q86.5.
func BenchmarkDispatchConcurrent5(b *testing.B) {
	_, js, cleanup := startTestNATS(b)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)
	for _, h := range DefaultHandlers() {
		bus.Register(h)
	}

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var wg sync.WaitGroup
		for c := 0; c < 5; c++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				event := &Event{
					Type:      EventSessionStart,
					SessionID: fmt.Sprintf("bench-concurrent-%d", id),
					CWD:       "/tmp",
					Raw:       json.RawMessage(fmt.Sprintf(`{"session_id":"bench-%d","cwd":"/tmp","hook_event_name":"SessionStart"}`, id)),
				}
				bus.Dispatch(ctx, event)
			}(c)
		}
		wg.Wait()
	}
}

// startTestNATS is compatible with both *testing.T and *testing.B via testing.TB.
// The actual implementation is in bus_test.go; we call it through testing.TB.
// Since Go testing.TB doesn't support TempDir on *testing.B before Go 1.15,
// this function is defined separately here.

// TestEmitLatencyReport runs a latency measurement and reports p50/p95/p99.
// This is a test (not benchmark) so it appears in regular test output.
func TestEmitLatencyReport(t *testing.T) {
	_, js, cleanup := startTestNATS(t)
	defer cleanup()

	bus := New()
	bus.SetJetStream(js)
	for _, h := range DefaultHandlers() {
		bus.Register(h)
	}

	ctx := context.Background()
	const iterations = 100

	// Serial latency.
	var serialLatencies []time.Duration
	for i := 0; i < iterations; i++ {
		event := &Event{
			Type:      EventOjJobCreated,
			SessionID: fmt.Sprintf("latency-%d", i),
			Raw:       json.RawMessage(fmt.Sprintf(`{"job_id":"j-%d","job_name":"bench"}`, i)),
		}
		start := time.Now()
		bus.Dispatch(ctx, event)
		serialLatencies = append(serialLatencies, time.Since(start))
	}

	// Concurrent latency (5 emitters).
	var concurrentLatencies []time.Duration
	var mu sync.Mutex
	for i := 0; i < iterations/5; i++ {
		var wg sync.WaitGroup
		for c := 0; c < 5; c++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				event := &Event{
					Type:      EventOjJobCreated,
					SessionID: fmt.Sprintf("conc-%d", id),
					Raw:       json.RawMessage(fmt.Sprintf(`{"job_id":"j-%d","job_name":"bench"}`, id)),
				}
				start := time.Now()
				bus.Dispatch(ctx, event)
				d := time.Since(start)
				mu.Lock()
				concurrentLatencies = append(concurrentLatencies, d)
				mu.Unlock()
			}(i*5 + c)
		}
		wg.Wait()
	}

	reportLatencies(t, "serial dispatch+JetStream", serialLatencies)
	reportLatencies(t, "concurrent (5x) dispatch+JetStream", concurrentLatencies)
}

func reportLatencies(t *testing.T, label string, latencies []time.Duration) {
	t.Helper()
	if len(latencies) == 0 {
		t.Logf("%s: no data", label)
		return
	}

	// Sort for percentile calculation.
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sortDurations(sorted)

	p50 := sorted[len(sorted)*50/100]
	p95 := sorted[len(sorted)*95/100]
	p99 := sorted[len(sorted)*99/100]
	max := sorted[len(sorted)-1]

	var total time.Duration
	for _, d := range sorted {
		total += d
	}
	avg := total / time.Duration(len(sorted))

	t.Logf("%s (n=%d): avg=%v p50=%v p95=%v p99=%v max=%v",
		label, len(sorted), avg, p50, p95, p99, max)

	// bd-4q86.5 target: <50ms for in-process dispatch.
	if p95 > 50*time.Millisecond {
		t.Logf("WARNING: %s p95 (%v) exceeds 50ms target", label, p95)
	}
}

func sortDurations(d []time.Duration) {
	for i := 1; i < len(d); i++ {
		key := d[i]
		j := i - 1
		for j >= 0 && d[j] > key {
			d[j+1] = d[j]
			j--
		}
		d[j+1] = key
	}
}
