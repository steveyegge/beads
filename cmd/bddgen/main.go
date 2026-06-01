// bddgen generates net/rpc client/server wrappers for storage.Storage.
//
// Invoke via go generate from internal/storage/rpc/.
// Writes types.go, server.go, and client.go to the output directory.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

func main() {
	log.SetFlags(0)
	storagePath := flag.String("storage", "", "path to storage.go (default: ../storage.go)")
	outDir := flag.String("out", ".", "output directory")
	flag.Parse()

	if *storagePath == "" {
		*storagePath = filepath.Join("..", "storage.go")
	}

	methods, err := parseStorage(*storagePath)
	if err != nil {
		log.Fatalf("bddgen: %v", err)
	}

	for _, gen := range []struct {
		name string
		fn   func([]methodInfo) ([]byte, error)
	}{
		{"types.go", genTypes},
		{"server.go", genServer},
		{"client.go", genClient},
	} {
		src, err := gen.fn(methods)
		if err != nil {
			log.Fatalf("bddgen: %s: %v", gen.name, err)
		}
		if err := os.WriteFile(filepath.Join(*outDir, gen.name), src, 0o644); err != nil { // #nosec G306 — generated Go source needs to be readable
			log.Fatalf("bddgen: write %s: %v", gen.name, err)
		}
	}
}

// methodInfo describes one method on storage.Storage.
type methodInfo struct {
	Name    string
	Params  []paramInfo
	Results []resultInfo
}

type paramInfo struct {
	Name      string // original name (e.g. "id")
	FieldName string // exported field name (e.g. "Id")
	TypeStr   string // qualified type string (e.g. "string")
}

type resultInfo struct {
	FieldName string // exported field name (e.g. "Issue")
	TypeStr   string // qualified type string (e.g. "*types.Issue")
}

// parseStorage reads the Storage interface from storagePath and returns
// methodInfos for all methods that can be proxied over net/rpc.
func parseStorage(path string) ([]methodInfo, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	var iface *ast.InterfaceType
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != "Storage" {
				continue
			}
			if it, ok := ts.Type.(*ast.InterfaceType); ok {
				iface = it
			}
		}
	}
	if iface == nil {
		return nil, fmt.Errorf("Storage interface not found in %s", path)
	}

	var methods []methodInfo
	for _, m := range iface.Methods.List {
		if len(m.Names) == 0 {
			continue // embedded interface — skip
		}
		name := m.Names[0].Name
		ft, ok := m.Type.(*ast.FuncType)
		if !ok {
			continue
		}

		// Skip methods that can't cross the RPC boundary.
		if name == "Close" || name == "RunInTransaction" {
			continue
		}

		// Skip methods with func-typed parameters.
		if hasFuncParam(ft) {
			continue
		}

		mi := methodInfo{Name: name}

		// Parameters — skip the leading context.Context.
		for _, field := range ft.Params.List {
			typeStr := nodeStr(fset, field.Type)
			if typeStr == "context.Context" {
				continue
			}
			typeStr = qualifyStorageType(typeStr)
			names := field.Names
			if len(names) == 0 {
				// Unnamed param — generate a placeholder.
				names = []*ast.Ident{{Name: fmt.Sprintf("arg%d", len(mi.Params))}}
			}
			for _, n := range names {
				mi.Params = append(mi.Params, paramInfo{
					Name:      n.Name,
					FieldName: exportedName(n.Name),
					TypeStr:   typeStr,
				})
			}
		}

		// Results — skip trailing error.
		if ft.Results != nil {
			for _, field := range ft.Results.List {
				typeStr := nodeStr(fset, field.Type)
				if typeStr == "error" {
					continue
				}
				typeStr = qualifyStorageType(typeStr)
				mi.Results = append(mi.Results, resultInfo{
					FieldName: resultFieldName(typeStr),
					TypeStr:   typeStr,
				})
			}
		}

		methods = append(methods, mi)
	}
	return methods, nil
}

// hasFuncParam returns true if any parameter has a func type.
func hasFuncParam(ft *ast.FuncType) bool {
	if ft.Params == nil {
		return false
	}
	for _, p := range ft.Params.List {
		if _, ok := p.Type.(*ast.FuncType); ok {
			return true
		}
	}
	return false
}

