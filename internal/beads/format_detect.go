package beads

import (
	"bytes"
	"fmt"
)

// Format represents the serialization format for beads data.
type Format string

const (
	// FormatTOON is the TOON format (optimized for tokens and readability)
	FormatTOON Format = "toon"

	// FormatJSONL is the JSONL format (one JSON object per line, or array)
	FormatJSONL Format = "jsonl"
)

// DetectFormat detects whether the given data is in TOON or JSONL format.
// Returns an error if the data format cannot be determined.
func DetectFormat(data []byte) (Format, error) {
	// Trim whitespace
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return "", fmt.Errorf("cannot detect format: data is empty or whitespace-only")
	}

	// Check if starts with "issues[" (TOON format)
	if bytes.HasPrefix(trimmed, []byte("issues[")) {
		return FormatTOON, nil
	}

	// Check if starts with "{" or "[" (JSONL format)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return FormatJSONL, nil
	}

	return "", fmt.Errorf("cannot detect format: data does not match TOON (issues[) or JSONL ({ or [) patterns")
}
