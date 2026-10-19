package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An unset or a set must not also rewrite the end of the file.
// commentOutYamlKey used to drop the document's trailing newline, so
// `bd config unset` produced a no-newline-at-end-of-file change on top of the
// line it meant to comment out — and did so even for a key the document does
// not contain, i.e. when nothing was edited at all. updateYamlKey's flat path
// had the same bufio.Scanner + strings.Join shape, so `bd config set` of a
// single-segment key dropped the terminator from every terminated config.yaml
// it touched. A config.yaml is git-tracked, so that is a spurious line in
// someone's review.
//
// The guarantee is scoped to LF documents. A CRLF file is still normalized to
// LF by an unset that actually comments a key out — that predates this fix and
// is pinned below as known-and-accepted rather than quietly claimed away. An
// unset that changes nothing no longer rewrites the file at all; see
// TestUnsetOfAnAbsentKeyLeavesACRLFFileUntouched.
func TestCommentOutYamlKeyPreservesTheTrailingNewline(t *testing.T) {
	for _, tc := range []struct {
		name, content, key string
	}{
		{"nested key present", "# header\nissue_prefix: vp\ndolt:\n  mode: server\n", "dolt.mode"},
		{"flat dotted key present", "issue_prefix: vp\ndolt.mode: server\n", "dolt.mode"},
		{"single-segment key present", "issue_prefix: vp\nexport.auto: true\n", "issue_prefix"},
		{"key absent — nothing is edited", "issue_prefix: vp\n", "dolt.mode"},

		// A blank line at the end of a YAML file is an ordinary shape, and
		// bufio.Scanner collapses the whole run rather than just the final
		// newline. Asserting only that SOME trailing newline survived cannot
		// see that: the output still ends in "\n", one short.
		{"ends in a blank line", "issue_prefix: vp\ndolt:\n  mode: server\n\n", "dolt.mode"},
		{"ends in two blank lines", "issue_prefix: vp\ndolt:\n  mode: server\n\n\n", "dolt.mode"},
		{"ends in a blank line, key absent", "issue_prefix: vp\nother: x\n\n", "dolt.mode"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := commentOutYamlKey(tc.content, tc.key)
			if err != nil {
				t.Fatalf("commentOutYamlKey: %v", err)
			}
			want := tc.content[len(strings.TrimRight(tc.content, "\n")):]
			if got := out[len(strings.TrimRight(out, "\n")):]; got != want {
				t.Errorf("trailing newline run = %q, want %q:\nin:  %q\nout: %q", got, want, tc.content, out)
			}
		})
	}

	// A document that genuinely has no trailing newline keeps not having one:
	// the rule is "preserve", not "always append".
	t.Run("absent trailing newline is not invented", func(t *testing.T) {
		out, err := commentOutYamlKey("issue_prefix: vp\ndolt.mode: server", "dolt.mode")
		if err != nil {
			t.Fatalf("commentOutYamlKey: %v", err)
		}
		if strings.HasSuffix(out, "\n") {
			t.Errorf("a trailing newline was invented for a document that had none: %q", out)
		}
	})

	// bufio.ScanLines strips the "\r" from every line, so an unset rewrites a
	// CRLF document to LF throughout. That is pre-existing and unchanged by the
	// trailing-run logic, but the tail is where it is least visible: TrimRight
	// on "\n" leaves the final "\r" behind, so the run that gets re-attached is
	// a bare "\n" and the end of the file reads clean while every line ending
	// above it changed. Pinned so the normalization is documented, and so a
	// future change to it has to be deliberate.
	t.Run("CRLF is normalized to LF, trailing run included", func(t *testing.T) {
		out, err := commentOutYamlKey("issue_prefix: vp\r\ndolt.mode: server\r\n", "dolt.mode")
		if err != nil {
			t.Fatalf("commentOutYamlKey: %v", err)
		}
		if want := "issue_prefix: vp\n# dolt.mode: server\n"; out != want {
			t.Errorf("CRLF normalization changed:\ngot:  %q\nwant: %q", out, want)
		}
	})
}