// nodeStr renders an AST expression as a Go source string.
func nodeStr(fset *token.FileSet, node ast.Node) string {
	var buf bytes.Buffer
	_ = printer.Fprint(&buf, fset, node)
	return buf.String()
}

// qualifyStorageType prefixes unqualified exported types with "storage." so
// the generated code (in package rpc) can reference them.
func qualifyStorageType(s string) string {
	return rewriteBase(s)
}

func rewriteBase(s string) string {
	switch {
	case strings.HasPrefix(s, "[]*"):
		return "[]*" + qualify(s[3:])
	case strings.HasPrefix(s, "[]"):
		return "[]" + qualify(s[2:])
	case strings.HasPrefix(s, "*"):
		return "*" + qualify(s[1:])
	}
	return qualify(s)
}

func qualify(base string) string {
	if base == "" {
		return base
	}
	// Already package-qualified or built-in.
	if strings.Contains(base, ".") || !unicode.IsUpper(rune(base[0])) {
		return base
	}
	// map / func / interface types won't start with an uppercase letter after
	// the prefix stripping above, so this only fires for exported identifiers.
	return "storage." + base
}

// exportedName capitalises the first rune of s.
func exportedName(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// resultFieldName derives an exported struct field name from a Go type string.
func resultFieldName(typeStr string) string {
	switch typeStr {
	case "string":
		return "Value"
	case "bool":
		return "OK"
	case "int", "int64":
		return "N"
	}
	if strings.HasPrefix(typeStr, "map[") {
		return "Data"
	}
	// Strip pointer/slice decorators.
	base := typeStr
	isSlice := false
	if strings.HasPrefix(base, "[]*") {
		base = base[3:]
		isSlice = true
	} else if strings.HasPrefix(base, "[]") {
		base = base[2:]
		isSlice = true
	} else if strings.HasPrefix(base, "*") {
		base = base[1:]
	}
	// Use the last component after ".".
	if idx := strings.LastIndex(base, "."); idx >= 0 {
		base = base[idx+1:]
	}
	if isSlice {
		return base + "s"
	}
	return base
}

// usedImports scans type strings for package qualifiers and returns the needed
// import paths.
func usedImports(typeStrs []string, always ...string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(path string) {
		if !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	for _, a := range always {
		add(a)
	}
	for _, s := range typeStrs {
		if strings.Contains(s, "types.") {
			add("github.com/steveyegge/beads/internal/types")
		}
		if strings.Contains(s, "storage.") {
			add("github.com/steveyegge/beads/internal/storage")
		}
		if strings.Contains(s, "time.") {
			add("time")
		}
	}
	return out
}

func allTypeStrs(methods []methodInfo) []string {
	var out []string
	for _, m := range methods {
		for _, p := range m.Params {
			out = append(out, p.TypeStr)
		}
		for _, r := range m.Results {
			out = append(out, r.TypeStr)
		}
	}
	return out
}

// fmtSource runs gofmt on src and returns the formatted bytes.
func fmtSource(src []byte) ([]byte, error) {
	return format.Source(src)
}

// importBlock formats a list of import paths into a Go import block.
func importBlock(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("import (\n")
	for _, p := range paths {
		fmt.Fprintf(&b, "\t%q\n", p)
	}
	b.WriteString(")\n")
	return b.String()
}

// ── types.go ────────────────────────────────────────────────────────────────

func genTypes(methods []methodInfo) ([]byte, error) {
	typeStrs := allTypeStrs(methods)
	imports := usedImports(typeStrs)

	var b strings.Builder
	b.WriteString("// Code generated by bddgen. DO NOT EDIT.\n\npackage rpc\n\n")
	b.WriteString(importBlock(imports))
	b.WriteString("\n")

	// Handwritten RPCError — included verbatim by the generator.
	b.WriteString("// RPCError carries sentinel error identity across net/rpc boundaries.\n")
	b.WriteString("type RPCError struct {\n\tKind string\n\tMsg  string\n}\n\n")

	for _, m := range methods {
		// Args struct
		fmt.Fprintf(&b, "type %sArgs struct {\n", m.Name)
		for _, p := range m.Params {
			fmt.Fprintf(&b, "\t%s %s\n", p.FieldName, p.TypeStr)
		}
		b.WriteString("}\n\n")

		// Reply struct
		fmt.Fprintf(&b, "type %sReply struct {\n", m.Name)
		for _, r := range m.Results {
			fmt.Fprintf(&b, "\t%s %s\n", r.FieldName, r.TypeStr)
		}
		b.WriteString("\tRPCError *RPCError\n")
		b.WriteString("}\n\n")
	}

	return fmtSource([]byte(b.String()))
}

// ── server.go ───────────────────────────────────────────────────────────────

func genServer(methods []methodInfo) ([]byte, error) {
	// server.go never references types.* or time.Time directly in expressions —
	// it only operates on args/reply structs defined in types.go (same package).
	imports := usedImports(nil,
		"context",
		"errors",
		"os",
		"strconv",
		"time",
		"github.com/steveyegge/beads/internal/storage",
	)

	var b strings.Builder
	b.WriteString("// Code generated by bddgen. DO NOT EDIT.\n\npackage rpc\n\n")
	b.WriteString(importBlock(imports))
	b.WriteString(`
// daemonServer wraps a storage.Storage for net/rpc dispatch.
type daemonServer struct {
	store storage.Storage
	root  context.Context
}

func daemonCallContext(root context.Context) (context.Context, context.CancelFunc) {
	timeout := 30 * time.Second
	if v := os.Getenv("BEADS_DAEMON_CALL_TIMEOUT"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			timeout = time.Duration(secs) * time.Second
		}
	}
	return context.WithTimeout(root, timeout)
}

func encodeRPCError(err error) *RPCError {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, storage.ErrAlreadyClaimed):
		return &RPCError{Kind: "ErrAlreadyClaimed", Msg: err.Error()}
	case errors.Is(err, storage.ErrNotClaimable):
		return &RPCError{Kind: "ErrNotClaimable", Msg: err.Error()}
	case errors.Is(err, storage.ErrNotFound):
		return &RPCError{Kind: "ErrNotFound", Msg: err.Error()}
	case errors.Is(err, storage.ErrNotInitialized):
		return &RPCError{Kind: "ErrNotInitialized", Msg: err.Error()}
	case errors.Is(err, storage.ErrPrefixMismatch):
		return &RPCError{Kind: "ErrPrefixMismatch", Msg: err.Error()}
	default:
		return &RPCError{Kind: "", Msg: err.Error()}
	}
}

`)

	for _, m := range methods {
		// Server method: takes Args, populates Reply, returns nil.
		fmt.Fprintf(&b, "func (s *daemonServer) %s(args *%sArgs, reply *%sReply) error {\n", m.Name, m.Name, m.Name)
		b.WriteString("\tctx, cancel := daemonCallContext(s.root)\n")
		b.WriteString("\tdefer cancel()\n")

		// Build the call expression.
		var argExprs []string
		argExprs = append(argExprs, "ctx")
		for _, p := range m.Params {
			argExprs = append(argExprs, "args."+p.FieldName)
		}
		call := fmt.Sprintf("s.store.%s(%s)", m.Name, strings.Join(argExprs, ", "))

		if len(m.Results) == 0 {
			fmt.Fprintf(&b, "\terr := %s\n", call)
		} else {
			var rets []string
			for i := range m.Results {
				rets = append(rets, fmt.Sprintf("r%d", i))
			}
			rets = append(rets, "err")
			fmt.Fprintf(&b, "\t%s := %s\n", strings.Join(rets, ", "), call)
			for i, r := range m.Results {
				fmt.Fprintf(&b, "\treply.%s = r%d\n", r.FieldName, i)
			}
		}
		b.WriteString("\treply.RPCError = encodeRPCError(err)\n")
		b.WriteString("\treturn nil\n")
		b.WriteString("}\n\n")
	}

	return fmtSource([]byte(b.String()))
}

// ── client.go ───────────────────────────────────────────────────────────────

func genClient(methods []methodInfo) ([]byte, error) {
	typeStrs := allTypeStrs(methods)
	imports := usedImports(typeStrs,
		"context",
		"errors",
		"fmt",
		"net/rpc",
		"github.com/steveyegge/beads/internal/storage",
	)

	var b strings.Builder
	b.WriteString("// Code generated by bddgen. DO NOT EDIT.\n\npackage rpc\n\n")
	b.WriteString(importBlock(imports))
	b.WriteString(`
// daemonClient implements storage.Storage over net/rpc.
type daemonClient struct {
	client *rpc.Client
}

// Close closes the underlying RPC connection.
func (c *daemonClient) Close() error {
	return c.client.Close()
}

// RunInTransaction is not supported over the daemon RPC boundary.
func (c *daemonClient) RunInTransaction(ctx context.Context, commitMsg string, fn func(storage.Transaction) error) error {
	return fmt.Errorf("RunInTransaction: not supported over daemon RPC")
}

func decodeRPCError(r *RPCError) error {
	if r == nil {
		return nil
	}
	switch r.Kind {
	case "ErrAlreadyClaimed":
		return fmt.Errorf("%s: %w", r.Msg, storage.ErrAlreadyClaimed)
	case "ErrNotClaimable":
		return fmt.Errorf("%s: %w", r.Msg, storage.ErrNotClaimable)
	case "ErrNotFound":
		return fmt.Errorf("%s: %w", r.Msg, storage.ErrNotFound)
	case "ErrNotInitialized":
		return fmt.Errorf("%s: %w", r.Msg, storage.ErrNotInitialized)
	case "ErrPrefixMismatch":
		return fmt.Errorf("%s: %w", r.Msg, storage.ErrPrefixMismatch)
	default:
		return errors.New(r.Msg)
	}
}

`)

	for _, m := range methods {
		// Reconstruct the original method signature for the client.
		var sigParams []string
		sigParams = append(sigParams, "ctx context.Context")
		for _, p := range m.Params {
			sigParams = append(sigParams, p.Name+" "+p.TypeStr)
		}

		var sigResults []string
		for _, r := range m.Results {
			sigResults = append(sigResults, r.TypeStr)
		}
		sigResults = append(sigResults, "error")

		var retDecl string
		if len(sigResults) == 1 {
			retDecl = " " + sigResults[0]
		} else {
			retDecl = " (" + strings.Join(sigResults, ", ") + ")"
		}

		fmt.Fprintf(&b, "func (c *daemonClient) %s(%s)%s {\n",
			m.Name, strings.Join(sigParams, ", "), retDecl)

		// Build the args struct literal.
		var fieldInits []string
		for _, p := range m.Params {
			fieldInits = append(fieldInits, p.FieldName+": "+p.Name)
		}
		if len(fieldInits) > 0 {
			fmt.Fprintf(&b, "\targs := &%sArgs{%s}\n", m.Name, strings.Join(fieldInits, ", "))
		} else {
			fmt.Fprintf(&b, "\targs := &%sArgs{}\n", m.Name)
		}
		fmt.Fprintf(&b, "\tvar reply %sReply\n", m.Name)
		fmt.Fprintf(&b, "\tif err := c.client.Call(\"daemonServer.%s\", args, &reply); err != nil {\n", m.Name)

		// Error return depends on number of non-error results.
		if len(m.Results) == 0 {
			b.WriteString("\t\treturn err\n")
		} else {
			var zeroVals []string
			for _, r := range m.Results {
				zeroVals = append(zeroVals, zeroValue(r.TypeStr))
			}
			fmt.Fprintf(&b, "\t\treturn %s, err\n", strings.Join(zeroVals, ", "))
		}
		b.WriteString("\t}\n")

		// Return reply fields + decoded error.
		if len(m.Results) == 0 {
			b.WriteString("\treturn decodeRPCError(reply.RPCError)\n")
		} else {
			var retVals []string
			for _, r := range m.Results {
				retVals = append(retVals, "reply."+r.FieldName)
			}
			retVals = append(retVals, "decodeRPCError(reply.RPCError)")
			fmt.Fprintf(&b, "\treturn %s\n", strings.Join(retVals, ", "))
		}
		b.WriteString("}\n\n")
	}

	// Ensure daemonClient satisfies storage.Storage at compile time.
	b.WriteString("var _ storage.Storage = (*daemonClient)(nil)\n")

	return fmtSource([]byte(b.String()))
}

// zeroValue returns the Go zero value literal for a type string.
func zeroValue(typeStr string) string {
	switch typeStr {
	case "string":
		return `""`
	case "bool":
		return "false"
	case "int", "int64":
		return "0"
	}
	// Pointer and slice types: nil.
	if strings.HasPrefix(typeStr, "*") || strings.HasPrefix(typeStr, "[]") || strings.HasPrefix(typeStr, "map[") {
		return "nil"
	}
	return "nil"
}
