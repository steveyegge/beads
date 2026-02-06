package comment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyComment(t *testing.T) {
	tests := []struct {
		content string
		isDoc   bool
		want    Kind
	}{
		{"TODO: fix this later", false, KindTodo},
		{"FIXME: memory leak here", false, KindTodo},
		{"HACK: temporary workaround", false, KindTodo},
		{"@invariant max_items <= 10", false, KindInvariant},
		{"INVARIANT: discount never exceeds 50%", false, KindInvariant},
		{"ASSERT: token must be valid", false, KindInvariant},
		{"See auth.go:validateToken", false, KindReference},
		{"Ref: docs/SPEC.md", false, KindReference},
		{"@links order.go:MaxItems", false, KindReference},
		{"Handles retry with exponential backoff", true, KindDoc},
		{"Handles retry with exponential backoff", false, KindInline},
		{"increments the counter", false, KindInline},
	}

	for _, tt := range tests {
		got := ClassifyComment(tt.content, tt.isDoc)
		if got != tt.want {
			t.Errorf("ClassifyComment(%q, %v) = %v, want %v", tt.content, tt.isDoc, got, tt.want)
		}
	}
}

func TestExtractReferences(t *testing.T) {
	tests := []struct {
		content  string
		wantLen  int
		wantType RefTargetType
	}{
		{"See auth.go:validateToken", 1, RefFile},
		{"Ref: docs/AUTH_SPEC.md", 1, RefSpec},
		{"@links order.go:MaxItems", 1, RefFile},
		{"https://jwt.io/introduction", 1, RefURL},
		{"See auth.go:validate and Ref: SPEC.md", 2, RefFile},
		{"No references here", 0, ""},
		// False positive filters:
		{"See GH#804 for details", 0, ""},              // GitHub issue ref (2 chars, below 3-char min)
		{"See GH#1062 for why", 0, ""},                  // Another GH ref
		{"See Decision 008", 0, ""},                      // Prose word, excluded
		{"See Gas Town decision", 0, ""},                 // "Gas" excluded as common prose word
		{"See ValidateToken for auth", 1, RefFunction},  // Real function ref
	}

	for _, tt := range tests {
		refs := ExtractReferences(tt.content)
		if len(refs) != tt.wantLen {
			t.Errorf("ExtractReferences(%q) returned %d refs, want %d", tt.content, len(refs), tt.wantLen)
			continue
		}
		if tt.wantLen > 0 && refs[0].TargetType != tt.wantType {
			t.Errorf("ExtractReferences(%q)[0].TargetType = %v, want %v", tt.content, refs[0].TargetType, tt.wantType)
		}
	}
}

func TestExtractTags(t *testing.T) {
	tests := []struct {
		content string
		wantLen int
		wantTag string
	}{
		{"@links auth.go:validateToken", 1, "links"},
		{"@invariant max <= 10", 1, "invariant"},
		{"@todo remove_after=2026-03-01", 1, "todo"},
		{"No tags here", 0, ""},
		{"@links foo.go\n@invariant bar > 0", 2, "links"},
	}

	for _, tt := range tests {
		tags := ExtractTags(tt.content)
		if len(tags) != tt.wantLen {
			t.Errorf("ExtractTags(%q) returned %d tags, want %d", tt.content, len(tags), tt.wantLen)
			continue
		}
		if tt.wantLen > 0 && tags[0].Name != tt.wantTag {
			t.Errorf("ExtractTags(%q)[0].Name = %v, want %v", tt.content, tags[0].Name, tt.wantTag)
		}
	}
}

