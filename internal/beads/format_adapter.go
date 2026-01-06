package beads

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/steveyegge/beads/internal/types"
	toon "github.com/toon-format/toon-go"
)

// MarshalToTOON serializes a slice of issues to TOON format.
// Uses JSON-to-map conversion workaround for toon-go type compatibility.
func MarshalToTOON(issues []*types.Issue) ([]byte, error) {
	// Convert issues to JSON first, then to map to work around toon-go's type checking
	jsonData, err := json.Marshal(issues)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal to JSON: %w", err)
	}
	
	// Unmarshal JSON to generic interface{} (map[string]interface{})
	var data interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to generic: %w", err)
	}
	
	// Now marshal to TOON (toon-go can handle generic maps)
	toonData, err := toon.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal to TOON: %w", err)
	}
	return toonData, nil
}

// UnmarshalFromTOON deserializes issues from TOON format.
func UnmarshalFromTOON(data []byte) ([]*types.Issue, error) {
	var issues []*types.Issue
	err := toon.Unmarshal(data, &issues)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal from TOON: %w", err)
	}
	return issues, nil
}

// MarshalToJSONL serializes a slice of issues to JSONL format.
// Returns either one JSON object per line (for multiple issues) or a single JSON array.
func MarshalToJSONL(issues []*types.Issue) ([]byte, error) {
	if len(issues) == 0 {
		return []byte("[]"), nil
	}

	// Marshal as JSON array for consistency with existing code
	data, err := json.MarshalIndent(issues, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal to JSONL: %w", err)
	}
	return data, nil
}

// UnmarshalFromJSONL deserializes issues from JSONL format.
// Handles both single JSON array and line-by-line JSON objects.
func UnmarshalFromJSONL(data []byte) ([]*types.Issue, error) {
	var issues []*types.Issue

	// First, try unmarshaling as JSON array
	err := json.Unmarshal(data, &issues)
	if err == nil {
		return issues, nil
	}

	// If that fails, try line-by-line JSON objects
	scanner := bytes.NewBufferString(string(data))
	decoder := json.NewDecoder(scanner)

	for decoder.More() {
		var issue types.Issue
		if err := decoder.Decode(&issue); err != nil {
			return nil, fmt.Errorf("failed to unmarshal line from JSONL: %w", err)
		}
		issues = append(issues, &issue)
	}

	if len(issues) == 0 {
		return nil, fmt.Errorf("failed to unmarshal from JSONL: %w", err)
	}

	return issues, nil
}
