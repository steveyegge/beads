//go:build cgo

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
)

// TestCobraParallelPolicyGuard scans test source files and fails if any
// parallel test calls cobra's Help() or Execute() without setting explicit
// output writers. This prevents the data race where cobra's OutOrStdout()
// reads os.Stdout concurrently with captureStdout() redirecting it.
//
// The rule: if a test function body contains t.Parallel(), and calls
// .Help( or .Execute(, it must also contain .SetOut( and .SetErr(.
//
// This is intentionally blunt regex matching (80/20), not full AST analysis.
func TestCobraParallelPolicyGuard(t *testing.T) {
	t.Parallel()

	testFiles, err := filepath.Glob("*_test.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	// Match function blocks: "func TestXxx(t *testing.T) { ... }"
	// We split on func boundaries and check each one.
	funcPattern := regexp.MustCompile(`(?m)^func (Test\w+)\(`)

	for _, file := range testFiles {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		content := string(data)

		// Find all function start positions
		matches := funcPattern.FindAllStringIndex(content, -1)
		names := funcPattern.FindAllStringSubmatch(content, -1)

		for i, match := range matches {
			// Extract function body (from this func to the next, or EOF)
			start := match[0]
			end := len(content)
			if i+1 < len(matches) {
				end = matches[i+1][0]
			}
			body := content[start:end]
			funcName := names[i][1]

			// Strip single-line comments to avoid false positives from
			// comments like "// Not using t.Parallel() because ..."
			stripped := stripLineComments(body)

			if !strings.Contains(stripped, "t.Parallel()") {
				continue // not parallel, no issue
			}
			if !strings.Contains(stripped, ".Help(") && !strings.Contains(stripped, ".Execute(") {
				continue // doesn't call cobra methods
			}

			// Parallel + cobra calls: must have explicit output writers
			if !strings.Contains(stripped, ".SetOut(") || !strings.Contains(stripped, ".SetErr(") {
				t.Errorf("%s:%s calls t.Parallel() and cobra Help()/Execute() "+
					"without cmd.SetOut() and cmd.SetErr(). "+
					"This races with captureStdout(). "+
					"Either remove t.Parallel() or set explicit writers. "+
					"See stdioMutex comment in test_helpers_test.go.",
					file, funcName)
			}
		}
	}
}

// stripLineComments removes // comments from each line, preserving code.
func stripLineComments(s string) string {
	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		// Find // that's not inside a string literal (80/20: skip lines
		// that are entirely comments, handle inline comments naively)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue // entire line is a comment
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// TestStdioMutexContract verifies that stdioMutex actually serializes
// captureStdout calls. If someone removes or bypasses the mutex, this
// test fails deterministically without needing -race.
func TestStdioMutexContract(t *testing.T) {
	// reentrancy detector: if two goroutines are inside the critical
	// section at the same time, this counter exceeds 1.
	var inside atomic.Int32
	var violations atomic.Int32

	const goroutines = 4
	const iterations = 50

	done := make(chan struct{}, goroutines)

	for range goroutines {
		go func() {
			defer func() { done <- struct{}{} }()
			for range iterations {
				captureStdout(t, func() error {
					n := inside.Add(1)
					if n > 1 {
						violations.Add(1)
					}
					// Small busyloop to widen the race window
					sum := 0
					for j := range 100 {
						sum += j
					}
					_ = sum
					inside.Add(-1)
					return nil
				})
			}
		}()
	}

	for range goroutines {
		<-done
	}

	if v := violations.Load(); v > 0 {
		t.Fatalf("stdioMutex failed to serialize: %d concurrent entries detected", v)
	}
}
