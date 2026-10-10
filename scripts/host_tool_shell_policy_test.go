package scripts_test

// Policy: no new ungated bare `go` exec in shell scripts under scripts/ that
// a Bazel test actually runs. This is the shell-script half of
// host_tool_exec_policy_test.go's Go-exec policy (see that file's package
// comment for the full rationale shared by both: under `bazel test` PATH is
// `hermetic_bin:` plus the strict action env, and a host `go` toolchain is
// not guaranteed to be on it the way the hermetic-first `dolt` is -- see
// f4-spec.md §9). "dolt" is out of scope here for the same reason it is out
// of scope there: it is the one tool that PATH ordering already makes safe.
//
// Scope: unlike the Go scanner (which can cheaply scan every *_test.go file
// in the tree), finding every shell script a Bazel test actually executes
// requires reading each candidate script's BUILD.bazel wiring (sh_test
// srcs/args/data, a go_test's data, or a Go test's exec.Command of a fixed
// script path) by hand; there is no existing Starlark-aware tool in this repo
// to do that automatically. shellPolicyScope below is that set, as of this
// writing, with how each entry was confirmed to run under `bazel test`:
//
//   scripts/ci/fmt-check.sh          PROGRAM of //scripts/repochecks:fmt_test
//                                    (run_check.sh execs it); data of
//                                    //scripts/prlintmake:prlintmake_test
//   scripts/ci/gofmt-bin.sh          data of prlintmake_test, which execs it
//                                    directly (gofmtbin_test.go) and fmt-check.sh
//                                    execs it as its first line
//   scripts/check-build-tags.sh      PROGRAM of repochecks:build_tags_test
//   scripts/check-go-install-guidance.sh  PROGRAM of repochecks:go_install_guidance_test
//   scripts/check-winget-portable-alias.sh  PROGRAM of repochecks:winget_portable_alias_test
//   scripts/check-migration-hygiene.sh  PROGRAM of repochecks:migration_hygiene_test
//   scripts/check-doc-freshness.sh   PROGRAM of repochecks:doc_freshness_test
//   scripts/check-workapi-frontend-boundary.sh  PROGRAM of repochecks:workapi_frontend_boundary_test
//   scripts/check-testing-short.sh   exec.Command("bash", ...)'d directly by
//                                    testing_short_tree_test.go, in this package
//   scripts/repochecks/run_check.sh  srcs of every repochecks sh_test above
//   scripts/repochecks/types_gen_drift_test.sh  srcs of repochecks:types_gen_drift_test
//   scripts/migration-test/lib/binary.sh  data of //tests/migration's
//                                    historical_upgrade_tests and legacy_bridge_test
//   scripts/migration-test/historical-dolt-upgrade-test.sh  ditto
//   scripts/migration-test/legacy-bridge-test.sh  ditto
//   scripts/migrate-legacy-to-current.sh  ditto (//scripts:migration_harness)
//
// Deliberately NOT in scope: scripts/ci/lib/test-env.sh and scripts/ci/pr-lint.sh
// (its own TEST_SRCDIR gate predates this policy and is good practice, but
// neither is wired as Bazel test data/srcs/args anywhere today -- `git grep
// -n` confirms it -- so nothing runs it inside a hermetic sandbox); and
// scripts/upgrade-smoke-test.sh, which tests/upgrade_smoke/BUILD.bazel
// exports_files()s for a doc-path check but never sh_test/go_test-execs.
// Widen shellPolicyScope, with the same kind of BUILD.bazel citation as
// above, if that changes.
//
// Detection is line-based, not a real shell parser: a line is scanned unless
// it is a `#` comment, inside a `<<'EOF'`/`<<EOF`-style heredoc body, or
// looks like message/search text (`printf`/`echo`/`git grep`) rather than a
// command -- the same kind of pragmatic, documented-false-negative tradeoff
// check-build-tags.sh's own header comment makes for its Go-source scan. A
// scanned line is flagged if it contains a bare `go` followed by a verb this
// policy recognizes (env, build, run, ...), unless the whole file mentions
// TEST_SRCDIR anywhere (same position-agnostic, file-wide gate the Go
// scanner uses) or the line is in shellExecAllowlist.

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// shellPolicyScope is the set of scripts/-rooted shell scripts confirmed (see
// the package comment) to run inside a `bazel test` sandbox.
var shellPolicyScope = []string{
	"scripts/ci/fmt-check.sh",
	"scripts/ci/gofmt-bin.sh",
	"scripts/check-build-tags.sh",
	"scripts/check-go-install-guidance.sh",
	"scripts/check-winget-portable-alias.sh",
	"scripts/check-migration-hygiene.sh",
	"scripts/check-doc-freshness.sh",
	"scripts/check-workapi-frontend-boundary.sh",
	"scripts/check-testing-short.sh",
	"scripts/repochecks/run_check.sh",
	"scripts/repochecks/types_gen_drift_test.sh",
	"scripts/migration-test/lib/binary.sh",
	"scripts/migration-test/historical-dolt-upgrade-test.sh",
	"scripts/migration-test/legacy-bridge-test.sh",
	"scripts/migrate-legacy-to-current.sh",
}

