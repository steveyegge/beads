// Package inject provides a queue-based injection system for Claude Code hooks.
//
// The injection queue solves API 400 concurrency errors that occur when multiple
// hooks try to inject content simultaneously. Instead of writing to stdout directly,
// hooks queue their content, and a dedicated drain command outputs everything safely.
//
// Architecture:
//   - Queue Storage: .runtime/inject-queue/<session-id>.jsonl
//   - Queue Writers: gt mail check --inject, bd decision check --inject
//   - Queue Consumer: gt inject drain
//
// This package is a copy of the gastown inject package, designed to share
// the same queue location so gt and bd can coordinate injections.
package inject

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// DirRuntime is the runtime directory name (same as gastown)
const DirRuntime = ".runtime"

// EntryType identifies the type of queued content.
type EntryType string

const (
	// TypeMail indicates mail notification content.
	TypeMail EntryType = "mail"
	// TypeDecision indicates decision notification content.
	TypeDecision EntryType = "decision"
)

// Entry represents a single item in the injection queue.
type Entry struct {
	Type      EntryType `json:"type"`
	Content   string    `json:"content"`
	Timestamp int64     `json:"timestamp"`
}

// Queue manages the injection queue for a session.
type Queue struct {
	sessionID string
	queueDir  string
}

// NewQueue creates a queue for the given session.
// workDir should be the rig or workspace directory containing .runtime/.
func NewQueue(workDir, sessionID string) *Queue {
	return &Queue{
		sessionID: sessionID,
		queueDir:  filepath.Join(workDir, DirRuntime, "inject-queue"),
	}
}

// queueFile returns the path to this session's queue file.
func (q *Queue) queueFile() string {
	return filepath.Join(q.queueDir, q.sessionID+".jsonl")
}

// Enqueue adds an entry to the queue.
func (q *Queue) Enqueue(entryType EntryType, content string) error {
	// Ensure queue directory exists
	if err := os.MkdirAll(q.queueDir, 0755); err != nil {
		return fmt.Errorf("creating queue directory: %w", err)
	}

	// Create entry
	entry := Entry{
		Type:      entryType,
		Content:   content,
		Timestamp: time.Now().UnixMilli(),
	}

	// Marshal to JSON
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshaling entry: %w", err)
	}

	// Append to queue file with flock
	f, err := os.OpenFile(q.queueFile(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("opening queue file: %w", err)
	}
	defer f.Close()

	// Acquire exclusive lock for write
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquiring file lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	if _, err := f.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("writing to queue: %w", err)
	}

	return nil
}
