package rpc

import (
	"sync/atomic"
	"testing"
	"time"
)

// mockSession is a minimal serverIterSession for testing.
type mockSession struct {
	typeName string
	lastUsed atomic.Int64
	closed   atomic.Bool
}

func newMockSession(name string) *mockSession {
	s := &mockSession{typeName: name}
	s.lastUsed.Store(time.Now().UnixNano())
	return s
}

func (s *mockSession) TypeName() string      { return s.typeName }
func (s *mockSession) Touch()                { s.lastUsed.Store(time.Now().UnixNano()) }
func (s *mockSession) LastUsedAt() time.Time { return time.Unix(0, s.lastUsed.Load()) }
func (s *mockSession) Close() error {
	s.closed.Store(true)
	return nil
}

func newTestManager(maxCap int, idleReap time.Duration) *iterSessionManager {
	m := &iterSessionManager{
		sessions: make(map[string]serverIterSession),
		maxCap:   maxCap,
		idleReap: idleReap,
		stopCh:   make(chan struct{}),
		done:     make(chan struct{}),
	}
	go m.reapLoop()
	return m
}

func TestOpenSession_CapEnforcement(t *testing.T) {
	m := newTestManager(2, 0)
	defer m.stop()

	if err := m.openSession("a", newMockSession("A")); err != nil {
		t.Fatalf("open a: %v", err)
	}
	if err := m.openSession("b", newMockSession("B")); err != nil {
		t.Fatalf("open b: %v", err)
	}
	if err := m.openSession("c", newMockSession("C")); err == nil {
		t.Fatal("expected ErrTooManyIterators for third session, got nil")
	}
	if m.sessionsActive.Load() != 2 {
		t.Errorf("sessionsActive: got %d, want 2", m.sessionsActive.Load())
	}
}

func TestCloseSession_Idempotent(t *testing.T) {
	m := newTestManager(4, 0)
	defer m.stop()
	sess := newMockSession("X")
	_ = m.openSession("x", sess)
	m.closeSession("x")
	m.closeSession("x") // second call must not panic or double-close
	m.closeSession("y") // unknown id — no-op
	if !sess.closed.Load() {
		t.Error("session should be closed after closeSession")
	}
}

func TestReapStale_RemovesIdleSessions(t *testing.T) {
	m := newTestManager(10, 50*time.Millisecond)
	defer m.stop()

	sess := newMockSession("idle")
	// backdate last-used so the session is already idle
	sess.lastUsed.Store(time.Now().Add(-100 * time.Millisecond).UnixNano())
	_ = m.openSession("idle", sess)

	m.reapStale()

	m.mu.RLock()
	_, still := m.sessions["idle"]
	m.mu.RUnlock()
	if still {
		t.Error("expected idle session to be reaped")
	}
	if !sess.closed.Load() {
		t.Error("reaped session must be closed")
	}
}

func TestDrainAll_ClosesAll(t *testing.T) {
	m := newTestManager(10, 0)

	var sessions []*mockSession
	for i := 0; i < 5; i++ {
		s := newMockSession("s")
		sessions = append(sessions, s)
		_ = m.openSession(string(rune('a'+i)), s)
	}

	m.drainAll()

	for i, s := range sessions {
		if !s.closed.Load() {
			t.Errorf("session %d not closed after drainAll", i)
		}
	}
	m.mu.RLock()
	remaining := len(m.sessions)
	m.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("sessions map should be empty after drainAll, got %d", remaining)
	}
}

func TestIterClose_Idempotent(t *testing.T) {
	s := &daemonServer{iterMgr: newTestManager(4, 0)}
	defer s.iterMgr.stop()

	sess := newMockSession("iter")
	_ = s.iterMgr.openSession("tok", sess)

	// first close
	if err := s.IterClose(&IterCloseArgs{SessionID: "tok"}, &IterCloseReply{}); err != nil {
		t.Fatalf("IterClose first: %v", err)
	}
	// second close — idempotent
	if err := s.IterClose(&IterCloseArgs{SessionID: "tok"}, &IterCloseReply{}); err != nil {
		t.Fatalf("IterClose second: %v", err)
	}
	// unknown id — no-op
	if err := s.IterClose(&IterCloseArgs{SessionID: "missing"}, &IterCloseReply{}); err != nil {
		t.Fatalf("IterClose unknown: %v", err)
	}
}

func TestIterSessionManager_RaceDetector(t *testing.T) {
	if testing.Short() {
		t.Skip("race detector test skipped in -short mode")
	}
	m := newTestManager(100, 10*time.Millisecond)
	defer m.stop()

	const goroutines = 20
	done := make(chan struct{})
	for i := 0; i < goroutines; i++ {
		go func(n int) {
			defer func() { done <- struct{}{} }()
			id := string(rune('a' + n%26))
			sess := newMockSession("race")
			_ = m.openSession(id+string(rune('A'+n%26)), sess)
			_ = m.getSession(id + string(rune('A'+n%26)))
			m.closeSession(id + string(rune('A'+n%26)))
		}(i)
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
}
