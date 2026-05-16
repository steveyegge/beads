package config

import (
	"fmt"
	"os"
	"strings"
)

// ResolveArchiveFormat returns the effective archive format ("jsonl" or "none").
// It reads archive.format when explicitly configured; otherwise falls back to
// the legacy export.auto key with a deprecation warning when that key is
// explicitly set in config.yaml. Old workspaces that rely on export.auto's
// default (true) continue to work silently until re-initialized.
func ResolveArchiveFormat() string {
	src := GetValueSource("archive.format")
	if src == SourceConfigFile || src == SourceEnvVar {
		return GetString("archive.format")
	}
	// Fall back to export.auto for backwards compatibility.
	exportSrc := GetValueSource("export.auto")
	if exportSrc == SourceConfigFile || exportSrc == SourceEnvVar {
		fmt.Fprintf(os.Stderr, "⚠ export.auto is deprecated; run: bd config set archive.format jsonl (or none)\n")
	}
	if GetBool("export.auto") {
		return "jsonl"
	}
	return "none"
}

// SetArchiveFormat writes the archive format, translating the legacy
// export.auto key to archive.format. Deprecation warnings go to stderr.
// key must be "export.auto" or "archive.format".
func SetArchiveFormat(key, value string) error {
	switch key {
	case "export.auto":
		fmt.Fprintf(os.Stderr, "⚠ export.auto is deprecated; writing archive.format instead\n")
		switch strings.ToLower(value) {
		case "true", "1", "yes":
			return SetYamlConfig("archive.format", "jsonl")
		default:
			return SetYamlConfig("archive.format", "none")
		}
	case "archive.format":
		return SetYamlConfig("archive.format", value)
	default:
		return fmt.Errorf("SetArchiveFormat: unsupported key %q", key)
	}
}
