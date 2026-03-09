package localstate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreGetSet(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)

	// Get on missing key returns ""
	v, err := s.Get("no_such_key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "" {
		t.Fatalf("expected empty, got %q", v)
	}

	// Set and read back
	if err := s.Set("foo", "bar"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, err = s.Get("foo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if v != "bar" {
		t.Fatalf("expected bar, got %q", v)
	}

	// Overwrite
	if err := s.Set("foo", "baz"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, _ = s.Get("foo")
	if v != "baz" {
		t.Fatalf("expected baz, got %q", v)
	}

	// Verify file exists
	if _, err := os.Stat(filepath.Join(dir, fileName)); err != nil {
		t.Fatalf("state file not created: %v", err)
	}
}

func TestStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	// Write corrupt JSON
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{corrupt"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := New(dir)
	_, err := s.Get("key")
	if err == nil {
		t.Fatal("expected error on corrupt file")
	}
}
