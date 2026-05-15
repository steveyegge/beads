package rpc

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/steveyegge/beads/internal/configfile"
	"github.com/steveyegge/beads/internal/storage"
)

// newDaemonServer constructs a daemonServer with an initialized iterSessionManager.
// The caller must call srv.iterMgr.stop() when shutting down — AFTER the acceptLoop exits.
func newDaemonServer(ctx context.Context, store storage.Storage, cfg *configfile.Config) *daemonServer {
	return &daemonServer{
		store:   store,
		root:    ctx,
		iterMgr: newIterSessionManager(cfg),
	}
}

// --- Shared wire types for the iterator RPC surface (§2.2) ---

// IterStartReply is returned by all IterXxxStart methods.
type IterStartReply struct {
	SessionID string
	RPCError  *RPCError
}

// IterNextArgs is sent by all IterXxxNext methods.
type IterNextArgs struct {
	SessionID string
	BatchSize int
}

// IterCloseArgs is sent by IterClose.
type IterCloseArgs struct {
	SessionID string
}

// IterCloseReply is returned by IterClose.
type IterCloseReply struct {
	RPCError *RPCError
}

// --- serverIterSession interface (§3.1) ---

// serverIterSession is implemented by every per-type iterator session.
// The session owns a cursor into storage; Next() is NOT on this interface
// because its return type differs per iterator type (issues, events, etc.).
// Per-type sessions hold their own mu for concurrent Next serialization.
type serverIterSession interface {
	// TypeName returns the Go type name of the session (for debugging/metrics).
	TypeName() string
	// LastUsedAt returns the time of the last Touch() call.
	LastUsedAt() time.Time
	// Touch records the current time as the last-used timestamp.
	// Uses atomic store; safe to call from any goroutine.
	Touch()
	// Close releases resources held by the session.
	Close() error
}

// --- iterSessionManager (§3.2–3.4) ---

// iterSessionManager maintains the live iterator sessions for a daemonServer.
// It is safe for concurrent use; all exported methods are goroutine-safe.
type iterSessionManager struct {
	mu       sync.RWMutex
	sessions map[string]serverIterSession

	maxCap   int           // ErrTooManyIterators threshold
	idleReap time.Duration // idle-session reap interval

	// stats (atomic)
	sessionsActive     atomic.Int64
	sessionStartsTotal atomic.Int64
	sessionReapedTotal atomic.Int64
	rowsStreamedTotal  atomic.Int64

	stopCh chan struct{}
	done   chan struct{} // closed when reapLoop has exited
}

// newIterSessionManager creates and starts a session manager from the current
// configfile.Config values. The caller must call stop() when shutting down.
func newIterSessionManager(cfg *configfile.Config) *iterSessionManager {
	m := &iterSessionManager{
		sessions: make(map[string]serverIterSession),
		maxCap:   cfg.GetDaemonIterMax(),
		idleReap: time.Duration(cfg.GetDaemonIterIdleSeconds()) * time.Second,
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
	go m.reapLoop()
	return m
}

// openSession registers a new session under id.
// Returns ErrTooManyIterators when the session cap is reached.
func (m *iterSessionManager) openSession(id string, sess serverIterSession) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sessions) >= m.maxCap {
		return storage.ErrTooManyIterators
	}
	m.sessions[id] = sess
	m.sessionsActive.Add(1)
	m.sessionStartsTotal.Add(1)
	return nil
}

// closeSession closes and removes a session by id.
// It is idempotent: calling with an unknown id is a no-op.
func (m *iterSessionManager) closeSession(id string) {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if ok {
		_ = sess.Close()
		m.sessionsActive.Add(-1)
	}
}

// getSession returns the session with the given id, or nil if not found.
func (m *iterSessionManager) getSession(id string) serverIterSession {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessions[id]
}

// reapLoop runs until stop() is called, periodically reaping idle sessions.
func (m *iterSessionManager) reapLoop() {
	defer close(m.done)
	if m.idleReap <= 0 {
		// no idle reaping; just wait for stop
		<-m.stopCh
		m.drainAll()
		return
	}
	ticker := time.NewTicker(m.idleReap / 2)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.reapStale()
		case <-m.stopCh:
			m.drainAll()
			return
		}
	}
}

// reapStale removes sessions that have been idle longer than idleReap.
func (m *iterSessionManager) reapStale() {
	cutoff := time.Now().Add(-m.idleReap)
	var stale []string
	m.mu.RLock()
	for id, sess := range m.sessions {
		if sess.LastUsedAt().Before(cutoff) {
			stale = append(stale, id)
		}
	}
	m.mu.RUnlock()
	for _, id := range stale {
		m.closeSession(id)
		m.sessionReapedTotal.Add(1)
	}
}

// drainAll closes all live sessions synchronously.
// Called from reapLoop when stopCh is closed; no new sessions can arrive at
// that point because the acceptLoop has exited first.
func (m *iterSessionManager) drainAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.closeSession(id)
	}
}

// stop signals the reapLoop to drain and exit.
// Must be called AFTER the acceptLoop has exited so no new IterStart calls
// arrive while drainAll is running.
func (m *iterSessionManager) stop() {
	close(m.stopCh)
	<-m.done
}

// --- IterClose server method (§5) ---

// IterClose closes an iterator session.
// Idempotent: not an error if the session is already closed or unknown.
func (s *daemonServer) IterClose(args *IterCloseArgs, _ *IterCloseReply) error {
	s.iterMgr.closeSession(args.SessionID)
	return nil
}
