// Package lint provides a small go vet-style analyzer that flags
// fmt.Sprintf calls inside files that import pgx — the canonical SQL-safety
// guardrail per ADR be-l7t.3 §10. It is intentionally narrow: pgx parameter
// substitution must be used for any user-controlled value, so even a
// "definitely safe" Sprintf will be flagged. The rule's escape hatches are
// the //nolint:gosec annotations the postgres package already carries on the
// hand-written table-name interpolations.
package lint

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Analyzer flags fmt.Sprintf usage in pgx-importing files.
var Analyzer = &analysis.Analyzer{
	Name: "pgxsqlsafe",
	Doc:  "flags fmt.Sprintf usage in files that import pgx (use parameter substitution instead).",
	Run:  run,
}

func run(pass *analysis.Pass) (interface{}, error) {
	for _, f := range pass.Files {
		if !importsPgx(f) {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			ident, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if ident.Name == "fmt" && (sel.Sel.Name == "Sprintf" || sel.Sel.Name == "Sprint") {
				if hasNolintGosec(pass, n) {
					return true
				}
				pass.Reportf(call.Pos(), "fmt.%s in pgx-importing file: use $N parameter substitution instead", sel.Sel.Name)
			}
			return true
		})
	}
	return nil, nil
}

func importsPgx(f *ast.File) bool {
	for _, imp := range f.Imports {
		if imp == nil || imp.Path == nil {
			continue
		}
		path := strings.Trim(imp.Path.Value, `"`)
		if strings.HasPrefix(path, "github.com/jackc/pgx/") {
			return true
		}
	}
	return false
}

// hasNolintGosec checks for a //nolint:gosec comment on the same line as the
// reported node OR on the preceding line (golangci-lint convention). The
// analyzer treats gosec-suppressed sites as already audited.
func hasNolintGosec(pass *analysis.Pass, n ast.Node) bool {
	pos := pass.Fset.Position(n.Pos())
	for _, f := range pass.Files {
		for _, c := range f.Comments {
			for _, comment := range c.List {
				cp := pass.Fset.Position(comment.Slash)
				if cp.Filename != pos.Filename {
					continue
				}
				if (cp.Line == pos.Line || cp.Line == pos.Line-1) && strings.Contains(comment.Text, "nolint:gosec") {
					return true
				}
			}
		}
	}
	return false
}
