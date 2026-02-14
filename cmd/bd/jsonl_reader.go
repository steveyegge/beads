package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/steveyegge/beads/internal/types"
)

// loadIssuesFromJSONL reads all issues from a JSONL file.
func loadIssuesFromJSONL(path string) ([]*types.Issue, error) {
	// nolint:gosec // G304: path is validated JSONL file from findJSONLPath
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var issues []*types.Issue
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // 10MB max line size

	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		var issue types.Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNum, err)
		}
		issue.SetDefaults() // Apply defaults for omitted fields (beads-399)

		issues = append(issues, &issue)
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return issues, nil
}
