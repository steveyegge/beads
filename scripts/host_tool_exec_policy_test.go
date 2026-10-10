package scripts_test

// Policy: no new ungated host `go` or `gofmt` exec under Bazel
// (//scripts:go_sources_test, which declares //:repo_go_srcs and
// //:repo_go_test_srcs, so a non-Go change leaves it cached).
//
// Background (f4-spec.md §9, a release-track planning note). Under `bazel test` the
// PATH is hermetic_bin: plus the strict action env
// (tools/bazel/test_env.sh); the hermetic pinned dolt resolves there, but
// `go` and `gofmt` do not — whether a bare `go` or `gofmt` on that PATH finds
// a host toolchain depends on the worker, so a bare exec of either succeeds
// on some workers and exits 127 on others (gastownhall/beads#7478 fixed one:
// TestRunCompactDoltTargetsOnlyAuthorizedActiveDatabase's fixture builder).
// `dolt` is deliberately NOT in scope here: the hermetic copy comes first on
// that PATH (test_env.sh), so a bare `exec.Command("dolt", ...)` or
// `exec.LookPath("dolt")` is not the worker-dependent failure this policy
// guards against, and the repository has on the order of 80 such call sites
// (internal/storage/dolt, cmd/bd, internal/testutil, ...); allowlisting them
// all here for no safety gain would make this policy file itself the
// un-reviewable thing. Extend bannedHostTools if that changes.
//
// Scope: every *_test.go file (repo-wide) plus the non-test .go files of
// internal/testutil, the one package dedicated to test helpers that other
// packages' tests import. Production code (cmd/bd's non-test files,
// internal/storage/..., internal/doltserver, ...) legitimately execs `dolt`
// (and `go`, for `bd preflight`/`bd doctor`) as part of what bd itself does;
// that is not in scope.
//
// A call site is gated, and so exempt, when:
//   - its tool-name argument is not a string literal (a runfiles-resolved
//     binary: bazeltest.RunfileEnv/Runfile/PrebuiltBD/testGo, or a variable
//     a helper returns only after such a check) — the scan below only looks
//     at literal arguments, so this case never reaches it;
//   - the enclosing function (or func literal) also mentions
//     `bazeltest.IsBazel()` or the literal "TEST_SRCDIR" anywhere in its
//     body (most of the repo's gated builders use
//     `if bazeltest.IsBazel() { ...; return }` before an unguarded fallback
//     exec, in the same function) — checked by position, not control flow,
//     so this is deliberately permissive: it treats the whole function as
//     gated once the mention appears anywhere in it, not only the branch
//     that is actually unreachable under Bazel; or
//   - it is in hostToolExecAllowlist below, each entry naming the gating
//     mechanism the first two bullets cannot see.
//
// Mutation check: TestHostToolExecPolicyScan's fixtures include a bare
// `exec.Command("go", "version")` with no gate and no allowlist entry, and
// assert the scan reports it.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bannedHostTools are the literal exec argument values this policy flags
// when ungated and unallowlisted. See the package comment above for why
// "dolt" is not here.
var bannedHostTools = map[string]bool{"go": true, "gofmt": true}

// hostToolExecHit is one call this scan found, gated or not.
type hostToolExecHit struct {
	line  int
	tool  string
	call  string // "exec.Command", "exec.CommandContext", "exec.LookPath"
	gated bool
}

// findHostToolExecCalls scans a parsed file for exec.Command / CommandContext
// / LookPath calls whose tool-name argument is a string literal naming a
// bannedHostTools entry, and reports, for each, whether its enclosing
// function (or func literal) also mentions bazeltest.IsBazel() or the string
// literal "TEST_SRCDIR" anywhere in its body.
func findHostToolExecCalls(fset *token.FileSet, file *ast.File) []hostToolExecHit {
	// Every function/func-literal body in the file, used to find the
	// smallest one enclosing a given call.
	var bodies []*ast.BlockStmt
	ast.Inspect(file, func(n ast.Node) bool {
		switch fn := n.(type) {
		case *ast.FuncDecl:
			if fn.Body != nil {
				bodies = append(bodies, fn.Body)
			}
		case *ast.FuncLit:
			bodies = append(bodies, fn.Body)
		}
		return true
	})
	enclosing := func(pos token.Pos) ast.Node {
		var best *ast.BlockStmt
		for _, b := range bodies {
			if b.Pos() <= pos && pos <= b.End() {
				if best == nil || (b.End()-b.Pos()) < (best.End()-best.Pos()) {
					best = b
				}
			}
		}
		if best != nil {
			return best
		}
		return file
	}
	gated := func(scope ast.Node) bool {
		found := false
		ast.Inspect(scope, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.SelectorExpr:
				if x.Sel.Name == "IsBazel" {
					found = true
				}
			case *ast.BasicLit:
				if x.Kind == token.STRING {
					if v, err := strconv.Unquote(x.Value); err == nil && v == "TEST_SRCDIR" {
						found = true
					}
				}
			}
			return true
		})
		return found
	}

	literalArg := func(e ast.Expr) (string, bool) {
		lit, ok := e.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(lit.Value)
		if err != nil {
			return "", false
		}
		return v, true
	}

	var hits []hostToolExecHit
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok || pkg.Name != "exec" {
			return true
		}
		argIdx := -1
		switch sel.Sel.Name {
		case "Command", "LookPath":
			argIdx = 0
		case "CommandContext":
			argIdx = 1
		default:
			return true
		}
		if len(call.Args) <= argIdx {
			return true
		}
		tool, ok := literalArg(call.Args[argIdx])
		if !ok || !bannedHostTools[tool] {
			return true
		}
		hits = append(hits, hostToolExecHit{
			line:  fset.Position(call.Pos()).Line,
			tool:  tool,
			call:  "exec." + sel.Sel.Name,
			gated: gated(enclosing(call.Pos())),
		})
		return true
	})
	return hits
}

