package rpc

import (
	"sync"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage"
)

// TestIterSessionManager_CapEnforcement verifies that:
//  1. Sessions fill to the configured cap.
//  2. The next open past the cap returns ErrTooManyIterators.
//  3. After closing one session, a new open succeeds.
func TestIterSessionManager_CapEnforcement(t *testing.T) {
	m := newTestManager(3, 0)
	defer m.stop()

	if err := m.openSession("a", newMockSession("A")); err != nil {
		t.Fatalf("open a: %v", err)
	}
	if err := m.openSession("b", newMockSession("B")); err != nil {
		t.Fatalf("open b: %v", err)
	}
	if err := m.openSession("c", newMockSession("C")); err != nil {
		t.Fatalf("open c: %v", err)
	}

	// Fourth open must fail with ErrTooManyIterators.
	if err := m.openSession("d", newMockSession("D")); !isErrTooMany(err) {
		t.Fatalf("expected ErrTooManyIterators for fourth session, got %v", err)
	}
	if m.sessionsActive.Load() != 3 {
		t.Errorf("sessionsActive: got %d, want 3", m.sessionsActive.Load())
	}

	// After closing one, the next open must succeed.
	m.closeSession("b")
	if err := m.openSession("e", newMockSession("E")); err != nil {
		t.Fatalf("open after close: %v", err)
	}
	if m.sessionsActive.Load() != 3 {
		t.Errorf("sessionsActive after reopen: got %d, want 3", m.sessionsActive.Load())
	}
}

// TestIterSessionManager_IdleReap verifies that sessions idle beyond the
// configured threshold are removed by reapStale, while active sessions are
// kept.
func TestIterSessionManager_IdleReap(t *testing.T) {
	idle := time.Duration(50 * time.Millisecond)
	m := newTestManager(10, idle)
	defer m.stop()

	// Add one session that has been idle for 2× the threshold.
	stale := newMockSession("stale")
	stale.lastUsed.Store(time.Now().Add(-2 * idle).UnixNano())
	_ = m.openSession("stale", stale)

	// Add one session that was just touched (active).
	active := newMockSession("active")
	_ = m.openSession("active", active)

	m.reapStale()

	m.mu.RLock()
	_, stillStale := m.sessions["stale"]
	_, stillActive := m.sessions["active"]
	m.mu.RUnlock()

	if stillStale {
		t.Error("stale session should have been reaped")
	}
	if !stale.closed.Load() {
		t.Error("reaped session must be closed")
	}
	if !stillActive {
		t.Error("active session should NOT be reaped")
	}
	if active.closed.Load() {
		t.Error("active session must NOT be closed")
	}
}

// TestIterSessionManager_DrainAll verifies that closing stopCh (via stop())
// causes all live sessions to be closed.
func TestIterSessionManager_DrainAll(t *testing.T) {
	m := newTestManager(10, 0)

	var sessions []*mockSession
	for i := 0; i < 4; i++ {
		s := newMockSession("s")
		sessions = append(sessions, s)
		_ = m.openSession(string(rune('a'+i)), s)
	}

	// stop() triggers drainAll and waits for reapLoop to exit.
	m.stop()

	for i, s := range sessions {
		if !s.closed.Load() {
			t.Errorf("session %d not closed after stop()", i)
		}
	}
	m.mu.RLock()
	remaining := len(m.sessions)
	m.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("sessions map should be empty after stop(), got %d", remaining)
	}
}

// TestIterSessionManager_Concurrent runs 32 goroutines concurrently opening
// and closing sessions to catch data races. Run with -race.
func TestIterSessionManager_Concurrent(t *testing.T) {
	m := newTestManager(100, 0)
	defer m.stop()

	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			id := string(rune('A' + i%26))
			suffix := string(rune('a' + i%26))
			key := id + suffix
			sess := newMockSession("concurrent")
			_ = m.openSession(key, sess)
			_ = m.getSession(key)
			m.closeSession(key)
		}()
	}
	wg.Wait()
}

// isErrTooMany is a helper used instead of errors.Is to allow the test package
// to check the error without importing storage (which would be a cycle risk if
// rpc_test were an external test package). Since these tests are package-internal,
// we can inspect the concrete error value directly.
func isErrTooMany(err error) bool {
	return err != nil && err.Error() == storage.ErrTooManyIterators.Error()
}
