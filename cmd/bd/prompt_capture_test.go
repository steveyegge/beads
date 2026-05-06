package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/utils"
)

func TestMergePromptLabels(t *testing.T) {
	got := mergePromptLabels([]string{"product", "prompt", " user-request ", "", "product", "audit"})
	want := []string{"prompt", "user-request", "product", "audit"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mergePromptLabels() = %#v, want %#v", got, want)
	}
}

func TestPromptCaptureMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Fatalf("restore cwd: %v", err)
		}
	})

	raw, err := promptCaptureMetadata("sess-42", "codex", "Summarize prompt")
	if err != nil {
		t.Fatalf("promptCaptureMetadata: %v", err)
	}

	var metadata map[string]string
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatalf("metadata json: %v", err)
	}

	for key, want := range map[string]string{
		"kind":        "prompt",
		"source":      "user_prompt",
		"session_id":  "sess-42",
		"source_tool": "codex",
		"summary":     "Summarize prompt",
	} {
		if metadata[key] != want {
			t.Errorf("metadata[%q] = %q, want %q", key, metadata[key], want)
		}
	}
	if !utils.PathsEqual(metadata["cwd"], tmpDir) {
		t.Errorf("metadata cwd = %q, want %q", metadata["cwd"], tmpDir)
	}
	if metadata["captured_at"] == "" {
		t.Error("metadata captured_at is empty")
	}
	if metadata["actor"] == "" {
		t.Error("metadata actor is empty")
	}
}

func TestGetPromptTextFlag(t *testing.T) {
	newCmd := func() *cobra.Command {
		cmd := &cobra.Command{Use: "capture"}
		cmd.Flags().StringP("description", "d", "", "Raw prompt text")
		cmd.Flags().String("body-file", "", "Read raw prompt text from file")
		cmd.Flags().Bool("stdin", false, "Read raw prompt text from stdin")
		return cmd
	}

	t.Run("Description", func(t *testing.T) {
		cmd := newCmd()
		if err := cmd.ParseFlags([]string{"--description", "raw user prompt"}); err != nil {
			t.Fatalf("parse flags: %v", err)
		}
		if got := getPromptTextFlag(cmd); got != "raw user prompt" {
			t.Fatalf("getPromptTextFlag() = %q", got)
		}
	})

	t.Run("BodyFile", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "prompt.txt")
		if err := os.WriteFile(path, []byte("prompt from file"), 0644); err != nil {
			t.Fatalf("write prompt file: %v", err)
		}
		cmd := newCmd()
		if err := cmd.ParseFlags([]string{"--body-file", path}); err != nil {
			t.Fatalf("parse flags: %v", err)
		}
		if got := getPromptTextFlag(cmd); got != "prompt from file" {
			t.Fatalf("getPromptTextFlag() = %q", got)
		}
	})

	t.Run("Stdin", func(t *testing.T) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatalf("pipe: %v", err)
		}
		oldStdin := os.Stdin
		os.Stdin = r
		t.Cleanup(func() { os.Stdin = oldStdin })
		go func() {
			_, _ = w.WriteString("prompt from stdin")
			_ = w.Close()
		}()

		cmd := newCmd()
		if err := cmd.ParseFlags([]string{"--stdin"}); err != nil {
			t.Fatalf("parse flags: %v", err)
		}
		if got := getPromptTextFlag(cmd); got != "prompt from stdin" {
			t.Fatalf("getPromptTextFlag() = %q", got)
		}
	})
}

func TestPromptCommandRegistered(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"prompt", "capture"})
	if err != nil {
		t.Fatalf("find prompt capture: %v", err)
	}
	if cmd != promptCaptureCmd {
		t.Fatalf("root prompt capture command mismatch")
	}

	for _, name := range []string{"title", "summary", "parent", "session", "source-tool", "description", "body-file", "stdin", "labels", "label", "silent"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing prompt capture flag %q", name)
		}
	}
	if !strings.Contains(cmd.Short, "Capture") {
		t.Errorf("unexpected short help: %q", cmd.Short)
	}
}