// hostToolExecAllowlist is file (repo-relative, slash-separated) -> line ->
// why the call there is safe despite findHostToolExecCalls not seeing a
// textual bazeltest.IsBazel()/TEST_SRCDIR gate in its enclosing function.
// Every entry here is a helper reached only when
// bazeltest.PrebuiltBD()/findPrebuiltBDBinary() (which itself calls
// bazeltest.IsBazel() under the hood) already reported no prebuilt bd
// binary — under Bazel that call never returns "", so the exec below it is
// unreachable there, just not through a mention this scan's textual check
// can find in the same function.
var hostToolExecAllowlist = map[string]map[int]string{
	"internal/storage/uow/doltserver_provider_test.go": {
		213: "buildBDBinary: only reached when bazeltest.PrebuiltBD() returned \"\"",
	},
	"internal/storage/embeddeddolt/concurrency_test.go": {
		76: "only reached when BEADS_TEST_EMBEDDED_TEST_BINARY is unset; Bazel's go_test sets it",
	},
	"cmd/bd/migrate_dolt_mode_frontdoor_integration_test.go": {
		78: "migrationFrontDoorBinary: only reached when findPrebuiltBDBinary() returned \"\"",
	},
	"cmd/bd/test_helpers_pure_test.go": {
		358: "goBuildBDCommand: only called by buildBDForInitTests after findPrebuiltBDBinary() returned \"\"",
	},
	"tests/regression/regression_test.go": {
		162: "buildCandidate: only reached when bazeltest.PrebuiltBD() returned \"\"",
	},
}

// hostToolExecInScope reports whether rel (repo-relative, slash-separated)
// is in this policy's scope: see the package comment.
func hostToolExecInScope(rel string) bool {
	if strings.HasSuffix(rel, "_test.go") {
		return true
	}
	return strings.HasPrefix(rel, "internal/testutil/") && strings.HasSuffix(rel, ".go")
}

