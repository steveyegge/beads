package rpc

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// QueryDeduplicator coalesces identical in-flight queries.
// When multiple clients request the same data within a short window,
// only the first executes while others wait for its result.
//
// This is particularly effective during status-line refresh storms
// where many Claude Code sessions simultaneously request the same data.
type QueryDeduplicator struct {
	mu       sync.Mutex
	inflight map[string]*inflightQuery
	timeout  time.Duration
}

// inflightQuery tracks a single in-flight query and its waiters
type inflightQuery struct {
	resultChan chan dedupResult
	waiting    int // number of waiters (including original requester)
	startTime  time.Time
}

// dedupResult holds the result of a deduplicated query
type dedupResult struct {
	response Response
}

// NewQueryDeduplicator creates a new query deduplicator.
// timeout specifies max wait time before falling back to direct execution.
func NewQueryDeduplicator(timeout time.Duration) *QueryDeduplicator {
	return &QueryDeduplicator{
		inflight: make(map[string]*inflightQuery),
		timeout:  timeout,
	}
}

// DedupableOps lists operations eligible for deduplication.
// These are read-only operations that produce deterministic results.
var DedupableOps = map[string]bool{
	OpList:                true,
	OpCount:               true,
	OpReady:               true,
	OpBlocked:             true,
	OpShow:                true,
	OpStats:               true,
	OpStale:               true,
	OpEpicStatus:          true,
	OpGetMutations:        true,
	OpGetMoleculeProgress: true,
	OpGetWorkerStatus:     true,
	OpGetConfig:           true,
	OpHealth:              true,
	OpStatus:              true,
	OpMetrics:             true,
}

// queryKey generates a unique hash for a query based on operation and args.
// Two queries with identical operation and args will have the same key.
func queryKey(operation string, args json.RawMessage) string {
	h := sha256.New()
	h.Write([]byte(operation))
	h.Write([]byte(":"))
	h.Write(args)
	return hex.EncodeToString(h.Sum(nil))[:16] // 16 chars is enough for collision avoidance
}

// Execute executes a query with deduplication.
// If an identical query is already in-flight, waits for its result.
// If not, executes the query and broadcasts result to any waiters.
//
// The executor function is only called by the first requester.
// Returns (response, wasDeduped) where wasDeduped indicates if result came from another query.
func (d *QueryDeduplicator) Execute(
	operation string,
	args json.RawMessage,
	executor func() Response,
) (Response, bool) {
	// Check if this operation is eligible for deduplication
	if !DedupableOps[operation] {
		return executor(), false
	}

	key := queryKey(operation, args)

	d.mu.Lock()

	// Check if there's already an in-flight query with this key
	if existing, ok := d.inflight[key]; ok {
		existing.waiting++
		d.mu.Unlock()

		// Wait for the result with timeout
		select {
		case result := <-existing.resultChan:
			// Got result - but need to re-broadcast for other waiters
			d.mu.Lock()
			if q, ok := d.inflight[key]; ok && q == existing {
				q.waiting--
				if q.waiting > 0 {
					// Re-broadcast for remaining waiters
					go func() { existing.resultChan <- result }()
				}
			}
			d.mu.Unlock()
			return result.response, true

		case <-time.After(d.timeout):
			// Timeout - fall back to direct execution
			d.mu.Lock()
			if q, ok := d.inflight[key]; ok && q == existing {
				q.waiting--
			}
			d.mu.Unlock()
			return executor(), false
		}
	}

	// No in-flight query - we become the executor
	inflight := &inflightQuery{
		resultChan: make(chan dedupResult, 1), // buffered for broadcast
		waiting:    1,
		startTime:  time.Now(),
	}
	d.inflight[key] = inflight
	d.mu.Unlock()

	// Execute the query
	response := executor()
	result := dedupResult{response: response}

	// Broadcast result to waiters
	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if we're still the registered query (could have been replaced on timeout)
	if q, ok := d.inflight[key]; ok && q == inflight {
		q.waiting--
		if q.waiting > 0 {
			// Send result to waiters
			inflight.resultChan <- result
		}
		// Clean up after a short delay to allow late arrivals to still benefit
		go func() {
			time.Sleep(10 * time.Millisecond)
			d.mu.Lock()
			if curr, ok := d.inflight[key]; ok && curr == inflight {
				delete(d.inflight, key)
			}
			d.mu.Unlock()
		}()
	}

	return response, false
}

// Stats returns statistics about the deduplicator's current state.
func (d *QueryDeduplicator) Stats() DedupStats {
	d.mu.Lock()
	defer d.mu.Unlock()

	stats := DedupStats{
		InflightQueries: len(d.inflight),
	}
	for _, q := range d.inflight {
		stats.TotalWaiters += q.waiting
	}
	return stats
}

// DedupStats holds deduplication statistics
type DedupStats struct {
	InflightQueries int // Number of unique queries currently in-flight
	TotalWaiters    int // Total number of clients waiting for results
}
