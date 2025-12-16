package jsonl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/steveyegge/beads/internal/types"
)

// ReadIssuesFromFile reads issues from a JSONL file
func ReadIssuesFromFile(path string) ([]*types.Issue, error) {
	// #nosec G304 - controlled path from caller
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open JSONL file: %w", err)
	}
	defer func() {
		if err := file.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to close JSONL file: %v\n", err)
		}
	}()

	var issues []*types.Issue
	scanner := bufio.NewScanner(file)
	// Increase buffer size to handle large JSONL lines (e.g., big descriptions)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024) // allow up to 64MB per line
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		var issue types.Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			return nil, fmt.Errorf("failed to parse issue at line %d: %w", lineNum, err)
		}
		issues = append(issues, &issue)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan JSONL file: %w", err)
	}

	return issues, nil
}

// ReadIssuesFromData reads issues from JSONL data in memory
func ReadIssuesFromData(data []byte) ([]*types.Issue, error) {
	var issues []*types.Issue
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// Increase buffer size to handle large JSONL lines
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if line == "" {
			continue
		}

		var issue types.Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			return nil, fmt.Errorf("failed to parse issue at line %d: %w", lineNum, err)
		}
		issues = append(issues, &issue)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan JSONL data: %w", err)
	}

	return issues, nil
}