func TestHostToolExecPolicyScan(t *testing.T) {
	parse := func(t *testing.T, src string) (*token.FileSet, *ast.File) {
		t.Helper()
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "fixture.go", src, 0)
		if err != nil {
			t.Fatalf("parse fixture: %v", err)
		}
		return fset, f
	}

	t.Run("ungated literal go exec is reported, unqated", func(t *testing.T) {
		fset, f := parse(t, `package p

import "os/exec"

func build() {
	exec.Command("go", "version")
}
`)
		hits := findHostToolExecCalls(fset, f)
		if len(hits) != 1 || hits[0].gated {
			t.Fatalf("hits = %+v, want one ungated hit (mutation check)", hits)
		}
	})

	t.Run("CommandContext and LookPath also match", func(t *testing.T) {
		fset, f := parse(t, `package p

import (
	"context"
	"os/exec"
)

func a(ctx context.Context) {
	exec.CommandContext(ctx, "go", "build")
}

func b() {
	exec.LookPath("gofmt")
}
`)
		hits := findHostToolExecCalls(fset, f)
		if len(hits) != 2 || hits[0].gated || hits[1].gated {
			t.Fatalf("hits = %+v, want two ungated hits", hits)
		}
	})

	t.Run("dolt is out of scope", func(t *testing.T) {
		fset, f := parse(t, `package p

import "os/exec"

func d() {
	exec.Command("dolt", "version")
}
`)
		if hits := findHostToolExecCalls(fset, f); len(hits) != 0 {
			t.Fatalf("hits = %+v, want none (dolt is not a bannedHostTools entry)", hits)
		}
	})

	t.Run("a variable tool name is not flagged", func(t *testing.T) {
		fset, f := parse(t, `package p

import "os/exec"

func build(goBin string) {
	exec.Command(goBin, "build")
}
`)
		if hits := findHostToolExecCalls(fset, f); len(hits) != 0 {
			t.Fatalf("hits = %+v, want none (not a literal)", hits)
		}
	})

	t.Run("same-function IsBazel gate, textually before the exec", func(t *testing.T) {
		fset, f := parse(t, `package p

import "os/exec"

func build() {
	if bazeltest.IsBazel() {
		return
	}
	exec.Command("go", "build")
}
`)
		hits := findHostToolExecCalls(fset, f)
		if len(hits) != 1 || !hits[0].gated {
			t.Fatalf("hits = %+v, want one gated hit", hits)
		}
	})

	t.Run("same-function IsBazel gate, textually after the exec, still counts", func(t *testing.T) {
		// Deliberately permissive (see the package comment): the check is
		// "does the function mention it anywhere", not control-flow aware.
		fset, f := parse(t, `package p

import "os/exec"

func build() {
	exec.Command("go", "build")
	if bazeltest.IsBazel() {
		panic("unreachable")
	}
}
`)
		hits := findHostToolExecCalls(fset, f)
		if len(hits) != 1 || !hits[0].gated {
			t.Fatalf("hits = %+v, want one gated hit", hits)
		}
	})

	t.Run("TEST_SRCDIR gate", func(t *testing.T) {
		fset, f := parse(t, `package p

import (
	"os"
	"os/exec"
)

func build() {
	if os.Getenv("TEST_SRCDIR") != "" {
		return
	}
	exec.LookPath("go")
}
`)
		hits := findHostToolExecCalls(fset, f)
		if len(hits) != 1 || !hits[0].gated {
			t.Fatalf("hits = %+v, want one gated hit", hits)
		}
	})

	t.Run("a sibling function's gate does not cover this one", func(t *testing.T) {
		fset, f := parse(t, `package p

import "os/exec"

func caller() {
	if bazeltest.IsBazel() {
		return
	}
	helper()
}

func helper() {
	exec.Command("go", "build")
}
`)
		hits := findHostToolExecCalls(fset, f)
		if len(hits) != 1 || hits[0].gated {
			t.Fatalf("hits = %+v, want one ungated hit (the gate is in a different function)", hits)
		}
	})

	t.Run("hostToolExecInScope", func(t *testing.T) {
		for rel, want := range map[string]bool{
			"cmd/bd/compact_gc_target_test.go":              true,
			"internal/testutil/localdoltserver.go":          true,
			"internal/testutil/integration/harness.go":      true,
			"internal/testutil/bazeltest/bazeltest_test.go": true,
			"cmd/bd/compact.go":                             false,
			"internal/storage/dolt/store.go":                false,
			"internal/doltserver/doltserver.go":             false,
			"scripts/ci/go-list-test-names/main.go":         false,
		} {
			if got := hostToolExecInScope(rel); got != want {
				t.Errorf("hostToolExecInScope(%q) = %v, want %v", rel, got, want)
			}
		}
	})
}

// TestNoUngatedHostToolExecsUnderBazel scans the real tree (every *_test.go
// file and internal/testutil's non-test .go files) and fails on an
// unallowlisted, ungated literal go/gofmt exec. See the package comment.
func TestNoUngatedHostToolExecsUnderBazel(t *testing.T) {
	root := sourceRepoRoot(t)
	files := repoFiles(t, root, 1000)

	seen := map[string]map[int]bool{}
	scanned := 0
	for _, rel := range files {
		if !hostToolExecInScope(rel) {
			continue
		}
		fset := token.NewFileSet()
		path := filepath.Join(root, filepath.FromSlash(rel))
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", rel, err)
		}
		scanned++
		for _, hit := range findHostToolExecCalls(fset, f) {
			if hit.gated {
				continue
			}
			if why, ok := hostToolExecAllowlist[rel][hit.line]; ok {
				if seen[rel] == nil {
					seen[rel] = map[int]bool{}
				}
				seen[rel][hit.line] = true
				_ = why
				continue
			}
			t.Errorf("%s:%d: ungated %s(%q): under `bazel test` the hermetic-first PATH may lack a host %s "+
				"toolchain, so this can exit 127 on some workers (f4-spec.md §9); gate it on bazeltest.IsBazel()/"+
				"TEST_SRCDIR, resolve the binary through bazeltest.RunfileEnv/PrebuiltBD, or add a justified "+
				"hostToolExecAllowlist entry", rel, hit.line, hit.call, hit.tool, hit.tool)
		}
	}
	if scanned < 1000 {
		t.Fatalf("scanned only %d in-scope files; the walk is broken", scanned)
	}
	for rel, lines := range hostToolExecAllowlist {
		for line := range lines {
			if !seen[rel][line] {
				t.Errorf("hostToolExecAllowlist entry %s:%d no longer matches an ungated host-tool exec; remove it", rel, line)
			}
		}
	}
}
