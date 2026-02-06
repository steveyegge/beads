package comment

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ScanFile extracts all comment nodes from a single Go source file.
func ScanFile(path string) ([]Node, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	// Build a map of line -> declaration name for associating comments.
	declMap := buildDeclMap(fset, f)

	var nodes []Node
	for _, cg := range f.Comments {
		content := cg.Text()
		pos := fset.Position(cg.Pos())
		end := fset.Position(cg.End())

		// Determine if this is a doc comment (immediately precedes a declaration).
		isDoc := isDocComment(fset, f, cg)
		assocDecl := findAssociatedDecl(declMap, pos.Line, end.Line)

		node := Node{
			File:           path,
			Line:           pos.Line,
			EndLine:        end.Line,
			Content:        strings.TrimSpace(content),
			Kind:           ClassifyComment(content, isDoc),
			References:     ExtractReferences(content),
			Tags:           ExtractTags(content),
			AssociatedDecl: assocDecl,
			FirstSeen:      time.Now(),
		}
		node.ID = HashComment(path, node.Content)
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// ScanDir recursively scans a directory for Go files and extracts comments.
func ScanDir(root string) (*ScanResult, error) {
	start := time.Now()
	result := &ScanResult{
		ByKind: make(map[Kind]int),
	}

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip errors
		}
		// Skip hidden dirs, vendor, testdata, .beads
		name := info.Name()
		if info.IsDir() {
			if strings.HasPrefix(name, ".") || name == "vendor" || name == "testdata" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") {
			return nil
		}
		// Skip test file comments but still track their declarations
		// so references like "See TestFoo" can be validated.
		isTestFile := strings.HasSuffix(name, "_test.go")

		// Make path relative to root for cleaner output.
		relPath, relErr := filepath.Rel(root, path)
		if relErr != nil {
			relPath = path
		}

		if isTestFile {
			// For test files, only extract declarations (for reference validation).
			// Don't add comment nodes — test comments aren't tracked.
			decls, declErr := ScanFileDecls(path)
			if declErr == nil {
				result.TestDecls = append(result.TestDecls, decls...)
			}
			return nil
		}

		nodes, scanErr := ScanFile(path)
		if scanErr != nil {
			return nil // skip unparseable files
		}

		for i := range nodes {
			nodes[i].File = relPath
			nodes[i].ID = HashComment(relPath, nodes[i].Content)
		}

		result.Nodes = append(result.Nodes, nodes...)
		result.FilesCount++
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Tally by kind.
	for _, n := range result.Nodes {
		result.ByKind[n.Kind]++
		if len(n.References) > 0 {
			for _, r := range n.References {
				if r.Status == RefBroken {
					result.BrokenRefs++
				}
			}
		}
	}
	result.Duration = time.Since(start)
	return result, nil
}

// ScanFileDecls extracts only the declaration names from a Go file (no comments).
// Used for _test.go files so references like "See TestFoo" can be validated.
func ScanFileDecls(path string) ([]string, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0) // No ParseComments flag = faster
	if err != nil {
		return nil, err
	}
	entries := buildDeclMap(fset, f)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.name)
	}
	return names, nil
}

// declEntry maps a line range to a declaration name.
type declEntry struct {
	startLine int
	endLine   int
	name      string
}

func buildDeclMap(fset *token.FileSet, f *ast.File) []declEntry {
	var entries []declEntry
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			start := fset.Position(d.Pos()).Line
			end := fset.Position(d.End()).Line
			name := d.Name.Name
			if d.Recv != nil && len(d.Recv.List) > 0 {
				// Method: include receiver type.
				if t, ok := d.Recv.List[0].Type.(*ast.StarExpr); ok {
					if ident, ok := t.X.(*ast.Ident); ok {
						name = ident.Name + "." + name
					}
				} else if ident, ok := d.Recv.List[0].Type.(*ast.Ident); ok {
					name = ident.Name + "." + name
				}
			}
			entries = append(entries, declEntry{start, end, name})
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.TypeSpec:
					start := fset.Position(s.Pos()).Line
					end := fset.Position(s.End()).Line
					entries = append(entries, declEntry{start, end, s.Name.Name})
				case *ast.ValueSpec:
					if len(s.Names) > 0 {
						start := fset.Position(s.Pos()).Line
						end := fset.Position(s.End()).Line
						entries = append(entries, declEntry{start, end, s.Names[0].Name})
					}
				}
			}
		}
	}
	return entries
}

// isDocComment checks if a comment group immediately precedes a declaration.
func isDocComment(fset *token.FileSet, f *ast.File, cg *ast.CommentGroup) bool {
	cgEnd := fset.Position(cg.End()).Line
	for _, decl := range f.Decls {
		declStart := fset.Position(decl.Pos()).Line
		if declStart == cgEnd+1 || declStart == cgEnd+2 {
			return true
		}
	}
	return false
}

// findAssociatedDecl finds the nearest declaration for a comment by line range.
func findAssociatedDecl(entries []declEntry, commentStart, commentEnd int) string {
	// First, check if comment is directly above a declaration (doc comment).
	for _, e := range entries {
		if commentEnd+1 == e.startLine || commentEnd+2 == e.startLine {
			return e.name
		}
	}
	// Otherwise, find the enclosing declaration (inline comment inside a function body).
	for _, e := range entries {
		if commentStart >= e.startLine && commentEnd <= e.endLine {
			return e.name
		}
	}
	return ""
}
