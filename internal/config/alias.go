package config

import (
	"fmt"
	"os"
	"strings"
)

// ResolveArchiveFormat returns the effective archive format for the current
// workspace, and a non-empty warningMsg when the value was resolved via the
// deprecated export.auto alias.
//
// Resolution order:
//  1. archive.format in config.yaml — the canonical key.
//  2. export.auto in config.yaml — the deprecated alias. When found,
//     warningMsg is set; the caller decides whether and where to print it.
//
// Returns ArchiveFormatNone when neither key is set.
func ResolveArchiveFormat() (format string, warningMsg string) {
	if f, err := GetArchiveFormat(); err == nil && f != "" {
		return f, ""
	}
	// Fall back to export.auto alias.
	raw := GetYamlConfig("export.auto")
	if raw == "" {
		return ArchiveFormatNone, ""
	}
	msg := "⚠ Deprecated config key 'export.auto' — migrate: bd config set archive.format jsonl\n"
	switch strings.ToLower(raw) {
	case "true", "1", "yes":
		return ArchiveFormatJSONL, msg
	default:
		return ArchiveFormatNone, msg
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
