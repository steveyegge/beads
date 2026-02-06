package comment

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ValidateReferences checks all references in the scan result against the filesystem
// and updates their status to valid or broken.
func ValidateReferences(root string, result *ScanResult) {
	// Build a set of all Go files for reference resolution.
	goFiles := make(map[string]bool)
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".go") {
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil {
				goFiles[rel] = true
			}
			goFiles[path] = true
		}
		return nil
	})

	// Build set of known declarations from scanned nodes + test files.
	knownDecls := make(map[string]bool)
	for _, n := range result.Nodes {
		if n.AssociatedDecl != "" {
			knownDecls[n.AssociatedDecl] = true
		}
	}
	for _, d := range result.TestDecls {
		knownDecls[d] = true
	}

	for i, node := range result.Nodes {
		for j, ref := range node.References {
			status := validateSingleRef(root, ref, goFiles, knownDecls)
			result.Nodes[i].References[j].Status = status
			if status == RefBroken {
				result.BrokenRefs++
			}
		}
	}
}

func validateSingleRef(root string, ref Ref, goFiles map[string]bool, knownDecls map[string]bool) RefStatus {
	switch ref.TargetType {
	case RefFile:
		target := ref.Target
		// Handle "file.go:funcName" format.
		if idx := strings.Index(target, ":"); idx > 0 {
			target = target[:idx]
		}
		// Exact match first.
		if goFiles[target] {
			return RefValid
		}
		// Basename match: "autoflush.go" should match "cmd/bd/autoflush.go".
		basename := filepath.Base(target)
		for f := range goFiles {
			if filepath.Base(f) == basename {
				return RefValid
			}
		}
		return RefBroken

	case RefFunction:
		if knownDecls[ref.Target] {
			return RefValid
		}
		return RefBroken

	case RefSpec:
		specPath := filepath.Join(root, ref.Target)
		if _, err := os.Stat(specPath); err == nil {
			return RefValid
		}
		for _, prefix := range []string{"specs/", "docs/", ""} {
			if _, err := os.Stat(filepath.Join(root, prefix, ref.Target)); err == nil {
				return RefValid
			}
		}
		return RefBroken

	case RefURL:
		return RefUnknown

	case RefComment:
		return RefUnknown
	}
	return RefUnknown
}

// DetectDrift analyzes a scan result for various kinds of drift.
// Uses per-file git blame (batched) instead of per-line git log.
func DetectDrift(root string, result *ScanResult) *DriftReport {
	report := &DriftReport{}

	// Group nodes by file for batched git blame.
	byFile := make(map[string][]int) // file -> indices into result.Nodes
	for i, node := range result.Nodes {
		byFile[node.File] = append(byFile[node.File], i)
	}

	// Run git blame per-file (not per-line) for staleness.
	blameCache := make(map[string]map[int]time.Time) // file -> line -> date
	for file, indices := range byFile {
		needBlame := false
		for _, idx := range indices {
			n := result.Nodes[idx]
			// Only blame doc/reference comments with associated declarations.
			if n.AssociatedDecl != "" && (n.Kind == KindDoc || n.Kind == KindReference) {
				needBlame = true
				break
			}
			if n.Kind == KindTodo {
				needBlame = true
				break
			}
		}
		if needBlame {
			blameCache[file] = gitBlameFile(root, file)
		}
	}

	for _, node := range result.Nodes {
		// Broken references.
		for _, ref := range node.References {
			if ref.Status == RefBroken {
				report.BrokenRefs = append(report.BrokenRefs, DriftItem{
					Node:    node,
					Reason:  fmt.Sprintf("Broken reference: %s → %s", ref.TargetType, ref.Target),
					Details: fmt.Sprintf("%s:%d", node.File, node.Line),
				})
			}
		}

		// Expired TODOs (use blame cache).
		if node.Kind == KindTodo {
			if blame, ok := blameCache[node.File]; ok {
				if lastMod, ok := blame[node.Line]; ok && !lastMod.IsZero() {
					age := int(time.Since(lastMod).Hours() / 24)
					if age > 90 {
						report.ExpiredTodos = append(report.ExpiredTodos, DriftItem{
							Node:    node,
							Reason:  fmt.Sprintf("TODO is %d days old", age),
							Details: node.Content,
						})
					}
				}
			}
		}

		// Stale doc comments: code changed but comment didn't.
		if node.AssociatedDecl != "" && (node.Kind == KindDoc || node.Kind == KindReference) {
			if blame, ok := blameCache[node.File]; ok {
				staleness := detectStalenessBatched(node, blame)
				if staleness > 30 {
					node.Staleness = float64(staleness)
					report.StaleComments = append(report.StaleComments, DriftItem{
						Node:    node,
						Reason:  fmt.Sprintf("Code changed %d days after comment was last modified", staleness),
						Details: fmt.Sprintf("Associated: %s", node.AssociatedDecl),
					})
				}
			}
		}

		// Inconsistent invariants.
		if node.Kind == KindInvariant {
			if issue := checkInvariantConsistency(root, node); issue != "" {
				report.Inconsistent = append(report.Inconsistent, DriftItem{
					Node:    node,
					Reason:  "Invariant may be inconsistent",
					Details: issue,
				})
			}
		}
	}

	return report
}