// The set twin of the test above, on the flat single-segment path only: every
// dotted key is handed to updateNestedYamlKey before the scanner is reached,
// so the rows below are all single-segment keys — the shapes `bd config set`
// writes through the same scanner + join tail that commentOutYamlKey had.
func TestUpdateYamlKeyPreservesTheTrailingNewline(t *testing.T) {
	for _, tc := range []struct {
		name, content, key, value, want string
	}{
		{"single-segment key present", "issue_prefix: vp\ndolt.mode: server\n", "issue_prefix", "zz", "issue_prefix: \"zz\"\ndolt.mode: server\n"},
		{"commented key present", "# actor: \"\"\nother: x\n", "actor", "b", "actor: \"b\"\nother: x\n"},
		{"key on the last line", "other: x\nactor: a\n", "actor", "b", "other: x\nactor: \"b\"\n"},
		{"key absent, appended", "other: x\n", "actor", "b", "other: x\n\nactor: \"b\"\n"},

		// The same blank-tail shapes as the unset table: the scanner drops the
		// whole run, not just the final newline, so the rows assert the exact
		// output rather than that SOME trailing newline survived.
		{"ends in a blank line", "actor: a\nother: x\n\n", "actor", "b", "actor: \"b\"\nother: x\n\n"},
		{"ends in two blank lines", "actor: a\nother: x\n\n\n", "actor", "b", "actor: \"b\"\nother: x\n\n\n"},
		{"ends in a blank line, key absent", "other: x\n\n", "actor", "b", "other: x\n\nactor: \"b\"\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, err := updateYamlKey(tc.content, tc.key, tc.value)
			if err != nil {
				t.Fatalf("updateYamlKey: %v", err)
			}
			if out != tc.want {
				t.Errorf("updateYamlKey = %q, want %q", out, tc.want)
			}
			gotRun := out[len(strings.TrimRight(out, "\n")):]
			wantRun := tc.content[len(strings.TrimRight(tc.content, "\n")):]
			if gotRun != wantRun {
				t.Errorf("trailing newline run = %q, want %q:\nin:  %q\nout: %q", gotRun, wantRun, tc.content, out)
			}
		})
	}

	// The append branch is where the loss was worst: the new key line became
	// the last line, so it landed unterminated, and the blank line the branch
	// adds as a separator came out of the run the scanner had already eaten.
	// Pin the exact shape: separator kept, the document's own terminator
	// re-attached after the new key.
	t.Run("appended key: separator kept, terminator restored", func(t *testing.T) {
		out, err := updateYamlKey("x\n", "newkey", "v")
		if err != nil {
			t.Fatalf("updateYamlKey: %v", err)
		}
		if want := "x\n\nnewkey: \"v\"\n"; out != want {
			t.Errorf("updateYamlKey(\"x\\n\", ...) = %q, want %q", out, want)
		}

		// The existing blank line is the separator; the document's two-newline
		// run is re-attached after the new key rather than collapsed.
		out, err = updateYamlKey("x\n\n", "newkey", "v")
		if err != nil {
			t.Fatalf("updateYamlKey: %v", err)
		}
		if want := "x\n\nnewkey: \"v\"\n\n"; out != want {
			t.Errorf("updateYamlKey(\"x\\n\\n\", ...) = %q, want %q", out, want)
		}
	})

	// The rule is preserve, not always-append: a document written without a
	// final newline keeps not having one, on both the update and append arms.
	t.Run("absent trailing newline is not invented", func(t *testing.T) {
		for _, tc := range []struct {
			name, content, key string
		}{
			{"key present", "actor: a\nother: x", "actor"},
			{"key absent, appended", "other: x", "actor"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				out, err := updateYamlKey(tc.content, tc.key, "b")
				if err != nil {
					t.Fatalf("updateYamlKey: %v", err)
				}
				if strings.HasSuffix(out, "\n") {
					t.Errorf("a trailing newline was invented for a document that had none: %q", out)
				}
			})
		}
	})

	// The set-side twin of unset's CRLF pin: updateYamlKey normalizes CRLF
	// after the nested dispatch, the rule commentOutYamlKey applies up front,
	// so a CRLF document comes back LF throughout, trailing run included.
	t.Run("CRLF is normalized to LF, trailing run included", func(t *testing.T) {
		out, err := updateYamlKey("actor: a\r\nother: x\r\n", "actor", "b")
		if err != nil {
			t.Fatalf("updateYamlKey: %v", err)
		}
		if want := "actor: \"b\"\nother: x\n"; out != want {
			t.Errorf("CRLF normalization changed:\ngot:  %q\nwant: %q", out, want)
		}

		// A CRLF file ending in blank lines keeps the whole run — the same run
		// an unset leaves on the same document, so the two verbs agree.
		for _, tc := range []struct {
			content, want string
		}{
			{"actor: a\r\nother: x\r\n\r\n", "actor: \"b\"\nother: x\n\n"},
			{"actor: a\r\nother: x\r\n\r\n\r\n", "actor: \"b\"\nother: x\n\n\n"},
		} {
			out, err := updateYamlKey(tc.content, "actor", "b")
			if err != nil {
				t.Fatalf("updateYamlKey: %v", err)
			}
			if out != tc.want {
				t.Errorf("CRLF blank-tail output = %q, want %q:\nin: %q", out, tc.want, tc.content)
			}
			unsetOut, err := commentOutYamlKey(tc.content, "actor")
			if err != nil {
				t.Fatalf("commentOutYamlKey: %v", err)
			}
			unsetRun := unsetOut[len(strings.TrimRight(unsetOut, "\n")):]
			if got := out[len(strings.TrimRight(out, "\n")):]; got != unsetRun {
				t.Errorf("set trailing run %q differs from unset's %q on the same document", got, unsetRun)
			}
		}
	})
}

