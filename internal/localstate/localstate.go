// Package localstate manages machine-local key-value state stored outside the
// Dolt-versioned database. This prevents merge conflicts when multiple machines
// push/pull the same Dolt remote, since machine-local values (like auto-push
// timestamps and tip-shown timestamps) differ per clone and cause cell-level
// conflicts in Dolt's three-way merge.
//
// See https://github.com/steveyegge/beads/issues/2466
package localstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const fileName = "local-state.json"

// Store provides thread-safe read/write access to machine-local state.
// State is persisted as a JSON file in the .beads/ directory.
type Store struct {
	path string
	mu   sync.Mutex
}

// New creates a Store that reads/writes local-state.json in beadsDir.
func New(beadsDir string) *Store {
	return &Store{path: filepath.Join(beadsDir, fileName)}
}

// Get retrieves a value by key. Returns "" if the key does not exist.
func (s *Store) Get(key string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return "", err
	}
	return data[key], nil
}

// Set stores a key-value pair, creating the file if it doesn't exist.
func (s *Store) Set(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := s.load()
	if err != nil {
		return err
	}
	data[key] = value
	return s.save(data)
}

// load reads the state file. Returns an empty map if the file doesn't exist.
func (s *Store) load() (map[string]string, error) {
	raw, err := os.ReadFile(s.path) // #nosec G304 - controlled path
	if os.IsNotExist(err) {
		return make(map[string]string), nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading local state: %w", err)
	}
	var data map[string]string
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parsing local state: %w", err)
	}
	if data == nil {
		data = make(map[string]string)
	}
	return data, nil
}

// save writes the state map to disk atomically.
func (s *Store) save(data map[string]string) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling local state: %w", err)
	}
	if err := os.WriteFile(s.path, raw, 0o600); err != nil {
		return fmt.Errorf("writing local state: %w", err)
	}
	return nil
}
