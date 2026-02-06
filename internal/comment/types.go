// Package comment provides first-class comment tracking for Go codebases.
// It scans source files, builds a comment graph with cross-references,
// and detects comment drift (stale comments, broken links, expired TODOs).
package comment

import (
	"crypto/sha256"
	"fmt"
	"time"
)

// Kind classifies a code comment.
type Kind string

const (
	KindDoc       Kind = "doc"       // Function/type/package documentation
	KindTodo      Kind = "todo"      // TODO, FIXME, HACK, XXX
	KindInvariant Kind = "invariant" // Business rules: @invariant, INVARIANT:, ASSERT:
	KindReference Kind = "reference" // Cross-references: See X, Ref: Y, @links
	KindInline    Kind = "inline"    // End-of-line comments
)

// RefTargetType identifies what a comment reference points to.
type RefTargetType string

const (
	RefFunction RefTargetType = "function"
	RefFile     RefTargetType = "file"
	RefComment  RefTargetType = "comment"
	RefURL      RefTargetType = "url"
	RefSpec     RefTargetType = "spec"
)

// RefStatus tracks whether a reference is still valid.
type RefStatus string

const (
	RefValid   RefStatus = "valid"
	RefBroken  RefStatus = "broken"
	RefStale   RefStatus = "stale"
	RefUnknown RefStatus = "unknown"
)

// Tag represents a structured @-tag extracted from a comment.
type Tag struct {
	Name  string `json:"name"`  // e.g., "links", "invariant", "todo"
	Value string `json:"value"` // e.g., "auth.go:validateToken"
}

// Ref represents a cross-reference extracted from a comment.
type Ref struct {
	TargetType RefTargetType `json:"target_type"`
	Target     string        `json:"target"`    // The raw reference text
	Resolved   string        `json:"resolved"`  // Resolved absolute path or identifier
	Status     RefStatus     `json:"status"`
}

// Node represents a single comment (or comment group) in the graph.
type Node struct {
	ID             string    `json:"id"`
	File           string    `json:"file"`
	Line           int       `json:"line"`
	EndLine        int       `json:"end_line"`
	Content        string    `json:"content"`
	Kind           Kind      `json:"kind"`
	References     []Ref     `json:"references,omitempty"`
	Tags           []Tag     `json:"tags,omitempty"`
	AssociatedDecl string    `json:"associated_decl,omitempty"`
	FirstSeen      time.Time `json:"first_seen"`
	LastCodeChange time.Time `json:"last_code_change,omitempty"`
	Staleness      float64   `json:"staleness"`
}

// HashComment produces a stable ID for a comment based on file and content.
// Using content-hash means moved comments (same text, different line) keep their ID.
func HashComment(file, content string) string {
	h := sha256.Sum256([]byte(file + ":" + content))
	return fmt.Sprintf("%x", h[:4]) // 8-char hex
}

// ScanResult holds the output of scanning a directory.
type ScanResult struct {
	Nodes      []Node        `json:"nodes"`
	TestDecls  []string      `json:"test_decls,omitempty"` // Declarations from _test.go files
	ByKind     map[Kind]int  `json:"by_kind"`
	BrokenRefs int           `json:"broken_refs"`
	StaleNodes int           `json:"stale_nodes"`
	FilesCount int           `json:"files_count"`
	Duration   time.Duration `json:"duration_ms"`
}

// DriftReport holds the output of drift detection.
type DriftReport struct {
	BrokenRefs   []DriftItem `json:"broken_refs"`
	StaleComments []DriftItem `json:"stale_comments"`
	ExpiredTodos  []DriftItem `json:"expired_todos"`
	Inconsistent  []DriftItem `json:"inconsistent_invariants"`
}

// DriftItem is a single drift finding.
type DriftItem struct {
	Node    Node   `json:"node"`
	Reason  string `json:"reason"`
	Details string `json:"details,omitempty"`
}
