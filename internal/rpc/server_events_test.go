package rpc

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestDispatchRemovesClosedWatcher(t *testing.T) {
	s := NewServer("", nil, "", "")
	watcherID, ch := s.registerWatcher()
	close(ch)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		_ = r.Close()
	}()

	s.dispatchIssueEvent(IssueEvent{Type: IssueEventUpdated})

	_ = w.Close()
	logBuf, _ := io.ReadAll(r)
	if !strings.Contains(string(logBuf), "watcher") || !strings.Contains(string(logBuf), "removed") {
		t.Fatalf("expected removal log, got %q", string(logBuf))
	}

	s.watchersMu.RLock()
	_, stillPresent := s.watchers[watcherID]
	s.watchersMu.RUnlock()
	if stillPresent {
		t.Fatalf("closed watcher %d still present after dispatch", watcherID)
	}
}

func TestDispatchKeepsActiveWatcher(t *testing.T) {
	s := NewServer("", nil, "", "")
	watcherID, ch := s.registerWatcher()

	done := make(chan struct{})
	go func() {
		<-ch
		close(done)
	}()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() {
		os.Stderr = oldStderr
		_ = r.Close()
	}()

	s.dispatchIssueEvent(IssueEvent{Type: IssueEventCreated})

	_ = w.Close()
	<-done

	s.watchersMu.RLock()
	_, stillPresent := s.watchers[watcherID]
	s.watchersMu.RUnlock()
	if !stillPresent {
		t.Fatalf("active watcher %d unexpectedly removed", watcherID)
	}

	logBuf, _ := io.ReadAll(r)
	if strings.Contains(string(logBuf), "removed") {
		t.Fatalf("unexpected removal log: %q", string(logBuf))
	}
}
