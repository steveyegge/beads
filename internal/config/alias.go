package config

import (
	"fmt"
	"os"
	"strings"
)

// ResolveArchiveFormat returns the effective archive format for the current workspace.
//
// Resolution order:
//  1. archive.format in config.yaml — the canonical key.
//  2. export.auto in config.yaml — the deprecated alias. When found, a
//     deprecation notice is printed to stderr and the translated value is
//     returned without mutating config on disk (read-only resolution).
//
// Returns ArchiveFormatNone when neither key is set.
func ResolveArchiveFormat() string {
	if format, err := GetArchiveFormat(); err == nil && format != "" {
		return format
	}
	// Fall back to export.auto alias.
	raw := GetYamlConfig("export.auto")
	if raw == "" {
		return ArchiveFormatNone
	}
	fmt.Fprintf(os.Stderr,
		"⚠ Deprecated config key 'export.auto' — migrate: bd config set archive.format jsonl\n")
	switch strings.ToLower(raw) {
	case "true", "1", "yes":
		return ArchiveFormatJSONL
	default:
		return ArchiveFormatNone
	}
}

// SetArchiveFormat writes the archive format config key, handling the
// export.auto → archive.format backwards-compat alias.
//
//   - key == "export.auto": translates "true"/"false" to "jsonl"/"none",
//     writes archive.format, and prints a deprecation notice to stderr.
//   - key == "archive.format": validates and writes directly.
//
// Any other key is rejected.
func SetArchiveFormat(key, value string) error {
	switch key {
	case "export.auto":
		fmt.Fprintf(os.Stderr,
			"⚠ 'export.auto' is deprecated — writing 'archive.format' instead.\n"+
				"  Migrate with: bd config set archive.format jsonl\n")
		switch strings.ToLower(value) {
		case "true", "1", "yes":
			value = ArchiveFormatJSONL
		default:
			value = ArchiveFormatNone
		}
		return SetYamlConfig("archive.format", value)
	case "archive.format":
		return SetYamlConfig("archive.format", value)
	default:
		return fmt.Errorf("SetArchiveFormat: unexpected key %q", key)
	}
}
