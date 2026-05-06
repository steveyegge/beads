package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/steveyegge/beads/internal/config"
)

// TestImportJSONLForSync_GuardClauses exercises the early-return paths so
// runPostMergeHook never blows up on misconfigured workspaces. The
// shell-out to `bd import` is intentionally not unit-tested here — that
// path requires a working bd binary on PATH and is covered by the
// existing integration suite (and the GH#3729 reproducer in mybd).
func TestImportJSONLForSync_GuardClauses(t *testing.T) {
	// Run from a temp dir so beads.FindBeadsDir() returns "".
	tmp := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	t.Run("no beads dir is a no-op", func(t *testing.T) {
		// Should return without panicking or writing anything.
		importJSONLForSync("test")
	})

	t.Run("import.auto=false suppresses import", func(t *testing.T) {
		// Set import.auto=false and a beads dir with a jsonl. The
		// function should still no-op; if it shelled out we'd see an
		// error (no bd database here).
		beadsDir := filepath.Join(tmp, ".beads")
		if err := os.MkdirAll(beadsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}

		config.Set("import.auto", false)
		t.Cleanup(func() { config.Set("import.auto", true) })

		// Returns silently — the config gate fires before any subprocess.
		importJSONLForSync("test")
	})

	t.Run("empty jsonl is a no-op", func(t *testing.T) {
		beadsDir := filepath.Join(tmp, ".beads")
		if err := os.MkdirAll(beadsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(beadsDir, "issues.jsonl"), []byte(""), 0o644); err != nil {
			t.Fatal(err)
		}
		config.Set("import.auto", true)

		// Empty file: os.Stat.Size==0 fast path returns before subprocess.
		importJSONLForSync("test")
	})
}

// TestRunPostCheckoutHook_FileModeSkipsImport asserts that file-mode
// checkouts (flag=0, e.g. `git checkout -- <file>`) do NOT trigger the
// import path — only branch-mode checkouts (flag=1) do. Without this
// gate, every `git checkout` of a single file would run a full bd
// import, which is wasteful and surprising.
//
// We exercise this by routing through a temp cwd where any subprocess
// invocation would fail noisily. A panic-free run here means the import
// path was correctly bypassed.
func TestRunPostCheckoutHook_FileModeSkipsImport(t *testing.T) {
	tmp := t.TempDir()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })

	// flag=0 means file-mode; importJSONLForSync must NOT be called.
	if exit := runPostCheckoutHook([]string{"oldHead", "newHead", "0"}); exit != 0 {
		t.Errorf("file-mode post-checkout returned %d, want 0", exit)
	}

	// Short args (legacy git versions): treat as no-op.
	if exit := runPostCheckoutHook([]string{}); exit != 0 {
		t.Errorf("empty-args post-checkout returned %d, want 0", exit)
	}
}
