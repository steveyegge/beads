// Package deps provides dependency management business logic for the bd CLI.
// It handles external references, dependency tree operations, and hierarchy checks.
package deps

import (
	"fmt"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// IsChildOf returns true if childID is a hierarchical child of parentID.
// For example, "bd-abc.1" is a child of "bd-abc", and "bd-abc.1.2" is a child of "bd-abc.1".
func IsChildOf(childID, parentID string) bool {
	_, actualParentID, depth := types.ParseHierarchicalID(childID)
	if depth == 0 {
		return false
	}
	if actualParentID == parentID {
		return true
	}
	return strings.HasPrefix(childID, parentID+".")
}

// ValidateExternalRef validates the format of an external dependency reference.
// Valid format: external:<project>:<capability>
func ValidateExternalRef(ref string) error {
	if !strings.HasPrefix(ref, "external:") {
		return fmt.Errorf("external reference must start with 'external:'")
	}

	parts := strings.SplitN(ref, ":", 3)
	if len(parts) != 3 {
		return fmt.Errorf("invalid external reference format: expected 'external:<project>:<capability>', got '%s'", ref)
	}

	project := parts[1]
	capability := parts[2]

	if project == "" {
		return fmt.Errorf("external reference missing project name")
	}
	if capability == "" {
		return fmt.Errorf("external reference missing capability name")
	}

	return nil
}

// IsExternalRef returns true if the dependency reference is an external reference.
func IsExternalRef(ref string) bool {
	return strings.HasPrefix(ref, "external:")
}

// ParseExternalRef parses an external reference into project and capability.
// Returns empty strings if the format is invalid.
func ParseExternalRef(ref string) (project, capability string) {
	if !IsExternalRef(ref) {
		return "", ""
	}
	parts := strings.SplitN(ref, ":", 3)
	if len(parts) != 3 {
		return "", ""
	}
	return parts[1], parts[2]
}

// FilterTreeByStatus filters the tree to only include nodes with the given status.
// Keeps parent chain to maintain tree structure.
func FilterTreeByStatus(tree []*types.TreeNode, status types.Status) []*types.TreeNode {
	if len(tree) == 0 {
		return tree
	}

	matches := make(map[string]bool)
	for _, node := range tree {
		if node.Status == status {
			matches[node.ID] = true
		}
	}

	if len(matches) == 0 {
		return []*types.TreeNode{}
	}

	parentOf := make(map[string]string)
	for _, node := range tree {
		if node.ParentID != "" && node.ParentID != node.ID {
			parentOf[node.ID] = node.ParentID
		}
	}

	keep := make(map[string]bool)
	for id := range matches {
		keep[id] = true
		current := id
		for {
			parent, ok := parentOf[current]
			if !ok || parent == current {
				break
			}
			keep[parent] = true
			current = parent
		}
	}

	var filtered []*types.TreeNode
	for _, node := range tree {
		if keep[node.ID] {
			filtered = append(filtered, node)
		}
	}

	return filtered
}

// MergeBidirectionalTrees merges up and down trees into a single visualization.
// The root appears once, with dependencies shown below and dependents shown above.
func MergeBidirectionalTrees(downTree, upTree []*types.TreeNode, rootID string) []*types.TreeNode {
	var result []*types.TreeNode

	hasUpNodes := false
	for _, node := range upTree {
		if node.ID != rootID {
			hasUpNodes = true
			break
		}
	}

	if hasUpNodes {
		for _, node := range upTree {
			if node.ID == rootID {
				continue
			}
			upNode := *node
			upNode.Depth = node.Depth
			result = append(result, &upNode)
		}
	}

	result = append(result, downTree...)

	return result
}

// GetStatusEmoji returns a symbol indicator for a given status.
func GetStatusEmoji(status types.Status) string {
	switch status {
	case types.StatusOpen:
		return "\u2610" // Ballot Box
	case types.StatusInProgress:
		return "\u25E7" // Square Left Half Black
	case types.StatusBlocked:
		return "\u26A0" // Warning Sign
	case types.StatusDeferred:
		return "\u2744" // Snowflake (on ice)
	case types.StatusClosed:
		return "\u2611" // Ballot Box with Check
	default:
		return "?"
	}
}

// OutputMermaidTree outputs a dependency tree in Mermaid.js flowchart format to stdout.
func OutputMermaidTree(tree []*types.TreeNode, rootID string) {
	if len(tree) == 0 {
		fmt.Println("flowchart TD")
		fmt.Printf("  %s[\"No dependencies\"]\n", rootID)
		return
	}

	fmt.Println("flowchart TD")

	nodesSeen := make(map[string]bool)
	for _, node := range tree {
		if !nodesSeen[node.ID] {
			emoji := GetStatusEmoji(node.Status)
			label := fmt.Sprintf("%s %s: %s", emoji, node.ID, node.Title)
			label = strings.ReplaceAll(label, "\\", "\\\\")
			label = strings.ReplaceAll(label, "\"", "\\\"")
			fmt.Printf("  %s[\"%s\"]\n", node.ID, label)

			nodesSeen[node.ID] = true
		}
	}

	fmt.Println()

	for _, node := range tree {
		if node.ParentID != "" && node.ParentID != node.ID {
			fmt.Printf("  %s --> %s\n", node.ParentID, node.ID)
		}
	}
}

// FormatTreeNode formats a single tree node with status, ready indicator, etc.
// The styleFunc parameter renders the ID string based on status.
// The passStyleBold parameter renders the "[READY]" badge.
// The mutedFunc parameter renders muted text.
// The warnFunc parameter renders warning text (for truncation).
// The isExternalRef parameter checks if a node ID is an external reference.
func FormatTreeNode(node *types.TreeNode, styleFunc func(types.Status, string) string, passStyleBold func(string) string, warnFunc func(string) string, isExternalRef func(string) bool) string {
	if isExternalRef(node.ID) {
		var idStr string
		switch node.Status {
		case types.StatusClosed:
			idStr = styleFunc(types.StatusClosed, node.Title)
		case types.StatusBlocked:
			idStr = styleFunc(types.StatusBlocked, node.Title)
		default:
			idStr = node.Title
		}
		return fmt.Sprintf("%s (external)", idStr)
	}

	idStr := styleFunc(node.Status, node.ID)

	line := fmt.Sprintf("%s: %s [P%d] (%s)",
		idStr, node.Title, node.Priority, node.Status)

	if node.Status == types.StatusOpen && node.Depth == 0 {
		line += " " + passStyleBold("[READY]")
	}

	return line
}

// TreeRenderer holds state for rendering a tree with proper connectors.
type TreeRenderer struct {
	seen             map[string]bool
	activeConnectors []bool
	maxDepth         int
	direction        string
	// Styling callbacks
	MutedFunc      func(string) string
	WarnFunc       func(string) string
	StyleFunc      func(types.Status, string) string
	PassStyleBold  func(string) string
	IsExternalRef  func(string) bool
}

// NewTreeRenderer creates a new tree renderer.
func NewTreeRenderer(maxDepth int, direction string) *TreeRenderer {
	return &TreeRenderer{
		seen:             make(map[string]bool),
		activeConnectors: make([]bool, maxDepth+1),
		maxDepth:         maxDepth,
		direction:        direction,
	}
}

// RenderTree renders the tree with proper box-drawing connectors.
func (r *TreeRenderer) RenderTree(tree []*types.TreeNode) {
	if len(tree) == 0 {
		return
	}

	children := make(map[string][]*types.TreeNode)
	var root *types.TreeNode

	for _, node := range tree {
		if node.Depth == 0 {
			root = node
		} else {
			children[node.ParentID] = append(children[node.ParentID], node)
		}
	}

	if root == nil && len(tree) > 0 {
		root = tree[0]
	}

	r.renderNode(root, children, 0, true)
}

func (r *TreeRenderer) renderNode(node *types.TreeNode, children map[string][]*types.TreeNode, depth int, isLast bool) {
	if node == nil {
		return
	}

	var prefix strings.Builder

	for i := 0; i < depth; i++ {
		if r.activeConnectors[i] {
			prefix.WriteString("\u2502   ") // │
		} else {
			prefix.WriteString("    ")
		}
	}

	if depth > 0 {
		if isLast {
			prefix.WriteString("\u2514\u2500\u2500 ") // └──
		} else {
			prefix.WriteString("\u251C\u2500\u2500 ") // ├──
		}
	}

	if r.seen[node.ID] {
		fmt.Printf("%s%s\n", prefix.String(), r.MutedFunc(node.ID+" (shown above)"))
		return
	}
	r.seen[node.ID] = true

	line := FormatTreeNode(node, r.StyleFunc, r.PassStyleBold, r.WarnFunc, r.IsExternalRef)

	if node.Truncated || (depth == r.maxDepth && len(children[node.ID]) > 0) {
		line += r.WarnFunc(" \u2026") // …
	}

	fmt.Printf("%s%s\n", prefix.String(), line)

	nodeChildren := children[node.ID]
	for i, child := range nodeChildren {
		if depth > 0 {
			r.activeConnectors[depth] = (i < len(nodeChildren) - 1)
		}
		r.renderNode(child, children, depth+1, i == len(nodeChildren)-1)
	}
}