// gitBlameFile runs git blame on an entire file and returns line -> last-modified date.
// One subprocess per file instead of per line.
func gitBlameFile(root string, file string) map[int]time.Time {
	result := make(map[int]time.Time)

	cmd := exec.Command("git", "blame", "--porcelain", file)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return result
	}

	// Parse porcelain format: look for "author-time" lines and "filename" boundaries.
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	var currentTime time.Time
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "author-time ") {
			ts := strings.TrimPrefix(line, "author-time ")
			if epoch, err := strconv.ParseInt(ts, 10, 64); err == nil {
				currentTime = time.Unix(epoch, 0)
			}
		}
		// The content line starts with a tab.
		if strings.HasPrefix(line, "\t") {
			lineNum++
			if !currentTime.IsZero() {
				result[lineNum] = currentTime
			}
		}
	}

	return result
}

// detectStalenessBatched checks staleness using pre-fetched blame data.
func detectStalenessBatched(node Node, blame map[int]time.Time) int {
	// Find oldest date in comment lines.
	var commentDate time.Time
	for line := node.Line; line <= node.EndLine; line++ {
		if t, ok := blame[line]; ok {
			if commentDate.IsZero() || t.Before(commentDate) {
				commentDate = t
			}
		}
	}
	if commentDate.IsZero() {
		return 0
	}

	// Find newest date in code lines after the comment (next 20 lines).
	var codeDate time.Time
	for line := node.EndLine + 1; line <= node.EndLine+20; line++ {
		if t, ok := blame[line]; ok {
			if t.After(codeDate) {
				codeDate = t
			}
		}
	}
	if codeDate.IsZero() {
		return 0
	}

	if codeDate.After(commentDate) {
		return int(codeDate.Sub(commentDate).Hours() / 24)
	}
	return 0
}

// numberPattern matches numeric values in comments like "max 5 items" or "<= 10".
var numberPattern = regexp.MustCompile(`\b(\d+)\b`)

// checkInvariantConsistency does a basic check: if the invariant mentions a number
// and the associated code has a different number, flag it.
func checkInvariantConsistency(root string, node Node) string {
	numbers := numberPattern.FindAllString(node.Content, -1)
	if len(numbers) == 0 {
		return ""
	}
	if node.AssociatedDecl == "" {
		return ""
	}

	content, err := os.ReadFile(filepath.Join(root, node.File))
	if err != nil {
		return ""
	}

	lines := strings.Split(string(content), "\n")
	searchStart := node.EndLine // EndLine is 1-indexed, lines is 0-indexed
	searchEnd := searchStart + 5
	if searchEnd > len(lines) {
		searchEnd = len(lines)
	}

	for _, commentNum := range numbers {
		cn, _ := strconv.Atoi(commentNum)
		if cn == 0 {
			continue
		}
		for lineIdx := searchStart; lineIdx < searchEnd; lineIdx++ {
			lineNums := numberPattern.FindAllString(lines[lineIdx], -1)
			for _, ln := range lineNums {
				lnVal, _ := strconv.Atoi(ln)
				if lnVal != 0 && lnVal != cn {
					return fmt.Sprintf("Comment says %d but code has %d (line %d)", cn, lnVal, lineIdx+1)
				}
			}
		}
	}
	return ""
}
