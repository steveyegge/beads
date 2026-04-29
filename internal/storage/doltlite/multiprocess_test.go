//go:build cgo

package doltlite_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/steveyegge/beads/internal/storage/doltlite"
)

func TestConcurrentOpenWhilePeerStoreAlive(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	readyPath := filepath.Join(t.TempDir(), "ready")

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestConcurrentOpenHelper")
	cmd.Env = append(os.Environ(),
		"BEADS_DOLTLITE_OPEN_HELPER=1",
		"BEADS_DOLTLITE_TEST_DIR="+beadsDir,
		"BEADS_DOLTLITE_READY="+readyPath,
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(readyPath); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not open store")
		}
		time.Sleep(25 * time.Millisecond)
	}

	openCtx, openCancel := context.WithTimeout(t.Context(), time.Second)
	defer openCancel()
	store, err := doltlite.New(openCtx, beadsDir, "beads", "main")
	if err != nil {
		t.Fatalf("second open while peer store alive: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close second store: %v", err)
	}
}

func TestConcurrentOpenHelper(t *testing.T) {
	if os.Getenv("BEADS_DOLTLITE_OPEN_HELPER") != "1" {
		t.Skip("helper only")
	}
	beadsDir := os.Getenv("BEADS_DOLTLITE_TEST_DIR")
	readyPath := os.Getenv("BEADS_DOLTLITE_READY")
	store, err := doltlite.New(t.Context(), beadsDir, "beads", "main")
	if err != nil {
		t.Fatalf("helper open: %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := os.WriteFile(readyPath, []byte("ready\n"), 0o600); err != nil {
		t.Fatalf("write ready: %v", err)
	}
	time.Sleep(2 * time.Second)
}