// shellExecAllowlist is file (repo-relative) -> line -> why a bare `go` exec
// there is safe despite findShellHostToolExecs flagging it: in every case the
// enclosing shell function returns early with an already-built/located
// binary a Bazel target's `env` sets (BEADS_TEST_GO-style dataflow gating,
// not a textual TEST_SRCDIR check), the shell-script analogue of
// host_tool_exec_policy_test.go's hostToolExecAllowlist.
var shellExecAllowlist = map[string]map[int]string{
	"scripts/ci/gofmt-bin.sh": {
		100: "only reached when GOFMT is unset; //scripts/repochecks:fmt_test sets it " +
			"(--env GOFMT=$(rlocationpath @go_sdk//:bin/gofmt)), and gofmtbin_test.go's " +
			"testGo(t) puts BEADS_TEST_GO's directory first on PATH before calling this script",
		104: "same gate as line 100",
		105: "same gate as line 100",
	},
	"scripts/migration-test/lib/binary.sh": {
		218: "build_verified_v091_source_binary: only reached when SOURCE_TAG_SQLITE_BIN is " +
			"unset; tests/migration/defs.bzl sets it from //tests/migration:bd_source_tag " +
			"for every historical_upgrade_tests target",
		281: "build_candidate: only reached when CANDIDATE_BIN is unset; tests/migration/defs.bzl " +
			"sets it from //cmd/bd:bd_for_tests",
	},
}

// shellHostToolVerbs are the `go` subcommands this policy treats as a real
// exec rather than incidental prose (e.g. "go.mod", "go$pinned").
var shellHostToolVerbs = regexp.MustCompile(`\bgo(?:\s+)(env|build|run|version|vet|mod|install|test|fmt|list|generate|get|work|doc|bug)\b`)

// heredocStart matches a `<<` (optionally `<<-`) redirection naming its
// terminator, with or without quotes: `<<EOF`, `<<-EOF`, `<<'EOF'`, `<<"EOF"`.
var heredocStart = regexp.MustCompile(`<<-?\s*(['"]?)(\w+)['"]?`)

type shellExecHit struct {
	line int
	verb string
}

// findShellHostToolExecs scans src (one shell script's contents) for bare
// `go <verb>` lines, skipping comments, heredoc bodies, and printf/echo/git
// grep message or search text. It reports every hit regardless of
// TEST_SRCDIR or the allowlist -- callers apply those.
func findShellHostToolExecs(src string) []shellExecHit {
	var hits []shellExecHit
	scanner := bufio.NewScanner(strings.NewReader(src))
	lineno := 0
	inHeredoc := false
	heredocDelim := ""
	for scanner.Scan() {
		lineno++
		line := scanner.Text()
		if inHeredoc {
			if strings.TrimSpace(line) == heredocDelim {
				inHeredoc = false
			}
			continue
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			continue
		}
		if m := heredocStart.FindStringSubmatch(line); m != nil {
			inHeredoc = true
			heredocDelim = m[2]
			// The redirection line itself (e.g. `cat >&2 <<'EOF'`) is still
			// scanned below; only what follows is heredoc body.
		}
		if strings.Contains(trimmed, "printf") || strings.HasPrefix(trimmed, "echo") ||
			strings.Contains(trimmed, "echo ") || strings.Contains(trimmed, "git grep") {
			continue
		}
		for _, m := range shellHostToolVerbs.FindAllStringSubmatch(line, -1) {
			hits = append(hits, shellExecHit{line: lineno, verb: m[1]})
		}
	}
	return hits
}

// shellFileIsGated reports whether src mentions TEST_SRCDIR anywhere, the
// file-wide, position-agnostic gate scripts/ci/fmt-check.sh and
// scripts/ci/lib/test-env.sh both use (see #7478).
func shellFileIsGated(src string) bool {
	return strings.Contains(src, "TEST_SRCDIR")
}

