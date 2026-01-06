package toon

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/types"
)

// Format represents the serialization format
type Format string

const (
	// FormatTOON is the TOON format (optimized for tokens and readability)
	FormatTOON Format = "toon"

	// FormatJSONL is the JSONL format (one JSON object per line, or array)
	FormatJSONL Format = "jsonl"
	
	// FormatUnknown is for unknown or no extension (caller should decide default)
	FormatUnknown Format = "unknown"
)

// DetectFormatFromExtension detects format based on file extension
func DetectFormatFromExtension(filename string) Format {
	// Get the file extension (without the dot)
	ext := strings.ToLower(filepath.Ext(filename))

	if ext == ".toon" {
		return FormatTOON
	}
	
	if ext == ".jsonl" {
		return FormatJSONL
	}
	
	// Return unknown for unrecognized extensions
	return FormatUnknown
}

// DecodeTOON decodes TOON format data to a slice of issues.
// This function can accept either string or []byte input.
// This function uses the internal/beads package for the actual unmarshaling.
func DecodeTOON(data interface{}) ([]*types.Issue, error) {
	var bytes []byte
	
	switch v := data.(type) {
	case string:
		bytes = []byte(v)
	case []byte:
		bytes = v
	default:
		return nil, fmt.Errorf("DecodeTOON expects string or []byte, got %T", data)
	}
	
	return beads.UnmarshalFromTOON(bytes)
}

// DecodeTOONBytes decodes TOON format bytes to a slice of issues.
func DecodeTOONBytes(data []byte) ([]*types.Issue, error) {
	return beads.UnmarshalFromTOON(data)
}

// EncodeTOON encodes a slice of issues to TOON format.
func EncodeTOON(issues []*types.Issue) ([]byte, error) {
	return beads.MarshalToTOON(issues)
}

// DetectFormat detects the format of byte data (TOON vs JSONL)
// Returns FormatUnknown if format cannot be determined
func DetectFormat(data []byte) Format {
	format, err := beads.DetectFormat(data)
	if err != nil {
		return FormatUnknown
	}
	return Format(format)
}

// DecodeJSON decodes JSONL (or JSON array) format data to a slice of issues.
// Supports both JSONL (one JSON object per line) and JSON array formats.
func DecodeJSON(data string) ([]*types.Issue, error) {
	bytes := []byte(data)
	
	// Try to parse as JSON array first
	var issuesArray []*types.Issue
	if err := json.Unmarshal(bytes, &issuesArray); err == nil {
		return issuesArray, nil
	}
	
	// Fall back to JSONL format (one JSON object per line)
	var issues []*types.Issue
	lines := strings.Split(strings.TrimSpace(data), "\n")
	
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		
		var issue *types.Issue
		if err := json.Unmarshal([]byte(line), &issue); err != nil {
			return nil, fmt.Errorf("failed to parse JSON line: %w", err)
		}
		
		issues = append(issues, issue)
	}
	
	return issues, nil
}

// EncodeLineByLine encodes a slice of issues to JSONL format (one JSON object per line).
func EncodeLineByLine(issues []*types.Issue) ([]string, error) {
	var lines []string
	
	for _, issue := range issues {
		bytes, err := json.Marshal(issue)
		if err != nil {
			return nil, fmt.Errorf("failed to encode issue: %w", err)
		}
		
		lines = append(lines, string(bytes))
	}
	
	return lines, nil
}
