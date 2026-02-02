package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/steveyegge/beads/internal/debug"
)

// MaxConcurrentBdProcesses is the maximum number of bd processes that can
// access a single dolt database simultaneously. Dolt embedded uses file-level
// locking; concurrent processes contend for the lock and hang indefinitely
// when too many run at once. This process-level semaphore (using flock slot
// files) limits concurrency across ALL bd callers — gt, hooks, CLI — not
// just goroutines within a single process.
const MaxConcurrentBdProcesses = 3

// ProcessSemaphore represents a held slot in the file-based counting semaphore.
// Close the semaphore to release the slot.
type ProcessSemaphore struct {
	file *os.File
	slot int
}

// acquireProcessSemaphore acquires one of N flock-based slots for the given
// beads directory. If all slots are taken, it blocks on slot 0 until one
// becomes available. Returns a ProcessSemaphore that must be released via
// Release() when done.
func acquireProcessSemaphore(beadsDir string) (*ProcessSemaphore, error) {
	semDir := filepath.Join(beadsDir, "sem")
	if err := os.MkdirAll(semDir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create semaphore directory: %w", err)
	}

	// Try each slot with non-blocking flock
	for i := 0; i < MaxConcurrentBdProcesses; i++ {
		slotPath := filepath.Join(semDir, fmt.Sprintf("slot.%d", i))
		// #nosec G304 - controlled path from config
		f, err := os.OpenFile(slotPath, os.O_CREATE|os.O_RDWR, 0600)
		if err != nil {
			continue
		}
		if err := flockExclusive(f); err == nil {
			debug.Logf("process semaphore: acquired slot %d in %s", i, semDir)
			return &ProcessSemaphore{file: f, slot: i}, nil
		}
		_ = f.Close()
	}

	// All slots taken — block on slot 0 until it becomes available
	slotPath := filepath.Join(semDir, "slot.0")
	debug.Logf("process semaphore: all %d slots taken in %s, waiting on slot 0", MaxConcurrentBdProcesses, semDir)
	// #nosec G304 - controlled path from config
	f, err := os.OpenFile(slotPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("cannot open semaphore slot file: %w", err)
	}
	if err := flockExclusiveBlocking(f); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("cannot acquire semaphore slot: %w", err)
	}
	debug.Logf("process semaphore: acquired slot 0 (after wait) in %s", semDir)
	return &ProcessSemaphore{file: f, slot: 0}, nil
}

// Release releases the semaphore slot. Safe to call multiple times.
func (s *ProcessSemaphore) Release() {
	if s == nil || s.file == nil {
		return
	}
	debug.Logf("process semaphore: releasing slot %d", s.slot)
	_ = s.file.Close() // closing the fd releases the flock
	s.file = nil
}