// The same property through the public writer, which is what `bd config unset`
// actually calls.
func TestUnsetThroughTheFileKeepsTheTrailingNewline(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(beadsDir, "config.yaml")
	// The file ends in a blank line, the shape a single appended "\n" does not
	// restore.
	const body = "issue_prefix: vp\nexport.auto: true\ndolt:\n  mode: server\n\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DIR", beadsDir)
	if _, err := UnsetYamlConfig("dolt.mode"); err != nil {
		t.Fatalf("UnsetYamlConfig: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := body[len(strings.TrimRight(body, "\n")):]
	if got := string(after)[len(strings.TrimRight(string(after), "\n")):]; got != want {
		t.Errorf("unset rewrote the end of the file: trailing run %q, want %q\n%q", got, want, string(after))
	}
}

// The set twin of the test above, through the public writer `bd config set`
// actually calls. The key is single-segment, so the write goes down the flat
// path this fix targets, past a nested block it must not disturb.
func TestSetThroughTheFileKeepsTheTrailingNewline(t *testing.T) {
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(beadsDir, "config.yaml")
	// The file ends in a blank line, the shape a single appended "\n" does not
	// restore.
	const body = "issue_prefix: vp\nactor: alice\ndolt:\n  mode: server\n\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BEADS_DIR", beadsDir)
	if err := SetYamlConfig("actor", "bob"); err != nil {
		t.Fatalf("SetYamlConfig: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := body[len(strings.TrimRight(body, "\n")):]
	if got := string(after)[len(strings.TrimRight(string(after), "\n")):]; got != want {
		t.Errorf("set rewrote the end of the file: trailing run %q, want %q\n%q", got, want, string(after))
	}
}

// An unset of a key the document does not contain must not report a write. The
// bool that says so used to come from comparing commentOutYamlKey's result
// against the raw file bytes, and those sit on opposite sides of the CRLF
// normalization the callee applies unilaterally — so on a CRLF file the two
// strings always differed and every unset looked like a change. `bd config
// unset` then printed "Unset <key> (in config.yaml)" for a key that was never
// there, the exact false claim this command's fix set out to remove, and
// rewrote the whole file to LF on the way past. Windows is a supported
// platform and every other change-report fixture in this package is LF, so
// nothing else in-tree can catch a regression here.
//
// Both public callers had the shape, so both are pinned.
func TestUnsetOfAnAbsentKeyLeavesACRLFFileUntouched(t *testing.T) {
	const body = "issue_prefix: vp\r\nexport.auto: true\r\n"

	assertUntouched := func(t *testing.T, path string) {
		t.Helper()
		after, err := os.ReadFile(path) //nolint:gosec // test-owned temp path
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != body {
			t.Errorf("a no-op unset rewrote the file:\ngot:  %q\nwant: %q", string(after), body)
		}
	}

	t.Run("UnsetYamlConfig", func(t *testing.T) {
		beadsDir := filepath.Join(t.TempDir(), ".beads")
		if err := os.MkdirAll(beadsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(beadsDir, "config.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("BEADS_DIR", beadsDir)

		changed, err := UnsetYamlConfig("dolt.mode")
		if err != nil {
			t.Fatalf("UnsetYamlConfig: %v", err)
		}
		if changed {
			t.Error("UnsetYamlConfig() changed = true for a key the file does not contain")
		}
		assertUntouched(t, path)
	})

	t.Run("UnsetUserYamlConfig", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		path, err := UserConfigYamlPath()
		if err != nil {
			t.Fatalf("UserConfigYamlPath: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		changed, err := UnsetUserYamlConfig("dolt.mode")
		if err != nil {
			t.Fatalf("UnsetUserYamlConfig: %v", err)
		}
		if changed {
			t.Error("UnsetUserYamlConfig() changed = true for a key the file does not contain")
		}
		assertUntouched(t, path)
	})
}

// The other direction of the same compare. Normalizing before the call is what
// keeps the absent-key case above a no-op, but it also means each caller now
// decides between "write" and "was not set" on content it has already rewritten
// in memory. A caller that returned early whenever that normalization changed
// anything would pass every test above and turn each unset on a CRLF
// config.yaml into a silent "<key> was not set" while the key stayed effective.
// The absent-key cell only proves the no-op direction, and the commentOutYamlKey
// CRLF pin sits below the decision, so neither can see that.
//
// The written file comes back LF throughout. That is base behavior, pinned here
// through both callers as known-and-accepted, like the callee-level pin above.
func TestUnsetOfAPresentKeyInACRLFFileReportsTheWrite(t *testing.T) {
	const (
		body = "issue_prefix: vp\r\ndolt.mode: server\r\nexport.auto: true\r\n"
		want = "issue_prefix: vp\n# dolt.mode: server\nexport.auto: true\n"
	)

	assertWritten := func(t *testing.T, path string) {
		t.Helper()
		after, err := os.ReadFile(path) //nolint:gosec // test-owned temp path
		if err != nil {
			t.Fatal(err)
		}
		if string(after) != want {
			t.Errorf("the unset did not write the key out:\ngot:  %q\nwant: %q", string(after), want)
		}
	}

	t.Run("UnsetYamlConfig", func(t *testing.T) {
		beadsDir := filepath.Join(t.TempDir(), ".beads")
		if err := os.MkdirAll(beadsDir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(beadsDir, "config.yaml")
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("BEADS_DIR", beadsDir)

		changed, err := UnsetYamlConfig("dolt.mode")
		if err != nil {
			t.Fatalf("UnsetYamlConfig: %v", err)
		}
		if !changed {
			t.Error("UnsetYamlConfig() changed = false for a key the file contains")
		}
		assertWritten(t, path)
	})

	t.Run("UnsetUserYamlConfig", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		path, err := UserConfigYamlPath()
		if err != nil {
			t.Fatalf("UserConfigYamlPath: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		changed, err := UnsetUserYamlConfig("dolt.mode")
		if err != nil {
			t.Fatalf("UnsetUserYamlConfig: %v", err)
		}
		if !changed {
			t.Error("UnsetUserYamlConfig() changed = false for a key the file contains")
		}
		assertWritten(t, path)
	})
}

// The boundary of the fix. Every dotted key is handed to updateNestedYamlKey
// before updateYamlKey's scanner and CRLF normalization are reached, and the
// nested writer's line-rewriting branches deliberately carry "\r" through on
// the lines they do not touch. So the wrapper must stay byte-identical to a
// direct call on the same CRLF documents: it would diverge the moment the
// normalization moved above the dispatch, silently changing what the nested
// writer sees.
func TestUpdateYamlKeyLeavesTheNestedPathAlone(t *testing.T) {
	for _, tc := range []struct{ name, content string }{
		{"literal dotted key", "issue_prefix: vp\r\ndolt.mode: server\r\n"},
		{"nested block", "issue_prefix: vp\r\ndolt:\r\n  mode: server\r\n"},
		{"absent key", "issue_prefix: vp\r\n"},
		{"comment-only document", "# just a comment\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viaSet, err := updateYamlKey(tc.content, "dolt.mode", "embedded")
			if err != nil {
				t.Fatalf("updateYamlKey: %v", err)
			}
			direct, ok, err := updateNestedYamlKey(tc.content, "dolt.mode", "embedded")
			if err != nil {
				t.Fatalf("updateNestedYamlKey: %v", err)
			}
			if !ok {
				t.Fatal("updateNestedYamlKey returned ok=false for a dotted key")
			}
			if viaSet != direct {
				t.Errorf("updateYamlKey diverged from updateNestedYamlKey on the same document:\nvia set: %q\ndirect:  %q", viaSet, direct)
			}
		})
	}
}
