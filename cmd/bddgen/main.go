// bddgen generates net/rpc client/server wrappers for storage.Storage.
//
// Invoke via go generate from internal/storage/rpc/.
// Writes types.go, server.go, and client.go to the output directory.
// Also writes iter_types.go, iter_server.go, and iter_client.go for Iter*
// methods detected in the Storage and DependencyQueryStore interfaces.
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
	depsPath := flag.String("deps", "", "path to dependency_queries.go (default: ../dependency_queries.go)")
	outDir := flag.String("out", ".", "output directory")
	flag.Parse()

	if *storagePath == "" {
		*storagePath = filepath.Join("..", "storage.go")
	}
	if *depsPath == "" {
		*depsPath = filepath.Join("..", "dependency_queries.go")
	}

	methods, iterMethods, err := parseStorage(*storagePath)
	if err != nil {
		log.Fatalf("bddgen: %v", err)
	}

	// Collect Iter* methods from DependencyQueryStore as well.
	depIterMethods, err := parseIterInterface(*depsPath, "DependencyQueryStore")
	if err != nil {
		log.Fatalf("bddgen: deps: %v", err)
	}
	iterMethods = append(iterMethods, depIterMethods...)

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

	for _, gen := range []struct {
		name string
		fn   func([]iterMethodInfo) ([]byte, error)
	}{
		{"iter_types.go", genIterTypes},
		{"iter_server.go", genIterServer},
		{"iter_client.go", genIterClient},
	} {
		src, err := gen.fn(iterMethods)
		if err != nil {
			log.Fatalf("bddgen: %s: %v", gen.name, err)
		}
		if err := os.WriteFile(filepath.Join(*outDir, gen.name), src, 0o644); err != nil { // #nosec G306
			log.Fatalf("bddgen: write %s: %v", gen.name, err)
		}
	}
}

// ── methodInfo for regular (non-Iter) methods ────────────────────────────────

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

// ── iterMethodInfo for Iter* methods ─────────────────────────────────────────

// iterMethodInfo describes one Iter* method on storage.Storage or
// DependencyQueryStore.
type iterMethodInfo struct {
	Name           string      // e.g. "IterIssues"
	ShortName      string      // Name without "Iter" prefix, e.g. "Issues"
	SessionType    string      // e.g. "issueIterSession"
	ClientType     string      // e.g. "rpcIssueIter"
	Params         []paramInfo // non-ctx params (excludes BatchSize)
	ElemType       string      // e.g. "types.Issue"
	FallbackMethod string      // slice-path method for ErrTooManyIterators, or ""
	// StoreIface is the qualified Go type for the interface that declares this
	// method ("storage.Storage" or "storage.DependencyQueryStore"). The server
	// uses this to type-assert s.store when the method is not on Storage.
	StoreIface string
}

// iterSliceFallback maps Iter* method names to their slice-path equivalents.
// Empty string means no direct slice fallback exists.
var iterSliceFallback = map[string]string{
	"IterIssues":                   "SearchIssues",
	"IterDependentsWithMetadata":   "GetDependentsWithMetadata",
	"IterDependenciesWithMetadata": "GetDependenciesWithMetadata",
	"IterIssueComments":            "GetIssueComments",
	"IterEvents":                   "GetEvents",
	"IterAllEventsSince":           "GetAllEventsSince",
	"IterReadyWork":                "GetReadyWork",
	"IterBlockedIssues":            "GetBlockedIssues",
	"IterWisps":                    "ListWisps",
	// IterAllDependencyRecords: no direct 1:1 slice fallback (GetAllDependencyRecords
	// returns map[string][]*types.Dependency, not a flat slice).
}

// parseStorage reads the Storage interface from storagePath and returns:
//   - methods: regular (non-Iter) methodInfos for the slice-RPC path
//   - iterMethods: Iter* methodInfos for the streaming path
func parseStorage(path string) (methods []methodInfo, iterMethods []iterMethodInfo, err error) {
	fset := token.NewFileSet()
	f, errP := parser.ParseFile(fset, path, nil, 0)
	if errP != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, errP)
	}

	iface := findInterface(f, "Storage")
	if iface == nil {
		return nil, nil, fmt.Errorf("Storage interface not found in %s", path)
	}

	methods, iterMethods = extractMethods(fset, iface)
	return methods, iterMethods, nil
}