func TestHashComment(t *testing.T) {
	h1 := HashComment("foo.go", "some content")
	h2 := HashComment("foo.go", "some content")
	h3 := HashComment("bar.go", "some content")

	if h1 != h2 {
		t.Error("Same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("Different files should produce different hashes")
	}
	if len(h1) != 8 {
		t.Errorf("Hash length should be 8, got %d", len(h1))
	}
}

func TestScanFile(t *testing.T) {
	// Create a temporary Go file with known comments.
	dir := t.TempDir()
	src := filepath.Join(dir, "example.go")

	content := `package example

// Package example provides test utilities.

// TODO: remove this after migration
const Legacy = true

// MaxItems is the maximum items per order.
// @invariant max_items <= 10
// See order_spec.md for details
const MaxItems = 10

// HandleRequest processes incoming requests.
// See auth.go:validateToken for token validation.
func HandleRequest() {
	// increment counter
	counter++
}
`
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	nodes, err := ScanFile(src)
	if err != nil {
		t.Fatal(err)
	}

	if len(nodes) == 0 {
		t.Fatal("Expected at least one comment node")
	}

	// Check we found different kinds.
	kinds := make(map[Kind]int)
	for _, n := range nodes {
		kinds[n.Kind]++
		t.Logf("  Node: kind=%s line=%d content=%q assoc=%s", n.Kind, n.Line, n.Content[:min(len(n.Content), 50)], n.AssociatedDecl)
	}

	if kinds[KindTodo] == 0 {
		t.Error("Expected to find a TODO comment")
	}
	// Doc or reference comments should be present (some doc comments may be classified as reference due to "See" patterns).
	if kinds[KindDoc]+kinds[KindReference] == 0 {
		t.Error("Expected to find doc or reference comments")
	}
}

func TestScanDir(t *testing.T) {
	dir := t.TempDir()

	// Create a simple Go file.
	src := filepath.Join(dir, "main.go")
	content := `package main

// Main entry point.
// TODO: add graceful shutdown
func main() {
	// See config.go:loadConfig
}
`
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	result, err := ScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	if result.FilesCount != 1 {
		t.Errorf("Expected 1 file, got %d", result.FilesCount)
	}
	if len(result.Nodes) == 0 {
		t.Error("Expected at least one comment node")
	}
	if result.ByKind[KindTodo] == 0 {
		t.Error("Expected to count at least one TODO")
	}
}

func TestGraphRoundtrip(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "comments.db")

	g, err := OpenGraph(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()

	result := &ScanResult{
		Nodes: []Node{
			{
				ID:      "abc12345",
				File:    "main.go",
				Line:    10,
				EndLine: 12,
				Content: "TODO: fix this",
				Kind:    KindTodo,
				References: []Ref{
					{TargetType: RefFile, Target: "auth.go:validate", Status: RefBroken},
				},
				Tags: []Tag{{Name: "todo", Value: "fix this"}},
			},
			{
				ID:      "def67890",
				File:    "main.go",
				Line:    20,
				EndLine: 20,
				Content: "increment counter",
				Kind:    KindInline,
			},
		},
		ByKind:     map[Kind]int{KindTodo: 1, KindInline: 1},
		FilesCount: 1,
	}

	if err := g.StoreScanResult(result); err != nil {
		t.Fatal(err)
	}

	// Retrieve all nodes.
	nodes, err := g.GetAllNodes()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("Expected 2 nodes, got %d", len(nodes))
	}

	// Check first node has reference.
	if len(nodes[0].References) != 1 {
		t.Errorf("Expected 1 reference on first node, got %d", len(nodes[0].References))
	}

	// Check stats.
	stats, err := g.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats["total"] != 2 {
		t.Errorf("Expected total=2, got %d", stats["total"])
	}

	// Check broken refs query.
	broken, err := g.GetBrokenRefs()
	if err != nil {
		t.Fatal(err)
	}
	if len(broken) != 1 {
		t.Errorf("Expected 1 broken ref node, got %d", len(broken))
	}

	// Check file filter.
	fileNodes, err := g.GetNodesByFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(fileNodes) != 2 {
		t.Errorf("Expected 2 nodes for main.go, got %d", len(fileNodes))
	}
}

func TestValidateReferences(t *testing.T) {
	dir := t.TempDir()

	// Create auth.go so it can be referenced.
	if err := os.WriteFile(filepath.Join(dir, "auth.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	result := &ScanResult{
		Nodes: []Node{
			{
				ID:   "test1",
				File: "main.go",
				Line: 1,
				References: []Ref{
					{TargetType: RefFile, Target: "auth.go", Status: RefUnknown},
					{TargetType: RefFile, Target: "nonexistent.go", Status: RefUnknown},
				},
			},
		},
	}

	ValidateReferences(dir, result)

	refs := result.Nodes[0].References
	if refs[0].Status != RefValid {
		t.Errorf("auth.go should be valid, got %s", refs[0].Status)
	}
	if refs[1].Status != RefBroken {
		t.Errorf("nonexistent.go should be broken, got %s", refs[1].Status)
	}
}
