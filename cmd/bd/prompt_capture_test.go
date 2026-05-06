package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/steveyegge/beads/internal/utils"
)

var promptCaptureCLIMutex sync.Mutex

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

func TestPromptCaptureCommandCreatesPromptBead(t *testing.T) {
	dir := t.TempDir()
	runPromptCaptureCommand(t, dir, "init", "--prefix", "pc", "--quiet")

	parentOut := runPromptCaptureCommand(t, dir, "create", "Parent prompt work", "--json")
	parentID := parsePromptCaptureIssueID(t, parentOut)

	bodyPath := filepath.Join(dir, "prompt.txt")
	if err := os.WriteFile(bodyPath, []byte("please capture this exact prompt"), 0600); err != nil {
		t.Fatalf("write prompt body: %v", err)
	}

	out := runPromptCaptureCommand(
		t,
		dir,
		"prompt",
		"capture",
		"--json",
		"--title", "Capture CLI prompt",
		"--summary", "Capture prompt from CLI",
		"--session", "sess-cli",
		"--source-tool", "codex",
		"--parent", parentID,
		"--label", "product",
		"--labels", "handoff",
		"--body-file", bodyPath,
	)

	var issue struct {
		ID                 string          `json:"id"`
		Title              string          `json:"title"`
		Description        string          `json:"description"`
		AcceptanceCriteria string          `json:"acceptance_criteria"`
		SourceSystem       string          `json:"source_system"`
		Labels             []string        `json:"labels"`
		Metadata           json.RawMessage `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(extractPromptCaptureJSON(out)), &issue); err != nil {
		t.Fatalf("parse prompt capture JSON: %v\n%s", err, out)
	}

	if issue.ID == "" {
		t.Fatal("expected prompt issue ID")
	}
	if issue.Title != "Capture CLI prompt" {
		t.Fatalf("title = %q", issue.Title)
	}
	if issue.Description != "please capture this exact prompt" {
		t.Fatalf("description = %q", issue.Description)
	}
	if !strings.Contains(issue.AcceptanceCriteria, "Prompt captured") {
		t.Fatalf("acceptance criteria = %q", issue.AcceptanceCriteria)
	}
	if issue.SourceSystem != "bd prompt capture" {
		t.Fatalf("source system = %q", issue.SourceSystem)
	}

	for _, want := range []string{"prompt", "user-request", "product", "handoff"} {
		if !hasPromptCaptureLabel(issue.Labels, want) {
			t.Fatalf("missing label %q in %#v", want, issue.Labels)
		}
	}

	var metadata map[string]string
	if err := json.Unmarshal(issue.Metadata, &metadata); err != nil {
		t.Fatalf("metadata JSON: %v\n%s", err, issue.Metadata)
	}
	for key, want := range map[string]string{
		"kind":        "prompt",
		"source":      "user_prompt",
		"session_id":  "sess-cli",
		"source_tool": "codex",
		"summary":     "Capture prompt from CLI",
	} {
		if metadata[key] != want {
			t.Fatalf("metadata[%q] = %q, want %q", key, metadata[key], want)
		}
	}
	if metadata["actor"] != "prompt-test-user" {
		t.Fatalf("metadata actor = %q", metadata["actor"])
	}
	if !utils.PathsEqual(metadata["cwd"], dir) {
		t.Fatalf("metadata cwd = %q, want %q", metadata["cwd"], dir)
	}

	showOut := runPromptCaptureCommand(t, dir, "show", issue.ID, "--json")
	var shown []struct {
		Dependencies []struct {
			ID             string `json:"id"`
			DependencyType string `json:"dependency_type"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(extractPromptCaptureJSON(showOut)), &shown); err != nil {
		t.Fatalf("parse show JSON: %v\n%s", err, showOut)
	}
	if len(shown) != 1 {
		t.Fatalf("expected one shown issue, got %d", len(shown))
	}
	foundParent := false
	for _, dep := range shown[0].Dependencies {
		if dep.ID == parentID && dep.DependencyType == "parent-child" {
			foundParent = true
			break
		}
	}
	if !foundParent {
		t.Fatalf("expected parent dependency %s in %#v", parentID, shown[0].Dependencies)
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

func runPromptCaptureCommand(t *testing.T, dir string, args ...string) string {
	t.Helper()

	promptCaptureCLIMutex.Lock()
	defer promptCaptureCLIMutex.Unlock()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	oldDir, _ := os.Getwd()
	oldArgs := os.Args

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}

	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout = wOut
	os.Stderr = wErr

	restoreEnv := setPromptCaptureEnv(t, "BEADS_TEST_MODE", "1")
	defer restoreEnv()
	restoreActor := setPromptCaptureEnv(t, "BEADS_ACTOR", "prompt-test-user")
	defer restoreActor()
	restoreLegacyActor := setPromptCaptureEnv(t, "BD_ACTOR", "prompt-test-user")
	defer restoreLegacyActor()
	restoreBeadsDir := setPromptCaptureEnv(t, "BEADS_DIR", filepath.Join(dir, ".beads"))
	defer restoreBeadsDir()

	rootCmd.SetArgs(args)
	os.Args = append([]string{"bd"}, args...)
	err := rootCmd.Execute()

	if store != nil {
		_ = store.Close()
		store = nil
	}
	dbPath = ""
	actor = ""
	jsonOutput = false
	sandboxMode = false
	readonlyMode = false
	serverMode = false
	rootCtx = nil
	rootCancel = nil
	resetCommandContext()

	time.Sleep(10 * time.Millisecond)

	_ = wOut.Close()
	_ = wErr.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	_ = os.Chdir(oldDir)
	os.Args = oldArgs
	rootCmd.SetArgs(nil)

	var outBuf, errBuf bytes.Buffer
	_, _ = io.Copy(&outBuf, rOut)
	_, _ = io.Copy(&errBuf, rErr)
	_ = rOut.Close()
	_ = rErr.Close()

	stdout := outBuf.String()
	stderr := errBuf.String()
	if err != nil {
		t.Fatalf("bd %v failed: %v\nStdout: %s\nStderr: %s", args, err, stdout, stderr)
	}

	return stdout
}

func setPromptCaptureEnv(t *testing.T, key, value string) func() {
	t.Helper()
	old, ok := os.LookupEnv(key)
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
	return func() {
		if ok {
			_ = os.Setenv(key, old)
		} else {
			_ = os.Unsetenv(key)
		}
	}
}

func parsePromptCaptureIssueID(t *testing.T, out string) string {
	t.Helper()
	var issue struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(extractPromptCaptureJSON(out)), &issue); err != nil {
		t.Fatalf("parse issue JSON: %v\n%s", err, out)
	}
	if issue.ID == "" {
		t.Fatalf("missing issue ID in output: %s", out)
	}
	return issue.ID
}

func extractPromptCaptureJSON(s string) string {
	if i := strings.IndexAny(s, "[{"); i >= 0 {
		return s[i:]
	}
	return s
}

func hasPromptCaptureLabel(labels []string, want string) bool {
	for _, label := range labels {
		if label == want {
			return true
		}
	}
	return false
}