// parseIterInterface reads a named interface from path and returns only its
// Iter* methods as iterMethodInfos.
func parseIterInterface(path, ifaceName string) ([]iterMethodInfo, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	iface := findInterface(f, ifaceName)
	if iface == nil {
		// File may not have this interface (e.g. a future rename). Non-fatal.
		return nil, nil
	}

	_, iterMethods := extractMethodsWithIface(fset, iface, "storage."+ifaceName)
	return iterMethods, nil
}

// findInterface scans the top-level declarations of f for an interface named name.
func findInterface(f *ast.File, name string) *ast.InterfaceType {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gd.Specs {
			ts, ok := spec.(*ast.TypeSpec)
			if !ok || ts.Name.Name != name {
				continue
			}
			if it, ok := ts.Type.(*ast.InterfaceType); ok {
				return it
			}
		}
	}
	return nil
}

// extractMethods splits interface methods into regular and Iter* groups.
// ifaceName is used to populate iterMethodInfo.StoreIface.
func extractMethods(fset *token.FileSet, iface *ast.InterfaceType) ([]methodInfo, []iterMethodInfo) {
	return extractMethodsWithIface(fset, iface, "storage.Storage")
}

func extractMethodsWithIface(fset *token.FileSet, iface *ast.InterfaceType, ifaceName string) ([]methodInfo, []iterMethodInfo) {
	var methods []methodInfo
	var iterMethods []iterMethodInfo

	for _, m := range iface.Methods.List {
		if len(m.Names) == 0 {
			continue // embedded interface
		}
		name := m.Names[0].Name
		ft, ok := m.Type.(*ast.FuncType)
		if !ok {
			continue
		}

		// Collect result type strings to detect Iter[T] returns.
		var resultTypes []string
		if ft.Results != nil {
			for _, field := range ft.Results.List {
				resultTypes = append(resultTypes, nodeStr(fset, field.Type))
			}
		}

		if isIterMethod(resultTypes) {
			if im, ok := buildIterMethod(fset, name, ft, resultTypes); ok {
				im.StoreIface = ifaceName
				iterMethods = append(iterMethods, im)
			}
			continue
		}

		// Skip methods that can't cross the regular RPC boundary.
		if name == "Close" || name == "RunInTransaction" {
			continue
		}
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

	return methods, iterMethods
}

// isIterMethod returns true when at least one result type is an Iter[T].
func isIterMethod(resultTypes []string) bool {
	for _, t := range resultTypes {
		if strings.HasPrefix(t, "Iter[") && strings.HasSuffix(t, "]") {
			return true
		}
	}
	return false
}

// buildIterMethod constructs an iterMethodInfo from a parsed Iter* method.
func buildIterMethod(fset *token.FileSet, name string, ft *ast.FuncType, resultTypes []string) (iterMethodInfo, bool) {
	if !strings.HasPrefix(name, "Iter") {
		return iterMethodInfo{}, false
	}
	shortName := name[4:] // strip "Iter"

	// Extract element type from the first Iter[T] result.
	var elemType string
	for _, rt := range resultTypes {
		if strings.HasPrefix(rt, "Iter[") && strings.HasSuffix(rt, "]") {
			inner := rt[5 : len(rt)-1]
			elemType = qualifyStorageType(inner)
			break
		}
	}
	if elemType == "" {
		return iterMethodInfo{}, false
	}

	// Collect non-ctx params.
	var params []paramInfo
	for _, field := range ft.Params.List {
		typeStr := nodeStr(fset, field.Type)
		if typeStr == "context.Context" {
			continue
		}
		typeStr = qualifyStorageType(typeStr)
		fieldNames := field.Names
		if len(fieldNames) == 0 {
			fieldNames = []*ast.Ident{{Name: fmt.Sprintf("arg%d", len(params))}}
		}
		for _, n := range fieldNames {
			params = append(params, paramInfo{
				Name:      n.Name,
				FieldName: exportedName(n.Name),
				TypeStr:   typeStr,
			})
		}
	}

	// Derive session and client type names.
	//   "Issues" → "issueIterSession", "rpcIssueIter"
	//   "DependentsWithMetadata" → "dependentsWithMetadataIterSession", "rpcDependentsWithMetadataIter"
	sessionType := lowerFirst(shortName) + "IterSession"
	clientType := "rpc" + shortName + "Iter"

	return iterMethodInfo{
		Name:           name,
		ShortName:      shortName,
		SessionType:    sessionType,
		ClientType:     clientType,
		Params:         params,
		ElemType:       elemType,
		FallbackMethod: iterSliceFallback[name],
	}, true
}

// lowerFirst lowercases the first rune of s.
func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToLower(r[0])
	return string(r)
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

func allIterTypeStrs(methods []iterMethodInfo) []string {
	var out []string
	for _, m := range methods {
		out = append(out, m.ElemType)
		for _, p := range m.Params {
			out = append(out, p.TypeStr)
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
	iterMgr *iterSessionManager
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
	case errors.Is(err, storage.ErrTooManyIterators):
		return &RPCError{Kind: "ErrTooManyIterators", Msg: err.Error()}
	case errors.Is(err, storage.ErrIterSessionNotFound):
		return &RPCError{Kind: "ErrIterSessionNotFound", Msg: err.Error()}
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
	case "ErrTooManyIterators":
		return fmt.Errorf("%s: %w", r.Msg, storage.ErrTooManyIterators)
	case "ErrIterSessionNotFound":
		return fmt.Errorf("%s: %w", r.Msg, storage.ErrIterSessionNotFound)
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

// ── iter_types.go ────────────────────────────────────────────────────────────

func genIterTypes(methods []iterMethodInfo) ([]byte, error) {
	typeStrs := allIterTypeStrs(methods)
	imports := usedImports(typeStrs)

	var b strings.Builder
	b.WriteString("// Code generated by bddgen. DO NOT EDIT.\n\npackage rpc\n\n")
	b.WriteString(importBlock(imports))
	b.WriteString("\n")

	for _, m := range methods {
		// IterXxxStartArgs — method params + BatchSize.
		fmt.Fprintf(&b, "// %sStartArgs is the argument struct for %sStart.\n", m.Name, m.Name)
		fmt.Fprintf(&b, "type %sStartArgs struct {\n", m.Name)
		for _, p := range m.Params {
			fmt.Fprintf(&b, "\t%s %s\n", p.FieldName, p.TypeStr)
		}
		b.WriteString("\t// BatchSize controls rows per IterXxxNext RPC. 0 uses the daemon default.\n")
		b.WriteString("\tBatchSize int\n")
		b.WriteString("}\n\n")

		// IterXxxNextReply — batch of items + Done + RPCError.
		fmt.Fprintf(&b, "// %sNextReply is the reply struct for %sNext.\n", m.Name, m.Name)
		fmt.Fprintf(&b, "type %sNextReply struct {\n", m.Name)
		fmt.Fprintf(&b, "\tItems    []*%s\n", m.ElemType)
		b.WriteString("\tDone     bool\n")
		b.WriteString("\tRPCError *RPCError\n")
		b.WriteString("}\n\n")
	}

	return fmtSource([]byte(b.String()))
}

// ── iter_server.go ───────────────────────────────────────────────────────────

func genIterServer(methods []iterMethodInfo) ([]byte, error) {
	typeStrs := allIterTypeStrs(methods)
	imports := usedImports(typeStrs,
		"context",
		"crypto/rand",
		"encoding/hex",
		"sync",
		"sync/atomic",
		"time",
		"github.com/steveyegge/beads/internal/storage",
	)

	var b strings.Builder
	b.WriteString("// Code generated by bddgen. DO NOT EDIT.\n\npackage rpc\n\n")
	b.WriteString(importBlock(imports))
	b.WriteString(`
const defaultIterBatchSize = 100

// newSessionID returns a random 16-byte hex string for iterator session IDs.
func newSessionID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		panic("bddgen: newSessionID: " + err.Error())
	}
	return hex.EncodeToString(buf[:])
}

`)

	for _, m := range methods {
		// Build the store call argument list.
		var storeArgExprs []string
		storeArgExprs = append(storeArgExprs, "ctx")
		for _, p := range m.Params {
			storeArgExprs = append(storeArgExprs, "args."+p.FieldName)
		}
		storeCall := fmt.Sprintf("s.store.%s(%s)", m.Name, strings.Join(storeArgExprs, ", "))

		// IterXxxStart
		fmt.Fprintf(&b, "// %sStart opens a new iterator session for %s.\n", m.Name, m.Name)
		fmt.Fprintf(&b, "func (s *daemonServer) %sStart(args *%sStartArgs, reply *IterStartReply) error {\n", m.Name, m.Name)
		b.WriteString("\tctx, cancel := daemonCallContext(s.root)\n")
		b.WriteString("\tdefer cancel()\n")

		// For methods on interfaces other than storage.Storage, type-assert the store.
		storeExpr := "s.store"
		if m.StoreIface != "" && m.StoreIface != "storage.Storage" {
			storeVar := "store_" + m.Name
			fmt.Fprintf(&b, "\t%s, ok := s.store.(%s)\n", storeVar, m.StoreIface)
			b.WriteString("\tif !ok {\n")
			fmt.Fprintf(&b, "\t\treply.RPCError = &RPCError{Kind: \"\", Msg: \"store does not implement %s\"}\n", m.StoreIface)
			b.WriteString("\t\treturn nil\n")
			b.WriteString("\t}\n")
			storeExpr = storeVar
			// Rebuild storeCall with the narrowed var.
			var narrowArgExprs []string
			narrowArgExprs = append(narrowArgExprs, "ctx")
			for _, p := range m.Params {
				narrowArgExprs = append(narrowArgExprs, "args."+p.FieldName)
			}
			storeCall = fmt.Sprintf("%s.%s(%s)", storeExpr, m.Name, strings.Join(narrowArgExprs, ", "))
		}

		fmt.Fprintf(&b, "\tit, err := %s\n", storeCall)
		b.WriteString("\tif err != nil {\n")
		b.WriteString("\t\treply.RPCError = encodeRPCError(err)\n")
		b.WriteString("\t\treturn nil\n")
		b.WriteString("\t}\n")
		_ = storeExpr // used above
		fmt.Fprintf(&b, "\tsess := &%s{iter: it}\n", m.SessionType)
		b.WriteString("\tsess.Touch()\n")
		b.WriteString("\tsessionID := newSessionID()\n")
		b.WriteString("\tif err := s.iterMgr.openSession(sessionID, sess); err != nil {\n")
		b.WriteString("\t\t_ = it.Close()\n")
		b.WriteString("\t\treply.RPCError = encodeRPCError(err)\n")
		b.WriteString("\t\treturn nil\n")
		b.WriteString("\t}\n")
		b.WriteString("\treply.SessionID = sessionID\n")
		b.WriteString("\treturn nil\n")
		b.WriteString("}\n\n")

		// IterXxxNext
		fmt.Fprintf(&b, "// %sNext fetches the next batch from a %s session.\n", m.Name, m.Name)
		fmt.Fprintf(&b, "func (s *daemonServer) %sNext(args *IterNextArgs, reply *%sNextReply) error {\n", m.Name, m.Name)
		b.WriteString("\traw := s.iterMgr.getSession(args.SessionID)\n")
		b.WriteString("\tif raw == nil {\n")
		b.WriteString("\t\treply.RPCError = encodeRPCError(storage.ErrIterSessionNotFound)\n")
		b.WriteString("\t\treturn nil\n")
		b.WriteString("\t}\n")
		fmt.Fprintf(&b, "\tsess, ok := raw.(*%s)\n", m.SessionType)
		b.WriteString("\tif !ok {\n")
		b.WriteString("\t\treply.RPCError = encodeRPCError(storage.ErrIterSessionNotFound)\n")
		b.WriteString("\t\treturn nil\n")
		b.WriteString("\t}\n")
		b.WriteString("\tsess.mu.Lock()\n")
		b.WriteString("\tdefer sess.mu.Unlock()\n")
		b.WriteString("\tsess.Touch()\n")
		b.WriteString("\tbatchSize := args.BatchSize\n")
		b.WriteString("\tif batchSize <= 0 {\n")
		b.WriteString("\t\tbatchSize = defaultIterBatchSize\n")
		b.WriteString("\t}\n")
		b.WriteString("\tctx := context.Background()\n")
		b.WriteString("\tfor len(reply.Items) < batchSize {\n")
		b.WriteString("\t\tif !sess.iter.Next(ctx) {\n")
		b.WriteString("\t\t\treply.Done = true\n")
		b.WriteString("\t\t\tif iterErr := sess.iter.Err(); iterErr != nil {\n")
		b.WriteString("\t\t\t\treply.RPCError = encodeRPCError(iterErr)\n")
		b.WriteString("\t\t\t}\n")
		b.WriteString("\t\t\ts.iterMgr.closeSession(args.SessionID)\n")
		b.WriteString("\t\t\tbreak\n")
		b.WriteString("\t\t}\n")
		fmt.Fprintf(&b, "\t\tv := *sess.iter.Value()\n")
		fmt.Fprintf(&b, "\t\treply.Items = append(reply.Items, &v)\n")
		b.WriteString("\t}\n")
		b.WriteString("\ts.iterMgr.rowsStreamedTotal.Add(int64(len(reply.Items)))\n")
		b.WriteString("\treturn nil\n")
		b.WriteString("}\n\n")

		// xxxIterSession struct
		fmt.Fprintf(&b, "// %s is the server-side session for %s.\n", m.SessionType, m.Name)
		fmt.Fprintf(&b, "type %s struct {\n", m.SessionType)
		b.WriteString("\tmu       sync.Mutex\n")
		fmt.Fprintf(&b, "\titer     storage.Iter[%s]\n", m.ElemType)
		b.WriteString("\tlastUsed atomic.Int64\n")
		b.WriteString("}\n\n")
		fmt.Fprintf(&b, "func (s *%s) TypeName() string      { return %q }\n", m.SessionType, m.SessionType)
		fmt.Fprintf(&b, "func (s *%s) Touch()                { s.lastUsed.Store(time.Now().UnixNano()) }\n", m.SessionType)
		fmt.Fprintf(&b, "func (s *%s) LastUsedAt() time.Time { return time.Unix(0, s.lastUsed.Load()) }\n", m.SessionType)
		fmt.Fprintf(&b, "func (s *%s) Close() error          { return s.iter.Close() }\n\n", m.SessionType)
	}

	return fmtSource([]byte(b.String()))
}

// ── iter_client.go ───────────────────────────────────────────────────────────

func genIterClient(methods []iterMethodInfo) ([]byte, error) {
	typeStrs := allIterTypeStrs(methods)
	imports := usedImports(typeStrs,
		"context",
		"errors",
		"net/rpc",
		"github.com/steveyegge/beads/internal/storage",
	)

	var b strings.Builder
	b.WriteString("// Code generated by bddgen. DO NOT EDIT.\n\npackage rpc\n\n")
	b.WriteString(importBlock(imports))
	b.WriteString(`
// rpcIterClose sends an IterClose RPC to the daemon, releasing the server-side
// iterator session. Errors are ignored — the server's idle reaper will clean up
// any session that leaks due to a failed close.
func rpcIterClose(client *rpc.Client, sessionID string) error {
	args := &IterCloseArgs{SessionID: sessionID}
	var reply IterCloseReply
	return client.Call("daemonServer.IterClose", args, &reply)
}

`)

	for _, m := range methods {
		// rpcXxxIter struct
		fmt.Fprintf(&b, "// %s implements storage.Iter[%s] over net/rpc.\n", m.ClientType, m.ElemType)
		fmt.Fprintf(&b, "type %s struct {\n", m.ClientType)
		b.WriteString("\tclient    *rpc.Client\n")
		b.WriteString("\tsessionID string\n")
		fmt.Fprintf(&b, "\tbuf       []*%s\n", m.ElemType)
		b.WriteString("\tpos       int\n")
		b.WriteString("\texhausted bool\n")
		b.WriteString("\terr       error\n")
		b.WriteString("}\n\n")

		// Next method with context cancellation via rpc.Client.Go
		fmt.Fprintf(&b, "func (it *%s) Next(ctx context.Context) bool {\n", m.ClientType)
		b.WriteString("\tif it.exhausted {\n\t\treturn false\n\t}\n")
		b.WriteString("\t// Serve from buffer if available.\n")
		b.WriteString("\tif it.pos+1 < len(it.buf) {\n")
		b.WriteString("\t\tit.pos++\n")
		b.WriteString("\t\treturn true\n")
		b.WriteString("\t}\n")
		b.WriteString("\t// Fetch next batch from server.\n")
		b.WriteString("\targs := &IterNextArgs{SessionID: it.sessionID}\n")
		fmt.Fprintf(&b, "\tvar reply %sNextReply\n", m.Name)
		fmt.Fprintf(&b, "\tcall := it.client.Go(\"daemonServer.%sNext\", args, &reply, nil)\n", m.Name)
		b.WriteString("\tselect {\n")
		b.WriteString("\tcase <-ctx.Done():\n")
		b.WriteString("\t\tit.err = ctx.Err()\n")
		b.WriteString("\t\tit.exhausted = true\n")
		b.WriteString("\t\tgo rpcIterClose(it.client, it.sessionID) //nolint:errcheck\n")
		b.WriteString("\t\treturn false\n")
		b.WriteString("\tcase <-call.Done:\n")
		b.WriteString("\t}\n")
		b.WriteString("\tif call.Error != nil {\n")
		b.WriteString("\t\tit.err = call.Error\n")
		b.WriteString("\t\tit.exhausted = true\n")
		b.WriteString("\t\treturn false\n")
		b.WriteString("\t}\n")
		b.WriteString("\tif reply.RPCError != nil {\n")
		b.WriteString("\t\tit.err = decodeRPCError(reply.RPCError)\n")
		b.WriteString("\t\tit.exhausted = true\n")
		b.WriteString("\t\treturn false\n")
		b.WriteString("\t}\n")
		b.WriteString("\tit.buf = reply.Items\n")
		b.WriteString("\tit.pos = 0\n")
		b.WriteString("\tif reply.Done {\n")
		b.WriteString("\t\tit.exhausted = true\n")
		b.WriteString("\t}\n")
		b.WriteString("\treturn len(it.buf) > 0\n")
		b.WriteString("}\n\n")

		fmt.Fprintf(&b, "func (it *%s) Value() *%s {\n", m.ClientType, m.ElemType)
		b.WriteString("\tif it.pos < 0 || it.pos >= len(it.buf) {\n\t\treturn nil\n\t}\n")
		b.WriteString("\treturn it.buf[it.pos]\n")
		b.WriteString("}\n\n")

		fmt.Fprintf(&b, "func (it *%s) Err() error { return it.err }\n\n", m.ClientType)

		fmt.Fprintf(&b, "func (it *%s) Close() error {\n", m.ClientType)
		b.WriteString("\tif !it.exhausted {\n")
		b.WriteString("\t\tit.exhausted = true\n")
		b.WriteString("\t\treturn rpcIterClose(it.client, it.sessionID)\n")
		b.WriteString("\t}\n")
		b.WriteString("\treturn nil\n")
		b.WriteString("}\n\n")

		// Compile-time interface check
		fmt.Fprintf(&b, "var _ storage.Iter[%s] = (*%s)(nil)\n\n", m.ElemType, m.ClientType)

		// daemonClient.IterXxx method
		var sigParams []string
		sigParams = append(sigParams, "ctx context.Context")
		for _, p := range m.Params {
			sigParams = append(sigParams, p.Name+" "+p.TypeStr)
		}
		fmt.Fprintf(&b, "func (c *daemonClient) %s(%s) (storage.Iter[%s], error) {\n",
			m.Name, strings.Join(sigParams, ", "), m.ElemType)

		// Build start args.
		var fieldInits []string
		for _, p := range m.Params {
			fieldInits = append(fieldInits, p.FieldName+": "+p.Name)
		}
		if len(fieldInits) > 0 {
			fmt.Fprintf(&b, "\tstartArgs := &%sStartArgs{%s}\n", m.Name, strings.Join(fieldInits, ", "))
		} else {
			fmt.Fprintf(&b, "\tstartArgs := &%sStartArgs{}\n", m.Name)
		}
		b.WriteString("\tvar startReply IterStartReply\n")
		fmt.Fprintf(&b, "\tif err := c.client.Call(\"daemonServer.%sStart\", startArgs, &startReply); err != nil {\n", m.Name)
		b.WriteString("\t\treturn nil, err\n")
		b.WriteString("\t}\n")
		b.WriteString("\tif startReply.RPCError != nil {\n")
		b.WriteString("\t\trpcErr := decodeRPCError(startReply.RPCError)\n")

		if m.FallbackMethod != "" {
			// Generate slice-path fallback.
			b.WriteString("\t\tif errors.Is(rpcErr, storage.ErrTooManyIterators) {\n")
			var fallbackArgs []string
			fallbackArgs = append(fallbackArgs, "ctx")
			for _, p := range m.Params {
				fallbackArgs = append(fallbackArgs, p.Name)
			}
			fmt.Fprintf(&b, "\t\t\titems, err := c.%s(%s)\n", m.FallbackMethod, strings.Join(fallbackArgs, ", "))
			b.WriteString("\t\t\tif err != nil {\n\t\t\t\treturn nil, err\n\t\t\t}\n")
			fmt.Fprintf(&b, "\t\t\treturn storage.NewSliceIter(items), nil\n")
			b.WriteString("\t\t}\n")
		} else {
			// No direct slice fallback; propagate the error.
			b.WriteString("\t\t// No direct slice fallback for this method.\n")
		}

		b.WriteString("\t\treturn nil, rpcErr\n")
		b.WriteString("\t}\n")
		fmt.Fprintf(&b, "\treturn &%s{client: c.client, sessionID: startReply.SessionID, pos: -1}, nil\n", m.ClientType)
		b.WriteString("}\n\n")
	}

	return fmtSource([]byte(b.String()))
}
