package main

import (
	"fmt"

	"github.com/steveyegge/beads/internal/beads"
	"github.com/steveyegge/beads/internal/toon"
	"github.com/steveyegge/beads/internal/types"
)

// Type aliases for convenience
type Format = toon.Format

const (
	// FormatTOON is the TOON format (optimized for tokens and readability)
	FormatTOON = toon.FormatTOON

	// FormatJSONL is the JSONL format (one JSON object per line, or array)
	FormatJSONL = toon.FormatJSONL
	
	// FormatUnknown is for unknown or no extension
	FormatUnknown = toon.FormatUnknown
)

// DetectFormatFromExtension detects format based on file extension
// For unknown extensions, defaults to JSONL
func DetectFormatFromExtension(filename string) Format {
	detected := toon.DetectFormatFromExtension(filename)
	// Default to JSONL for unknown extensions in cmd/bd context
	if detected == toon.FormatUnknown {
		return FormatJSONL
	}
	return detected
}

// DecodeTOON decodes TOON format string to issues
func DecodeTOON(toonStr string) ([]*types.Issue, error) {
	return toon.DecodeTOON(toonStr)
}

// DecodeJSONL decodes JSONL format string to issues
func DecodeJSONL(jsonlStr string) ([]*types.Issue, error) {
	return beads.UnmarshalFromJSONL([]byte(jsonlStr))
}

// EncodeTOON encodes issues to TOON format
func EncodeTOON(issues []*types.Issue) ([]byte, error) {
	return toon.EncodeTOON(issues)
}

// EncodeJSONL encodes issues to JSONL format
func EncodeJSONL(issues []*types.Issue) ([]byte, error) {
	return beads.MarshalToJSONL(issues)
}

// DecodeFormat detects format from content and decodes appropriately
func DecodeFormat(data []byte) ([]*types.Issue, Format, error) {
	format, err := beads.DetectFormat(data)
	if err != nil {
		return nil, "", fmt.Errorf("cannot detect format: %w", err)
	}

	var issues []*types.Issue

	switch format {
	case beads.FormatTOON:
		issues, err = beads.UnmarshalFromTOON(data)
		if err != nil {
			return nil, Format(format), fmt.Errorf("failed to decode TOON: %w", err)
		}
	case beads.FormatJSONL:
		issues, err = beads.UnmarshalFromJSONL(data)
		if err != nil {
			return nil, Format(format), fmt.Errorf("failed to decode JSONL: %w", err)
		}
	default:
		return nil, "", fmt.Errorf("unsupported format: %s", format)
	}

	return issues, Format(format), nil
}