func TestShellHostToolExecPolicyScan(t *testing.T) {
	t.Run("ungated bare go build is reported", func(t *testing.T) {
		src := "#!/usr/bin/env bash\ngo build -o out ./cmd/bd\n"
		hits := findShellHostToolExecs(src)
		if len(hits) != 1 || hits[0].line != 2 || hits[0].verb != "build" {
			t.Fatalf("got %+v, want one hit at line 2 (build)", hits)
		}
		if shellFileIsGated(src) {
			t.Fatal("fixture has no TEST_SRCDIR mention; must not be gated")
		}
	})

	t.Run("comment lines are not scanned", func(t *testing.T) {
		src := "#!/usr/bin/env bash\n# go build is mentioned here only in prose\n"
		if hits := findShellHostToolExecs(src); len(hits) != 0 {
			t.Fatalf("got %+v, want no hits", hits)
		}
	})

	t.Run("heredoc bodies are not scanned", func(t *testing.T) {
		src := "#!/usr/bin/env bash\ncat >&2 <<'EOF'\nrun `go build|test|run` yourself\nEOF\n"
		if hits := findShellHostToolExecs(src); len(hits) != 0 {
			t.Fatalf("got %+v, want no hits (heredoc body)", hits)
		}
	})

	t.Run("printf and echo message text are not scanned", func(t *testing.T) {
		src := "#!/usr/bin/env bash\n" +
			"printf 'error: unsupported go install module path\\n' >&2\n" +
			"echo \"run go version to check\"\n"
		if hits := findShellHostToolExecs(src); len(hits) != 0 {
			t.Fatalf("got %+v, want no hits (printf/echo text)", hits)
		}
	})

	t.Run("git grep search patterns are not scanned", func(t *testing.T) {
		src := "#!/usr/bin/env bash\n" +
			"git grep -n -E 'go install github\\.com/x/y@latest' -- . || true\n"
		if hits := findShellHostToolExecs(src); len(hits) != 0 {
			t.Fatalf("got %+v, want no hits (git grep pattern)", hits)
		}
	})

	t.Run("command substitution exec is reported", func(t *testing.T) {
		src := "#!/usr/bin/env bash\n" +
			`if [[ "$(go env GOVERSION 2>/dev/null)" == "go1.25" ]]; then` + "\n"
		hits := findShellHostToolExecs(src)
		if len(hits) != 1 || hits[0].verb != "env" {
			t.Fatalf("got %+v, want one `env` hit", hits)
		}
	})

	t.Run("a TEST_SRCDIR mention anywhere gates the whole file", func(t *testing.T) {
		src := "#!/usr/bin/env bash\n" +
			"if [[ -z \"${TEST_SRCDIR:-}\" ]]; then\n" +
			"  go build -o out ./cmd/bd\n" +
			"fi\n"
		if hits := findShellHostToolExecs(src); len(hits) != 1 {
			t.Fatalf("got %+v, want the scan itself to still find the line", hits)
		}
		if !shellFileIsGated(src) {
			t.Fatal("file mentions TEST_SRCDIR; shellFileIsGated must report true")
		}
	})

	t.Run("dolt is never flagged", func(t *testing.T) {
		src := "#!/usr/bin/env bash\ndolt sql -q 'select 1'\n"
		if hits := findShellHostToolExecs(src); len(hits) != 0 {
			t.Fatalf("got %+v, want no hits (dolt is out of scope; see package comment)", hits)
		}
	})
}

// TestNoUngatedBareGoInBazelReachedShellScripts scans shellPolicyScope and
// fails on an unallowlisted, ungated bare `go` exec. See the package comment
// for scope and the detection tradeoffs.
func TestNoUngatedBareGoInBazelReachedShellScripts(t *testing.T) {
	root := sourceRepoRoot(t)
	seen := map[string]map[int]bool{}
	for _, rel := range shellPolicyScope {
		path := filepath.Join(root, filepath.FromSlash(rel))
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v (shellPolicyScope entry no longer exists; update it)", rel, err)
		}
		src := string(data)
		gated := shellFileIsGated(src)
		for _, hit := range findShellHostToolExecs(src) {
			if gated {
				continue
			}
			if why, ok := shellExecAllowlist[rel][hit.line]; ok {
				if seen[rel] == nil {
					seen[rel] = map[int]bool{}
				}
				seen[rel][hit.line] = true
				_ = why
				continue
			}
			t.Errorf("%s:%d: ungated bare `go %s`: under `bazel test` the hermetic-first PATH "+
				"may lack a host go toolchain, so this can exit 127 on some workers (f4-spec.md "+
				"§9); gate it on TEST_SRCDIR, resolve go through a Bazel-wired env var, or add a "+
				"justified shellExecAllowlist entry", rel, hit.line, hit.verb)
		}
	}
	for rel, lines := range shellExecAllowlist {
		for line := range lines {
			if !seen[rel][line] {
				t.Errorf("shellExecAllowlist entry %s:%d no longer matches an ungated bare go exec; remove it", rel, line)
			}
		}
	}
}

// TestShellPolicyScopeFilesExist is a guard against shellPolicyScope rotting
// silently: repoFiles-style tests elsewhere skip missing files quietly, but
// this list is hand-maintained, so a missing entry should fail loudly instead
// of just scanning fewer files than the comment above claims.
func TestShellPolicyScopeFilesExist(t *testing.T) {
	root := sourceRepoRoot(t)
	for _, rel := range shellPolicyScope {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Errorf("shellPolicyScope entry %s: %v", rel, err)
		}
	}
	if len(shellPolicyScope) < 10 {
		t.Fatalf("shellPolicyScope has only %d entries; did a bulk edit truncate it?", len(shellPolicyScope))
	}
}
