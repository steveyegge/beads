package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTouchDatabaseFile verifies the TouchDatabaseFile helper function
func TestTouchDatabaseFile(t *testing.T) {
	tmpDir := t.TempDir()
	testFile := filepath.Join(tmpDir, "test.db")

	// Create a test file
	if err := os.WriteFile(testFile, []byte("test"), 0600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Get initial mtime
	infoBefore, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Failed to stat file: %v", err)
	}

	// Wait a bit to ensure mtime difference (1s for filesystems with coarse resolution)
	time.Sleep(1 * time.Second)

	// Touch the file
	if err := TouchDatabaseFile(testFile, ""); err != nil {
		t.Fatalf("TouchDatabaseFile failed: %v", err)
	}

	// Get new mtime
	infoAfter, err := os.Stat(testFile)
	if err != nil {
		t.Fatalf("Failed to stat file after touch: %v", err)
	}

	// Verify mtime was updated
	if !infoAfter.ModTime().After(infoBefore.ModTime()) {
		t.Errorf("File mtime should be updated after touch")
	}
}

// TestTouchDatabaseFileWithClockSkew verifies handling of future JSONL timestamps
func TestTouchDatabaseFileWithClockSkew(t *testing.T) {
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, "test.db")
	jsonlFile := filepath.Join(tmpDir, "issues.jsonl")

	// Create test files
	if err := os.WriteFile(dbFile, []byte("db"), 0600); err != nil {
		t.Fatalf("Failed to create db file: %v", err)
	}
	if err := os.WriteFile(jsonlFile, []byte("jsonl"), 0600); err != nil {
		t.Fatalf("Failed to create jsonl file: %v", err)
	}

	// Set JSONL mtime to 1 hour in the future (simulating clock skew)
	futureTime := time.Now().Add(1 * time.Hour)
	if err := os.Chtimes(jsonlFile, futureTime, futureTime); err != nil {
		t.Fatalf("Failed to set future mtime: %v", err)
	}

	// Touch the DB file with JSONL path
	if err := TouchDatabaseFile(dbFile, jsonlFile); err != nil {
		t.Fatalf("TouchDatabaseFile failed: %v", err)
	}

	// Get DB mtime
	dbInfo, err := os.Stat(dbFile)
	if err != nil {
		t.Fatalf("Failed to stat db file: %v", err)
	}

	jsonlInfo, err := os.Stat(jsonlFile)
	if err != nil {
		t.Fatalf("Failed to stat jsonl file: %v", err)
	}

	// Verify DB mtime is at least as new as JSONL mtime
	// (should be JSONL mtime + 1ns to handle clock skew)
	if dbInfo.ModTime().Before(jsonlInfo.ModTime()) {
		t.Errorf("DB mtime should be >= JSONL mtime when JSONL is in future")
		t.Errorf("DB mtime: %v, JSONL mtime: %v", dbInfo.ModTime(), jsonlInfo.ModTime())
	}
}

// TestTouchDatabaseFile_NonExistentFile verifies that TouchDatabaseFile
// returns nil (no error) when the database file doesn't exist.
// This is the case for Dolt backend which uses JSONL-only (no beads.db).
// Fixes bd-f74e54: bd sync warning about missing beads.db in Dolt mode.
func TestTouchDatabaseFile_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentFile := filepath.Join(tmpDir, "nonexistent.db")

	// Touch a non-existent file should return nil (not an error)
	err := TouchDatabaseFile(nonExistentFile, "")
	if err != nil {
		t.Errorf("TouchDatabaseFile should return nil for non-existent file, got: %v", err)
	}

	// Verify the file was NOT created
	if _, err := os.Stat(nonExistentFile); !os.IsNotExist(err) {
		t.Errorf("TouchDatabaseFile should not create the file if it doesn't exist")
	}
}
