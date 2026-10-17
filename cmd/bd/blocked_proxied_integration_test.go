//go:build cgo

package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/steveyegge/beads/internal/types"
)

func bdProxiedBlockedJSON(t *testing.T, bd string, p proxiedProject, args ...string) []*types.BlockedIssue {
	t.Helper()
	fullArgs := append([]string{"blocked", "--json"}, args...)
	stdout, stderr, err := bdProxiedRunBuffers(t, bd, p.dir, fullArgs...)
	if err != nil {
		t.Fatalf("bd blocked --json %s failed: %v\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), err, stdout, stderr)
	}
	s := strings.TrimSpace(stdout)
	start := strings.Index(s, "[")
	if start < 0 {
		t.Fatalf("no JSON array in blocked --json output: %s", stdout)
	}
	var out []*types.BlockedIssue
	if err := json.Unmarshal([]byte(s[start:]), &out); err != nil {
		t.Fatalf("parse blocked JSON: %v\n%s", err, s[start:])
	}
	return out
}

func bdProxiedBlockedCapture(t *testing.T, bd string, p proxiedProject, args ...string) (string, string) {
	t.Helper()
	fullArgs := append([]string{"blocked"}, args...)
	stdout, stderr, err := bdProxiedRunBuffers(t, bd, p.dir, fullArgs...)
	if err != nil {
		t.Fatalf("bd blocked %s failed: %v\nstdout:\n%s\nstderr:\n%s",
			strings.Join(args, " "), err, stdout, stderr)
	}
	return stdout, stderr
}

func TestProxiedServerBlocked(t *testing.T) {
	requireSharedProxiedServer(t)
	t.Parallel()
	bd := buildEmbeddedBD(t)

	t.Run("json_and_text_with_blockers", func(t *testing.T) {
		t.Parallel()
		p := newSharedProxiedProject(t, bd, "bk1")
		blocker := bdProxiedCreate(t, bd, p.dir, "The blocker")
		dependent := bdProxiedCreate(t, bd, p.dir, "I am blocked", "--deps", "depends-on:"+blocker.ID)

		got := bdProxiedBlockedJSON(t, bd, p)
		var entry *types.BlockedIssue
		for _, bi := range got {
			if bi.ID == dependent.ID {
				entry = bi
				break
			}
		}
		if entry == nil {
			out, _ := json.Marshal(got)
			t.Fatalf("expected %s in blocked listing, got: %s", dependent.ID, out)
		}
		if entry.BlockedByCount != 1 {
			t.Errorf("BlockedByCount = %d, want 1", entry.BlockedByCount)
		}
		if len(entry.BlockedBy) != 1 || entry.BlockedBy[0] != blocker.ID {
			t.Errorf("BlockedBy = %v, want [%s]", entry.BlockedBy, blocker.ID)
		}

		stdout, _ := bdProxiedBlockedCapture(t, bd, p)
		if !strings.Contains(stdout, "Blocked issues") {
			t.Errorf("expected heading in text output, got: %s", stdout)
		}
		if !strings.Contains(stdout, "Blocked by 1 open dependencies") {
			t.Errorf("expected blocker summary line, got: %s", stdout)
		}
	})

	t.Run("flags_reads_ids_from_stdin", func(t *testing.T) {
		t.Parallel()
		p := newSharedProxiedProject(t, bd, "bkf")
		blocker := bdProxiedCreate(t, bd, p.dir, "Flag blocker")
		dependent := bdProxiedCreate(t, bd, p.dir, "Flag dependent", "--deps", "depends-on:"+blocker.ID)

		cmd := exec.Command(bd, "blocked", "--flags", "--json")
		cmd.Dir = p.dir
		cmd.Env = bdProxiedEnv(p.dir)
		cmd.Stdin = strings.NewReader(dependent.ID + "\n" + blocker.ID + "\nbk-no-such-row\n")
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("bd blocked --flags --json: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
		var rows []struct {
			ID        string `json:"id"`
			IsBlocked bool   `json:"is_blocked"`
		}
		if err := json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &rows); err != nil {
			t.Fatalf("parse: %v\n%s", err, stdout.String())
		}
		if len(rows) != 2 || rows[0].ID != dependent.ID || !rows[0].IsBlocked || rows[1].ID != blocker.ID || rows[1].IsBlocked {
			t.Errorf("rows = %+v, want [{%s true} {%s false}] (unknown id omitted)", rows, dependent.ID, blocker.ID)
		}
	})

	t.Run("empty_case", func(t *testing.T) {
		t.Parallel()
		p := newSharedProxiedProject(t, bd, "bk2")
		bdProxiedCreate(t, bd, p.dir, "Nothing blocked here")

		out, err := bdProxiedRun(t, bd, p.dir, "blocked", "--json")
		if err != nil {
			t.Fatalf("bd blocked --json failed: %v\n%s", err, out)
		}
		var empty []*types.BlockedIssue
		if err := json.Unmarshal(bytes.TrimSpace(out), &empty); err != nil {
			t.Fatalf("parse empty blocked JSON: %v\n%s", err, out)
		}
		if len(empty) != 0 {
			t.Errorf("expected [] for no blockers, got %d entries", len(empty))
		}

		stdout, _ := bdProxiedBlockedCapture(t, bd, p)
		if !strings.Contains(stdout, "No blocked issues") {
			t.Errorf("expected 'No blocked issues' message, got: %s", stdout)
		}
	})

	t.Run("parent_filter_restricts_to_descendants", func(t *testing.T) {
		t.Parallel()
		p := newSharedProxiedProject(t, bd, "bk3")
		parent := bdProxiedCreate(t, bd, p.dir, "Parent epic", "--type", "epic")
		blocker := bdProxiedCreate(t, bd, p.dir, "Inside blocker")
		inside := bdProxiedCreate(t, bd, p.dir, "Inside blocked", "--parent", parent.ID, "--deps", "depends-on:"+blocker.ID)
		outsideBlocker := bdProxiedCreate(t, bd, p.dir, "Outside blocker")
		outside := bdProxiedCreate(t, bd, p.dir, "Outside blocked", "--deps", "depends-on:"+outsideBlocker.ID)

		got := bdProxiedBlockedJSON(t, bd, p, "--parent", parent.ID)
		ids := map[string]bool{}
		for _, bi := range got {
			ids[bi.ID] = true
		}
		if !ids[inside.ID] {
			t.Errorf("expected descendant %s in --parent result", inside.ID)
		}
		if ids[outside.ID] {
			t.Errorf("non-descendant %s leaked into --parent result", outside.ID)
		}
	})
}
